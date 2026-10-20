// node vprobe.mjs video|bitmap "q1" "q2" ...  : for each query print up to 6 licence-ok candidates, no people words.
import { search } from './commons.mjs';
const [ft, ...qs] = process.argv.slice(2);
const PEOPLE = /\b(man|men|woman|women|girl|boy|child|children|kid|people|person|portrait|crowd|selfie|baby|team|player|players|tourist|family|student|students|soldier|military|police|interview|speech|president|lecture|talk|conference|concert|dance|dancing|war|bomb|attack|death|dead|blood|hunting|fishing|slaughter|wrestling|fight)\b/i;
for (const q of qs) {
  const r = await search(q, { filetype: ft, limit: 30 });
  console.log('== ' + q);
  let n = 0;
  for (const c of r) {
    if (!c.ok || PEOPLE.test(c.title + ' ' + c.desc)) continue;
    if (ft === 'video' && (c.dur < 4 || c.size > 60e6)) continue;
    console.log(`  ${c.title.slice(5, 78).padEnd(74)} ${String(c.dur ? Math.round(c.dur) : '').padStart(4)}s ${String((c.size / 1024) | 0).padStart(7)}KB ${c.w}x${c.h} ${c.licence}`);
    if (++n >= 6) break;
  }
}
