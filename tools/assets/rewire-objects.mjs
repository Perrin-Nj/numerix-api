#!/usr/bin/env node
// Points every question's role "object" media at the real-object photo that matches (instead of the 3D emoji sprite),
// and gives counting questions without an object photo one when their text names an object we have.
// Usage: node tools/assets/rewire-objects.mjs [--dry-run]. Idempotent. Reads tools/assets/objects-map.json, seed/assets/sprites.json.
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..');
const DRY = process.argv.includes('--dry-run');
const read = (p) => JSON.parse(fs.readFileSync(path.join(ROOT, p), 'utf8'));
const map = read('tools/assets/objects-map.json');
const sprites = read('tools/assets/sprites-legacy.json');
const ALIAS = { 'dollar-banknote': 'money-bag' };
const slugOfSprite = Object.fromEntries(sprites.map((s) => [s.id, s.tags.find((t) => t.facet === 'object')?.value]));
const newId = (slug) => (map[ALIAS[slug] ?? slug] ?? {}).id;

// keyword -> slug, from the French and English names of each object (plural by trailing s or x)
const names = fs.readFileSync(path.join(ROOT, 'tools/assets/objects.mjs'), 'utf8').split('const LIST = `')[1].split('`')[0].split('\n').map((l) => l.split('|'));
const norm = (s) => s.toLowerCase().normalize('NFD').replace(/[̀-ͯ]/g, '');
const KW = [];
const SKIP = new Set(['sun', 'rainbow', 'cloud-with-rain', 'ringed-planet', 'marble', 'star', 'tennis', 'compass', 'leaf', 'shell']);
for (const [slug, fr, en] of names) {
  if (!newId(slug) || SKIP.has(slug)) continue;
  for (const n of [fr, en]) {
    const w = norm(n).replace(/^(un |une |le |la |les |l')/, '');
    if (w.length > 3) KW.push([new RegExp(`\\b${w.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}(s|x)?\\b`), slug]);
  }
}

const scenes = {};
for (const w of fs.readdirSync(path.join(ROOT, 'seed', 'assets')).filter((x) => x.endsWith('.json'))) {
  const stem = w.replace('.json', '');
  if (!fs.existsSync(path.join(ROOT, 'seed', 'questions', w))) continue;
  const list = read(`seed/assets/${w}`);
  if (Array.isArray(list)) scenes[stem] = list.filter((a) => a.role === 'scene' && a.review_status === 'approved').map((a) => a.id);
}
let swapped = 0, dropped = 0, added = 0, files = 0, sceneAdded = 0;
const dir = path.join(ROOT, 'seed', 'questions');
for (const f of fs.readdirSync(dir).filter((x) => x.endsWith('.json') && !x.startsWith('_'))) {
  const raw = fs.readFileSync(path.join(dir, f), 'utf8');
  const data = JSON.parse(raw);
  const list = Array.isArray(data) ? data : data.questions;
  let changed = false;
  for (const q of list) {
    const media = q.media ?? [];
    const out = [];
    for (const m of media) {
      if (m.role !== 'object') { out.push(m); continue; }
      const slug = slugOfSprite[m.asset];
      if (!slug) { out.push(m); continue; } // already a new object photo
      const id = newId(slug);
      if (id) { out.push({ ...m, asset: id }); swapped++; } else dropped++;
      changed = true;
    }
    const counting = (q.type === 'numeric' || q.type === 'word') && /\d/.test(`${q.math_text ?? ''} ${q.prompt?.fr ?? ''}`);
    if (counting && !out.some((m) => m.role === 'object')) {
      const text = norm(`${q.prompt?.fr ?? ''} ${q.prompt?.en ?? ''}`);
      const hit = KW.find(([rx]) => rx.test(text));
      if (hit) { out.push({ role: 'object', asset: newId(hit[1]) }); added++; changed = true; }
    }
    // a question without any picture gets a real scene photo of its world (same quest, same photo)
    if (!out.some((m) => m.role === 'scene')) {
      const pool = scenes[f.replace('.json', '')] ?? [];
      if (pool.length) { let h = 0; for (const ch of q.quest ?? q.id) h = (h * 31 + ch.charCodeAt(0)) >>> 0; out.unshift({ role: 'scene', asset: pool[h % pool.length] }); sceneAdded++; changed = true; }
    }
    if (out.length) q.media = out; else delete q.media;
  }
  if (changed) { files++; if (!DRY) fs.writeFileSync(path.join(dir, f), JSON.stringify(data, null, 1) + '\n'); }
}
console.log({ swapped, dropped, added, sceneAdded, files });
