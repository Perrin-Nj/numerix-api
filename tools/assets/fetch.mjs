#!/usr/bin/env node
// Numerix asset acquisition. Node 20+, dep: sharp. See docs/ASSETS.md.
// Usage: node tools/assets/fetch.mjs [--dry-run] [--only world] [--photos-only|--sprites-only]
// Idempotent: picks are pinned in seed/assets/picks.json; a re-run only fetches what is not pinned.
// With PEXELS_API_KEY set, slots pinned to a non-Pexels photo are re-searched on Pexels and upgraded.
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
const DRY = args.includes('--dry-run');
const ONLY = args.includes('--only') ? args[args.indexOf('--only') + 1] : null;
const KEY = process.env.PEXELS_API_KEY || '';
const FLUENT_RAW = 'https://raw.githubusercontent.com/microsoft/fluentui-emoji/main/assets';
const FLUENT_LICENCE = 'https://raw.githubusercontent.com/microsoft/fluentui-emoji/main/LICENSE';

const PEOPLE = /\b(man|men|woman|women|boy|boys|girl|girls|child|children|kid|kids|baby|babies|people|person|portrait|family|crowd|face|faces|selfie|couple|student|students|player|players|fan|fans|wedding|bride|swimmer|runner|athlete|team|audience|tourist|tourists|visitor|visitors|mother|father|lady|guy|teen|worker|workers|model|trainer|president|obama|officer|soldier|soldiers|airman|navy|sailor|pilot|crew|staff|mayor|doctor|nurse|teacher|dancer|singer|musician|musicians|skater|skaters|spectators|trump|biden|clinton|pianist|conductor|concerto|performing|performance|concert|stage|performer|cast|actor|actress|play)\b/i;
const BRAND = /\b(logo|trademark|nike|adidas|coca|pepsi|disney|lego|nintendo|mcdonald|starbucks|apple inc|iphone|google|facebook|samsung|tesla|ferrari|toyota|bmw|nasa logo|wall-e|pixar|nikon|canon|sony|dynacord|ikea|converse|ford|chevrolet|honda|heineken|budweiser|marlboro)\b/i;
const UNSAFE = /\b(gun|rifle|weapon|sword|knife|blood|skull|dead|death|war|military|bomb|cigarette|beer|wine|alcohol|nude|naked|sexy|bikini|corpse|zombie|shark attack|eod|uav|suas|military|combat|troops|missile|tank|grenade|fighter|bomber|warship|battleship|funeral|cemetery|prison|gambling|casino|poker|betting|lottery)\b/i;
const OK_LIC = new Set(['CC0', 'PDM', 'CC-BY-2.0', 'CC-BY-3.0', 'CC-BY-4.0', 'PEXELS']);
const LIC_URL = {
  CC0: 'https://creativecommons.org/publicdomain/zero/1.0/',
  PDM: 'https://creativecommons.org/publicdomain/mark/1.0/',
  'CC-BY-2.0': 'https://creativecommons.org/licenses/by/2.0/',
  'CC-BY-3.0': 'https://creativecommons.org/licenses/by/3.0/',
  'CC-BY-4.0': 'https://creativecommons.org/licenses/by/4.0/',
  PEXELS: 'https://www.pexels.com/license/',
  MIT: FLUENT_LICENCE,
};

const readJson = (p, d) => (fs.existsSync(p) ? JSON.parse(fs.readFileSync(p, 'utf8')) : d);
const writeJson = (p, o) => {
  const s = JSON.stringify(o, null, 1) + '\n';
  if (!fs.existsSync(p) || fs.readFileSync(p, 'utf8') !== s) fs.writeFileSync(p, s);
};
const sha = (b, n = 40, alg = 'sha256') => crypto.createHash(alg).update(b).digest('hex').slice(0, n);
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

fs.mkdirSync(FILES, { recursive: true });
fs.mkdirSync(path.join(CACHE, 'json'), { recursive: true });
fs.mkdirSync(path.join(CACHE, 'img'), { recursive: true });

const manifest = readJson(path.join(ROOT, 'tools', 'assets', 'manifest.json'));
const picksPath = path.join(SEED, 'picks.json');
const picks = readJson(picksPath, { photos: {}, sprites: {} });
const rejected = readJson(path.join(SEED, 'rejected.json'), {}); // "source:sid" -> reason
const reviews = readJson(path.join(ROOT, 'tools', 'assets', 'reviews.json'), {}); // assetId -> {status, alt_fr, alt_en, people_free}

// ---- http with throttle, backoff, disk cache ----
const last = {};
const gaps = { openverse: 3600, commons: 1100, pexels: 2000, other: 1500 };
const exhausted = new Set();
async function http(url, { host = 'other', headers = {}, json = true, cache = true } = {}) {
  const ck = path.join(CACHE, json ? 'json' : 'img', sha(url, 24, 'sha1') + (json ? '.json' : '.bin'));
  if (cache && fs.existsSync(ck)) return json ? JSON.parse(fs.readFileSync(ck, 'utf8')) : fs.readFileSync(ck);
  for (let i = 0; i < 4; i++) {
    const wait = (last[host] || 0) + gaps[host] - Date.now();
    if (wait > 0) await sleep(wait);
    last[host] = Date.now();
    const res = await fetch(url, { headers: { 'User-Agent': url.includes('staticflickr') ? 'Numerix-asset-fetch/1.0' : UA, ...headers } });
    if (host === 'openverse') {
      const left = Number(res.headers.get('x-ratelimit-available-anon_sustained'));
      if (res.headers.has('x-ratelimit-available-anon_sustained') && left < 4) exhausted.add('openverse');
    }
    if (res.status === 429 || res.status >= 500) {
      if (host === 'openverse' && res.status === 429 && i > 0) { exhausted.add('openverse'); throw new Error('openverse quota'); }
      await sleep(2 ** i * 4000);
      continue;
    }
    if (!res.ok) throw new Error(`${res.status} ${url}`);
    const out = json ? await res.json() : Buffer.from(await res.arrayBuffer());
    if (cache) fs.writeFileSync(ck, json ? JSON.stringify(out) : out);
    return out;
  }
  throw new Error('retries exhausted ' + url);
}

// ---- providers: each returns normalised candidates ----
const stripHtml = (s) => String(s || '').replace(/<[^>]*>/g, '').replace(/&amp;/g, '&').replace(/&quot;/g, '"').replace(/&#039;/g, "'").replace(/\s+/g, ' ').trim();

async function openverse(q) {
  if (exhausted.has('openverse')) return [];
  const u = `https://api.openverse.org/v1/images/?q=${encodeURIComponent(q)}&license=cc0,pdm,by&category=photograph&extension=jpg&aspect_ratio=wide&mature=false&page_size=20`;
  const j = await http(u, { host: 'openverse' });
  return (j.results || []).map((r) => {
    const lic = r.license === 'by' ? `CC-BY-${r.license_version}` : r.license === 'cc0' ? 'CC0' : r.license === 'pdm' ? 'PDM' : '';
    return {
      source: 'openverse', sid: r.id, url: r.url, title: r.title || '', tags: (r.tags || []).map((t) => t.name),
      width: r.width, height: r.height, creator: r.creator || 'Unknown', creator_url: r.creator_url || null,
      licence: lic, licence_url: r.license_url, source_url: r.foreign_landing_url,
      attribution_text: r.attribution || '', mature: r.mature || (r.unstable__sensitivity || []).length > 0,
      provider: r.provider,
    };
  });
}

async function commons(q) {
  const u = 'https://commons.wikimedia.org/w/api.php?action=query&format=json&generator=search&gsrnamespace=6&gsrlimit=20' +
    `&gsrsearch=${encodeURIComponent(q + ' filetype:bitmap')}&prop=imageinfo&iiprop=url|size|mime|extmetadata&iiurlwidth=1600`;
  const j = await http(u, { host: 'commons' });
  const out = [];
  for (const p of Object.values(j.query?.pages || {})) {
    const ii = p.imageinfo?.[0];
    if (!ii || ii.mime !== 'image/jpeg') continue;
    const m = ii.extmetadata || {};
    const short = (m.LicenseShortName?.value || '').trim();
    const terms = stripHtml(m.UsageTerms?.value);
    if (/\b(SA|NC|ND)\b|-SA|-NC|-ND|GFDL|fair use| or /i.test(short + ' ' + terms)) continue;
    let lic = '';
    if (/^CC0/i.test(short)) lic = 'CC0';
    else if (/^(public domain|pd)/i.test(short)) lic = 'PDM';
    else { const mm = short.match(/^CC BY (\d\.\d)$/i); if (mm) lic = `CC-BY-${mm[1]}`; }
    if (!lic) continue;
    const creator = stripHtml(m.Artist?.value) || stripHtml(m.Credit?.value);
    const title = p.title.replace(/^File:/, '').replace(/\.\w+$/, '').replace(/_/g, ' ');
    const src = ii.descriptionurl;
    out.push({
      source: 'wikimedia', sid: String(p.pageid), url: ii.thumburl || ii.url, title: title + ' ' + stripHtml(m.ImageDescription?.value).slice(0, 160),
      tags: stripHtml(m.Categories?.value).split('|'), width: ii.thumbwidth || ii.width, height: ii.thumbheight || ii.height,
      creator: creator.slice(0, 120) || 'Unknown', creator_url: null, licence: lic, licence_url: LIC_URL[lic], source_url: src,
      attribution_text: `"${title}" by ${creator.slice(0, 120) || 'Unknown'}, ${lic === 'CC0' ? 'CC0' : lic === 'PDM' ? 'public domain' : lic.replace('CC-BY-', 'CC BY ')}, via Wikimedia Commons (${src})`,
      mature: false, provider: 'wikimedia',
    });
  }
  return out;
}

async function pexels(q) {
  const j = await http(`https://api.pexels.com/v1/search?query=${encodeURIComponent(q)}&orientation=landscape&size=large&per_page=20`, { host: 'pexels', headers: { Authorization: KEY } });
  return (j.photos || []).map((p) => ({
    source: 'pexels', sid: String(p.id), url: p.src.large2x || p.src.original, title: p.alt || '', tags: [],
    width: p.width, height: p.height, creator: p.photographer, creator_url: p.photographer_url, licence: 'PEXELS',
    licence_url: LIC_URL.PEXELS, source_url: p.url, attribution_text: `Photo by ${p.photographer} on Pexels`, mature: false, provider: 'pexels', avg: p.avg_color,
  }));
}

// ---- selection ----
const parseSlot = (s) => { const [q, topic, objects, mood] = s.split('|'); return { qs: q.split(';'), topic, objects: objects.split(','), mood }; };
function score(c, qs) {
  const text = (c.title + ' ' + c.tags.join(' ')).toLowerCase();
  const toks = [...new Set(qs.flatMap((q) => q.replace(/^commons:/, '').toLowerCase().split(/\s+/)).filter((t) => t.length > 3))];
  const hit = toks.filter((t) => text.includes(t)).length;
  const first = qs[0].replace(/^commons:/, '').toLowerCase().split(/\s+/)[0];
  return hit + (text.includes(first) ? 1 : 0) + Math.min(c.width, 2400) / 2400;
}
function usable(c, minW, used) {
  const text = c.title + ' ' + c.tags.join(' ');
  return OK_LIC.has(c.licence) && c.url && c.width >= minW && c.width / c.height >= 1.3 && !c.mature &&
    !PEOPLE.test(text) && !BRAND.test(text) && !UNSAFE.test(text) &&
    !rejected[`${c.source}:${c.sid}`] && !rejected[`title:${c.title}`] && !used.has(`${c.source}:${c.sid}`) && !(c.licence !== 'PEXELS' && c.licence !== 'CC0' && c.licence !== 'PDM' && !c.creator);
}
async function search(slot, minW, used, providers) {
  const found = [];
  for (const q of slot.qs) {
    const forced = q.startsWith('commons:'); // manifest can force a Commons title search
    for (const prov of forced ? [commons] : providers) {
      let list = [];
      try { list = await prov(forced ? q.slice(8) : q); } catch (e) { console.warn('  search fail', prov.name, q, e.message); }
      found.push(...list.filter((c) => usable(c, minW, used)));
    }
    if (found.length >= 3) break;
  }
  return found.sort((a, b) => score(b, slot.qs) - score(a, slot.qs));
}

// ---- image processing ----
const hex = ({ r, g, b }) => '#' + [r, g, b].map((v) => v.toString(16).padStart(2, '0')).join('');
async function processPhoto(c, role) {
  const raw = await http(c.url, { host: 'other', json: false });
  const id = 'a_' + sha(raw, 20);
  const img = sharp(raw, { failOn: 'none' }).rotate();
  const meta = await sharp(await img.clone().toBuffer()).metadata();
  const ratio = role === 'hero' ? 16 / 9 : 4 / 3;
  const widths = (role === 'hero' ? [480, 960, 1600] : [480, 960]).filter((w) => w <= meta.width || w === 480);
  const target = role === 'hero' ? 120000 : 110000;
  const mk = (w, q) => img.clone().resize({ width: w, height: Math.round(w / ratio), fit: 'cover', position: sharp.strategy.attention }).webp({ quality: q, effort: 4 }).toBuffer();
  let q = 80;
  const probe = widths.includes(960) ? 960 : widths[widths.length - 1];
  for (; q > 52; q -= 6) if ((await mk(probe, q)).length <= target) break;
  const variants = [];
  for (const w of widths) {
    const b = await mk(w, q);
    const file = `${id}-${w}.webp`;
    if (!DRY) fs.writeFileSync(path.join(FILES, file), b);
    variants.push({ w, file, size: b.length });
  }
  const small = await mk(480, 80);
  const dom = hex((await sharp(small).stats()).dominant);
  const lq = 'data:image/webp;base64,' + (await sharp(small).resize(24).webp({ quality: 40 }).toBuffer()).toString('base64');
  return { id, width: Math.round(Math.min(meta.width, 1600)), height: Math.round(Math.min(meta.width, 1600) / ratio), dominant_color: c.avg || dom, lqip: lq, variants, quality: q };
}
async function processSprite(name) {
  const file = name.toLowerCase().replace(/ /g, '_') + '_3d.png';
  const url = `${FLUENT_RAW}/${encodeURIComponent(name)}/3D/${encodeURIComponent(file)}`;
  const raw = await http(url, { host: 'other', json: false });
  const id = 'a_' + sha(raw, 20);
  const png = sharp(raw).resize(256, 256, { fit: 'contain', background: { r: 0, g: 0, b: 0, alpha: 0 } });
  const b = await png.clone().webp({ quality: 90, alphaQuality: 100 }).toBuffer();
  const f = `${id}-256.webp`;
  if (!DRY) fs.writeFileSync(path.join(FILES, f), b);
  const st = await sharp(raw).flatten({ background: '#ffffff' }).stats();
  const lq = 'data:image/webp;base64,' + (await png.clone().resize(24, 24).webp({ quality: 40 }).toBuffer()).toString('base64');
  return { id, url, width: 256, height: 256, dominant_color: hex(st.dominant), lqip: lq, variants: [{ w: 256, file: f, size: b.length }] };
}

// ---- licence page verification ----
async function verifyLicences() {
  const urls = [...new Set(Object.values(LIC_URL))];
  for (const u of urls) {
    try { const r = await fetch(u, { headers: { 'User-Agent': UA } }); console.log(`licence page ${r.status} ${u}`); if (!r.ok && u === LIC_URL.PEXELS) exhausted.add('pexels'); }
    catch (e) { console.warn('licence page unreachable', u, e.message); }
  }
}

// ---- main ----
const worlds = Object.keys(manifest.worlds).filter((w) => !ONLY || w === ONLY);
if (DRY) console.log(`DRY RUN. pexels key: ${KEY ? 'yes' : 'no'}. Openverse gap ${gaps.openverse} ms, Commons ${gaps.commons} ms`);
else if (!args.includes('--no-verify')) await verifyLicences();

const used = new Set(Object.values(picks.photos).map((p) => `${p.source}:${p.sid}`));
let planned = 0;
if (!args.includes('--sprites-only')) {
  for (const w of worlds) {
    for (let i = 0; i < manifest.worlds[w].length; i++) {
      const key = `${w}/${i}`;
      const role = i === 0 ? 'hero' : 'scene';
      const slot = parseSlot(manifest.worlds[w][i]);
      const minW = role === 'hero' ? 1024 : 800;
      let cur = picks.photos[key];
      if (cur && rejected[`${cur.source}:${cur.sid}`]) { used.delete(`${cur.source}:${cur.sid}`); delete picks.photos[key]; cur = null; }
      const upgrade = KEY && cur && cur.source !== 'pexels';
      if (cur && !upgrade) continue;
      planned++;
      if (DRY) { console.log(`PLAN ${key} ${upgrade ? 'upgrade->pexels' : 'search'}: ${slot.qs.join(' | ')}`); continue; }
      const provs = upgrade || (KEY && !cur) ? [pexels, ...(cur ? [] : [openverse, commons])] : [openverse, commons];
      const cands = await search(slot, minW, used, provs);
      const pool = upgrade ? cands.filter((c) => c.source === 'pexels') : cands;
      let done = false;
      for (const pick of pool.slice(0, 12)) {
        try {
          const proc = await processPhoto(pick, role);
          if (cur) used.delete(`${cur.source}:${cur.sid}`);
          picks.photos[key] = { ...pick, ...proc, role, world: w, topic: slot.topic, objects: slot.objects, mood: slot.mood, tags: undefined };
          used.add(`${pick.source}:${pick.sid}`);
          console.log(`OK ${key} ${pick.source} ${pick.licence} ${proc.id} q${proc.quality} "${pick.title.slice(0, 50)}"`);
          writeJson(picksPath, picks);
          done = true;
          break;
        } catch (e) { console.warn(`FAIL ${key} ${pick.source}:${pick.sid} ${e.message}`); }
      }
      if (!done) console.warn(`NONE ${key}${cur ? ' (keeping current)' : ''}`);
    }
  }
}
if (!args.includes('--photos-only') && !ONLY) {
  for (const line of manifest.sprites) {
    const [name, fr, en, wl, topic] = line.split('|');
    if (picks.sprites[name]) continue;
    planned++;
    if (DRY) { console.log(`PLAN sprite ${name}`); continue; }
    try { picks.sprites[name] = await processSprite(name); }
    catch (e) { console.warn(`SKIP sprite ${name}: ${e.message}`); }
  }
  if (!DRY) writeJson(picksPath, picks);
}
writeJson(path.join(SEED, 'rejected.json'), rejected);
if (DRY) { console.log(`planned slots/sprites: ${planned}`); process.exit(0); }

// ---- emit seed json ----
const tag = (facet, v) => (Array.isArray(v) ? v : [v]).filter(Boolean).map((value) => ({ facet, value }));
const entries = {};
const keep = new Set();
for (const [key, p] of Object.entries(picks.photos)) {
  const rv = reviews[p.id] || {};
  const lic = p.licence;
  const e = {
    id: p.id, kind: 'photo', role: p.role, world_id: p.world, grade_band: 'all', mood: p.mood, dominant_color: p.dominant_color, lqip: p.lqip,
    width: p.width, height: p.height,
    alt_fr: rv.alt_fr || '', alt_en: rv.alt_en || p.title.slice(0, 160),
    source_name: p.source, source_url: p.source_url, creator: p.creator || 'Unknown', creator_url: p.creator_url, licence: lic, licence_url: p.licence_url,
    attribution_text: p.attribution_text, attribution_required: lic.startsWith('CC-BY') ? 1 : 0,
    people_free: rv.people_free ?? 1, brand_free: 1, review_status: rv.status || 'pending',
    tags: [...tag('world', p.world), ...tag('topic', p.topic), ...tag('object', p.objects), ...tag('mood', p.mood)],
    variants: p.variants.map(({ w, file, size }) => ({ w, file, size })),
  };
  (entries[p.world] ||= []).push(e);
  p.variants.forEach((v) => keep.add(v.file));
}
for (const w of Object.keys(entries)) entries[w].sort((a, b) => (a.role === 'hero' ? -1 : b.role === 'hero' ? 1 : 0) || a.id.localeCompare(b.id));
for (const w of worlds) writeJson(path.join(SEED, `${w}.json`), entries[w] || []);

const sprites = [];
for (const line of manifest.sprites) {
  const [name, fr, en, wl, topic] = line.split('|');
  const p = picks.sprites[name];
  if (!p) continue;
  const rv = reviews[p.id] || {};
  const slug = name.toLowerCase().replace(/ /g, '-');
  const wlist = wl.split(',');
  sprites.push({
    id: p.id, kind: 'sprite', world_id: wlist[0], grade_band: 'all', mood: 'joyful', dominant_color: p.dominant_color, lqip: p.lqip, width: 256, height: 256,
    alt_fr: `Icône 3D : ${fr}`, alt_en: `3D icon: ${en}`,
    source_name: 'fluent-emoji', source_url: `https://github.com/microsoft/fluentui-emoji/tree/main/assets/${encodeURIComponent(name)}`,
    creator: 'Microsoft Corporation', creator_url: 'https://github.com/microsoft/fluentui-emoji', licence: 'MIT', licence_url: FLUENT_LICENCE,
    attribution_text: 'Fluent Emoji by Microsoft, MIT', attribution_required: 1, people_free: 1, brand_free: 1, review_status: rv.status || 'approved',
    tags: [...wlist.flatMap((x) => tag('world', x)), ...tag('topic', topic), ...tag('object', slug.replace(/^pig-face$/, 'pig'))],
    variants: p.variants.map(({ w, file, size }) => ({ w, file, size })),
  });
  keep.add(p.variants[0].file);
}
if (!ONLY) writeJson(path.join(SEED, 'sprites.json'), sprites);

// credits: approved photos + one grouped MIT line
const all = Object.values(entries).flat().filter((e) => e.review_status === 'approved');
const credits = {
  _note: 'Data for the in-app credits page and GET /v1/credits.',
  photos: all.map((e) => ({ id: e.id, world: e.world_id, thumb: e.variants[0].file, attribution_text: e.attribution_text, creator: e.creator, creator_url: e.creator_url, source_name: e.source_name, source_url: e.source_url, licence: e.licence, licence_url: e.licence_url })),
  groups: [{ id: 'fluent-emoji', count: sprites.length, attribution_text: 'Fluent Emoji by Microsoft, MIT', copyright: 'Copyright (c) Microsoft Corporation.', licence: 'MIT', licence_url: FLUENT_LICENCE, source_url: 'https://github.com/microsoft/fluentui-emoji' }],
};
if (!ONLY) writeJson(path.join(SEED, 'credits.json'), credits);

// prune orphan binaries (only on a full run)
if (!ONLY) for (const f of fs.readdirSync(FILES)) if (!keep.has(f)) fs.unlinkSync(path.join(FILES, f));

// report
let bytes = 0; for (const f of fs.readdirSync(FILES)) bytes += fs.statSync(path.join(FILES, f)).size;
console.log('\nworld        photos approved pending  sources');
for (const w of worlds) {
  const l = entries[w] || [];
  const src = {}; l.forEach((e) => (src[e.source_name] = (src[e.source_name] || 0) + 1));
  console.log(w.padEnd(12), String(l.length).padStart(6), String(l.filter((e) => e.review_status === 'approved').length).padStart(8), String(l.filter((e) => e.review_status === 'pending').length).padStart(7), ' ', JSON.stringify(src));
}
console.log(`sprites ${sprites.length}; total ${(bytes / 1048576).toFixed(2)} MB in ${fs.readdirSync(FILES).length} files; rejected ${Object.keys(rejected).filter((k) => !k.startsWith("title:")).length}`);
