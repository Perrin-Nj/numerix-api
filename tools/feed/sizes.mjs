import { readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';
const dir = new URL('../../seed/feed-media/', import.meta.url);
const j = JSON.parse(readFileSync(new URL('../../seed/feed/items.json', import.meta.url), 'utf8'));
const sz = (f) => statSync(join(dir.pathname.replace(/^\/([A-Za-z]:)/, '$1'), f)).size;
for (const k of ['photo', 'animation', 'video']) {
  const its = j.filter((i) => i.kind === k);
  const main = its.map((i) => sz(i.src)); const all = its.flatMap((i) => [i.src, i.src_small, i.poster, ...(i.captions || []).map((c) => c.src)].filter(Boolean).map(sz));
  console.log(k, its.length, 'main KB min/avg/max', Math.min(...main) >> 10, (main.reduce((a, b) => a + b, 0) / main.length) >> 10, Math.max(...main) >> 10, 'total MB', (all.reduce((a, b) => a + b, 0) / 1048576).toFixed(1));
}
