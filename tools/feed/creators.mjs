import { readFileSync } from 'node:fs';
const j = JSON.parse(readFileSync(new URL('../../seed/feed/items.json', import.meta.url), 'utf8'));
const c = {}; for (const i of j) c[i.creator] = (c[i.creator] || 0) + 1;
console.log(Object.entries(c).filter(([k]) => k.length > 40 || /unknown|www|\//i.test(k) || !k).map(([k, n]) => `${n} ${k}`).join('\n'));
console.log('licences', JSON.stringify(j.reduce((a, i) => ((a[i.licence] = (a[i.licence] || 0) + 1), a), {})));
const src = {}; for (const i of j) { const h = new URL(i.source_url).hostname; src[h] = (src[h] || 0) + 1; } console.log('sources', JSON.stringify(src));
