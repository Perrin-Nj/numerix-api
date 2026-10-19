#!/usr/bin/env node
// Real-object photos for counting and measuring (replaces the Fluent 3D emoji sprites).
// Usage: node tools/assets/objects.mjs [--only slug,slug] [--dry-run]
// Source: Wikimedia Commons only (CC0, public domain, CC BY; SA/NC/ND/GFDL rejected). No people, brands or text.
// A plain bright background (border pixels all alike) is flood-filled to transparent and the object is centred on a
// square with 8% padding. Any other photo becomes a plain square crop (the app draws it as a soft rounded tile).
// Output: seed/assets/files/<id>-{256,512}.webp, seed/assets/objects.json, tools/assets/objects-map.json.
import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import { fileURLToPath } from 'node:url';
import sharp from 'sharp';

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..');
const SEED = path.join(ROOT, 'seed', 'assets');
const FILES = path.join(SEED, 'files');
const CACHE = path.join(ROOT, 'tools', 'assets', '.cache');
const UA = 'NumerixAssetBot/1.0 (contact: wandjiperrin@gmail.com) node-fetch';
const args = process.argv.slice(2);
const ONLY = args.includes('--only') ? args[args.indexOf('--only') + 1].split(',') : null;
const DRY = args.includes('--dry-run');

// slug|fr|en|search terms (; separated)
const LIST = `red-apple|pomme|apple|apple
banana|banane|banana|banana
strawberry|fraise|strawberry|strawberry
cherries|cerises|cherries|cherries
grapes|raisins|grapes|grapes
lemon|citron|lemon|lemon
tangerine|clémentine|tangerine|tangerine;mandarin orange
watermelon|melon d'eau|watermelon|watermelon
carrot|carotte|carrot|carrot
cookie|biscuit|cookie|cookie;biscuit
cupcake|petit gâteau|cupcake|cupcake
croissant|croissant|croissant|croissant
pancakes|crêpes|pancakes|pancakes
owl|hibou|owl|owl
rabbit|lapin|rabbit|rabbit
cow|vache|cow|cow
turtle|tortue|turtle|turtle
whale|baleine|whale|humpback whale
dolphin|dauphin|dolphin|dolphin
crab|crabe|crab|crab
tropical-fish|poisson tropical|tropical fish|clownfish;tropical fish
butterfly|papillon|butterfly|butterfly
honeybee|abeille|honeybee|honey bee
snail|escargot|snail|snail
horse|cheval|horse|horse
pig|cochon|pig|pig
t-rex|tyrannosaure|Tyrannosaurus skeleton|Tyrannosaurus rex skeleton
sauropod|sauropode|sauropod skeleton|Diplodocus skeleton
coin|pièce de monnaie|coin|coin
money-bag|sac d'argent|pile of coins|coins pile
hammer|marteau|hammer|hammer
wrench|clé à molette|wrench|wrench;spanner
straight-ruler|règle|ruler|ruler
scissors|ciseaux|scissors|scissors
pencil|crayon|pencil|pencil
star|étoile de mer|starfish|starfish
soccer-ball|ballon de soccer|soccer ball|football ball;soccer ball
tennis|balle de tennis|tennis ball|tennis ball
volleyball|ballon de volleyball|volleyball|volleyball ball
ice-hockey|rondelle de hockey|hockey puck|hockey puck
drum|tambour|drum|snare drum
guitar|guitare|guitar|acoustic guitar
violin|violon|violin|violin
trumpet|trompette|trumpet|trumpet
musical-keyboard|clavier de piano|piano keyboard|piano keys
sun|soleil|sun|sun sky
cloud-with-rain|nuage de pluie|rain cloud|rain cloud
snowflake|flocon de neige|snowflake|snowflake macro
umbrella|parapluie|umbrella|umbrella
thermometer|thermomètre|thermometer|thermometer
rainbow|arc-en-ciel|rainbow|rainbow
alarm-clock|réveil|alarm clock|alarm clock
game-die|dé|die|dice
puzzle-piece|pièce de casse-tête|puzzle piece|jigsaw puzzle piece
chess-pawn|pion d'échecs|chess pawn|chess pawn
robot|robot|robot|robot
artist-palette|palette de peintre|paint palette|paint palette
parrot|perroquet|parrot|parrot
rocket|fusée|rocket|rocket launch
ringed-planet|planète à anneaux|Saturn|Saturn planet
bus|autobus|bus|bus
sailboat|voilier|sailboat|sailboat
shell|coquillage|seashell|seashell
pebble|caillou|pebble|pebble
acorn|gland|acorn|acorn
pinecone|pomme de pin|pine cone|pine cone
orange|orange|orange|orange fruit
peach|pêche|peach|peach fruit
tomato|tomate|tomato|tomato
onion|oignon|onion|onion
mushroom|champignon|mushroom|mushroom
corn|maïs|corn cob|corn cob
bell-pepper|poivron|bell pepper|bell pepper
rose|rose|rose|rose flower
ladybug|coccinelle|ladybug|ladybug
duck|canard|duck|duck
frog|grenouille|frog|frog
penguin|manchot|penguin|penguin
dog|chien|dog|dog
candle|chandelle|candle|candle
cherry-tomato|tomate cerise|cherry tomato|cherry tomato
pineapple|ananas|pineapple|pineapple
blueberry|bleuet|blueberries|blueberries
peas|pois|peas|peas pod`
  .split('\n').map((l) => { const [slug, fr, en, q] = l.split('|'); return { slug, fr, en, qs: q.split(';') }; });
const qOver = JSON.parse(fs.readFileSync(path.join(ROOT, 'tools', 'assets', 'objects-queries.json'), 'utf8'));
for (const o of LIST) if (qOver[o.slug]) o.qs = qOver[o.slug].split(';');
const rejPath = path.join(ROOT, 'tools', 'assets', 'objects-reject.json');
const rejects = fs.existsSync(rejPath) ? JSON.parse(fs.readFileSync(rejPath, 'utf8')) : {};
const REDO = args.includes('--redo') ? args[args.indexOf('--redo') + 1].split(',') : [];

const PEOPLE = /\b(man|men|woman|women|boy|girl|child|children|kid|baby|people|person|portrait|family|crowd|face|selfie|couple|student|player|fan|wedding|bride|swimmer|runner|athlete|team|audience|tourist|mother|father|lady|guy|teen|worker|model|president|officer|soldier|sailor|pilot|crew|staff|doctor|nurse|teacher|dancer|singer|musician|skater|spectators|performance|concert|stage|actor|actress|girls|boys|holding|hand|hands|finger|fingers)\b/i;
const BRAND = /\b(logo|trademark|nike|adidas|coca|pepsi|disney|lego|nintendo|mcdonald|starbucks|iphone|google|facebook|samsung|tesla|ferrari|toyota|bmw|nasa logo|pixar|nikon|canon|sony|ikea|ford|honda|heineken|budweiser|apple inc|macintosh|ipod|ipad|imac|steinway|yamaha|fender|gibson|wilson|spalding|nba|fifa|stamp|poster|advert|label|brand|boeing|airbus|bombardier)\b/i;
const UNSAFE = /\b(gun|rifle|weapon|sword|knife|blood|skull|dead|death|war|military|bomb|cigarette|beer|wine|alcohol|nude|naked|sexy|corpse|zombie|fighter|bomber|warship|funeral|cemetery|prison|gambling|casino|poker|dissect|cooked|raw meat|slaughter|hunting|taxidermy|roadkill|carcass|stuffed)\b/i;
const ART = /\b(print|engraving|etching|drawing|illustration|painting|painted|lithograph|plate|iconographia|sculpture|statue|statuette|figurine|mosaic|relief|vase|tapestry|cape|sampler|embroider\w*|textile|sketch|poster|diagram|map|symbolic|parametric|render\w*|3d|cartoon|clipart|vector|svg|icon|museum|am \d+|inv|collection|cabinet|canyon|bridge|river|rapids|v-2|missile|rocket engine|launch pad|coat of arms|emblem|medal|token|tile|ceramic|pottery|porcelain|jewelry|jewellery|necklace|brooch|earring|silhouette|logo|flag|fresco|manuscript|book|page|stamp|postcard|album|cover)\b/i;
const EXTRA = /(moonmap|mesh|radiograph|diagram|toxocara|parasite|alice|concept|future|cgi|faceplate|vega|spectrum|farm|building|barn|cooler|medieval|penny|blue marble|earth|pressure|zero|street|city|stadium|match|club|harbor|harbour|skyline|crowd|landscape|pendant|dagger|jug|machine|accretion|hitch|knot|banknote|heraldry|store|for sale|market|roman|ancient|tomb|burial|coin of|MET DP|stuffed|trout|duster|cutting|figure|collection|shelf|shelves|window)/i;
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const sha = (b, n = 20) => crypto.createHash('sha256').update(b).digest('hex').slice(0, n);
fs.mkdirSync(path.join(CACHE, 'obj'), { recursive: true });
let lastReq = 0;
async function get(url, json) {
  const ck = path.join(CACHE, 'obj', crypto.createHash('sha1').update(url).digest('hex') + (json ? '.json' : '.bin'));
  if (fs.existsSync(ck)) return json ? JSON.parse(fs.readFileSync(ck, 'utf8')) : fs.readFileSync(ck);
  for (let i = 0; i < 5; i++) {
    const w = lastReq + 1100 - Date.now();
    if (w > 0) await sleep(w);
    lastReq = Date.now();
    const res = await fetch(url, { headers: { 'User-Agent': UA } });
    if (res.status === 429 || res.status >= 500) { await sleep(2 ** i * 4000); continue; }
    if (!res.ok) throw new Error(`${res.status} ${url}`);
    const out = json ? await res.json() : Buffer.from(await res.arrayBuffer());
    fs.writeFileSync(ck, json ? JSON.stringify(out) : out);
    return out;
  }
  throw new Error('retries ' + url);
}
const strip = (s) => String(s || '').replace(/<[^>]*>/g, '').replace(/&amp;/g, '&').replace(/&quot;/g, '"').replace(/&#039;/g, "'").replace(/\s+/g, ' ').trim();
const LIC_URL = {
  CC0: 'https://creativecommons.org/publicdomain/zero/1.0/', PDM: 'https://creativecommons.org/publicdomain/mark/1.0/',
  'CC-BY-2.0': 'https://creativecommons.org/licenses/by/2.0/', 'CC-BY-3.0': 'https://creativecommons.org/licenses/by/3.0/', 'CC-BY-4.0': 'https://creativecommons.org/licenses/by/4.0/',
};

async function commons(q, need) {
  const u = 'https://commons.wikimedia.org/w/api.php?action=query&format=json&generator=search&gsrnamespace=6&gsrlimit=30' +
    `&gsrsearch=${encodeURIComponent(q + ' filetype:bitmap')}&prop=imageinfo&iiprop=url|size|mime|extmetadata&iiurlwidth=800`;
  const j = await get(u, true);
  const out = [];
  for (const p of Object.values(j.query?.pages || {})) {
    const ii = p.imageinfo?.[0];
    if (!ii || !/^image\/(jpeg|png)$/.test(ii.mime) || (ii.thumbwidth || 0) < 500 || ii.width < 600) continue;
    const m = ii.extmetadata || {};
    const short = strip(m.LicenseShortName?.value);
    if (/\b(SA|NC|ND)\b|-SA|-NC|-ND|GFDL|fair use| or /i.test(short + ' ' + strip(m.UsageTerms?.value))) continue;
    let lic = '';
    if (/^CC0/i.test(short)) lic = 'CC0'; else if (/^(public domain|pd)/i.test(short)) lic = 'PDM';
    else { const mm = short.match(/^CC BY (\d\.\d)$/i); if (mm && ['2.0', '3.0', '4.0'].includes(mm[1])) lic = `CC-BY-${mm[1]}`; }
    if (!lic) continue;
    const creator = strip(m.Artist?.value) || strip(m.Credit?.value);
    const title = p.title.replace(/^File:/, '').replace(/\.\w+$/, '').replace(/_/g, ' ');
    const text = title + ' ' + strip(m.ImageDescription?.value).slice(0, 200) + ' ' + strip(m.Categories?.value);
    if (PEOPLE.test(text) || BRAND.test(text) || UNSAFE.test(text) || ART.test(title) || EXTRA.test(title)) continue;
    if (lic.startsWith('CC-BY') && !creator) continue;
    if (need && !need.test(title)) continue;
    out.push({ sid: String(p.pageid), url: ii.thumburl || ii.url, title, text, lic, creator: creator.slice(0, 120) || 'Unknown', src: ii.descriptionurl });
  }
  return out;
}

// {data RGBA raw, w, h, cut}: cut=true when the plain bright border was removed
async function cutout(raw) {
  const { data, info } = await sharp(raw, { failOn: 'none' }).rotate().resize({ width: 640, height: 640, fit: 'inside' }).ensureAlpha().raw().toBuffer({ resolveWithObject: true });
  const { width: w, height: h } = info;
  const px = (x, y) => (y * w + x) * 4;
  const border = [];
  for (let x = 0; x < w; x++) border.push(px(x, 0), px(x, h - 1));
  for (let y = 0; y < h; y++) border.push(px(0, y), px(w - 1, y));
  const med = [0, 1, 2].map((c) => border.map((i) => data[i + c]).sort((a, b) => a - b)[border.length >> 1]);
  const dist = (i) => Math.hypot(data[i] - med[0], data[i + 1] - med[1], data[i + 2] - med[2]);
  const plain = border.filter((i) => dist(i) < 30).length / border.length;
  if (plain < 0.9 || !med.every((v) => v > 185)) return { data, w, h, cut: false };
  const seen = new Uint8Array(w * h);
  const stack = [];
  const push = (x, y) => { const k = y * w + x; if (!seen[k] && dist(k * 4) < 42) { seen[k] = 1; stack.push(k); } };
  for (let x = 0; x < w; x++) { push(x, 0); push(x, h - 1); }
  for (let y = 0; y < h; y++) { push(0, y); push(w - 1, y); }
  while (stack.length) {
    const k = stack.pop(), x = k % w, y = (k / w) | 0;
    if (x > 0) push(x - 1, y);
    if (x < w - 1) push(x + 1, y);
    if (y > 0) push(x, y - 1);
    if (y < h - 1) push(x, y + 1);
  }
  let bg = 0;
  for (let k = 0; k < seen.length; k++) if (seen[k]) bg++;
  const frac = bg / seen.length;
  if (frac < 0.25 || frac > 0.9) return { data, w, h, cut: false };
  const out = Buffer.from(data);
  for (let k = 0; k < seen.length; k++) out[k * 4 + 3] = seen[k] ? 0 : 255;
  const alpha = await sharp(out, { raw: { width: w, height: h, channels: 4 } }).extractChannel(3).blur(0.8).raw().toBuffer();
  for (let k = 0; k < seen.length; k++) out[k * 4 + 3] = alpha[k];
  return { data: out, w, h, cut: true };
}

const tileOf = (raw) => sharp(raw, { failOn: 'none' }).rotate().resize(512, 512, { fit: 'cover', position: sharp.strategy.attention });
async function render(raw) {
  const c = await cutout(raw);
  if (!c.cut) return { cut: false, img: tileOf(raw) };
  const t = await sharp(c.data, { raw: { width: c.w, height: c.h, channels: 4 } }).trim({ background: { r: 0, g: 0, b: 0, alpha: 0 }, threshold: 8 }).toBuffer({ resolveWithObject: true }).catch(() => null);
  if (!t) return { cut: false, img: tileOf(raw) };
  const side = Math.max(t.info.width, t.info.height);
  const pad = Math.round(side * 0.08);
  const dx = side - t.info.width, dy = side - t.info.height;
  const img = sharp(t.data, { raw: { width: t.info.width, height: t.info.height, channels: t.info.channels } })
    .extend({ top: (dy >> 1) + pad, bottom: dy - (dy >> 1) + pad, left: (dx >> 1) + pad, right: dx - (dx >> 1) + pad, background: { r: 0, g: 0, b: 0, alpha: 0 } })
    .resize(512, 512, { fit: 'contain', background: { r: 0, g: 0, b: 0, alpha: 0 } });
  return { cut: true, img };
}

function score(c, o, cut) {
  const t = c.text.toLowerCase();
  const word = o.en.toLowerCase().split(' ').pop();
  let s = t.includes(word) ? 3 : 0;
  if (/white background|isolated|cutout|cut-out|studio|plain background/.test(t)) s += 2;
  if (c.title.toLowerCase().includes(word)) s += 2;
  if (cut) s += 3;
  return s;
}

const needOf = (o) => new RegExp(String.raw`\b(` + [...new Set(o.qs.map((q) => q.split(' ').pop()).concat(o.en.split(' ').pop()))].map((w) => w.replace(/s$/, '')).join('|') + ')', 'i');
const only = ONLY ? LIST.filter((o) => ONLY.includes(o.slug)) : REDO.length ? LIST.filter((o) => REDO.includes(o.slug)) : LIST;
const mapPath = path.join(ROOT, 'tools', 'assets', 'objects-map.json');
const map = fs.existsSync(mapPath) ? JSON.parse(fs.readFileSync(mapPath, 'utf8')) : {};
const spr = JSON.parse(fs.readFileSync(path.join(ROOT, 'tools', 'assets', 'sprites-legacy.json'), 'utf8'));
const spriteBySlug = Object.fromEntries(spr.map((s) => [s.tags.find((t) => t.facet === 'object')?.value, s]));
for (const slug of REDO) { if (map[slug]) { (rejects[slug] ||= []).push(map[slug].sid); delete map[slug]; } }
if (REDO.length) fs.writeFileSync(rejPath, JSON.stringify(rejects, null, 1));
const used = new Set(Object.values(map).map((m) => m.sid));
fs.mkdirSync(FILES, { recursive: true });

for (const o of only) {
  if (map[o.slug]?.id && !ONLY && !REDO.length) continue;
  let cands = [];
  for (const base of o.qs) {
    for (const q of [`${base} white background`, `${base} isolated`, base]) {
      try { cands.push(...(await commons(q, needOf(o)))); } catch (e) { console.warn('search fail', q, e.message); }
      if (cands.length >= 6) break;
    }
    if (cands.length >= 6) break;
  }
  const seen = new Set();
  cands = cands.filter((c) => !seen.has(c.sid) && !used.has(c.sid) && !(rejects[o.slug] || []).includes(c.sid) && seen.add(c.sid));
  cands.sort((a, b) => score(b, o, false) - score(a, o, false));
  let best = null;
  for (const c of cands.slice(0, 8)) {
    try {
      const r = await render(await get(c.url, false));
      const s = score(c, o, r.cut);
      if (!best || s > best.s) best = { c, r, s };
      if (r.cut && s >= 8) break;
    } catch (e) { console.warn('  img fail', c.title, e.message); }
  }
  if (!best) { console.log('NONE', o.slug); continue; }
  const { c, r } = best;
  const b512 = await r.img.clone().webp({ quality: 82, effort: 5, alphaQuality: 90 }).toBuffer();
  const b256 = await sharp(b512).resize(256, 256).webp({ quality: 82, effort: 5, alphaQuality: 90 }).toBuffer();
  const id = 'a_' + sha(b512);
  const lq = 'data:image/webp;base64,' + (await sharp(b512).resize(24, 24).webp({ quality: 40 }).toBuffer()).toString('base64');
  const st = await sharp(b256).flatten({ background: '#ffffff' }).stats();
  const dom = '#' + [st.dominant.r, st.dominant.g, st.dominant.b].map((v) => v.toString(16).padStart(2, '0')).join('');
  if (!DRY) {
    fs.writeFileSync(path.join(FILES, `${id}-512.webp`), b512);
    fs.writeFileSync(path.join(FILES, `${id}-256.webp`), b256);
  }
  used.add(c.sid);
  map[o.slug] = { id, sid: c.sid, title: c.title, cut: r.cut, lic: c.lic, creator: c.creator, src: c.src, size: [b256.length, b512.length], dom, lq };
  console.log(o.slug, r.cut ? 'CUT' : 'tile', c.lic, c.title.slice(0, 50));
  if (!DRY) fs.writeFileSync(mapPath, JSON.stringify(map, null, 1));
}

// ---- seed list ----
const docs = [];
for (const o of LIST) {
  const m = map[o.slug];
  if (!m) continue;
  const sp = spriteBySlug[o.slug];
  const tags = sp ? sp.tags.filter((t) => t.facet !== 'mood') : [{ facet: 'world', value: 'nature' }, { facet: 'topic', value: 'nature' }, { facet: 'object', value: o.slug }];
  const lic = m.lic;
  const text = `"${m.title}" by ${m.creator}, ${lic === 'CC0' ? 'CC0' : lic === 'PDM' ? 'public domain' : lic.replace('CC-BY-', 'CC BY ')}, via Wikimedia Commons (${m.src})`;
  docs.push({
    id: m.id, kind: 'photo', role: 'object', world_id: sp ? sp.world_id : 'nature', grade_band: 'all', mood: 'joyful', dominant_color: m.dom, lqip: m.lq,
    width: 512, height: 512, alt_fr: `Photo : ${o.fr}`, alt_en: `Photo: ${o.en}`,
    source_name: 'wikimedia', source_url: m.src, creator: m.creator, creator_url: m.src, licence: lic, licence_url: LIC_URL[lic],
    attribution_text: text, attribution_required: lic.startsWith('CC-BY') ? 1 : 0, people_free: 1, brand_free: 1, review_status: 'approved',
    tags, variants: [{ w: 256, file: `${m.id}-256.webp`, size: m.size[0] }, { w: 512, file: `${m.id}-512.webp`, size: m.size[1] }],
  });
}
if (!DRY) fs.writeFileSync(path.join(SEED, 'objects.json'), JSON.stringify(docs, null, 1) + '\n');
console.log('objects', docs.length, 'cut', docs.filter((d) => map[d.tags.find((t) => t.facet === 'object').value]?.cut).length);
