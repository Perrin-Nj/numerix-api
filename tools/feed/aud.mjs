import { readFileSync } from 'node:fs';
const j = JSON.parse(readFileSync(new URL('../../seed/feed/items.json', import.meta.url), 'utf8'));
console.log('audio', j.filter((i) => i.has_audio).map((i) => i.id).join(), 'questions', j.filter((i) => i.fr.question).length);
