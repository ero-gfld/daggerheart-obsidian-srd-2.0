// Stage 05: validate.
//
// Reusable validation suite for the Daggerheart SRD repository.
//
//	go run .build/05_validate/validate.go            # validate the repo in the CWD
//	go run .build/05_validate/validate.go -root .    # explicit repo root
//
// Exits non-zero when any ERROR-level finding is reported so it can gate CI.
// See .build/05_validate/README.md for what each check does and why it exists.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

const (
	sevError = "ERROR"
	sevWarn  = "WARN"
)

// config holds every tunable knob of the suite. Everything the checks need to
// know about this particular repository lives here so the suite can be pointed
// at a restructured repo without editing check logic.
type config struct {
	root string

	// sourceMD is the hand-cleaned markdown extracted from the SRD PDF: the
	// upstream truth that everything else in the repo is derived from.
	sourceMD string

	// readme is the generated single-file rendering of the SRD.
	readme string

	// contentDirs are the generated per-entity directories. Empty means
	// "auto-discover every non-dotted top-level directory".
	contentDirs []string

	// jsonDir holds the stage-03 intermediate used for duplicate detection.
	jsonDir string

	// deliberateRemovals lists source headings whose section bodies are
	// intentionally lifted out of README.md wholesale. A section matching one
	// of these may be absent from README.md, but it must still have emitted a
	// resolvable link into the generated content for every entity it held.
	deliberateRemovals []*regexp.Regexp

	// roundTripIgnore lists source lines (by regexp) that are known-and-accepted
	// to vanish from the published output. Keep this list empty if you can:
	// every entry is content a reader of the repo will never see.
	roundTripIgnore []*regexp.Regexp

	// categoryContext maps a content directory to heading keywords that make a
	// link into that directory plausible. Used by link-target-sanity.
	categoryContext map[string][]string

	maxPerCheck int
}

func defaultConfig(root string) *config {
	return &config{
		root:     root,
		sourceMD: findSourceMarkdown(root),
		readme:   "README.md",
		jsonDir:  filepath.Join(".build", "03_json"),
		deliberateRemovals: []*regexp.Regexp{
			regexp.MustCompile(`^TIER [1-4] (ADVERSARIES|ENVIRONMENTS)\b`),
			regexp.MustCompile(`^(ARCANA|BLADE|BONE|CODEX|DREAD|GRACE|MIDNIGHT|SAGE|SPLENDOR|VALOR) DOMAIN\b`),
		},
		roundTripIgnore: nil,
		categoryContext: map[string][]string{
			"abilities":       {"DOMAIN", "ABILIT", "CARD", "SPELL", "GRIMOIRE", "LOADOUT", "VAULT", "ARCANA", "BLADE", "BONE", "CODEX", "DREAD", "GRACE", "MIDNIGHT", "SAGE", "SPLENDOR", "VALOR"},
			"adversaries":     {"ADVERSAR", "MONSTER", "STAT BLOCK", "ENCOUNTER", "COMBAT", "BATTLE", "GM ", "ENEMY", "ENEMIES"},
			"environments":    {"ENVIRONMENT", "STAT BLOCK", "GM ", "SCENE", "LOCATION"},
			"weapons":         {"WEAPON", "EQUIPMENT", "LOOT", "INVENTORY", "GEAR", "ARMORY"},
			"armor":           {"ARMOR", "EQUIPMENT", "LOOT", "INVENTORY", "GEAR"},
			"items":           {"ITEM", "EQUIPMENT", "LOOT", "INVENTORY", "GEAR", "GOLD"},
			"consumables":     {"CONSUMABLE", "EQUIPMENT", "LOOT", "INVENTORY", "GEAR"},
			"classes":         {"CLASS", "CHARACTER", "SUBCLASS", "LEVEL"},
			"subclasses":      {"CLASS", "SUBCLASS", "CHARACTER", "FOUNDATION", "SPECIALIZATION", "MASTERY"},
			"ancestries":      {"ANCESTR", "HERITAGE", "CHARACTER"},
			"communities":     {"COMMUNIT", "HERITAGE", "CHARACTER"},
			"transformations": {"TRANSFORMATION", "CHARACTER", "HERITAGE"},
			"domains":         {"DOMAIN", "CLASS", "CARD"},
			"beastforms":      {"BEASTFORM", "DRUID", "WILDFORM", "CLASS"},
		},
		maxPerCheck: 40,
	}
}

// findSourceMarkdown picks the SRD source markdown the rest of the pipeline
// reads. Stages 02 and 04 hardcode a dated filename; we pick the newest dated
// file so this suite keeps working across SRD revisions.
func findSourceMarkdown(root string) string {
	matches, _ := filepath.Glob(filepath.Join(root, ".build", "01_pdf", "DH-SRD-*.md"))
	if len(matches) == 0 {
		return filepath.Join(".build", "01_pdf", "DH-SRD-2026-08-25.md")
	}
	sort.Strings(matches)
	rel, err := filepath.Rel(root, matches[len(matches)-1])
	if err != nil {
		return matches[len(matches)-1]
	}
	return rel
}

// ---------------------------------------------------------------------------
// Findings
// ---------------------------------------------------------------------------

type finding struct {
	check    string
	severity string
	file     string
	line     int // 1-indexed; 0 when the finding is not line-anchored
	message  string
}

type report struct {
	findings []finding
}

func (r *report) add(check, severity, file string, line int, format string, args ...any) {
	r.findings = append(r.findings, finding{
		check:    check,
		severity: severity,
		file:     filepath.ToSlash(file),
		line:     line,
		message:  fmt.Sprintf(format, args...),
	})
}

// ---------------------------------------------------------------------------
// Markdown primitives
// ---------------------------------------------------------------------------

var (
	headingRe = regexp.MustCompile(`^(#{1,6})[ \t]+(.*?)[ \t]*#*[ \t]*$`)
	linkRe    = regexp.MustCompile(`!?\[([^\]\n]*)\]\(([^)\n]*)\)`)
)

func parseHeading(line string) (int, string) {
	m := headingRe.FindStringSubmatch(line)
	if m == nil {
		return 0, ""
	}
	return len(m[1]), strings.TrimSpace(m[2])
}

// normalizeHeading reduces a heading to comparable form: letters and digits
// only, uppercased. "USING TRAITS FOR FLIGHT" -> "USINGTRAITSFORFLIGHT".
func normalizeHeading(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToUpper(r))
		}
	}
	return b.String()
}

// stripLinks turns "[Broadsword](weapons/Broadsword.md)" into "Broadsword" so
// that linkified output text compares equal to the plain source text.
func stripLinks(s string) string {
	return linkRe.ReplaceAllString(s, "$1")
}

// tokenize reduces a line to a slice of lowercase alphanumeric tokens. All
// markdown emphasis, punctuation, smart quotes and link syntax are discarded,
// which is what lets us compare source prose against reflowed, re-emphasized
// generated prose.
func tokenize(s string) []string {
	s = stripLinks(s)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
	}
	return strings.Fields(b.String())
}

// sanitizeFilename mirrors .build/04_md/extract_from_json.go exactly. Keep the
// two in sync: filename-collision detection depends on it.
func sanitizeFilename(name string) string {
	name = strings.ReplaceAll(name, "’", "")
	name = strings.ReplaceAll(name, "'", "")
	name = strings.ReplaceAll(name, ":", "")
	name = strings.ReplaceAll(name, "&", "and")
	return name
}

type mdFile struct {
	rel    string // repo-relative, slash-separated
	lines  []string
	tokens []string // whole-file token stream, contiguous across line breaks
	isGen  bool     // generated content file (not README, not source)
}

func readMD(root, rel string) (*mdFile, error) {
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return nil, err
	}
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	text = strings.TrimPrefix(text, "\uFEFF")
	f := &mdFile{rel: rel, lines: strings.Split(text, "\n")}
	for _, ln := range f.lines {
		f.tokens = append(f.tokens, tokenize(ln)...)
	}
	return f, nil
}

// ---------------------------------------------------------------------------
// Corpus: the searchable body of published text
// ---------------------------------------------------------------------------

const gramK = 5

type gramPos struct{ file, off int32 }

type corpus struct {
	files    []*mdFile
	index    map[string][]gramPos // gramK-grams -> positions
	shortSet map[string]struct{}  // every 1..gramK-1 gram, for very short lines
}

func buildCorpus(files []*mdFile) *corpus {
	c := &corpus{
		files:    files,
		index:    make(map[string][]gramPos, 1<<18),
		shortSet: make(map[string]struct{}, 1<<19),
	}
	for fi, f := range files {
		toks := f.tokens
		for i := range toks {
			for n := 1; n < gramK && i+n <= len(toks); n++ {
				c.shortSet[strings.Join(toks[i:i+n], " ")] = struct{}{}
			}
			if i+gramK <= len(toks) {
				key := strings.Join(toks[i:i+gramK], " ")
				c.index[key] = append(c.index[key], gramPos{int32(fi), int32(i)})
			}
		}
	}
	return c
}

// covers reports whether the token sequence appears contiguously inside any
// single corpus file.
func (c *corpus) covers(toks []string) bool {
	if len(toks) == 0 {
		return true
	}
	if len(toks) < gramK {
		_, ok := c.shortSet[strings.Join(toks, " ")]
		return ok
	}
	key := strings.Join(toks[:gramK], " ")
	for _, p := range c.index[key] {
		f := c.files[p.file]
		end := int(p.off) + len(toks)
		if end > len(f.tokens) {
			continue
		}
		if equalTokens(f.tokens[int(p.off):end], toks) {
			return true
		}
	}
	return false
}

func equalTokens(a, b []string) bool {
	for i := range b {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Repo model
// ---------------------------------------------------------------------------

type repo struct {
	cfg *config

	source *mdFile
	readme *mdFile

	// generated content files, keyed by repo-relative slash path
	content     []*mdFile
	byPath      map[string]*mdFile
	categories  []string
	byCategory  map[string][]*mdFile
	allRelPaths map[string]bool   // every real file path in the repo (case sensitive)
	lowerPaths  map[string]string // lowercased path -> real path
}

func loadRepo(cfg *config) (*repo, error) {
	r := &repo{
		cfg:         cfg,
		byPath:      map[string]*mdFile{},
		byCategory:  map[string][]*mdFile{},
		allRelPaths: map[string]bool{},
		lowerPaths:  map[string]string{},
	}

	var err error
	if r.source, err = readMD(cfg.root, cfg.sourceMD); err != nil {
		return nil, fmt.Errorf("reading source markdown: %w", err)
	}
	if r.readme, err = readMD(cfg.root, cfg.readme); err != nil {
		return nil, fmt.Errorf("reading %s: %w", cfg.readme, err)
	}

	dirs := cfg.contentDirs
	if len(dirs) == 0 {
		entries, err := os.ReadDir(cfg.root)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || e.Name() == "site" {
				continue
			}
			dirs = append(dirs, e.Name())
		}
	}
	sort.Strings(dirs)
	r.categories = dirs

	for _, dir := range dirs {
		entries, err := os.ReadDir(filepath.Join(cfg.root, dir))
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			names = append(names, e.Name())
		}
		sort.Strings(names)
		for _, name := range names {
			rel := dir + "/" + name
			if !strings.HasSuffix(name, ".md") {
				continue
			}
			f, err := readMD(cfg.root, rel)
			if err != nil {
				return nil, err
			}
			f.isGen = true
			r.content = append(r.content, f)
			r.byPath[rel] = f
			r.byCategory[dir] = append(r.byCategory[dir], f)
		}
	}

	// Every real file in the repo, for link resolution.
	_ = filepath.WalkDir(cfg.root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, rerr := filepath.Rel(cfg.root, p)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if d.IsDir() && (strings.HasPrefix(d.Name(), ".") && d.Name() != ".build" && d.Name() != ".github") {
			return filepath.SkipDir
		}
		r.allRelPaths[rel] = true
		lower := strings.ToLower(rel)
		if _, seen := r.lowerPaths[lower]; !seen {
			r.lowerPaths[lower] = rel
		}
		return nil
	})

	return r, nil
}

// publishedFiles is everything a reader of the repo can actually see.
func (r *repo) publishedFiles() []*mdFile {
	out := make([]*mdFile, 0, len(r.content)+1)
	out = append(out, r.readme)
	out = append(out, r.content...)
	return out
}

// ---------------------------------------------------------------------------
// Link extraction / resolution
// ---------------------------------------------------------------------------

type mdLink struct {
	file    *mdFile
	line    int // 1-indexed
	text    string
	rawDest string
	isImage bool
}

func extractLinks(f *mdFile) []mdLink {
	var out []mdLink
	inFence := false
	for i, ln := range f.lines {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		for _, loc := range linkRe.FindAllStringSubmatchIndex(ln, -1) {
			whole := ln[loc[0]:loc[1]]
			out = append(out, mdLink{
				file:    f,
				line:    i + 1,
				text:    ln[loc[2]:loc[3]],
				rawDest: strings.TrimSpace(ln[loc[4]:loc[5]]),
				isImage: strings.HasPrefix(whole, "!"),
			})
		}
	}
	return out
}

var externalRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`)

// classified link destination
type destInfo struct {
	skip     bool // anchors, external URLs, empty
	absolute bool
	escapes  bool // resolves outside the repo root
	target   string
	anchor   string
}

func classifyDest(fromDir, raw string) destInfo {
	d := strings.TrimSpace(raw)
	d = strings.TrimPrefix(d, "<")
	d = strings.TrimSuffix(d, ">")
	// strip an optional link title: (path "Title")
	if idx := strings.Index(d, ` "`); idx >= 0 {
		d = strings.TrimSpace(d[:idx])
	}
	if d == "" {
		return destInfo{skip: true}
	}
	if strings.HasPrefix(d, "#") {
		return destInfo{skip: true}
	}
	if externalRe.MatchString(d) && !strings.HasPrefix(d, "file:") {
		return destInfo{skip: true}
	}
	anchor := ""
	if idx := strings.Index(d, "#"); idx >= 0 {
		anchor = d[idx+1:]
		d = d[:idx]
	}
	if d == "" {
		return destInfo{skip: true}
	}
	if decoded, err := url.PathUnescape(d); err == nil {
		d = decoded
	}
	info := destInfo{anchor: anchor}
	if strings.HasPrefix(d, "/") || regexp.MustCompile(`^[A-Za-z]:[\\/]`).MatchString(d) || strings.HasPrefix(d, `\\`) {
		info.absolute = true
		info.target = strings.TrimPrefix(d, "/")
		return info
	}
	joined := path.Join(fromDir, d)
	if joined == ".." || strings.HasPrefix(joined, "../") {
		info.escapes = true
	}
	info.target = joined
	return info
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func main() {
	root := flag.String("root", ".", "repository root to validate")
	maxPer := flag.Int("max-per-check", 40, "maximum findings printed per check (0 = unlimited)")
	warnFatal := flag.Bool("warn-fatal", false, "treat WARN findings as failures too")
	flag.Parse()

	cfg := defaultConfig(*root)
	cfg.maxPerCheck = *maxPer

	r, err := loadRepo(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "validate: %v\n", err)
		os.Exit(2)
	}

	rep := &report{}

	checkContentRoundTrip(r, rep)
	checkSectionRemovalLinks(r, rep)
	checkLinkIntegrity(r, rep)
	checkLinkTargetSanity(r, rep)
	checkEntityCoverage(r, rep)
	checkStatBlocks(r, rep)
	checkDuplicates(r, rep)
	checkTables(r, rep)
	checkPortability(r, rep)

	errs, warns := printReport(rep, cfg.maxPerCheck)
	if errs > 0 || (*warnFatal && warns > 0) {
		os.Exit(1)
	}
}

func printReport(rep *report, maxPer int) (int, int) {
	sort.SliceStable(rep.findings, func(i, j int) bool {
		a, b := rep.findings[i], rep.findings[j]
		if a.check != b.check {
			return a.check < b.check
		}
		if a.severity != b.severity {
			return a.severity < b.severity // ERROR before WARN
		}
		if a.file != b.file {
			return a.file < b.file
		}
		return a.line < b.line
	})

	perCheck := map[string]int{}
	perSeverity := map[string]int{}
	shown := map[string]int{}
	suppressed := map[string]int{}
	checkOrder := []string{}

	for _, f := range rep.findings {
		if _, seen := perCheck[f.check]; !seen {
			checkOrder = append(checkOrder, f.check)
		}
		perCheck[f.check]++
		perSeverity[f.severity]++
	}

	current := ""
	for _, f := range rep.findings {
		if f.check != current {
			current = f.check
			fmt.Printf("\n== %s (%d) ==\n", f.check, perCheck[f.check])
		}
		if maxPer > 0 && shown[f.check] >= maxPer {
			suppressed[f.check]++
			continue
		}
		shown[f.check]++
		loc := f.file
		if f.line > 0 {
			loc = fmt.Sprintf("%s:%d", f.file, f.line)
		}
		fmt.Printf("%-5s %s: %s\n", f.severity, loc, f.message)
	}
	for _, c := range checkOrder {
		if suppressed[c] > 0 {
			fmt.Printf("      ... and %d more %s findings (raise -max-per-check to see them)\n", suppressed[c], c)
		}
	}

	fmt.Printf("\n--- summary ---\n")
	if len(checkOrder) == 0 {
		fmt.Printf("no findings; repository is clean\n")
	}
	for _, c := range checkOrder {
		fmt.Printf("%-24s %d\n", c, perCheck[c])
	}
	fmt.Printf("\nERROR %d\nWARN  %d\nTOTAL %d\n", perSeverity[sevError], perSeverity[sevWarn], len(rep.findings))
	return perSeverity[sevError], perSeverity[sevWarn]
}

// ---------------------------------------------------------------------------
// Check: content-round-trip
// ---------------------------------------------------------------------------

var (
	sentenceSplitRe = regexp.MustCompile("(?U)[^.!?]*[.!?]+[\"'\u201d\u2019)\\]]*(\\s+|$)")
	// labelRe matches a leading emphasised "Label:" field marker, e.g.
	// "**Gain advantage on:**", "_Found Family:_", "**_Relentless (3) - Passive:_**".
	labelRe = regexp.MustCompile(`^\s*(?:[-*+]\s+)?[*_]{1,3}\s*([^*_|]{1,80}?)\s*:\s*[*_]{1,3}\s*(.*)$`)
)

// splitSegments breaks a source line into independently checkable segments.
//
// Generated files re-emphasise, relabel and re-wrap source text, so a whole
// source line rarely survives as one contiguous token run even when every bit
// of its content is present. Splitting on table cells, pipe-delimited stat
// fields, "Label:" field markers and sentence boundaries lets the round-trip
// check ask the question that actually matters — "is this content anywhere?" —
// without drowning in formatting noise.
func splitSegments(line string) []string {
	trimmed := strings.TrimSpace(line)

	var parts []string
	switch {
	case strings.HasPrefix(trimmed, "|"):
		for _, cell := range strings.Split(strings.Trim(trimmed, "|"), "|") {
			if strings.TrimSpace(cell) != "" {
				parts = append(parts, cell)
			}
		}
	case strings.Contains(trimmed, " | "):
		parts = strings.Split(trimmed, " | ")
	default:
		parts = []string{trimmed}
	}

	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if m := labelRe.FindStringSubmatch(p); m != nil {
			if label := strings.TrimSpace(m[1]); label != "" {
				out = append(out, label)
			}
			p = strings.TrimSpace(m[2])
		}
		if p == "" {
			continue
		}
		out = append(out, splitSentences(p)...)
	}
	if len(out) == 0 {
		return []string{trimmed}
	}
	return out
}

func splitSentences(s string) []string {
	locs := sentenceSplitRe.FindAllStringIndex(s, -1)
	if len(locs) == 0 {
		return []string{s}
	}
	out := make([]string, 0, len(locs)+1)
	for _, loc := range locs {
		if seg := strings.TrimSpace(s[loc[0]:loc[1]]); seg != "" {
			out = append(out, seg)
		}
	}
	if rest := strings.TrimSpace(s[locs[len(locs)-1][1]:]); rest != "" {
		out = append(out, rest)
	}
	return out
}

func excerpt(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func checkContentRoundTrip(r *repo, rep *report) {
	const check = "content-round-trip"
	c := buildCorpus(r.publishedFiles())

	inFence := false
	for i, ln := range r.source.lines {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence || trimmed == "" {
			continue
		}
		if lvl, _ := parseHeading(ln); lvl > 0 {
			continue
		}
		toks := tokenize(ln)
		if len(toks) == 0 {
			continue // separators, rules, image-only lines
		}
		if matchesAny(r.cfg.roundTripIgnore, trimmed) {
			continue
		}
		if c.covers(toks) {
			continue
		}
		reported := false
		for _, seg := range splitSegments(ln) {
			st := tokenize(seg)
			if len(st) == 0 || c.covers(st) {
				continue
			}
			rep.add(check, sevError, r.cfg.sourceMD, i+1,
				"source content missing from README.md and every generated content file: %q", excerpt(seg, 140))
			reported = true
		}
		_ = reported
	}
}

func matchesAny(res []*regexp.Regexp, s string) bool {
	for _, re := range res {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Check: section-removal-link
// ---------------------------------------------------------------------------

type srcHeading struct {
	line  int // 1-indexed
	level int
	text  string
	norm  string
}

func sourceHeadings(f *mdFile) []srcHeading {
	var out []srcHeading
	for i, ln := range f.lines {
		lvl, text := parseHeading(ln)
		if lvl == 0 {
			continue
		}
		out = append(out, srcHeading{line: i + 1, level: lvl, text: text, norm: normalizeHeading(text)})
	}
	return out
}

func checkSectionRemovalLinks(r *repo, rep *report) {
	const check = "section-removal-link"

	readmeHeads := map[string]bool{}
	for _, ln := range r.readme.lines {
		if lvl, text := parseHeading(ln); lvl > 0 {
			readmeHeads[normalizeHeading(text)] = true
		}
	}

	// Headings present in generated content files.
	contentHeads := map[string]bool{}
	// Basenames of generated content files.
	contentNames := map[string]bool{}
	for _, f := range r.content {
		base := strings.TrimSuffix(path.Base(f.rel), ".md")
		contentNames[normalizeHeading(base)] = true
		for _, ln := range f.lines {
			if lvl, text := parseHeading(ln); lvl > 0 {
				contentHeads[normalizeHeading(text)] = true
			}
		}
	}

	possessive := strings.NewReplacer("\u2019s", "", "'s", "", "\u2019S", "", "'S", "")

	// resolved reports whether a source heading survived somewhere a reader can
	// reach. A heading also counts as resolved when it is an entity-qualified
	// form of a heading inside that entity's own file: the generator rewrites
	// "ASSASSIN'S HOPE FEATURE" to "HOPE FEATURE" inside classes/Assassin.md.
	resolved := func(text string) bool {
		norm := normalizeHeading(text)
		if readmeHeads[norm] || contentHeads[norm] || contentNames[norm] {
			return true
		}
		words := strings.Fields(text)
		for k := 1; k < len(words); k++ {
			prefix := normalizeHeading(possessive.Replace(strings.Join(words[:k], " ")))
			suffix := normalizeHeading(strings.Join(words[k:], " "))
			if prefix == "" || suffix == "" {
				continue
			}
			if contentNames[prefix] && (contentHeads[suffix] || contentNames[suffix]) {
				return true
			}
		}
		return false
	}

	heads := sourceHeadings(r.source)
	for i, h := range heads {
		if h.level < 2 {
			continue
		}
		if resolved(h.text) {
			continue
		}
		// A wrapper section may legitimately vanish when every child it held
		// was lifted into linked content (tier stat blocks, domain card lists).
		if matchesAny(r.cfg.deliberateRemovals, strings.ToUpper(h.text)) {
			allChildrenResolved := true
			childCount := 0
			for j := i + 1; j < len(heads) && heads[j].level > h.level; j++ {
				childCount++
				if !resolved(heads[j].text) {
					allChildrenResolved = false
				}
			}
			if childCount > 0 && allChildrenResolved {
				continue
			}
			rep.add(check, sevError, r.cfg.sourceMD, h.line,
				"section %q is on the deliberate-removal list but its entities did not all emit a resolvable link", h.text)
			continue
		}
		rep.add(check, sevError, r.cfg.sourceMD, h.line,
			"section %q disappeared from README.md and no generated content file carries it; the extractor dropped this section without emitting a replacement link", h.text)
	}
}

// reachableContent walks links from README.md through generated content files.
func reachableContent(r *repo) map[string]bool {
	seen := map[string]bool{}
	queue := []*mdFile{r.readme}
	for len(queue) > 0 {
		f := queue[0]
		queue = queue[1:]
		fromDir := path.Dir(f.rel)
		if fromDir == "." {
			fromDir = ""
		}
		for _, l := range extractLinks(f) {
			d := classifyDest(fromDir, l.rawDest)
			if d.skip || d.absolute || d.escapes {
				continue
			}
			next, ok := r.byPath[d.target]
			if !ok || seen[d.target] {
				continue
			}
			seen[d.target] = true
			queue = append(queue, next)
		}
	}
	return seen
}

// ---------------------------------------------------------------------------
// Check: link-integrity / portability of links
// ---------------------------------------------------------------------------

func checkLinkIntegrity(r *repo, rep *report) {
	const (
		checkBroken   = "link-broken"
		checkCase     = "link-case-mismatch"
		checkPortable = "link-not-portable"
	)
	for _, f := range r.publishedFiles() {
		fromDir := path.Dir(f.rel)
		if fromDir == "." {
			fromDir = ""
		}
		for _, l := range extractLinks(f) {
			d := classifyDest(fromDir, l.rawDest)
			if d.skip {
				continue
			}
			if d.absolute {
				rep.add(checkPortable, sevError, f.rel, l.line,
					"link target %q is absolute or vault-rooted; links must be relative so the repo works nested inside a larger Obsidian vault", l.rawDest)
				continue
			}
			if d.escapes {
				rep.add(checkPortable, sevError, f.rel, l.line,
					"link target %q resolves outside the repository root; it will break when the repo is nested inside a vault", l.rawDest)
				continue
			}
			if r.allRelPaths[d.target] {
				continue
			}
			if real, ok := r.lowerPaths[strings.ToLower(d.target)]; ok {
				rep.add(checkCase, sevError, f.rel, l.line,
					"link target %q differs from the real path %q by case only; this resolves on Windows/macOS but 404s on Linux CI and GitHub Pages", d.target, real)
				continue
			}
			kind := "link"
			if l.isImage {
				kind = "image"
			}
			rep.add(checkBroken, sevError, f.rel, l.line,
				"%s [%s](%s) resolves to %q which does not exist", kind, excerpt(l.text, 60), l.rawDest, d.target)
		}
	}
}

// ---------------------------------------------------------------------------
// Check: link-target-sanity
// ---------------------------------------------------------------------------

func checkLinkTargetSanity(r *repo, rep *report) {
	const check = "link-target-sanity"

	for _, f := range r.publishedFiles() {
		fromDir := path.Dir(f.rel)
		if fromDir == "." {
			fromDir = ""
		}
		// ancestor heading chain, by level
		var chain [7]string
		var lastHeading string
		var lastHeadingLine int

		for i, ln := range f.lines {
			if lvl, text := parseHeading(ln); lvl > 0 {
				chain[lvl] = text
				for d := lvl + 1; d < len(chain); d++ {
					chain[d] = ""
				}
				lastHeading = text
				lastHeadingLine = i + 1
				continue
			}
			if strings.TrimSpace(ln) == "" {
				continue
			}
			links := extractLinks(&mdFile{rel: f.rel, lines: []string{ln}})
			if len(links) != 1 {
				continue
			}
			l := links[0]
			// Only inspect substitution-style references: a lone link (optionally
			// prefixed by "See:") standing in for a heading of the same name.
			body := strings.TrimSpace(stripLinks(ln))
			body = strings.TrimPrefix(body, "See:")
			body = strings.TrimSpace(strings.TrimLeft(body, "-*• "))
			if normalizeHeading(body) != normalizeHeading(l.text) {
				continue
			}
			if lastHeading == "" || normalizeHeading(lastHeading) != normalizeHeading(l.text) {
				continue
			}
			if i+1-lastHeadingLine > 4 {
				continue
			}
			d := classifyDest(fromDir, l.rawDest)
			if d.skip || d.absolute || d.escapes {
				continue
			}
			category := path.Dir(d.target)
			keywords, known := r.cfg.categoryContext[category]
			if !known {
				continue
			}
			ctx := ""
			for lvl := 1; lvl < len(chain); lvl++ {
				if chain[lvl] != "" && normalizeHeading(chain[lvl]) != normalizeHeading(l.text) {
					ctx += strings.ToUpper(chain[lvl]) + " | "
				}
			}
			plausible := false
			for _, kw := range keywords {
				if strings.Contains(ctx, kw) {
					plausible = true
					break
				}
			}
			if plausible {
				continue
			}
			rep.add(check, sevWarn, f.rel, i+1,
				"reference %q points at %s but the enclosing section context (%s) has nothing to do with %s; likely a heading-name collision that replaced unrelated content",
				excerpt(strings.TrimSpace(ln), 80), d.target, strings.TrimSuffix(strings.TrimSpace(ctx), "|"), category)
		}
	}
}

// ---------------------------------------------------------------------------
// Check: entity coverage / orphans
// ---------------------------------------------------------------------------

type listSpec struct {
	label     string
	category  string
	fromMD    func(r *repo) *mdFile
	heading   *regexp.Regexp // bullet lists: scan bullets under this heading
	inline    *regexp.Regexp // inline lists: capture group 1 is a comma list
	stripPart *regexp.Regexp // optional suffix to strip from each entry
	exclude   []string       // list entries that are not entities of their own
}

var entityLists = []listSpec{
	{
		label:     "ADVERSARIES BY TIER",
		category:  "adversaries",
		fromMD:    func(r *repo) *mdFile { return r.source },
		heading:   regexp.MustCompile(`(?i)^ADVERSARIES BY TIER$`),
		stripPart: regexp.MustCompile(`\s*\([^)]*\)\s*$`),
	},
	{
		label:     "ENVIRONMENT STAT BLOCKS BY TIER",
		category:  "environments",
		fromMD:    func(r *repo) *mdFile { return r.source },
		heading:   regexp.MustCompile(`(?i)^ENVIRONMENT STAT BLOCKS BY TIER$`),
		stripPart: regexp.MustCompile(`\s*\([^)]*\)\s*$`),
	},
	{
		label:    "class list",
		category: "classes",
		fromMD:   func(r *repo) *mdFile { return r.source },
		inline:   regexp.MustCompile(`classes in this SRD:\s*([^.]+)\.`),
	},
	{
		label:    "ancestry list",
		category: "ancestries",
		fromMD:   func(r *repo) *mdFile { return r.source },
		inline:   regexp.MustCompile(`following ancestries[^:]*:\s*([^.]+)\.`),
		// "Mixed Ancestry" is a rule for combining two ancestry cards, not an
		// ancestry with a card of its own.
		exclude: []string{"Mixed Ancestry"},
	},
	{
		label:    "community list",
		category: "communities",
		fromMD:   func(r *repo) *mdFile { return r.source },
		inline:   regexp.MustCompile(`following communities[^:]*:\s*([^.]+)\.`),
	},
	{
		label:    "domain list",
		category: "domains",
		fromMD:   func(r *repo) *mdFile { return r.source },
		inline:   regexp.MustCompile(`following (?:nine |ten )?domains[^:]*:\s*([^.]+)\.`),
	},
	{
		label:    "transformation list",
		category: "transformations",
		fromMD:   func(r *repo) *mdFile { return r.readme },
		inline:   regexp.MustCompile(`following transformations[^:]*:\s*([^.]+)\.`),
	},
}

func splitNameList(s string) []string {
	s = stripLinks(s)
	s = strings.ReplaceAll(s, ", and ", ", ")
	s = strings.ReplaceAll(s, " and ", ", ")
	var out []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(strings.Trim(strings.TrimSpace(part), "*_"))
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// nameArticles are dropped when relaxing an entity name. Kept deliberately
// short: every entry here is a way two spellings are allowed to differ.
var nameArticles = map[string]bool{"the": true, "a": true, "an": true}

// relaxName folds the two name variations the SRD PDF contains within itself,
// where an index list and the stat-block heading it points at print the same
// entity differently:
//
//   - an optional article inside the name — "Convergence, City of Portals" in
//     ENVIRONMENT STAT BLOCKS BY TIER vs "Convergence, the City of Portals" on
//     the stat block;
//   - the -or/-er agent-noun spelling split — "Outer Realms Corruptor" in
//     ADVERSARIES BY TIER vs "Outer Realms Corrupter" on the stat block.
//
// This is exactly the relaxation .build/04_md/extract_from_json.go applies when
// it resolves an index bullet to a generated file, so the validator agrees with
// the generator about which names are the same entity. It deliberately stops
// there: the generator additionally tolerates a single-character typo, which is
// far too permissive here, because "Fire Elemental" and "Fire Elementals" are
// one edit apart and a genuinely absent stat block must still be an ERROR.
// relaxName cannot reuse normalizeHeading, which throws the word boundaries away
// (it folds "Outer Realms Corruptor" to one run of letters); the relaxation is
// word-by-word, so it needs them.
func relaxName(name string) string {
	name = strings.ReplaceAll(name, "’", "")
	name = strings.ReplaceAll(name, "'", "")
	var b strings.Builder
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		b.WriteByte(' ')
	}
	fields := strings.Fields(b.String())
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if nameArticles[f] {
			continue
		}
		if len(f) > 4 && strings.HasSuffix(f, "or") {
			f = f[:len(f)-2] + "er"
		}
		out = append(out, f)
	}
	return strings.Join(out, " ")
}

// relaxedFileIndex maps each content directory's relaxed file basenames back to
// the real paths. A relaxed key with more than one file behind it is ambiguous
// and is never used to resolve anything.
func relaxedFileIndex(r *repo) map[string]map[string][]string {
	index := map[string]map[string][]string{}
	for rel := range r.allRelPaths {
		if !strings.HasSuffix(rel, ".md") {
			continue
		}
		dir := path.Dir(rel)
		if dir == "." {
			continue
		}
		key := relaxName(strings.TrimSuffix(path.Base(rel), ".md"))
		if key == "" {
			continue
		}
		if index[dir] == nil {
			index[dir] = map[string][]string{}
		}
		index[dir][key] = append(index[dir][key], rel)
	}
	return index
}

func checkEntityCoverage(r *repo, rep *report) {
	const (
		checkMissing = "entity-missing-file"
		checkOrphan  = "entity-orphan"
		checkList    = "entity-list-missing"
		checkCase    = "entity-name-case-mismatch"
		checkVariant = "entity-name-variant"
	)

	relaxed := relaxedFileIndex(r)
	listed := map[string]bool{} // "category/Name.md" seen in some index list

	for _, spec := range entityLists {
		f := spec.fromMD(r)
		var names []string
		var lines []int

		if spec.heading != nil {
			inSection := false
			var sectionLevel int
			for i, ln := range f.lines {
				if lvl, text := parseHeading(ln); lvl > 0 {
					if spec.heading.MatchString(strings.TrimSpace(text)) {
						inSection = true
						sectionLevel = lvl
						continue
					}
					if inSection && lvl <= sectionLevel {
						inSection = false
					}
					continue
				}
				if !inSection {
					continue
				}
				trimmed := strings.TrimSpace(ln)
				if !strings.HasPrefix(trimmed, "- ") && !strings.HasPrefix(trimmed, "* ") {
					continue
				}
				name := strings.TrimSpace(trimmed[2:])
				if spec.stripPart != nil {
					name = spec.stripPart.ReplaceAllString(name, "")
				}
				name = strings.TrimSpace(stripLinks(name))
				if name != "" {
					names = append(names, name)
					lines = append(lines, i+1)
				}
			}
		} else if spec.inline != nil {
			for i, ln := range f.lines {
				m := spec.inline.FindStringSubmatch(stripLinks(ln))
				if m == nil {
					continue
				}
				for _, n := range splitNameList(m[1]) {
					names = append(names, n)
					lines = append(lines, i+1)
				}
			}
		}

		if len(names) == 0 {
			rep.add(checkList, sevWarn, f.rel, 0,
				"index list %q (category %s) could not be located; entity coverage for this category was not verified", spec.label, spec.category)
			continue
		}

		for i, name := range names {
			if containsString(spec.exclude, name) {
				continue
			}
			rel := spec.category + "/" + sanitizeFilename(name) + ".md"
			if r.allRelPaths[rel] {
				listed[rel] = true
				continue
			}
			if real, ok := r.lowerPaths[strings.ToLower(rel)]; ok {
				listed[real] = true
				rep.add(checkCase, sevWarn, f.rel, lines[i],
					"%q is listed in %s but the generated file is %q; the index name and the stat-block name disagree on capitalisation", name, spec.label, real)
				continue
			}
			// The index and the stat block may print the same entity with a
			// different article or -or/-er spelling; both are in the PDF and both
			// are preserved verbatim, so resolve the file but say the two
			// spellings exist. Only an unambiguous single candidate counts.
			if cands := relaxed[spec.category][relaxName(name)]; len(cands) == 1 {
				listed[cands[0]] = true
				rep.add(checkVariant, sevWarn, f.rel, lines[i],
					"%q is listed in %s but the generated file is %q; the index name and the stat-block name are different spellings of the same entity, both printed in the PDF", name, spec.label, cands[0])
				continue
			}
			listed[rel] = true
			rep.add(checkMissing, sevError, f.rel, lines[i],
				"%q is listed in %s but no generated file exists at %s", name, spec.label, rel)
		}
	}

	reachable := reachableContent(r)
	for _, f := range r.content {
		if reachable[f.rel] || listed[f.rel] {
			continue
		}
		rep.add(checkOrphan, sevError, f.rel, 0,
			"generated file is unreachable: no chain of relative links from README.md reaches it and it appears in no SRD index list, so a reader can never find this content")
	}
}

// ---------------------------------------------------------------------------
// Check: stat-block completeness
// ---------------------------------------------------------------------------

type fieldRule struct {
	label string
	re    *regexp.Regexp
}

type blockRule struct {
	category    string
	fields      []fieldRule
	featureRe   *regexp.Regexp
	minFeatures int
	featureWhat string
}

var blockRules = []blockRule{
	{
		category: "adversaries",
		fields: []fieldRule{
			{"Tier/Role", regexp.MustCompile(`(?i)\bTier\s+\d+\s+\w`)},
			{"Motives & Tactics", regexp.MustCompile(`(?i)Motives\s*(&|and)\s*Tactics\s*:`)},
			{"Difficulty", regexp.MustCompile(`(?i)\bDifficulty\s*:`)},
			{"Thresholds", regexp.MustCompile(`(?i)\bThresholds\s*:`)},
			{"HP", regexp.MustCompile(`(?i)\bHP\s*:`)},
			{"Stress", regexp.MustCompile(`(?i)\bStress\s*:`)},
			{"ATK", regexp.MustCompile(`(?i)\bATK\s*:`)},
		},
		featureRe:   regexp.MustCompile(`^\*\*_.+_[.:]?\*\*`),
		minFeatures: 1,
		featureWhat: "feature",
	},
	{
		category: "environments",
		fields: []fieldRule{
			{"Tier/Type", regexp.MustCompile(`(?i)\bTier\s+\d+\s+\w`)},
			{"Impulses", regexp.MustCompile(`(?i)\bImpulses\s*:`)},
			{"Difficulty", regexp.MustCompile(`(?i)\bDifficulty\s*:`)},
			{"Potential Adversaries", regexp.MustCompile(`(?i)Potential\s+Adversaries\s*:`)},
		},
		featureRe:   regexp.MustCompile(`^\*\*_.+_[.:]?\*\*`),
		minFeatures: 1,
		featureWhat: "feature",
	},
	{
		category: "abilities",
		fields: []fieldRule{
			{"Level", regexp.MustCompile(`(?i)\bLevel\s+\d+`)},
			{"Recall Cost", regexp.MustCompile(`(?i)Recall\s+Cost`)},
		},
	},
	{
		category: "classes",
		fields: []fieldRule{
			{"domains", regexp.MustCompile(`(?i)\bDOMAINS\b`)},
			{"starting evasion", regexp.MustCompile(`(?i)STARTING\s+EVASION`)},
			{"starting hit points", regexp.MustCompile(`(?i)STARTING\s+HIT\s+POINTS`)},
			{"class items", regexp.MustCompile(`(?i)CLASS\s+ITEMS`)},
		},
		featureRe:   regexp.MustCompile(`\]\(\.\./subclasses/`),
		minFeatures: 1,
		featureWhat: "subclass link",
	},
}

var emphasisStripper = strings.NewReplacer("*", "", "_", "")

func checkStatBlocks(r *repo, rep *report) {
	const check = "statblock-incomplete"
	for _, rule := range blockRules {
		for _, f := range r.byCategory[rule.category] {
			// Emphasis markers are word characters to Go's regexp engine, so
			// "**_Tier 1 Solo._**" defeats a \b anchor. Strip them first.
			body := emphasisStripper.Replace(strings.Join(f.lines, "\n"))
			var missing []string
			for _, field := range rule.fields {
				if !field.re.MatchString(body) {
					missing = append(missing, field.label)
				}
			}
			if len(missing) > 0 {
				rep.add(check, sevError, f.rel, 1,
					"%s entry is missing required field(s): %s", rule.category, strings.Join(missing, ", "))
			}
			if rule.featureRe == nil {
				continue
			}
			count := 0
			for _, ln := range f.lines {
				if rule.featureRe.MatchString(strings.TrimSpace(ln)) {
					count++
				}
			}
			if count < rule.minFeatures {
				rep.add(check, sevError, f.rel, 1,
					"%s entry has %d %s(s); at least %d required", rule.category, count, rule.featureWhat, rule.minFeatures)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Check: duplicate / collision detection
// ---------------------------------------------------------------------------

func checkDuplicates(r *repo, rep *report) {
	const (
		checkDupName   = "duplicate-entity-name"
		checkCollision = "filename-collision"
	)

	// 1. Filenames within a category that differ only by case.
	for _, cat := range r.categories {
		byLower := map[string][]string{}
		for _, f := range r.byCategory[cat] {
			base := path.Base(f.rel)
			byLower[strings.ToLower(base)] = append(byLower[strings.ToLower(base)], base)
		}
		for lower, names := range byLower {
			if len(names) > 1 {
				sort.Strings(names)
				rep.add(checkCollision, sevError, cat, 0,
					"filenames differ only by case (%s): %s — one will overwrite the other on Windows/macOS", lower, strings.Join(names, ", "))
			}
		}
	}

	// 2. Entity names in the stage-03 JSON that collide after sanitization,
	//    or that are outright duplicated. A collision means one generated file
	//    silently overwrote another.
	entries, err := os.ReadDir(filepath.Join(r.cfg.root, r.cfg.jsonDir))
	if err != nil {
		rep.add(checkDupName, sevWarn, r.cfg.jsonDir, 0,
			"could not read stage-03 JSON (%v); duplicate-name detection was skipped", err)
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		category := strings.TrimSuffix(e.Name(), ".json")
		rel := path.Join(filepath.ToSlash(r.cfg.jsonDir), e.Name())
		raw, err := os.ReadFile(filepath.Join(r.cfg.root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		raw = []byte(strings.TrimPrefix(string(raw), "\uFEFF"))
		var items []map[string]json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			rep.add(checkDupName, sevWarn, rel, 0,
				"could not parse stage-03 JSON (%v); duplicate-name detection skipped for %s", err, category)
			continue
		}
		exact := map[string]int{}
		sanitized := map[string][]string{}
		for _, item := range items {
			nameRaw, ok := item["name"]
			if !ok {
				continue
			}
			var name string
			if json.Unmarshal(nameRaw, &name) != nil || strings.TrimSpace(name) == "" {
				continue
			}
			exact[name]++
			key := strings.ToLower(sanitizeFilename(name))
			if !containsString(sanitized[key], name) {
				sanitized[key] = append(sanitized[key], name)
			}
		}
		for name, n := range exact {
			if n > 1 {
				rep.add(checkDupName, sevError, rel, jsonNameLine(raw, name),
					"entity name %q appears %d times in category %s; only one generated file can survive", name, n, category)
			}
		}
		for key, names := range sanitized {
			if len(names) > 1 {
				sort.Strings(names)
				rep.add(checkCollision, sevError, rel, jsonNameLine(raw, names[0]),
					"names %s all sanitize to the same filename %q in category %s; the generated files overwrite each other",
					strings.Join(quoteAll(names), ", "), key+".md", category)
			}
		}
	}
}

// jsonNameLine locates the 1-indexed line of a "name" value inside raw JSON so
// findings stay actionable.
func jsonNameLine(raw []byte, name string) int {
	encoded, err := json.Marshal(name)
	if err != nil {
		return 0
	}
	needle := `"name": ` + string(encoded)
	idx := strings.Index(string(raw), needle)
	if idx < 0 {
		needle = `"name":` + string(encoded)
		idx = strings.Index(string(raw), needle)
	}
	if idx < 0 {
		return 0
	}
	return 1 + strings.Count(string(raw[:idx]), "\n")
}

func containsString(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func quoteAll(s []string) []string {
	out := make([]string, len(s))
	for i, v := range s {
		out[i] = fmt.Sprintf("%q", v)
	}
	return out
}

// ---------------------------------------------------------------------------
// Check: table integrity
// ---------------------------------------------------------------------------

var separatorCellRe = regexp.MustCompile(`^:?-{1,}:?$`)

func countCells(line string) int {
	t := strings.TrimSpace(line)
	t = strings.TrimPrefix(t, "|")
	t = strings.TrimSuffix(t, "|")
	// split on unescaped pipes
	n := 1
	for i := 0; i < len(t); i++ {
		if t[i] == '|' && (i == 0 || t[i-1] != '\\') {
			n++
		}
	}
	return n
}

func isSeparatorRow(line string) bool {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, "|") {
		return false
	}
	body := strings.Trim(t, "|")
	for _, cell := range strings.Split(body, "|") {
		if !separatorCellRe.MatchString(strings.TrimSpace(cell)) {
			return false
		}
	}
	return true
}

func checkTables(r *repo, rep *report) {
	const check = "table-column-mismatch"
	files := append([]*mdFile{r.source}, r.publishedFiles()...)
	for _, f := range files {
		i := 0
		for i < len(f.lines) {
			if !strings.HasPrefix(strings.TrimSpace(f.lines[i]), "|") {
				i++
				continue
			}
			start := i
			for i < len(f.lines) && strings.HasPrefix(strings.TrimSpace(f.lines[i]), "|") {
				i++
			}
			rows := f.lines[start:i]
			if len(rows) < 2 {
				continue
			}
			want := countCells(rows[0])
			if !isSeparatorRow(rows[1]) {
				rep.add(check, sevWarn, f.rel, start+2,
					"table starting at line %d has no delimiter row; it will not render as a table", start+1)
			}
			for j, row := range rows {
				if got := countCells(row); got != want {
					kind := "body row"
					if j == 1 {
						kind = "delimiter row"
					}
					rep.add(check, sevError, f.rel, start+j+1,
						"table %s has %d columns but the header row (line %d) has %d: %s", kind, got, start+1, want, excerpt(row, 100))
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Check: filename portability
// ---------------------------------------------------------------------------

var (
	reservedCharRe = regexp.MustCompile(`[<>:"|?*\x00-\x1f]`)
	reservedNameRe = regexp.MustCompile(`(?i)^(CON|PRN|AUX|NUL|COM[1-9]|LPT[1-9])(\.|$)`)
	awkwardCharRe  = regexp.MustCompile(`[#\[\]^]`)
	maxBasenameLen = 255
	maxPathLenWarn = 200
)

func checkPortability(r *repo, rep *report) {
	const check = "filename-portability"
	for _, f := range r.content {
		base := path.Base(f.rel)
		if m := reservedCharRe.FindString(base); m != "" {
			rep.add(check, sevError, f.rel, 0,
				"filename contains %q, which is illegal on Windows and breaks Obsidian vault sync", m)
		}
		if strings.HasSuffix(base, ".") || strings.HasSuffix(strings.TrimSuffix(base, ".md"), " ") {
			rep.add(check, sevError, f.rel, 0, "filename has a trailing dot or space; Windows silently strips these")
		}
		if strings.HasPrefix(base, " ") {
			rep.add(check, sevError, f.rel, 0, "filename has a leading space")
		}
		if reservedNameRe.MatchString(base) {
			rep.add(check, sevError, f.rel, 0, "filename uses a reserved Windows device name")
		}
		if len(base) > maxBasenameLen {
			rep.add(check, sevError, f.rel, 0, "filename is %d bytes long; keep it under %d", len(base), maxBasenameLen)
		} else if len(f.rel) > maxPathLenWarn {
			rep.add(check, sevWarn, f.rel, 0, "path is %d bytes long; deep clones on Windows may exceed MAX_PATH", len(f.rel))
		}
		if m := awkwardCharRe.FindString(base); m != "" {
			rep.add(check, sevWarn, f.rel, 0,
				"filename contains %q, which needs escaping in markdown links and confuses Obsidian's [[wikilink]] parser", m)
		}
	}
}
