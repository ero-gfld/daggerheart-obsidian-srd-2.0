package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"text/template"
	"unicode"
)

func main() {
	jsonDir := ".build/03_json"
	templateDir := ".build/04_md/templates"
	outputDir := ".build/04_md/docs"
	srdBasePath := ".build/01_pdf/DH-SRD-2026-08-25.md"
	srdPath := "README.md"

	entries, err := os.ReadDir(jsonDir)
	if err != nil {
		fmt.Printf("Error reading %s: %v\n", jsonDir, err)
		os.Exit(1)
	}

	sectionLinks, categoryLinks, err := loadSRDLinkMaps(jsonDir)
	if err != nil {
		fmt.Printf("Error loading link maps from %s: %v\n", jsonDir, err)
		os.Exit(1)
	}

	// Structural placement (which grouping section an entity sits in) and a
	// backstop for the literal heading text, read straight from the source SRD
	// markdown. The CSV/JSON stages carry the printed heading of every feature
	// group now, and those values win; this only fills a gap, because without a
	// literal the templates have to guess the plural from a feature count and
	// would silently "correct" the PDF's own singular/plural quirks
	// (MASTERY FEATURE, ANCESTRY FEATURE).
	srcInfo := loadSourceEntityInfo(srdBasePath, categoryLinks)

	funcs := template.FuncMap{
		"upper":            strings.ToUpper,
		"urlEncode":        url.PathEscape,
		"featureQuestions": featureQuestions,
		"featureLabel":     featureLabel,
		"fileName":         sanitizeFilename,
		"abilityLink":      abilityLink,
		"optionAt":         optionAt,
		"add1":             add1,
		"literalHeading":   literalHeading,
		"headingOr":        headingOr,
		"featureGroups":    featureGroups,
	}

	var beastforms []map[string]any
	var beastformTiers []map[string]any
	if bf, err := loadJSON(filepath.Join(jsonDir, "beastforms.json")); err == nil {
		beastforms = bf
		beastformTiers = groupBeastformsByTier(beastforms)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		base := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		templatePath := filepath.Join(templateDir, base+".md")
		if _, err := os.Stat(templatePath); err != nil {
			continue
		}
		jsonPath := filepath.Join(jsonDir, entry.Name())
		items, err := loadJSON(jsonPath)
		if err != nil {
			fmt.Printf("Error reading %s: %v\n", jsonPath, err)
			continue
		}
		templateName := filepath.Base(templatePath)
		tmpl, err := template.New(templateName).Funcs(funcs).ParseFiles(templatePath)
		if err != nil {
			fmt.Printf("Error parsing template %s: %v\n", templatePath, err)
			continue
		}

		categoryDir := filepath.Join(outputDir, base)
		if err := os.MkdirAll(categoryDir, 0755); err != nil {
			fmt.Printf("Error creating %s: %v\n", categoryDir, err)
			continue
		}

		for _, item := range items {
			name, _ := item["name"].(string)
			if strings.TrimSpace(name) == "" {
				continue
			}
			if base == "classes" && len(beastformTiers) > 0 {
				item["beastform_tiers"] = beastformTiers
			}
			normalizeItem(item)
			applySourceHeadings(base, item, srcInfo)
			outPath := filepath.Join(categoryDir, sanitizeFilename(name)+".md")
			if err := renderTemplate(tmpl, templateName, outPath, item); err != nil {
				fmt.Printf("Error rendering %s (%s): %v\n", outPath, name, err)
				continue
			}
		}
	}

	if err := generateSRD(srdBasePath, srdPath, sectionLinks, categoryLinks); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating %s: %v\n", srdPath, err)
		os.Exit(1)
	}
}

func loadJSON(path string) ([]map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(raw) >= 3 && raw[0] == 0xEF && raw[1] == 0xBB && raw[2] == 0xBF {
		raw = raw[3:]
	}
	var out []map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func renderTemplate(tmpl *template.Template, name, outPath string, data map[string]any) error {
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := tmpl.ExecuteTemplate(f, name, data); err != nil {
		return err
	}
	if err := collapseExtraBlankLines(outPath); err != nil {
		return err
	}
	return ensureCanonicalFilename(outPath)
}

func normalizeItem(item map[string]any) {
	ensureSlice(item, "feature")
	ensureSlice(item, "background")
	ensureSlice(item, "connection")
	ensureSlice(item, "foundation")
	ensureSlice(item, "specialization")
	ensureSlice(item, "mastery")
	ensureSlice(item, "question")
	ensureString(item, "suggested_secondary")
	// Literal heading text as printed in the PDF. The CSV stage emits these
	// today (see the "* Heading" columns in .build/02_csv/extract_from_md.go);
	// they stay optional so a hand-edited or older JSON set still renders, in
	// which case applySourceHeadings recovers them from the source markdown and,
	// failing that, each template falls back to its own wording.
	for _, key := range literalHeadingFields {
		ensureString(item, key)
	}
	ensureString(item, "parent_group")
	ensureString(item, "parent_group_anchor")
	stripEmbeddedFeatureQuestions(item)
}

// entityInfo is what the source markdown knows about one entity's own section
// that the CSV/JSON stages currently throw away.
type entityInfo struct {
	// children holds the literal text of the headings inside the entity's
	// block, exactly as the PDF prints them.
	children []string
	// parentGroup is the enclosing heading when the entity is nested inside a
	// grouping section rather than directly under its category root — the four
	// elemental kin sit under ELEMENTAL KIN, for example.
	parentGroup string
}

type headingSlot struct {
	field string
	match *regexp.Regexp
	// optional marks a group that legitimately does not exist for every entity
	// of the category — six subclasses print no SPELLCAST TRAIT block at all —
	// so an unfilled slot is not worth warning about.
	optional bool
}

// literalHeadingSlots maps a feature group onto the item field that carries its
// printed heading. The field names are exactly the JSON keys stage 03 derives
// from the CSV "* Heading" columns, so a value the data already carries is used
// verbatim and this table only ever fills a gap.
//
// A class's hope feature is class-prefixed in the PDF ("ASSASSIN'S HOPE
// FEATURE") and that is what gets printed: both the source markdown and the PDF
// use the prefixed form, and 05_validate's section-removal-link check resolves
// either spelling.
var literalHeadingSlots = map[string][]headingSlot{
	"adversaries": {
		{field: "features_heading", match: regexp.MustCompile(`(?i)^FEATURES?$`)},
	},
	"ancestries": {
		{field: "features_heading", match: regexp.MustCompile(`(?i)^ANCESTRY FEATURES?$`)},
	},
	"classes": {
		{field: "class_features_heading", match: regexp.MustCompile(`(?i)^CLASS FEATURES?$`)},
		{field: "hope_feature_heading", match: regexp.MustCompile(`(?i)^(?:.+[’']S\s+)?HOPE FEATURES?$`)},
	},
	"communities": {
		{field: "features_heading", match: regexp.MustCompile(`(?i)^COMMUNITY FEATURES?$`)},
	},
	"environments": {
		{field: "features_heading", match: regexp.MustCompile(`(?i)^FEATURES?$`)},
	},
	"subclasses": {
		{field: "spellcast_heading", match: regexp.MustCompile(`(?i)^SPELLCAST TRAITS?$`), optional: true},
		{field: "foundation_heading", match: regexp.MustCompile(`(?i)^FOUNDATION FEATURES?$`)},
		{field: "specialization_heading", match: regexp.MustCompile(`(?i)^SPECIALIZATION FEATURES?$`)},
		{field: "mastery_heading", match: regexp.MustCompile(`(?i)^MASTERY FEATURES?$`)},
	},
	"transformations": {
		{field: "features_heading", match: regexp.MustCompile(`(?i)^TRANSFORMATION FEATURES?$`)},
		{field: "questions_heading", match: regexp.MustCompile(`(?i)^TRANSFORMATION QUESTIONS?$`)},
	},
}

// literalHeadingFields is every field literalHeadingSlots can fill, so
// normalizeItem can declare them all without the templates tripping over a
// missing key.
var literalHeadingFields = func() []string {
	seen := map[string]bool{}
	var out []string
	for _, slots := range literalHeadingSlots {
		for _, slot := range slots {
			if !seen[slot.field] {
				seen[slot.field] = true
				out = append(out, slot.field)
			}
		}
	}
	sort.Strings(out)
	return out
}()

// loadSourceEntityInfo walks the source SRD once and records, per entity, the
// literal headings inside its block and the group section it belongs to.
//
// An entity is only recognised at its canonical definition site (see
// canonicalCategoryFor), so a campaign-frame section that reuses an entity name
// never contributes headings to that entity. Returns nil when the source cannot
// be read; every consumer treats that as "no extra information".
func loadSourceEntityInfo(srcPath string, categoryLinks map[string]map[string]linkTarget) map[string]map[string]entityInfo {
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: cannot read %s for literal heading text: %v\n", srcPath, err)
		return nil
	}
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")

	type openBlock struct {
		category string
		key      string
		level    int
	}
	info := map[string]map[string]entityInfo{}
	var stack headingStack
	var open []openBlock

	for _, line := range lines {
		level, title := parseHeading(line)
		if level == 0 {
			continue
		}
		for len(open) > 0 && level <= open[len(open)-1].level {
			open = open[:len(open)-1]
		}
		// Attribute the heading to the innermost block still open.
		if len(open) > 0 {
			ob := open[len(open)-1]
			e := info[ob.category][ob.key]
			e.children = append(e.children, title)
			info[ob.category][ob.key] = e
		}

		parent := stack.raw(level - 1)
		stack.push(level, title)

		category := canonicalCategoryFor(level, &stack)
		if category == "" {
			// Subclasses have no README removal of their own (they live inside
			// an already-removed class chapter), but their printed feature
			// headings still have to reach subclasses/*.md.
			if level == 4 && stack.at(1) == "CORE MATERIALS" {
				if _, isClass := categoryLinks["classes"][normalizeHeading(stack.raw(2))]; isClass {
					if _, isSubclass := categoryLinks["subclasses"][normalizeHeading(title)]; isSubclass {
						category = "subclasses"
					}
				}
			}
		}
		if category == "" {
			continue
		}
		key := normalizeHeading(title)
		if _, ok := categoryLinks[category][key]; !ok {
			continue
		}
		if info[category] == nil {
			info[category] = map[string]entityInfo{}
		}
		e := info[category][key]
		e.parentGroup = parent
		info[category][key] = e
		open = append(open, openBlock{category: category, key: key, level: level})
	}
	return info
}

// applySourceHeadings copies the PDF's literal heading text (and group
// membership) onto an item, without ever overwriting a value the JSON already
// carries — so the day the CSV stage grows these columns, they win.
func applySourceHeadings(category string, item map[string]any, info map[string]map[string]entityInfo) {
	name, _ := item["name"].(string)
	entity := info[category][normalizeHeading(name)]

	for _, slot := range literalHeadingSlots[category] {
		if existing, _ := item[slot.field].(string); strings.TrimSpace(existing) != "" {
			continue
		}
		for _, child := range entity.children {
			if slot.match.MatchString(strings.TrimSpace(child)) {
				item[slot.field] = strings.TrimSpace(child)
				break
			}
		}
		if existing, _ := item[slot.field].(string); strings.TrimSpace(existing) == "" && !slot.optional {
			// Neither the data nor the source markdown printed this heading, so
			// the template is about to fall back to its own hardcoded wording —
			// for the feature groups that pluralise, a guess from the feature
			// count, which is wrong on roughly half the 2.0 subclass pages. Say
			// so out loud rather than silently "correcting" the PDF.
			fmt.Fprintf(os.Stderr, "WARNING: %s/%s: no literal %s in the JSON or the source markdown; the template's fallback wording will be used\n",
				category, name, slot.field)
		}
	}

	// A grouping heading is only interesting when it is not the category's own
	// root section: "Aetheris" sits directly under ANCESTRIES, "Earthkin" sits
	// under ELEMENTAL KIN.
	if category == "ancestries" {
		group := strings.TrimSpace(entity.parentGroup)
		if group != "" && normalizeHeading(group) != "ancestries" {
			if existing, _ := item["parent_group"].(string); strings.TrimSpace(existing) == "" {
				item["parent_group"] = titleCaseHeading(group)
				item["parent_group_anchor"] = headingAnchor(group)
			}
		}
	}
}

// literalHeading returns the heading exactly as the PDF prints it when the
// pipeline managed to carry it through, and otherwise falls back to deriving
// the plural from the number of entries. The fallback is a guess: the SRD does
// print "MASTERY FEATURE" above two features in four places, so wherever the
// literal is available it must win.
func literalHeading(literal any, base string, count int) string {
	if s, ok := literal.(string); ok {
		if trimmed := strings.TrimSpace(s); trimmed != "" {
			return trimmed
		}
	}
	if count > 1 {
		return base + "S"
	}
	return base
}

// featureGroups splits a feature list into render groups: one top-level feature
// plus the entries the extractor nested under it.
//
// Four adversaries (Roc, Vampire Lord, Supreme Demiurge Adonix, Mountain Troll)
// print an "- Evolution" feature followed by content that only applies once the
// creature has evolved: replacement standard attacks and extra features. Stage
// 02 records that nesting as a "parent" naming the evolution feature. Rendering
// the list flat would put "Bloodbath - Reaction" next to "Terrifying - Passive"
// as if a GM could use both from the first round, so the children are handed to
// the template separately and drawn as an indented list under their parent.
//
// A child whose named parent has not been seen is emitted as its own top-level
// group rather than dropped: a data glitch must not silently lose a feature.
func featureGroups(features any) []map[string]any {
	list, ok := features.([]any)
	if !ok {
		return nil
	}
	var groups []map[string]any
	byName := map[string]int{}
	for _, entry := range list {
		f, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		parent, _ := f["parent"].(string)
		if idx, found := byName[strings.TrimSpace(parent)]; found && strings.TrimSpace(parent) != "" {
			g := groups[idx]
			g["children"] = append(g["children"].([]any), f)
			continue
		}
		name, _ := f["name"].(string)
		if name = strings.TrimSpace(name); name != "" {
			byName[name] = len(groups)
		}
		groups = append(groups, map[string]any{"feature": f, "children": []any{}})
	}
	return groups
}

// headingOr is literalHeading for the groups whose printed heading does not vary
// with the number of entries under it (FEATURES on every adversary and
// environment, SPELLCAST TRAIT, "<CLASS>'S HOPE FEATURE"). The fallback is the
// wording the template hardcoded before the data carried the real thing.
func headingOr(literal any, fallback string) string {
	if s, ok := literal.(string); ok {
		if trimmed := strings.TrimSpace(s); trimmed != "" {
			return trimmed
		}
	}
	return fallback
}

func sanitizeFilename(name string) string {
	name = strings.ReplaceAll(name, "’", "")
	name = strings.ReplaceAll(name, "'", "")
	name = strings.ReplaceAll(name, ":", "")
	name = strings.ReplaceAll(name, "&", "and")
	return name
}

func ensureSlice(item map[string]any, key string) {
	if _, ok := item[key]; !ok {
		item[key] = []any{}
	}
}

func ensureString(item map[string]any, key string) {
	if _, ok := item[key]; !ok {
		item[key] = ""
	}
}

func stripEmbeddedFeatureQuestions(item map[string]any) {
	features, ok := item["feature"].([]any)
	if !ok {
		return
	}
	for _, entry := range features {
		f, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		text, _ := f["text"].(string)
		question, _ := f["question"].(string)
		if text == "" || question == "" {
			continue
		}
		if strings.Contains(text, question) {
			f["question"] = ""
		}
	}
}

func collapseExtraBlankLines(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	out := normalizeMarkdown(strings.ReplaceAll(string(raw), "\r\n", "\n"))
	return os.WriteFile(path, []byte(out), 0644)
}

type linkTarget struct {
	name     string
	path     string
	category string
}

func generateSRD(basePath, outPath string, sectionLinks, categoryLinks map[string]map[string]linkTarget) error {
	baseRaw, err := os.ReadFile(basePath)
	if err != nil {
		return err
	}
	content := strings.ReplaceAll(string(baseRaw), "\r\n", "\n")

	content = replaceTableNamesWithLinks(content, sectionLinks)
	content = replaceListItemsWithLinks(content, categoryLinks)
	content, removed, warnings := replaceHeadingBlocksWithLinks(content, categoryLinks)
	for _, w := range warnings {
		fmt.Fprintf(os.Stderr, "WARNING: %s\n", w)
	}
	content = replaceClassDomainLinks(content, categoryLinks["classes"], categoryLinks["domains"])
	content = replaceClassMentionList(content, categoryLinks["classes"])
	content = replaceAncestryMentionList(content, categoryLinks["ancestries"])
	content = insertCategoryParagraph(content, categoryLinks["communities"],
		"COMMUNITIES", "The core ruleset includes the following communities:")
	content = insertCategoryParagraph(content, categoryLinks["ancestries"],
		"ANCESTRIES", "This SRD describes the following ancestries:")
	content = insertCategoryParagraph(content, categoryLinks["transformations"],
		"TRANSFORMATIONS", "The core materials include the following transformations:")
	content = insertDomainCardReferenceList(content, categoryLinks["domains"])
	content = removeDomainCardReferenceSections(content, categoryLinks["domains"])
	content = removeSectionsByHeadingPrefix(content, 2, []string{
		"TIER 1 ENVIRONMENTS",
		"TIER 2 ENVIRONMENTS",
		"TIER 3 ENVIRONMENTS",
		"TIER 4 ENVIRONMENTS",
		"TIER 1 ADVERSARIES",
		"TIER 2 ADVERSARIES",
		"TIER 3 ADVERSARIES",
		"TIER 4 ADVERSARIES",
	})
	content = normalizeMarkdown(content)
	content = insertContents(content)
	content = demoteHeadings(content)
	if err := os.WriteFile(outPath, []byte(content), 0644); err != nil {
		return err
	}
	// The output is written before the invariants are judged so a failing build
	// still leaves a diffable artifact; the non-zero exit is what gates CI.
	return checkBuildInvariants(removed, categoryLinks, content)
}

func loadSRDLinkMaps(jsonDir string) (map[string]map[string]linkTarget, map[string]map[string]linkTarget, error) {
	sectionLinks := map[string]map[string]linkTarget{}
	categoryLinks := map[string]map[string]linkTarget{}
	sectionMap := map[string]string{
		"WEAPONS":     "weapons",
		"ARMOR":       "armor",
		"CONSUMABLES": "consumables",
		"ITEMS":       "items",
		"LOOT":        "items",
	}

	entries, err := os.ReadDir(jsonDir)
	if err != nil {
		return nil, nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		base := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		items, err := loadJSON(filepath.Join(jsonDir, entry.Name()))
		if err != nil {
			return nil, nil, err
		}
		for _, item := range items {
			name, _ := item["name"].(string)
			if strings.TrimSpace(name) == "" {
				continue
			}
			link := buildSRDLink(base, name)
			key := normalizeHeading(name)
			for section, sectionBase := range sectionMap {
				if base == sectionBase {
					if sectionLinks[section] == nil {
						sectionLinks[section] = map[string]linkTarget{}
					}
					sectionLinks[section][key] = linkTarget{name: name, path: link, category: base}
				}
			}
			if categoryLinks[base] == nil {
				categoryLinks[base] = map[string]linkTarget{}
			}
			categoryLinks[base][key] = linkTarget{name: name, path: link, category: base}
		}
	}
	return sectionLinks, categoryLinks, nil
}

func buildSRDLink(category, name string) string {
	file := url.PathEscape(sanitizeFilename(name))
	return fmt.Sprintf("%s/%s.md", category, file)
}

// headingStack tracks the enclosing heading title at every level while walking
// a markdown document. Pushing a heading clears everything below it, so at any
// point stack[n] is the innermost level-n ancestor of the current line.
type headingStack [8]string

func (s *headingStack) push(level int, title string) {
	if level < 1 || level >= len(s) {
		return
	}
	s[level] = strings.TrimSpace(title)
	for d := level + 1; d < len(s); d++ {
		s[d] = ""
	}
}

// raw returns the ancestor heading at the given level exactly as printed.
func (s *headingStack) raw(level int) string {
	if level < 1 || level >= len(s) {
		return ""
	}
	return s[level]
}

// at returns the ancestor heading at the given level, upper-cased for matching.
func (s *headingStack) at(level int) string {
	return strings.ToUpper(s.raw(level))
}

var (
	tierAdversariesRe  = regexp.MustCompile(`^TIER [1-4] ADVERSARIES\b`)
	tierEnvironmentsRe = regexp.MustCompile(`^TIER [1-4] ENVIRONMENTS\b`)
)

// canonicalCategories are the categories whose entities have a single, known
// definition site in the source SRD and are therefore lifted out of README.md
// into their own generated file.
var canonicalCategories = []string{
	"abilities", "adversaries", "ancestries", "classes",
	"communities", "domains", "environments", "transformations",
}

// canonicalCategoryFor reports which category (if any) a heading at this depth
// and under these ancestors is the *definition site* for.
//
// This is the guard that stops a heading being swallowed just because it shares
// a name with an entity. The 2.0 SRD deliberately reuses entity names as
// campaign-frame guidance headings — the Witherwild frame has its own
// ORDERBORNE / SLYBORNE / SEABORNE / WANDERBORNE / FUNGRIL sections, monster
// hunting has its own REANIMATED / VAMPIRE / WEREWOLF sections, and the
// floating-magic-school frame has a FLIGHT section unrelated to the Arcana
// domain card of the same name. All of those sit at a different depth, under a
// different ancestor, than the entity's real definition, so requiring an exact
// context match leaves them untouched.
func canonicalCategoryFor(level int, s *headingStack) string {
	switch {
	case level == 3 && tierAdversariesRe.MatchString(s.at(2)):
		return "adversaries"
	case level == 3 && tierEnvironmentsRe.MatchString(s.at(2)):
		return "environments"
	case level == 5 && s.at(2) == "DOMAIN CARD REFERENCE" && strings.HasSuffix(s.at(3), " DOMAIN"):
		return "abilities"
	case s.at(1) != "CORE MATERIALS":
		return ""
	case level == 2:
		return "classes"
	case level == 4 && s.at(2) == "DOMAINS" && s.at(3) == "":
		return "domains"
	case level == 3 && s.at(2) == "ANCESTRIES":
		return "ancestries"
	case level == 4 && s.at(2) == "ANCESTRIES" && s.at(3) == "ELEMENTAL KIN":
		return "ancestries"
	case level == 3 && s.at(2) == "COMMUNITIES":
		return "communities"
	case level == 3 && s.at(2) == "TRANSFORMATIONS":
		return "transformations"
	}
	return ""
}

// entityChildHeadings is the sub-structure an entity block of each category may
// legitimately contain. A nil entry means "no restriction": class blocks are
// whole chapters and own their subclasses, beastform tables and appendices.
// Anything else that turns up inside an entity block is not part of that entity,
// so the block stops there rather than swallowing it.
var entityChildHeadings = map[string]map[string]bool{
	"abilities":    {},
	"adversaries":  {"features": true, "feature": true},
	"ancestries":   {"ancestry feature": true, "ancestry features": true},
	"classes":      nil,
	"communities":  {"community feature": true, "community features": true},
	"domains":      {},
	"environments": {"features": true, "feature": true},
	"transformations": {
		"transformation feature": true, "transformation features": true,
		"transformation question": true, "transformation questions": true,
	},
}

// bulletIndexCategories are lifted out as a bare linked bullet instead of
// keeping their heading with a "See:" line under it. Both are chapter-level
// sections whose entire body moves to the generated file, so leaving the empty
// heading behind would put ten domains and thirteen classes into the book's
// top-level structure (and its contents list) with nothing under them. Every
// other category keeps its heading, because those headings carry the book's own
// grouping — the four elemental kin are nested under ELEMENTAL KIN, and
// flattening them into a bullet list would file the ancestries that merely
// follow that section under it as well.
var bulletIndexCategories = map[string]bool{
	"classes": true,
	"domains": true,
}

// replaceHeadingBlocksWithLinks lifts every entity definition block out of the
// SRD body and leaves a link to the generated file in its place. It returns the
// rewritten content, the number of blocks removed per category (for the build
// invariant) and any structural warnings.
func replaceHeadingBlocksWithLinks(content string, categoryLinks map[string]map[string]linkTarget) (string, map[string]int, []string) {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	removed := map[string]int{}
	var warnings []string
	var stack headingStack

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		level, title := parseHeading(line)
		if level == 0 {
			out = append(out, line)
			continue
		}
		stack.push(level, title)
		category := canonicalCategoryFor(level, &stack)
		if category == "" {
			out = append(out, line)
			continue
		}
		target, ok := categoryLinks[category][normalizeHeading(title)]
		if !ok {
			out = append(out, line)
			continue
		}
		// Every removal emits a link: nothing ever disappears silently.
		if bulletIndexCategories[category] {
			out = append(out, fmt.Sprintf("- [%s](%s)", target.name, target.path), "")
		} else {
			out = append(out, line, "", fmt.Sprintf("See: [%s](%s)", target.name, target.path), "")
		}
		removed[category]++

		allow, restricted := entityChildHeadings[category]
		if !restricted {
			allow = nil
		}
		headingLine := i
		i = skipEntityBlock(lines, i, level, allow, func(at int, unexpected string) {
			warnings = append(warnings, fmt.Sprintf(
				"%s block %q (source line %d) contains unexpected sub-heading %q at line %d; the block was truncated there so the unrelated section is not swallowed",
				category, title, headingLine+1, unexpected, at+1))
		})
	}
	return strings.Join(out, "\n"), removed, warnings
}

// skipEntityBlock returns the index of the last line belonging to the entity
// block that starts at `start`.
//
// Unlike skipHeadingBlock — which is correct for wrapper sections that really
// do own everything beneath them — this stops at the first descendant heading
// that is not part of the entity's known sub-structure, so a mis-scoped block
// can never eat a sibling or an unrelated deeper section.
func skipEntityBlock(lines []string, start, level int, allow map[string]bool, onUnexpected func(int, string)) int {
	for i := start + 1; i < len(lines); i++ {
		nextLevel, title := parseHeading(lines[i])
		if nextLevel == 0 {
			continue
		}
		if nextLevel <= level {
			return i - 1
		}
		if allow == nil || allow[normalizeHeading(title)] {
			continue
		}
		if onUnexpected != nil {
			onUnexpected(i, title)
		}
		return i - 1
	}
	return len(lines) - 1
}

// checkBuildInvariants fails the build when README generation and the JSON
// stage disagree. Invariant 1 (per-category removed-block count == JSON entry
// count) is the cheap check that would have caught every one of the twelve
// spurious heading matches that silently deleted ~1,089 words of 2.0 rules
// text. Invariant 2 catches the same class of bug from the other side: domain
// card links belong on domains/*.md, never in README.md.
func checkBuildInvariants(removed map[string]int, categoryLinks map[string]map[string]linkTarget, readme string) error {
	var problems []string
	for _, category := range canonicalCategories {
		want := len(categoryLinks[category])
		got := removed[category]
		if want == got {
			continue
		}
		problems = append(problems, fmt.Sprintf(
			"  %-15s %d heading block(s) replaced by links, but %s.json holds %d entries (delta %+d)",
			category, got, category, want, got-want))
	}
	if n := strings.Count(readme, "](abilities/"); n > 0 {
		problems = append(problems, fmt.Sprintf(
			"  %-15s README.md contains %d link(s) into abilities/; domain card links belong on domains/*.md only",
			"abilities", n))
	}
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("README build invariants failed:\n%s\n\n"+
		"A surplus means a heading elsewhere in the SRD shares an entity name and was\n"+
		"treated as that entity's definition; a shortfall means an entity's definition\n"+
		"site moved and its section is now duplicated in README.md. Either way, check\n"+
		"canonicalCategoryFor in .build/04_md/extract_from_json.go against the source\n"+
		"markdown's heading structure", strings.Join(problems, "\n"))
}

func replaceTableNamesWithLinks(content string, sectionLinks map[string]map[string]linkTarget) string {
	lines := strings.Split(content, "\n")
	currentSection := ""
	for i, line := range lines {
		level, title := parseHeading(line)
		if level == 2 {
			currentSection = strings.ToUpper(strings.TrimSpace(title))
		}
		if !strings.HasPrefix(strings.TrimSpace(line), "|") {
			continue
		}
		links := sectionLinks[currentSection]
		if len(links) == 0 {
			continue
		}
		columnIndex := 1
		if currentSection == "LOOT" || currentSection == "ITEMS" || currentSection == "CONSUMABLES" {
			columnIndex = 2
		}
		updated, changed := replaceTableCell(line, links, columnIndex)
		if changed {
			lines[i] = updated
		}
	}
	return strings.Join(lines, "\n")
}

func replaceListItemsWithLinks(content string, categoryLinks map[string]map[string]linkTarget) string {
	lines := strings.Split(content, "\n")
	inAdversaryList := false
	inEnvironmentSection := false
	for i, line := range lines {
		level, title := parseHeading(line)
		if level > 0 {
			if level == 3 && strings.EqualFold(strings.TrimSpace(title), "ADVERSARIES BY TIER") {
				inAdversaryList = true
			} else if level <= 3 {
				inAdversaryList = false
			}
			if level == 2 {
				inEnvironmentSection = strings.EqualFold(strings.TrimSpace(title), "USING ENVIRONMENTS")
			}
		}
		trimmed := strings.TrimLeft(line, " \t")
		if !strings.HasPrefix(trimmed, "- ") {
			continue
		}
		itemText := strings.TrimSpace(trimmed[2:])
		var updated string
		var changed bool
		if inAdversaryList {
			updated, changed = linkifyListItem(itemText, categoryLinks["adversaries"], false)
		} else if inEnvironmentSection {
			updated, changed = linkifyListItem(itemText, categoryLinks["environments"], true)
		}
		if changed {
			lines[i] = strings.Replace(line, trimmed, "- "+updated, 1)
		}
	}
	return strings.Join(lines, "\n")
}

func replaceClassDomainLinks(content string, classLinks, domainLinks map[string]linkTarget) string {
	if len(classLinks) == 0 || len(domainLinks) == 0 {
		return content
	}
	lines := strings.Split(content, "\n")
	inClassDomains := false
	for i, line := range lines {
		level, title := parseHeading(line)
		if level > 0 {
			if strings.EqualFold(strings.TrimSpace(title), "CLASS DOMAINS") {
				inClassDomains = true
			} else if level <= 3 {
				inClassDomains = false
			}
		}
		if !inClassDomains {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "- **") || !strings.Contains(trimmed, ":") {
			continue
		}
		start := strings.Index(trimmed, "**")
		if start == -1 {
			continue
		}
		rest := trimmed[start+2:]
		end := strings.Index(rest, "**")
		if end == -1 {
			continue
		}
		className := strings.TrimSuffix(rest[:end], ":")
		after := strings.TrimSpace(rest[end+2:])
		domainPart := strings.TrimSpace(strings.TrimPrefix(after, ":"))
		if domainPart == "" {
			continue
		}
		linkedDomains := linkDomainList(domainPart, domainLinks)
		linkedClass := className
		// "Assassion" is a typo in the PDF's class-domains list and is preserved
		// verbatim in the source markdown, so the displayed text stays as
		// printed and only the href is resolved.
		if target, ok, exact := resolveLinkTarget(classLinks, className); ok {
			label := className
			if exact {
				label = target.name
			}
			linkedClass = fmt.Sprintf("[%s](%s)", label, target.path)
		}
		lines[i] = fmt.Sprintf("- **%s:** %s", linkedClass, linkedDomains)
	}
	return strings.Join(lines, "\n")
}

func replaceClassMentionList(content string, classLinks map[string]linkTarget) string {
	if len(classLinks) == 0 {
		return content
	}
	prefix := "the Daggerheart Core Set includes only the following nine classes:"
	start := strings.Index(content, prefix)
	if start == -1 {
		return content
	}
	end := strings.Index(content[start:], ".")
	if end == -1 {
		return content
	}
	end += start
	listText := content[start+len(prefix) : end]
	parts := strings.Split(listText, ",")
	if len(parts) < 2 {
		return content
	}
	for i, part := range parts {
		parts[i] = strings.TrimSpace(part)
	}
	lastPart := parts[len(parts)-1]
	if strings.HasPrefix(lastPart, "and ") {
		lastPart = strings.TrimSpace(strings.TrimPrefix(lastPart, "and "))
		parts[len(parts)-1] = lastPart
	}
	for i, name := range parts {
		target, ok := classLinks[normalizeHeading(name)]
		if !ok {
			continue
		}
		parts[i] = fmt.Sprintf("[%s](%s)", target.name, target.path)
	}
	if len(parts) > 1 {
		parts[len(parts)-1] = "and " + parts[len(parts)-1]
	}
	repl := prefix + " " + strings.Join(parts, ", ") + "."
	content = content[:start] + repl + content[end+1:]
	return content
}

func replaceAncestryMentionList(content string, ancestryLinks map[string]linkTarget) string {
	if len(ancestryLinks) == 0 {
		return content
	}
	prefix := "The Daggerheart Core Set includes only the following ancestries:"
	start := strings.Index(content, prefix)
	if start == -1 {
		return content
	}
	end := strings.Index(content[start:], ".")
	if end == -1 {
		return content
	}
	end += start
	listText := content[start+len(prefix) : end]
	parts := strings.Split(listText, ",")
	if len(parts) < 2 {
		return content
	}
	for i, part := range parts {
		parts[i] = strings.TrimSpace(part)
	}
	lastPart := parts[len(parts)-1]
	if strings.HasPrefix(lastPart, "and ") {
		lastPart = strings.TrimSpace(strings.TrimPrefix(lastPart, "and "))
		parts[len(parts)-1] = lastPart
	}
	for i, name := range parts {
		target, ok := ancestryLinks[normalizeHeading(name)]
		if !ok {
			continue
		}
		parts[i] = fmt.Sprintf("[%s](%s)", target.name, target.path)
	}
	if len(parts) > 1 {
		parts[len(parts)-1] = "and " + parts[len(parts)-1]
	}
	repl := prefix + " " + strings.Join(parts, ", ") + "."
	content = content[:start] + repl + content[end+1:]
	return content
}

// insertCategoryParagraph inserts a linked list of every entry in the given
// category at the end of the named level-2 section's intro (before its first
// remaining subheading, or at the end of the section).
func insertCategoryParagraph(content string, links map[string]linkTarget, sectionTitle, lead string) string {
	if len(links) == 0 {
		return content
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		level, title := parseHeading(line)
		if level == 2 && strings.EqualFold(strings.TrimSpace(title), sectionTitle) {
			end := i + 1
			for end < len(lines) {
				nextLevel, _ := parseHeading(lines[end])
				if nextLevel > 0 && nextLevel <= 4 {
					break
				}
				end++
			}
			var names []string
			for _, target := range links {
				names = append(names, target.name)
			}
			sort.Strings(names)
			var linked []string
			for _, name := range names {
				target := links[normalizeHeading(name)]
				linked = append(linked, fmt.Sprintf("[%s](%s)", target.name, target.path))
			}
			paragraph := lead + " " + strings.Join(linked, ", ") + "."
			block := []string{"", paragraph, ""}
			out := append([]string{}, lines[:end]...)
			out = append(out, block...)
			out = append(out, lines[end:]...)
			return strings.Join(out, "\n")
		}
	}
	return content
}

func insertDomainCardReferenceList(content string, domainLinks map[string]linkTarget) string {
	if len(domainLinks) == 0 {
		return content
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		level, title := parseHeading(line)
		if level == 2 && strings.EqualFold(strings.TrimSpace(title), "DOMAIN CARD REFERENCE") {
			insertAt := i + 1
			var names []string
			for _, target := range domainLinks {
				names = append(names, target.name)
			}
			sort.Strings(names)
			var block []string
			block = append(block, "")
			for _, name := range names {
				target := domainLinks[normalizeHeading(name)]
				block = append(block, fmt.Sprintf("- [%s](%s)", target.name, target.path))
			}
			block = append(block, "")
			out := append([]string{}, lines[:insertAt]...)
			out = append(out, block...)
			out = append(out, lines[insertAt:]...)
			return strings.Join(out, "\n")
		}
	}
	return content
}

func removeDomainCardReferenceSections(content string, domainLinks map[string]linkTarget) string {
	if len(domainLinks) == 0 {
		return content
	}
	lines := strings.Split(content, "\n")
	var out []string
	for i := 0; i < len(lines); i++ {
		level, title := parseHeading(lines[i])
		if level == 3 && strings.HasSuffix(strings.ToUpper(strings.TrimSpace(title)), " DOMAIN") {
			name := strings.TrimSpace(strings.TrimSuffix(title, "DOMAIN"))
			name = strings.TrimSpace(strings.TrimSuffix(name, "Domain"))
			if _, ok := domainLinks[normalizeHeading(name)]; ok {
				i = skipHeadingBlock(lines, i, level)
				continue
			}
		}
		out = append(out, lines[i])
	}
	return strings.Join(out, "\n")
}

func linkDomainList(value string, domainLinks map[string]linkTarget) string {
	parts := strings.Split(value, "&")
	if len(parts) == 1 {
		return linkSingleDomain(value, domainLinks)
	}
	for i, part := range parts {
		parts[i] = linkSingleDomain(part, domainLinks)
	}
	return strings.Join(parts, " & ")
}

func linkSingleDomain(value string, domainLinks map[string]linkTarget) string {
	name := strings.TrimSpace(value)
	if name == "" {
		return value
	}
	if target, ok := domainLinks[normalizeHeading(name)]; ok {
		return fmt.Sprintf("[%s](%s)", target.name, target.path)
	}
	return name
}

// linkNameArticles are dropped when relaxing a name key. The SRD's index lists
// and its headings disagree about them: the index prints "Convergence, City of
// Portals" while the stat-block heading prints "Convergence, the City of
// Portals". Both spellings are in the PDF and both are preserved verbatim.
var linkNameArticles = map[string]bool{"the": true, "a": true, "an": true}

// relaxLinkKey folds the two name variations the PDF itself contains: an
// optional article inside the printed name, and the -or/-er agent-noun spelling
// split ("Outer Realms Corruptor" in the index vs "Outer Realms Corrupter" on
// the stat block).
func relaxLinkKey(key string) string {
	fields := strings.Fields(key)
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if linkNameArticles[f] {
			continue
		}
		if len(f) > 4 && strings.HasSuffix(f, "or") {
			f = f[:len(f)-2] + "er"
		}
		out = append(out, f)
	}
	return strings.Join(out, " ")
}

// editDistanceIsOne reports whether a and b differ by exactly one substitution,
// insertion or deletion. Both inputs are normalized keys, so byte comparison is
// safe. Used only as a last resort and only when the answer is unambiguous.
func editDistanceIsOne(a, b string) bool {
	if a == b {
		return false
	}
	if len(a) > len(b) {
		a, b = b, a
	}
	if len(b)-len(a) > 1 {
		return false
	}
	i := 0
	for i < len(a) && a[i] == b[i] {
		i++
	}
	if len(a) == len(b) {
		return a[i+1:] == b[i+1:]
	}
	return a[i:] == b[i+1:]
}

// resolveLinkTarget maps a printed name onto a generated file. It returns the
// target, whether a target was found at all, and whether the match was exact.
//
// The SRD prints some names two ways on purpose, and those variants must stay
// verbatim in the source markdown, so the lookup — not the text — has to be the
// tolerant side. Each relaxation is applied only when it resolves to exactly
// one candidate, so no ambiguous match is ever accepted.
func resolveLinkTarget(links map[string]linkTarget, name string) (linkTarget, bool, bool) {
	key := normalizeHeading(name)
	if key == "" || len(links) == 0 {
		return linkTarget{}, false, false
	}
	if target, ok := links[key]; ok {
		return target, true, true
	}

	relaxed := relaxLinkKey(key)
	var match linkTarget
	matches := 0
	for k, target := range links {
		if relaxLinkKey(k) == relaxed {
			match = target
			matches++
		}
	}
	if matches == 1 {
		return match, true, false
	}
	if matches > 1 {
		return linkTarget{}, false, false
	}

	// Single-character typo, e.g. the PDF's "Assassion" for "Assassin".
	// Guarded by a length floor so short names cannot collide by accident.
	if len(key) < 5 {
		return linkTarget{}, false, false
	}
	matches = 0
	for k, target := range links {
		if editDistanceIsOne(key, k) {
			match = target
			matches++
		}
	}
	if matches == 1 {
		return match, true, false
	}
	return linkTarget{}, false, false
}

func linkifyListItem(item string, links map[string]linkTarget, allowSuffix bool) (string, bool) {
	if len(links) == 0 {
		return item, false
	}
	parts := strings.Split(item, "•")
	changed := false
	for i, part := range parts {
		part = strings.TrimSpace(part)
		base := part
		suffix := ""
		if allowSuffix {
			if idx := strings.Index(part, " ("); idx != -1 {
				base = strings.TrimSpace(part[:idx])
				suffix = part[idx:]
			}
		}
		target, ok, exact := resolveLinkTarget(links, base)
		if ok {
			// On an exact match the index and the stat block agree, so the
			// canonical entity name is used. On a tolerated variant the PDF
			// prints two different spellings on purpose; the index keeps the
			// one the PDF actually prints and only the href is resolved.
			label := base
			if exact {
				label = target.name
			}
			parts[i] = fmt.Sprintf("[%s](%s)%s", label, target.path, suffix)
			changed = true
		} else {
			parts[i] = part
		}
	}
	return strings.Join(parts, " • "), changed
}

func replaceTableCell(line string, links map[string]linkTarget, columnIndex int) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "|") {
		return line, false
	}
	parts := strings.Split(line, "|")
	if len(parts) <= columnIndex {
		return line, false
	}
	if isTableSeparatorRow(parts) {
		return line, false
	}
	cell := strings.TrimSpace(parts[columnIndex])
	cellPlain := stripMarkdownEmphasis(cell)
	if strings.EqualFold(cellPlain, "name") || strings.EqualFold(cellPlain, "roll") || strings.EqualFold(cellPlain, "loot") {
		return line, false
	}
	key := normalizeHeading(cell)
	target, ok := links[key]
	if !ok {
		return line, false
	}
	parts[columnIndex] = " [" + target.name + "](" + target.path + ") "
	return strings.Join(parts, "|"), true
}

func isTableSeparatorRow(parts []string) bool {
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		for _, r := range trimmed {
			if r != '-' {
				return false
			}
		}
		return true
	}
	return false
}

func stripMarkdownEmphasis(value string) string {
	out := strings.ReplaceAll(value, "**", "")
	out = strings.ReplaceAll(out, "__", "")
	out = strings.ReplaceAll(out, "*", "")
	out = strings.ReplaceAll(out, "_", "")
	return strings.TrimSpace(out)
}

func abilityLink(name string) string {
	clean := strings.TrimSpace(name)
	if clean == "" || clean == "—" {
		return "—"
	}
	return fmt.Sprintf("[%s](../abilities/%s.md)", clean, url.PathEscape(sanitizeFilename(clean)))
}

func optionAt(options any, index int) string {
	if index <= 0 {
		return ""
	}
	switch typed := options.(type) {
	case []any:
		if len(typed) < index {
			return ""
		}
		if val, ok := typed[index-1].(string); ok {
			return val
		}
	case []string:
		if len(typed) < index {
			return ""
		}
		return typed[index-1]
	}
	return ""
}

func add1(value int) int {
	return value + 1
}

func parseHeading(line string) (int, string) {
	trimmed := strings.TrimLeft(line, " \t")
	if !strings.HasPrefix(trimmed, "#") {
		return 0, ""
	}
	hashes := 0
	for hashes < len(trimmed) && trimmed[hashes] == '#' {
		hashes++
	}
	if hashes == 0 || hashes >= len(trimmed) || trimmed[hashes] != ' ' {
		return 0, ""
	}
	return hashes, strings.TrimSpace(trimmed[hashes:])
}

func skipHeadingBlock(lines []string, start, level int) int {
	for i := start + 1; i < len(lines); i++ {
		nextLevel, _ := parseHeading(lines[i])
		if nextLevel > 0 && nextLevel <= level {
			return i - 1
		}
	}
	return len(lines) - 1
}

func normalizeHeading(text string) string {
	clean := strings.ToLower(strings.TrimSpace(text))
	clean = strings.ReplaceAll(clean, "’", "")
	clean = strings.ReplaceAll(clean, "'", "")
	var b strings.Builder
	space := false
	for _, r := range clean {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			space = false
			continue
		}
		if !space {
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(b.String())
}

func removeSectionsByHeadingPrefix(content string, level int, prefixes []string) string {
	lines := strings.Split(content, "\n")
	var out []string
	for i := 0; i < len(lines); i++ {
		lvl, title := parseHeading(lines[i])
		if lvl == level && hasHeadingPrefix(title, prefixes) {
			i = skipHeadingBlock(lines, i, lvl)
			continue
		}
		out = append(out, lines[i])
	}
	return strings.Join(out, "\n")
}

func hasHeadingPrefix(title string, prefixes []string) bool {
	upper := strings.ToUpper(strings.TrimSpace(title))
	for _, prefix := range prefixes {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}
	return false
}

func rtrimLines(input string) string {
	lines := strings.Split(input, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	return strings.Join(lines, "\n")
}

func removeBlankLinesBetweenListItems(input string) string {
	lines := strings.Split(input, "\n")
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if line == "" && i > 0 && i+1 < len(lines) {
			if isListItem(lines[i-1]) && isListItem(lines[i+1]) {
				continue
			}
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func isListItem(line string) bool {
	trimmed := strings.TrimLeft(line, " \t")
	if strings.HasPrefix(trimmed, "- ") {
		return true
	}
	dot := strings.IndexByte(trimmed, '.')
	if dot <= 0 {
		return false
	}
	for i := 0; i < dot; i++ {
		if trimmed[i] < '0' || trimmed[i] > '9' {
			return false
		}
	}
	return len(trimmed) > dot+1 && trimmed[dot+1] == ' '
}

func normalizeMarkdown(input string) string {
	out := rtrimLines(input)
	for strings.Contains(out, "\n\n\n") {
		out = strings.ReplaceAll(out, "\n\n\n", "\n\n")
	}
	out = removeBlankLinesBetweenListItems(out)
	return out
}

func ensureCanonicalFilename(path string) error {
	dir := filepath.Dir(path)
	want := filepath.Base(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.EqualFold(name, want) || name == want {
			continue
		}
		tmp := want + ".casefix"
		oldPath := filepath.Join(dir, name)
		tmpPath := filepath.Join(dir, tmp)
		if err := os.Rename(oldPath, tmpPath); err != nil {
			return err
		}
		return os.Rename(tmpPath, path)
	}
	return nil
}

func insertContents(content string) string {
	lines := strings.Split(content, "\n")
	var toc []string
	seenFirst := false
	slugs := slugger{}
	for _, line := range lines {
		level, title := parseHeading(line)
		if level == 0 {
			continue
		}
		// Every heading consumes a slug, including the deeper ones that never
		// reach the contents list; that is what makes the duplicate counter
		// agree with GitHub's.
		anchor := slugs.slug(title)
		if level > 2 {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(title), "contents") {
			continue
		}
		if level == 1 && !seenFirst {
			seenFirst = true
			continue
		}
		if anchor == "" {
			continue
		}
		display := titleCaseHeading(title)
		if level == 1 {
			if len(toc) > 0 && toc[len(toc)-1] != "" {
				toc = append(toc, "")
			}
			toc = append(toc, fmt.Sprintf("**[%s](#%s)**", display, anchor), "")
			continue
		}
		toc = append(toc, fmt.Sprintf("- [%s](#%s)", display, anchor))
	}
	if len(toc) == 0 {
		return content
	}
	for i, line := range lines {
		level, title := parseHeading(line)
		if level == 1 && strings.EqualFold(strings.TrimSpace(title), "INTRODUCTION") {
			block := []string{
				"**REFERENCE SITE**",
				"",
				"[matthttam.github.io/daggerheart-srd-2.0](https://matthttam.github.io/daggerheart-srd-2.0/)",
				"",
				"# CONTENTS",
				"",
			}
			block = append(block, toc...)
			block = append(block, "")
			out := append([]string{}, lines[:i]...)
			out = append(out, block...)
			out = append(out, lines[i:]...)
			return strings.Join(out, "\n")
		}
	}
	block := append([]string{
		"#### REFERENCE SITE",
		"",
		"[matthttam.github.io/daggerheart-srd-2.0](https://matthttam.github.io/daggerheart-srd-2.0/)",
		"",
		"# CONTENTS",
		"",
	}, toc...)
	block = append(block, "", "")
	return strings.Join(append(block, lines...), "\n")
}

func demoteHeadings(content string) string {
	lines := strings.Split(content, "\n")
	first := true
	for i, line := range lines {
		level, title := parseHeading(line)
		if level == 0 {
			continue
		}
		if first {
			first = false
			continue
		}
		level++
		if level > 6 {
			level = 6
		}
		lines[i] = strings.Repeat("#", level) + " " + strings.TrimSpace(title)
	}
	return strings.Join(lines, "\n")
}

// headingAnchor mirrors github-slugger, the algorithm GitHub and MkDocs' toc
// extension both use: lower-case, DROP punctuation outright (do not collapse it
// to a dash), keep letters, digits, "-" and "_", and map each remaining space to
// a single "-". "Turn Order & Action Economy" therefore anchors as
// "turn-order--action-economy" (two dashes), not "turn-order-action-economy".
func headingAnchor(title string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(title)) {
		switch {
		case r == ' ':
			b.WriteByte('-')
		case r == '-' || r == '_':
			b.WriteRune(r)
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		}
	}
	return b.String()
}

// slugger reproduces github-slugger's duplicate handling: the Nth repeat of a
// slug gets "-N" appended. It must be fed EVERY heading in document order, not
// just the ones that end up in the contents list, or the counters drift.
type slugger map[string]int

func (s slugger) slug(title string) string {
	base := headingAnchor(title)
	if base == "" {
		return ""
	}
	n := s[base]
	s[base] = n + 1
	if n == 0 {
		return base
	}
	return fmt.Sprintf("%s-%d", base, n)
}

func titleCaseHeading(title string) string {
	smallWords := map[string]bool{
		"a": true, "an": true, "and": true, "as": true, "at": true,
		"but": true, "by": true, "for": true, "from": true, "in": true,
		"of": true, "on": true, "or": true, "the": true, "to": true,
		"via": true, "with": true, "over": true, "into": true,
	}
	acronyms := map[string]bool{
		"GM": true, "SRD": true, "HP": true, "XP": true, "PC": true, "NPC": true, "ATK": true,
	}
	parts := strings.Fields(title)
	for i, part := range parts {
		upper := strings.ToUpper(part)
		lower := strings.ToLower(part)
		if i > 0 && smallWords[lower] {
			parts[i] = lower
			continue
		}
		if part == upper && acronyms[part] {
			parts[i] = part
			continue
		}
		parts[i] = titleizeWord(lower)
	}
	return strings.Join(parts, " ")
}

func titleizeWord(word string) string {
	if word == "" {
		return word
	}
	segments := strings.Split(word, "-")
	for i, seg := range segments {
		if seg == "" {
			continue
		}
		runes := []rune(seg)
		runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
		segments[i] = string(runes)
	}
	return strings.Join(segments, "-")
}

func groupBeastformsByTier(beastforms []map[string]any) []map[string]any {
	tierBuckets := map[int][]map[string]any{}
	for _, beast := range beastforms {
		tier := tierFromValue(beast["tier"])
		tierBuckets[tier] = append(tierBuckets[tier], beast)
	}
	ordered := []int{1, 2, 3, 4}
	seen := map[int]bool{}
	for _, t := range ordered {
		seen[t] = true
	}
	var tiers []map[string]any
	for _, t := range ordered {
		items := tierBuckets[t]
		if len(items) == 0 {
			continue
		}
		tiers = append(tiers, map[string]any{
			"tier":  t,
			"items": items,
		})
	}
	for t, items := range tierBuckets {
		if seen[t] || len(items) == 0 {
			continue
		}
		tiers = append(tiers, map[string]any{
			"tier":  t,
			"items": items,
		})
	}
	return tiers
}

func tierFromValue(value any) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		var out int
		fmt.Sscanf(v, "%d", &out)
		if out > 0 {
			return out
		}
	}
	return 0
}

// featureLabel renders a feature name as its inline label: names keep their
// printed terminal period (2.0 countdown/pool labels); all others get the
// usual trailing colon.
func featureLabel(name string) string {
	if strings.HasSuffix(name, ".") {
		return name
	}
	return name + ":"
}

func featureQuestions(features any) []string {
	list, ok := features.([]any)
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, entry := range list {
		m, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		q, ok := m["question"].(string)
		if !ok {
			continue
		}
		q = strings.TrimSpace(q)
		if q == "" || seen[q] {
			continue
		}
		seen[q] = true
		out = append(out, q)
	}
	return out
}
