// node list.mjs videos|anims  : print the ids of the finished clips, comma separated
import { readFileSync } from 'node:fs';
import { HERE } from './commons.mjs';
const rows = (await import(process.argv[2] === 'videos' ? './catalog/videos.mjs' : './catalog/anims.mjs')).default;
const info = JSON.parse(readFileSync(HERE + '/work/clips.json', 'utf8'));
const ok = rows.filter((r) => info[r.id]?.done).map((r) => r.id);
const missing = rows.filter((r) => !info[r.id]?.done).map((r) => r.id);
console.log(ok.join(','));
if (missing.length) console.error('missing:', missing.join(','));
