// node probe.mjs <filetype> <query...>  : list licence-ok candidates (title, dur, size, licence)
import { search } from './commons.mjs';
const [ft, ...q] = process.argv.slice(2);
const r = await search(q.join(' '), { filetype: ft, limit: 25 });
for (const c of r) {
  if (!c.ok) continue;
  console.log(`${c.title.slice(5, 70).padEnd(66)} ${String(c.dur ? c.dur.toFixed(0) : '').padStart(4)}s ${String((c.size / 1024) | 0).padStart(7)}KB ${c.w}x${c.h} ${c.licence}`);
}
