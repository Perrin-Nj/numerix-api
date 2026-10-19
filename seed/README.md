# Seed files

Loaded by `cmd/seed` (idempotent: only changed objects are written). Validate with `go run ./cmd/seed --validate-only` and `go test ./internal/seedload ./internal/content`.

| File | Content |
|---|---|
| `worlds.json` | `{schema:1, worlds:[{id, ord, name{fr,en}, tagline, hook, accent_color, grade_min, grade_max, strand, hero_asset?}]}` 20 worlds |
| `quests.json` | `{schema:1, quests:[{id "world.slug", world, ord, skill (real PDA id), name, goal, grade_min, grade_max, target_count, icon_asset?}]}` 89 quests |
| `misconceptions.json` | extra `mis_*` error codes. The 99 curriculum codes come from the embedded graph. Use only these codes. |
| `questions/<world>.json` | `{world, schema:1, questions:[Question]}`. `_examples.json` (world `_examples`) is demo data, never loaded in production |
| `assets/*.json`, `assets/files/` | written by tools/assets: one array of assets per world, `sprites.json`, `vocab.json`; bytes in `files/` |
| `schema/denylist.txt`, `names.txt` | embedded word lists (brands and rude words refused; allowed first names) |

## Question (all texts are `{fr, en}`; both required)

```
id        "cuisine.fournee.007"  world.quest.NNN (001-899; 9xx = samples), prefix = quest id
quest, skill  the quest id and ITS skill (must match quests.json)
type      numeric | choice | truefalse | order | match | word | visual | estimate
grade 1-6 (inside the quest range and >= the skill's year), difficulty 1-5
story     one short line giving the numbers a purpose        prompt   what to answer
math_text optional, language neutral ("8 + 7 = ?")
visual    {kind: bars|pictograph|clock|numberline|grid|fraction|dice, data:{...}}   (required for type visual)
choices   3-5 of {id, text, misconception | random:true}     (kind choice; wrong ones need a code, max 1 random)
items     order: [{id,text,key}]   match: {left:[{id,text}], right:[{id,text}]}
answer    numeric {kind,value|[q,r],calc}   choice {kind,correct,calc_choice{id:expr}}   truefalse {kind,value:bool,misconception?}
          order {kind,sequence[ids],by:asc|desc,misconception?}   match {kind,pairs[[l,r]],misconception?}
          range {kind,min,max,exact_for_check,calc}   (max-min <= 20% of the value)
common_errors  numeric/range: [{value, misconception, feedback{fr,en}}]
hint, explain {fr,en}  hint guides without the answer; explain teaches (shown only after answering)
tags[], media[{role scene|object|visual|choice:<id>, asset | criteria{world,topic,object,mood,kind,setting}}]  max 3
status draft|review|published, author, version, source (filled by the loader)
```

`type` to `answer.kind`: numeric>numeric, choice>choice, truefalse>truefalse, order>order, match>match, word/visual>numeric|choice, estimate>range|choice.

`calc`: numbers, `+ - * / ( )`, `divmod(a,b)` `min` `max` `abs` `round` `gcd` `pow(a,n)` `sum(...)`, exact fractions. The server recomputes it.

Rules enforced (hard errors unless noted): answer inside the options, distinct distractors, both languages, calc equals the answer, no answer in story/prompt/math_text/hint (digits or words), no timers or hurry words, no em or en dash, no emoji, deny list, reading limits by grade band (story 25/35/45 words, prompt 20/30/40, sentence 14/18/22, option 8/10/12), at most 3 media. Warnings: unknown capitalised names, missing calc, tags under 2, near duplicate prompts, fewer than 4 types in a quest. A world file whose questions are all `published` must meet the type quotas (numeric 10, choice 10, word 8, truefalse 5, order 5, match 5, visual 5, estimate 4) and target-2 per quest.

## visual.data shapes (read by the play pages)

- `bars`: `{title{fr,en}, bars:[{label{fr,en}, value}], max?}`
- `pictograph`: `{title, each, rows:[{label, count}]}`
- `clock`: `{hour 1-12, minute 0-59}`
- `numberline`: `{min,max,step,labels[],arrow?}`
- `grid`: `{cols,rows,cells:[{x,y,label}]}`
- `fraction`: `{parts, filled}`
- `dice`: `{dice:[1-6,...]}`

Strings inside `data` are scanned for answer leaks; numbers are not.

## Client answer format

choice: the value of the chosen card as sent in `choices` (number or text). truefalse: boolean. order: item ids. match: `[[left,right],...]`. numeric: number or `[quotient, remainder]`.
