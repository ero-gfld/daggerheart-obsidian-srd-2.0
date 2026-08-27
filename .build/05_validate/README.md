# 05_validate

A reusable validation suite for this repository. It is the last stage of the
pipeline and the only one that reads *everything* — the SRD source markdown, the
generated `README.md`, and every generated content file — and asks whether they
still agree with each other.

```sh
go run .build/05_validate/validate.go
```

Run it from the repository root. It exits **non-zero when any ERROR-level check
fails**, so it can gate CI:

```yaml
- uses: actions/setup-go@v5
  with:
    go-version: '1.24'
- run: go run .build/05_validate/validate.go
```

Flags:

| Flag | Default | Purpose |
| --- | --- | --- |
| `-root` | `.` | Repository root to validate. Useful for validating a fixture copy. |
| `-max-per-check` | `40` | Cap the findings printed per check (`0` = unlimited). |
| `-warn-fatal` | `false` | Also exit non-zero when only WARN findings are present. |

Everything the checks know about *this* repo — which markdown is the source,
which directories hold generated content, which sections may legitimately be
lifted out of `README.md`, which heading keywords make a link plausible — lives
in the `config` struct at the top of `validate.go`. Point the checks at a
restructured repo by editing that, not the check bodies.

## Why this suite exists

The pipeline is `source md → CSV → JSON → generated content dirs + README.md`.
Every stage is a lossy transformation driven by pattern matching, and the
failure mode that matters is silent: text that exists in the SRD source simply
stops existing in the published output, with nothing in the build log to say so.
Three such losses were found by hand before this suite was written, and they are
the reference cases every check is measured against:

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

All three are the same shape of bug: **content silently stopped being
reachable.** So the suite is built around one dominant check — content
round-trip — with the other checks guarding the ways output can be wrong while
still containing all the words.

---

## Checks

### `content-round-trip` — ERROR

**Catches bugs 1, 2 and 3.** This is the load-bearing check; everything else is
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
