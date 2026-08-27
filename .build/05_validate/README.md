# 05_validate

A reusable validation suite for this repository. It is the last stage of the
pipeline and the only one that reads *everything* — the SRD source markdown, the
generated `README.md`, and every generated content file — and asks whether they
still agree with each other.

```sh
go run .build/05_validate/validate.go
```

Run it from the repository root. It exits **non-zero when any ERROR-level check
fails**, and that is what gates CI: `.github/workflows/ci.yml` runs it before
`mkdocs gh-deploy`, so a failure blocks publishing instead of shipping broken
content to GitHub Pages.

```yaml
- uses: actions/setup-go@v5
  with:
    go-version: '1.24'
    cache: false
- run: go run .build/05_validate/validate.go
```

WARN findings do **not** fail the build — that would need `-warn-fatal` — because
the four the suite currently reports are faithful reflections of inconsistencies
printed in the SRD PDF itself, not defects in the generated output.

Flags:

| Flag | Default | Purpose |
| --- | --- | --- |
| `-root` | `.` | Repository root to validate. Useful for validating a fixture copy. |
| `-max-per-check` | `40` | Cap the findings printed per check (`0` = unlimited). |
| `-warn-fatal` | `false` | Also exit non-zero when only WARN findings are present. |

Everything the checks know about *this* repo — which markdown is the source,
which directories hold generated content, which sections may legitimately be
lifted out of `README.md`, which heading keywords make a link plausible, which
published lines are repo metadata rather than SRD content — lives
in the `config` struct at the top of `validate.go`. Point the checks at a
restructured repo by editing that, not the check bodies.

## Why this suite exists

The pipeline is `source md → CSV → JSON → generated content dirs + README.md`.
Every stage is a lossy transformation driven by pattern matching, and the
failure mode that matters is silent: text that exists in the SRD source simply
stops existing in the published output, with nothing in the build log to say so.
Three such losses were found by hand before this suite was written, and a fourth
bug — the mirror image of the other three — was found by the suite itself. They
are the reference cases every check is measured against:

1. **Reborne's truncated feature.** `communities/Reborne.md` drops the second
   paragraph of *Found Family* — the extractor stops a multi-paragraph feature
   at the first colon-bearing line. The paragraph is in the source and appears
   nowhere in the repository.
2. **The FLIGHT collision.** The source's `### FLIGHT` section (a
   floating-magic-school campaign rule) was replaced in `README.md` by
   `See: [Flight](abilities/Flight.md)` — a link to an unrelated *Arcana domain
   card* that merely shares the heading name. The substitution also swallowed
   two sibling sections, `USING TRAITS FOR FLIGHT` and `LESS LETHAL CAMPAIGNS`.
3. **Monster Hunting's transformations.** The `REANIMATED` / `VAMPIRE` /
   `WEREWOLF` subsections under Monster Hunting Campaigns vanished with no link
   at all. *Vampire's Curse* and *Wolf's Curse* exist nowhere in the repo.
4. **The five invented sentences.** The round of fixes for bugs 1–3 restored
   reachability by *writing new prose*: three README sentences enumerating the
   ancestries, communities and transformations ("This SRD describes the
   following ancestries: …"), an editorial note on the four Elemental Kin
   ancestry pages ("*One of the Elemental Kin ancestries.*"), and a pointer note
   on `subclasses/Martial Artist.md` telling the reader the Martial Stances
   sheet was printed on the Brawler class page. None of the five appear anywhere
   in the SRD. The communities one was actively misleading: it sat immediately
   below the SRD's own "the Daggerheart Core Set includes only the following
   communities: Highborne, …" and then listed all fifteen. Every check in the
   suite passed on that tree, because each of those checks only ever asks
   whether source text *survived*.

Bugs 1–3 are the same shape: **content silently stopped being reachable.** Bug 4
is that shape run backwards: **content appeared that no one licensed.** So the
suite is built around two mirrored round-trip checks — source → output and
output → source — with the other checks guarding the ways output can be wrong
while still containing all the words.

---

## Checks

### `content-round-trip` — ERROR

**Catches bugs 1, 2 and 3.** This is the load-bearing check in the source →
output direction; `authored-prose` below is its mirror and everything else is
supporting fire.

Every non-blank, non-heading line of the SRD source markdown must appear either
in `README.md` or in some generated content file. "Appear" is measured on token
streams, not raw text: each file is flattened to a stream of lowercase
alphanumeric tokens with markdown emphasis, punctuation, smart quotes and link
syntax discarded, so a source line still matches after the generator re-wraps
it, re-emphasises it (`_Found Family:_` → `**_Found Family:_**`) or linkifies it
(`| Broadsword |` → `| [Broadsword](weapons/Broadsword.md) |`). A line matches
when its tokens occur as a contiguous run inside one file.

Because generated files also *restructure* source lines — splitting
`_Agility +1 | Evasion +2 | Melee Agility d4 phy_` into three labelled bullets,
relabelling `**Gain advantage on:**` to `**Advantages:**` — a line that does not
match as a whole is broken into segments (table cells, pipe-delimited stat
fields, `**Label:**` markers, sentences) and each segment is looked up
independently. A finding is raised only for a segment that is present *nowhere*.
That is the question that actually matters, and in practice it produces no
formatting-noise false positives on this repo.

Sections that are deliberately lifted out of `README.md` wholesale — the tier
stat-block sections, the domain card reference — need no special casing here,
because their content is required to turn up in the generated files they were
lifted into. `config.deliberateRemovals` exists for the companion heading check
below; `config.roundTripIgnore` can suppress individual source lines by regexp
and should be kept empty, since every entry in it is content a reader will
never see.

### `authored-prose` — ERROR

**Catches bug 4.** The reverse of `content-round-trip`, and the reason bug 4
survived three rounds of review: every other check in this suite reads the
source and asks whether it still shows up in the output, so none of them can
see text that was never in the source to begin with.

Every non-blank line of `README.md` and of every generated content file must
trace back to `.build/01_pdf/DH-SRD-*.md`. This repo's entire value proposition
is a verbatim rendering of a document published under the Darrington Press
Community Gaming License; a sentence the generator wrote is not SRD text, is not
covered by that license, and is indistinguishable to a reader from text that is.
Authored prose in generated output has to be impossible to ship, not merely
discouraged.

The comparison machinery is the same token/5-gram index `content-round-trip`
uses, pointed the other way: the corpus is built from the source markdown alone
and the published lines are the queries. Everything the forward check tolerates
is therefore tolerated here — re-wrapping, re-emphasising, smart quotes — and
four further output-side transformations are modelled on top:

- **Link syntax compares text, not target.** `[Broadsword](weapons/Broadsword.md)`
  is looked up as `Broadsword`. Stage 04's whole job is splicing links into
  sentences the SRD prints unlinked, and the target path is repo structure
  rather than prose. This is what lets the linkified enumerations of every
  ancestry and community (source lines 78 and 79) pass while the *invented*
  enumerations of bug 4 — which linkified the same names inside a sentence
  Darrington Press never wrote — do not.
- **Table rows re-rendered as bullets.** A weapon table row becomes
  `- **Trait:** Agility` / `- **Range:** Melee`, and a beastform's
  `_Agility +1 | Evasion +2 | Melee Agility d4 phy_` becomes
  `- **Attack:** Melee Agility d4 phy`. A line that does not match whole is
  broken into segments exactly as in the forward direction — table cells,
  pipe-delimited stat fields, `**Label:**` markers, sentences — and each segment
  is looked up on its own.
- **Emphasis-only lines.** A segment that is *nothing but* a sequence of
  emphasised fragments is one more thing the forward direction never sees:
  stage 04 assembles it out of separate cells of a single source row, so
  `**_Tier 1_** _Primary_ _Physical_ _Weapon_` is four independent values that no
  source line prints in that order. `emphasisOnlyRuns` splits those and requires
  each fragment to trace back — 303 weapon header lines, 61 armor header lines
  and the 24 beastform `**_Tier 1_** _(Fox, Mouse, Weasel, etc.)_` lines need
  it. Because every fragment is still required, the split loosens *contiguity*
  only; it never lets an unsourced word through. And it gives up the moment a
  letter or digit survives outside the emphasis runs, so a sentence with a bold
  word in the middle of it is still checked as one sentence and an invented
  sentence cannot be waved past a fragment at a time.
- **Headings are checked too**, unlike in the forward direction, which skips
  them because the generator legitimately rewrites and re-levels source
  headings. In this direction a heading nobody wrote is exactly as
  unpublishable as a sentence nobody wrote, and the current tree passes with
  headings included.

Fragments shorter than three tokens are not reported. They are the field labels,
navigation stubs and single cell values the stage-04 templates supply *around*
SRD content — the `**Trait Bonus:**` of a beastform bullet, the `**Option 1**`
column heading of the domain card grid, the 45 `See: [Aetheris](…)` stubs in
`README.md` — and the SRD prints no label in those positions for them to be
verbatim copies of. They are also the fragments the index can say least about:
below the 5-gram threshold, a lookup degrades to a bag test over every short
n-gram in the source, so a hit is nearly free and a miss nearly meaningless.
Prose starts above them. The known cost of that floor is that the
four "upgraded" beastforms (`Legendary Beast`, `Legendary Hybrid`,
`Mythic Beast`, `Mythic Hybrid`) publish `<no value>` in their stat bullets —
the SRD gives those forms no stat line and the template renders the empty field
anyway — and `no value` is two tokens, so this check does not report it. That is
a stage-04 template bug worth fixing at the source; it is not something to
loosen this floor for.

`config.authoredProseIgnore` is the allowlist, and it holds exactly two entries:
the `**REFERENCE SITE**` banner and the `matthttam.github.io` link beneath it
that stage 04 prepends to `README.md`. The PDF has no reference site — this repo
*is* the reference site — so those two lines are repo metadata rather than game
content. They are matched by exact shape, so a third line appearing under the
same banner is still reported. Every future entry has to clear the same bar: the
moment an allowlist entry describes the SRD rather than the repository, the
generator is authoring prose again.

### `section-removal-link` — ERROR

**Catches bug 2** (`USING TRAITS FOR FLIGHT`, `LESS LETHAL CAMPAIGNS`).

Round-trip proves the *words* survive somewhere; this check proves the
*structure* did. Every source heading must still be findable: as a heading in
`README.md`, as a heading inside a generated content file, or as the name of a
generated content file. A heading that is an entity-qualified form of a heading
in that entity's own file counts too — the generator rewrites
`ASSASSIN'S HOPE FEATURE` to `HOPE FEATURE` inside `classes/Assassin.md`.

A wrapper section listed in `config.deliberateRemovals` (`TIER 1 ADVERSARIES`,
`ARCANA DOMAIN`, …) may disappear on its own, but only if every child heading it
contained resolved. That is the "each removal must have emitted a resolvable
link" rule.

### `link-broken` — ERROR

Every relative markdown link in `README.md` and in every generated content file
must resolve to a file that exists. Targets are URL-decoded first (`%20` is
everywhere in this repo's links) and anchors are stripped. Without this, a
renamed entity leaves a trail of dead links that nothing else notices.

### `link-case-mismatch` — ERROR

A link whose target exists only under different capitalisation. This is an ERROR
rather than a WARN on purpose: it resolves fine on the case-insensitive
filesystems most contributors use (Windows, default macOS) and 404s on Linux CI
and GitHub Pages, so it is exactly the class of bug that reaches production.

### `link-not-portable` — ERROR

Link targets that are absolute (`/foo.md`, `C:\…`, UNC) or that resolve outside
the repository root. This repo is cloned *into* Obsidian vaults, often nested
below the vault root, so every internal link has to be relative and has to stay
inside the repo — a vault-rooted or escaping link breaks the moment the repo
is not the vault root.

### `link-target-sanity` — WARN

**Catches bug 2's root cause.** A `See:`-style or lone-link line that stands in
for a heading of the same name is checked against its surrounding heading
context: the link's target directory must be plausible given the section it
sits in (`config.categoryContext` maps each content directory to the heading
keywords that make it plausible). A domain-card link under
`FLOATING MAGIC SCHOOL CAMPAIGNS` is not.

WARN rather than ERROR because plausibility is a heuristic — a legitimate
cross-reference could land here. It is the check that names *why* bug 2
happened, where round-trip only names *that* it happened.

### `entity-missing-file` — ERROR

Every name in the SRD's own index lists must have a generated file:
`ADVERSARIES BY TIER`, `ENVIRONMENT STAT BLOCKS BY TIER`, the class list, the
ancestry list, the community list, the domain list and the transformation list.
A name in an index with no stat block behind it is a link the reader cannot
follow. Non-entities that appear in these lists (`Mixed Ancestry`, which is a
rule for combining two cards rather than a card) are excluded per list.

Before reporting, a name with no file at its exact path is retried against a
relaxed form of every filename in that category: leading articles (`the`, `a`,
`an`) dropped and a word-final `-or` folded to `-er`. This is the same
relaxation `.build/04_md/extract_from_json.go` uses to turn an index bullet into
a link, so the validator and the generator agree about which names are the same
entity; a hit is reported as `entity-name-variant` instead. The relaxation
deliberately stops short of the generator's edit-distance-1 stage, which would
make `Fire Elemental` and `Fire Elementals` interchangeable and let a genuinely
absent stat block pass. A relaxed key with more than one file behind it is
ambiguous and resolves nothing, so the check fails closed.

### `entity-name-variant` — WARN

The index list and the stat-block heading print the same entity with different
words, and both spellings are in the PDF: `Outer Realms Corruptor` in
`ADVERSARIES BY TIER` vs `Outer Realms Corrupter.md`, and `Convergence, City of
Portals` vs `Convergence, the City of Portals.md`. The reader can reach the file
— stage 04 links these correctly — so this is not an `entity-missing-file`. It
is a WARN rather than silence because the divergence is real: a future SRD
revision could turn one of the two spellings into a typo nobody notices.

### `entity-name-case-mismatch` — WARN

The index list and the generated filename name the same entity with different
capitalisation (`Will-o'-the-Wisps` vs `Will-O-The-Wisps.md`). Cosmetic on its
own, but it is one edit away from becoming a `link-case-mismatch`.

### `entity-orphan` — ERROR

The converse of coverage: every generated content file must be reachable — by
some chain of relative links starting at `README.md`, or by appearing in one of
the SRD index lists. An unreachable file is content that exists on disk and can
never be found by a reader, which is the same harm as bug 3 with a different
cause. This is what enforces "a removal must have emitted a resolvable link"
from the output side.

### `entity-list-missing` — WARN

An index list named in the configuration could not be located in the source at
all, so that category's coverage went unverified. Guards against the suite
silently going blind when the SRD is restructured.

### `statblock-incomplete` — ERROR

Structural completeness of each generated entity, per category:

| Category | Required |
| --- | --- |
| `adversaries` | Tier/Role, Motives & Tactics, Difficulty, Thresholds, HP, Stress, ATK, ≥1 feature |
| `environments` | Tier/Type, Impulses, Difficulty, Potential Adversaries, ≥1 feature |
| `abilities` | a Level line and a Recall Cost line |
| `classes` | domains, starting evasion, starting hit points, class items, ≥1 subclass link |

This is the check that catches bug 1's *class* of failure at the destination
rather than the source: a template that silently renders an empty field, or an
extractor that drops a stat line, produces a file that reads plausibly and is
missing something a table needs.

### `duplicate-entity-name` — ERROR

Two entries in the same stage-03 JSON category sharing a name. Only one of them
can become a file; the other is silently lost during generation.

### `filename-collision` — ERROR

Two failure modes, both silent overwrites:

- Filenames in one category differing only by case — fine on Linux, destructive
  on Windows/macOS checkouts and in Obsidian sync.
- Distinct entity names that `sanitizeFilename` maps to the same file
  (`Gambeson's Armor` and `Gambesons Armor` both become `Gambesons Armor.md`).
  The suite's `sanitizeFilename` mirrors the one in
  `.build/04_md/extract_from_json.go` — **keep the two in sync.**

### `table-column-mismatch` — ERROR

Every markdown table in the source and in the published output must have a
consistent cell count across header, delimiter and body rows. Source tables are
hand-edited during the "a lot of manual markdown cleanup" step of stage 01, and
a row that gains or loses a pipe shifts every value in it into the wrong column
— which then flows through CSV, JSON and into a stat block that is wrong but
looks fine. A table with no delimiter row is reported as WARN under the same
check name (it renders as plain text rather than wrong data).

### `filename-portability` — ERROR / WARN

Filenames must survive Windows, macOS and Obsidian:

- ERROR: reserved characters `< > : " | ? *` or control characters; trailing dot
  or space (Windows strips these silently); leading space; reserved device names
  (`CON`, `NUL`, `COM1`…); basename over 255 bytes.
- WARN: `#`, `[`, `]`, `^` in a filename — these need escaping in markdown links
  and confuse Obsidian's `[[wikilink]]` parser; and repo-relative paths over 200
  bytes, which risk `MAX_PATH` on deep Windows clones.

---

## Checks considered but not implemented

- **Anchor-link validation.** `README.md`'s table of contents links to
  `#heading` anchors. Verifying them requires reimplementing the slug algorithms
  of both GitHub and mkdocs-material, which disagree with each other; a check
  that is right for one renderer is wrong for the other. Anchors are skipped
  rather than half-checked.
- **Semantic value round-trip** (does `weapons/Broadsword.md` carry the same
  damage dice as the source table row?). This needs a per-category field mapping
  from source columns to template fields, i.e. a second implementation of stages
  02–04. Token-level round-trip catches the same corruption whenever a value
  disappears, and `table-column-mismatch` catches the misalignment that causes
  most of it.
- **Cross-referenced rules terms** (does every italicised condition such as
  `_Restrained_` have a definition somewhere?). Distinguishing a defined game
  term from ordinary emphasis needs a curated glossary; without one the check is
  mostly noise.
