import { readFileSync } from 'node:fs';
const j = JSON.parse(readFileSync(new URL('../../seed/feed/items.json', import.meta.url), 'utf8'));
for (const id of process.argv.slice(2)) { const i = j.find((x) => x.id === id); console.log(id, '|', i.creator, '|', i.licence, '|', i.attribution, '|', i.source_url); }
