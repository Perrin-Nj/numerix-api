// Picks and processes the photo items. node photos.mjs [--only id,id]
// 1. search Commons per catalog row (quality images first), keep licence-ok landscape JPEGs without people words
// 2. overrides.json {id: "File:Title.jpg" | index} replaces the auto pick after a visual review
// 3. writes work/photos/<id>.jpg (source thumb) and work/photos.json (pick + licence data + top candidates)
import { readFileSync, writeFileSync, existsSync, mkdirSync, rmSync } from 'node:fs';
import { join } from 'node:path';
import { HERE, search, byTitle, download, wpImages, categoryFiles } from './commons.mjs';
import queries1 from './queries.mjs';
import queries2 from './queries2.mjs';
const queries = { ...queries1, ...queries2 };
import { openverse, nasa } from './ov.mjs';
import rows from './catalog/photos.mjs';

const only = (process.argv.find((a) => a.startsWith('--only='))?.slice(7) || '').split(',').filter(Boolean);
const WORK = join(HERE, 'work');
mkdirSync(join(WORK, 'photos'), { recursive: true });
const OUT = join(WORK, 'photos.json');
const prev = existsSync(OUT) ? JSON.parse(readFileSync(OUT, 'utf8')) : {};
const ovPath = join(HERE, 'overrides.json');
const overrides = existsSync(ovPath) ? JSON.parse(readFileSync(ovPath, 'utf8')) : {};
const PEOPLE = /\b(man|men|woman|women|girl|boy|child|children|kid|people|person|portrait|crowd|selfie|nude|bikini|wedding|baby|team|player|players|tourist|tourists|visitors|family|couple|student|students|soldier|military|police|mask|smoking|beer|wine|alcohol|casino|gun|weapon|skull|dead|corpse|blood|zoo keeper|zookeeper|hunting|hunter|slaughter|fishing|carcass|roadkill)\b/i;
const used = new Set(Object.values(prev).map((x) => x.pick?.title));

function good(c) {
  if (!c.ok) return false;
  if (!/^image\/(jpeg|png)$/.test(c.mime)) return false;
  if (c.w < 900 || c.h < 600) return false;
  if (PEOPLE.test(`${c.title} ${c.desc} ${c.cats}`)) return false;
  return true;
}

for (const r of rows) {
  const [id, q] = r;
  if (only.length && !only.includes(id)) continue;
  if (prev[id]?.pick && !only.length) continue;
  let cands = [];
  const src = queries[id] || q;
  cands = (src.startsWith('nasa:') ? await nasa(src.slice(5)) : await openverse(src, { pages: 2 })).filter(good);
  cands.sort((x, y) => (/CC0|Public/.test(y.licence) ? 1 : 0) - (/CC0|Public/.test(x.licence) ? 1 : 0)); // stable: prefer no-attribution licences
  cands = cands.filter((c) => !used.has(c.title) || c.title === prev[id]?.pick?.title);
  let pick = cands[0];
  const ov = overrides[id];
  if (typeof ov === 'number') pick = cands[ov] || pick;
  else if (typeof ov === 'string') pick = (await byTitle(ov, 1100)) || pick;
  if (!pick) { console.log('NO CANDIDATE', id, q); prev[id] = { cands: [] }; continue; }
  used.add(pick.title);
  if (prev[id]?.pick?.url && prev[id].pick.url !== pick.url) rmSync(join(WORK, 'photos', id + '.jpg'), { force: true }); // re-picked: drop the stale file
  await download(pick.url, join(WORK, 'photos', id + '.jpg'));
  prev[id] = { pick, cands: cands.slice(0, 6).map((c) => c.title) };
  console.log('ok', id, '<-', pick.title, '|', pick.licence);
  writeFileSync(OUT, JSON.stringify(prev, null, 1));
}
writeFileSync(OUT, JSON.stringify(prev, null, 1));
