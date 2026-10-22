# Consignes de traduction du SRD (anglais → français)

You translate files of the Daggerheart SRD (tabletop RPG rules, Markdown, read in Obsidian) from English into French. The vault root is `C:\Users\evan.roussin\source\repos\cheanos-daggerheart`; the SRD is its subfolder `Règles (SRD)`.

## Before you start

1. Read `Règles (SRD)\GLOSSAIRE.md` in full. It is binding: every game term has exactly one French form, with the capitalisation shown.
2. Names of cards, adversaries, environments, items, consumables, weapons and armor are already translated in the tables `Règles (SRD)\.build\fr\noms\*.tsv` (columns: dossier, fichier, anglais, francais). Look a name up with the Grep tool on its English form. Domains, classes, subclasses, ancestries, communities and transformations are in the glossary.

## For each file you are given

Read it, translate it, and overwrite it at the same path with the Write tool. Translate everything a reader sees; change nothing else.

**Keep exactly:**
- the Markdown structure: same headings in the same order and at the same level (translate their text), same tables (same rows and columns), same lists, same blank lines, same bold and italic markers (`**`, `***`, `*`, `_`);
- every number and dice expression (`1d12+2`, `+3`, `8/15`, `(3)`), and `phy` / `mag` in stat lines;
- every link target: in `[text](target)` translate only `text`, never `target` (anchors such as `#HOPE%20&%20FEAR` included). Keep every link, even if the sentence order changes;
- the `—` placeholders in tables.

**Translate:**
- the `# Title` with the French name from the tables or the glossary, nothing else on that line;
- names of other cards, adversaries, items... when the text mentions them (same tables);
- feature titles: `***Relentless (3) - Passive:***` becomes `***Implacable (3) - Passif :***` (keep the ` - ` hyphen and the asterisks, add the French space before the colon);
- stat labels: Difficulty = Difficulté, Thresholds = Seuils, HP = PV, Stress = Stress, ATK = ATQ, Experience = Expérience, Motives & Tactics = Motivations et tactiques, Impulses = Pulsions, Potential Adversaries = Adversaires possibles, Feature = Aptitude.

**Style:**
- clear, natural French, as in a published rulebook; not word for word;
- address the player with *vous* (« vous gagnez », « Dépensez une Peur »);
- French typography: a space before `: ; ? !` (`**Difficulté :** 14`), « guillemets », typographic apostrophe ’;
- never add an em dash (—) in running text: use a colon, a comma or a new sentence;
- game terms keep the glossary's capital letter (Espoir, Peur, Stress, Évasion, Proche, Vulnérable, Mêlée...).

**Never:** run git, edit a file you were not given, add frontmatter, add notes or a « VO » line (a script adds those later).

## When you are done

From the vault root, run the checker on your files (Bash):

```
PYTHONIOENCODING=utf-8 python z_Outils/srd_fr/verifier.py "<file 1>" "<file 2>" ...
```

with paths relative to `Règles (SRD)` (e.g. `adversaries/Acid Burrower.md`). Fix what it reports and run it again. A report can be a false positive (an English word that is also a proper name, a number you rightly wrote out); leave those and say why.

Reply with: the number of files translated, the checker's last summary line, any remaining report you left and why, and any term you needed that the glossary lacks (with the French you chose).
