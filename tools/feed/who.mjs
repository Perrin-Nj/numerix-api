import { readFileSync } from 'node:fs';
const j = JSON.parse(readFileSync(new URL('../../seed/feed/items.json', import.meta.url), 'utf8'));
for (const i of j) if (/AI-generated|gemini|Ace of Spades|Lorie|AnonMoos/i.test(i.creator)) console.log(i.id, i.kind, i.creator, i.source_url);
