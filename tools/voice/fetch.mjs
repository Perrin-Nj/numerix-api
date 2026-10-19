// Fetch verified-licence HUMAN voice clips (Lingua Libre via Wikimedia Commons) for Numerix.
// Usage: cd tools/voice && npm i && node fetch.mjs [--dry] [--fr-only|--en-only]
// Idempotent: caches API lookups in .cache/, skips existing outputs, rewrites seed/voice/clips.json.
// Only Canadian-resident speakers are kept (self-declared location on the Lingua Libre speaker page).
// Locale is "fr-CA"/"en-CA" only when the speaker lists a Canadian residence; region is stored separately.
import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const OUT = path.resolve(HERE, '../../seed/voice');
const FILES = path.join(OUT, 'files');
const CACHE = path.join(HERE, '.cache');
const UA = 'NumerixVoiceTools/0.1 (wandjiperrin@gmail.com)';
const args = new Set(process.argv.slice(2));
const BUDGET = 25 * 1024 * 1024;
// Licences we accept (Commons template -> SPDX-like). Anything else is rejected.
const LICENCES = {
  'cc-zero': 'CC0-1.0', 'cc0': 'CC0-1.0',
  'cc-by-4.0': 'CC-BY-4.0', 'cc-by-sa-4.0': 'CC-BY-SA-4.0',
  'cc-by-3.0': 'CC-BY-3.0', 'cc-by-sa-3.0': 'CC-BY-SA-3.0',
};
const LANGS = {
  fr: { llQ: 'Q21', prefix: 'LL-Q150 (fra)-', locale: 'fr-CA' },
  en: { llQ: 'Q22', prefix: 'LL-Q1860 (eng)-', locale: 'en-CA' },
};

fs.mkdirSync(FILES, { recursive: true });
fs.mkdirSync(CACHE, { recursive: true });
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const nfc = (s) => s.normalize('NFC');

async function get(url, opts = {}, tries = 5) {
  for (let i = 0; i < tries; i++) {
    const r = await fetch(url, { ...opts, headers: { 'User-Agent': UA, ...(opts.headers || {}) } });
    if (r.ok) return r;
    if (r.status === 429 || r.status >= 500) { await sleep(1500 * (i + 1)); continue; }
    throw new Error(`${r.status} ${url}`);
  }
  throw new Error(`gave up ${url}`);
}
const jget = async (url, opts) => (await get(url, opts)).json();
const cached = async (name, fn) => {
  const f = path.join(CACHE, name);
  if (fs.existsSync(f)) return JSON.parse(fs.readFileSync(f, 'utf8'));
  const v = await fn();
  fs.writeFileSync(f, JSON.stringify(v));
  return v;
};

// ---------- vocabulary ----------
const FR_U = ['zéro', 'un', 'deux', 'trois', 'quatre', 'cinq', 'six', 'sept', 'huit', 'neuf', 'dix', 'onze', 'douze', 'treize', 'quatorze', 'quinze', 'seize', 'dix-sept', 'dix-huit', 'dix-neuf'];
const FR_T = { 2: 'vingt', 3: 'trente', 4: 'quarante', 5: 'cinquante', 6: 'soixante' };
function fr(n) {
  if (n < 20) return [FR_U[n]];
  if (n === 100) return ['cent'];
  if (n === 1000) return ['mille'];
  const t = Math.floor(n / 10), u = n % 10;
  if (t <= 6) {
    if (u === 0) return [FR_T[t]];
    if (u === 1) return [`${FR_T[t]} et un`, `${FR_T[t]}-et-un`];
    return [`${FR_T[t]}-${FR_U[u]}`];
  }
  if (t === 7) { // soixante-dix .. soixante-dix-neuf
    if (u === 0) return ['soixante-dix'];
    if (u === 1) return ['soixante et onze', 'soixante-et-onze'];
    return [`soixante-${FR_U[10 + u]}`];
  }
  if (t === 8) return u === 0 ? ['quatre-vingts', 'quatre-vingt'] : [`quatre-vingt-${FR_U[u]}`];
  return u === 0 ? ['quatre-vingt-dix'] : [`quatre-vingt-${FR_U[10 + u]}`];
}
const EN_U = ['zero', 'one', 'two', 'three', 'four', 'five', 'six', 'seven', 'eight', 'nine', 'ten', 'eleven', 'twelve', 'thirteen', 'fourteen', 'fifteen', 'sixteen', 'seventeen', 'eighteen', 'nineteen'];
const EN_T = { 2: 'twenty', 3: 'thirty', 4: 'forty', 5: 'fifty', 6: 'sixty', 7: 'seventy', 8: 'eighty', 9: 'ninety' };
function en(n) {
  if (n < 20) return [EN_U[n]];
  if (n === 100) return ['one hundred', 'hundred'];
  if (n === 1000) return ['one thousand', 'thousand'];
  const t = Math.floor(n / 10), u = n % 10;
  return u === 0 ? [EN_T[t]] : [`${EN_T[t]}-${EN_U[u]}`, `${EN_T[t]} ${EN_U[u]}`];
}
const nums = [...Array(101).keys(), 1000];
const hundreds = [200, 300, 400, 500, 600, 700, 800, 900];
const frH = (n) => [`${FR_U[n / 100]} cents`, `${FR_U[n / 100]}-cents`];
const enH = (n) => [`${EN_U[n / 100]} hundred`];
const LET = 'abcdefghijklmnopqrstuvwxyz'.split('');

// [key, category, {fr:[forms], en:[forms]}]
const V = [];
for (const n of nums) V.push([`num:${n}`, 'number', { fr: fr(n), en: en(n) }]);
for (const n of hundreds) V.push([`num:${n}`, 'number', { fr: frH(n), en: enH(n) }]);
const add = (cat, rows) => rows.forEach(([k, f, e]) => V.push([`${cat}:${k}`, cat, { fr: f, en: e }]));
add('op', [
  ['plus', ['plus'], ['plus']], ['minus', ['moins'], ['minus']], ['times', ['fois'], ['times']],
  ['equals', ['égale', 'égal'], ['equals']], ['divided', ['divisé', 'divisé par'], ['divided by', 'divided']],
]);
add('fraction', [
  ['half', ['demi', 'moitié'], ['half']], ['third', ['tiers'], ['third']], ['quarter', ['quart'], ['quarter']],
  ['fifth', ['cinquième'], ['fifth']], ['tenth', ['dixième'], ['tenth']], ['fraction', ['fraction'], ['fraction']],
]);
add('day', [
  ['mon', ['lundi'], ['Monday', 'monday']], ['tue', ['mardi'], ['Tuesday', 'tuesday']], ['wed', ['mercredi'], ['Wednesday', 'wednesday']],
  ['thu', ['jeudi'], ['Thursday', 'thursday']], ['fri', ['vendredi'], ['Friday', 'friday']], ['sat', ['samedi'], ['Saturday', 'saturday']],
  ['sun', ['dimanche'], ['Sunday', 'sunday']],
]);
add('month', [
  ['jan', ['janvier'], ['January', 'january']], ['feb', ['février'], ['February', 'february']], ['mar', ['mars'], ['March', 'march']],
  ['apr', ['avril'], ['April', 'april']], ['may', ['mai'], ['May', 'may']], ['jun', ['juin'], ['June', 'june']],
  ['jul', ['juillet'], ['July', 'july']], ['aug', ['août'], ['August', 'august']], ['sep', ['septembre'], ['September', 'september']],
  ['oct', ['octobre'], ['October', 'october']], ['nov', ['novembre'], ['November', 'november']], ['dec', ['décembre'], ['December', 'december']],
]);
add('shape', [
  ['circle', ['cercle', 'rond'], ['circle']], ['square', ['carré'], ['square']], ['triangle', ['triangle'], ['triangle']],
  ['rectangle', ['rectangle'], ['rectangle']], ['diamond', ['losange'], ['diamond']], ['star', ['étoile'], ['star']],
  ['oval', ['ovale'], ['oval']], ['cube', ['cube'], ['cube']], ['sphere', ['sphère'], ['sphere']], ['cone', ['cône'], ['cone']],
  ['pentagon', ['pentagone'], ['pentagon']], ['hexagon', ['hexagone'], ['hexagon']], ['heart', ['cœur', 'coeur'], ['heart']],
]);
add('colour', [
  ['red', ['rouge'], ['red']], ['blue', ['bleu'], ['blue']], ['green', ['vert'], ['green']], ['yellow', ['jaune'], ['yellow']],
  ['orange', ['orange'], ['orange']], ['pink', ['rose'], ['pink']], ['purple', ['violet'], ['purple']], ['black', ['noir'], ['black']],
  ['white', ['blanc'], ['white']], ['grey', ['gris'], ['grey', 'gray']], ['brown', ['brun', 'marron'], ['brown']],
]);
add('praise', [
  ['bravo', ['bravo'], ['bravo']], ['super', ['super'], ['super']], ['yes', ['oui'], ['yes']], ['no', ['non'], ['no']],
  ['try_again', ['essaie encore', 'réessaie'], ['try again']], ['well_done', ['bien joué', 'bravo'], ['well done']],
  ['great', ['génial', 'formidable'], ['great']], ['excellent', ['excellent'], ['excellent']], ['perfect', ['parfait'], ['perfect']],
  ['courage', ['courage'], ['keep going']], ['correct', ['correct', 'exact'], ['correct']], ['wrong', ['faux'], ['wrong']],
  ['thanks', ['merci'], ['thank you', 'thanks']], ['hello', ['bonjour'], ['hello']], ['goodbye', ['au revoir'], ['goodbye']],
  ['wow', ['wow', 'ouah'], ['wow']], ['awesome', ['fantastique'], ['awesome']], ['good', ['bien'], ['good']], ['again', ['encore'], ['again']],
]);
for (const l of LET) V.push([`letter:${l}`, 'letter', { fr: [l], en: [l] }]);

// ---------- speakers (Lingua Libre SPARQL + Wikidata location) ----------
const LL_SPARQL = 'https://lingualibre.org/bigdata/namespace/wdq/sparql';
const WD_SPARQL = 'https://query.wikidata.org/sparql';
const wdq = async (q) => (await jget(WD_SPARQL, { method: 'POST', headers: { Accept: 'application/sparql-results+json', 'Content-Type': 'application/x-www-form-urlencoded' }, body: 'query=' + encodeURIComponent(q) })).results.bindings;
const REGIONS = { Q176: 'Québec', Q1904: 'Ontario', Q1965: 'New Brunswick', Q1989: 'Saskatchewan', Q1973: 'British Columbia' };

async function loadSpeakers() {
  return cached('speakers.json', async () => {
    const q = `SELECT ?s ?n ?loc ?lang WHERE { ?s <https://lingualibre.org/prop/direct/P2> <https://lingualibre.org/entity/Q3>; <https://lingualibre.org/prop/direct/P14> ?loc; <https://lingualibre.org/prop/direct/P11> ?n; <https://lingualibre.org/prop/direct/P4> ?lang }`;
    const r = await jget(`${LL_SPARQL}?query=${encodeURIComponent(q)}`, { headers: { Accept: 'application/sparql-results+json' } });
    const rows = r.results.bindings
      .map((b) => ({ id: b.s.value.split('/').pop(), name: b.n.value, loc: b.loc.value, lang: b.lang.value.split('/').pop() }))
      .filter((x) => /^Q\d+$/.test(x.loc) && (x.lang === 'Q21' || x.lang === 'Q22'));
    const locs = [...new Set(rows.map((x) => x.loc))];
    const canada = new Set();
    for (let i = 0; i < locs.length; i += 400) {
      const ch = locs.slice(i, i + 400).map((l) => 'wd:' + l).join(' ');
      for (const b of await wdq(`SELECT ?c WHERE { VALUES ?c { ${ch} } ?c wdt:P17 wd:Q16 }`)) canada.add(b.c.value.split('/').pop());
    }
    if (locs.includes('Q16')) canada.add('Q16');
    const region = {};
    const cl = [...canada].filter((c) => c !== 'Q16');
    if (cl.length) {
      const rs = Object.keys(REGIONS).map((x) => 'wd:' + x).join(' ');
      for (const b of await wdq(`SELECT ?c ?r WHERE { VALUES ?c { ${cl.map((l) => 'wd:' + l).join(' ')} } VALUES ?r { ${rs} } ?c wdt:P131* ?r }`)) region[b.c.value.split('/').pop()] = REGIONS[b.r.value.split('/').pop()];
    }
    return rows.filter((x) => canada.has(x.loc)).map((x) => ({ ...x, region: region[x.loc] || 'Canada (province unspecified)' }));
  });
}

// ---------- Commons lookup ----------
async function lookup(titles) {
  const found = await cached('found.json', async () => ({}));
  const todo = titles.filter((t) => !(t in found));
  for (let i = 0; i < todo.length; i += 50) {
    const ch = todo.slice(i, i + 50);
    const u = 'https://commons.wikimedia.org/w/api.php?' + new URLSearchParams({
      action: 'query', format: 'json', formatversion: '2', titles: ch.join('|'), prop: 'imageinfo|revisions',
      iiprop: 'url|size|duration', rvprop: 'content', rvslots: 'main',
    });
    const j = await jget(u);
    const norm = {};
    for (const n of j.query?.normalized || []) norm[nfc(n.to)] = n.from;
    const byTitle = {};
    for (const p of j.query?.pages || []) byTitle[nfc(p.title)] = p;
    for (const t of ch) {
      const p = byTitle[nfc(t)];
      if (!p || p.missing) { found[t] = null; continue; }
      const wt = p.revisions?.[0]?.slots?.main?.content || '';
      const lic = (wt.match(/\{\{\s*(cc-[a-z0-9.\-]+|cc0)\s*\}\}/i) || [])[1]?.toLowerCase();
      const date = (wt.match(/\|\s*date\s*=\s*([^\n|]*)/) || [])[1]?.trim();
      found[t] = { url: p.imageinfo?.[0]?.url, dur: p.imageinfo?.[0]?.duration, tpl: lic || null, licence: LICENCES[lic] || null, date };
    }
    fs.writeFileSync(path.join(CACHE, 'found.json'), JSON.stringify(found));
    process.stdout.write(`\rlookup ${Math.min(i + 50, todo.length)}/${todo.length}`);
    await sleep(250);
  }
  if (todo.length) console.log();
  return found;
}

// ---------- ffmpeg ----------
async function ffmpegPath() {
  const env = process.env.FFMPEG;
  if (env) return env;
  const t = spawnSync('ffmpeg', ['-version']);
  if (!t.error && t.status === 0) return 'ffmpeg';
  try { return (await import('ffmpeg-static')).default; } catch { throw new Error('no ffmpeg: run `npm i` in tools/voice (ffmpeg-static@5.3.0) or set FFMPEG'); }
}
let FF;
const run = (a) => spawnSync(FF, ['-hide_banner', '-nostdin', ...a], { encoding: 'utf8', maxBuffer: 1 << 26 });
const stat = (file, extra = '') => {
  const r = run(['-i', file, '-af', `${extra}volumedetect`, '-f', 'null', '-']).stderr;
  const g = (re) => { const m = r.match(re); return m ? parseFloat(m[1]) : null; };
  return { max: g(/max_volume: (-?[\d.]+|-inf) dB/), mean: g(/mean_volume: (-?[\d.]+|-inf) dB/), h0: g(/histogram_0db: (\d+)/) };
};
const probeDur = (file) => {
  const r = run(['-i', file, '-f', 'null', '-']).stderr;
  const all = [...r.matchAll(/time=(\d+):(\d+):([\d.]+)/g)].pop();
  return all ? +all[1] * 3600 + +all[2] * 60 + parseFloat(all[3]) : 0;
};

function convert(src, dst) {
  const sr1 = 'silenceremove=start_periods=1:start_threshold=-45dB:start_silence=0.03:detection=peak';
  const trim = `${sr1},areverse,${sr1},areverse`; // trim both ends without cutting inside the word
  const pre = `aresample=48000,pan=mono|c0=0.5*c0+0.5*c1,${trim},apad=pad_dur=0.04`;
  // pass 1: measure loudness
  const m = run(['-i', src, '-af', `${pre},loudnorm=I=-16:TP=-1.5:LRA=11:print_format=json`, '-f', 'null', '-']).stderr;
  const js = m.match(/\{[\s\S]*?"target_offset"[\s\S]*?\}/);
  let norm;
  const meas = js && JSON.parse(js[0]);
  if (meas && isFinite(parseFloat(meas.input_i)) && parseFloat(meas.input_i) > -60) {
    norm = `loudnorm=I=-16:TP=-1.5:LRA=11:measured_I=${meas.input_i}:measured_TP=${meas.input_tp}:measured_LRA=${meas.input_lra}:measured_thresh=${meas.input_thresh}:offset=${meas.target_offset}:linear=true`;
  } else { // very short clip: peak-normalise to -3 dBFS
    const s = stat(src, `${pre},`);
    norm = s.max == null ? 'anull' : `volume=${(-3 - s.max).toFixed(2)}dB`;
  }
  const r = run(['-y', '-i', src, '-af', `${pre},${norm},alimiter=limit=0.8:level=0`, '-ac', '1', '-ar', '48000', '-c:a', 'aac', '-b:a', '64k', '-movflags', '+faststart', dst]);
  return r.status === 0;
}

// ---------- fallback: non-Canadian human speakers for keys no Canadian speaker covers ----------
// Labelled with their REAL country (fr-FR, en-US ...). Never presented as fr-CA/en-CA.
async function fallbackPicks(lang, missingKeys) {
  const forms = new Map(); // form -> key
  for (const [key, , fm] of V) if (missingKeys.has(key)) for (const f of fm[lang]) if (!forms.has(f)) forms.set(f, key);
  const vals = [...forms.keys()].map((f) => JSON.stringify(f)).join(' ');
  const P = 'https://lingualibre.org/prop/direct/';
  const q = `SELECT ?sp ?n ?t ?f ?loc WHERE { VALUES ?t { ${vals} } ?r <${P}P4> <https://lingualibre.org/entity/${LANGS[lang].llQ}>; <${P}P5> ?sp; <${P}P7> ?t; <${P}P3> ?f . ?sp <${P}P11> ?n . OPTIONAL { ?sp <${P}P14> ?loc } }`;
  const rows = (await cached(`fb-${lang}-${crypto.createHash('sha1').update(q).digest('hex').slice(0, 8)}.json`, async () =>
    (await jget(`${LL_SPARQL}?query=${encodeURIComponent(q)}`, { headers: { Accept: 'application/sparql-results+json' } })).results.bindings
      .map((b) => ({ sp: b.sp.value.split('/').pop(), n: b.n.value, t: b.t.value, f: decodeURIComponent(b.f.value.split('/').pop()), loc: b.loc?.value }))));
  const bySp = new Map();
  for (const r of rows) {
    if (!bySp.has(r.sp)) bySp.set(r.sp, { id: r.sp, name: r.n, loc: r.loc, keys: new Map() });
    bySp.get(r.sp).keys.set(forms.get(r.t), r);
  }
  // speaker location -> country code + label
  const locs = [...new Set([...bySp.values()].map((s) => s.loc).filter((l) => /^Q\d+$/.test(l || '')))];
  const geo = {};
  for (let i = 0; i < locs.length; i += 300) {
    const ch = locs.slice(i, i + 300).map((l) => 'wd:' + l).join(' ');
    for (const b of await wdq(`SELECT ?c ?cc0 ?cc1 ?l WHERE { VALUES ?c { ${ch} } OPTIONAL { ?c wdt:P297 ?cc0 } OPTIONAL { ?c wdt:P17/wdt:P297 ?cc1 } OPTIONAL { ?c rdfs:label ?l FILTER(lang(?l)="en") } }`))
      geo[b.c.value.split('/').pop()] = { cc: b.cc0?.value || b.cc1?.value, label: b.l?.value };
  }
  const near = lang === 'en' ? ['CA', 'US'] : ['CA', 'BE', 'CH', 'FR'];
  const sps = [...bySp.values()].map((s) => ({ ...s, geo: geo[s.loc] || {} }))
    .filter((s) => !(s.geo.cc === 'CA')); // Canadian ones were handled in tier 1
  const rankOf = (s) => { const i = near.indexOf(s.geo.cc); return i < 0 ? 99 : i; };
  const left = new Set(missingKeys);
  const out = [];
  for (let round = 0; round < 3 && left.size; round++) {
    sps.sort((a, b) => [...b.keys.keys()].filter((k) => left.has(k)).length - [...a.keys.keys()].filter((k) => left.has(k)).length || rankOf(a) - rankOf(b));
    const best = sps[0];
    if (!best) break;
    const got = [...best.keys.keys()].filter((k) => left.has(k));
    if (!got.length) break;
    for (const k of got) { out.push({ key: k, sp: best, row: best.keys.get(k) }); left.delete(k); }
  }
  return out;
}

// ---------- main ----------
FF = await ffmpegPath();
const speakers = await loadSpeakers();
const wantLangs = Object.keys(LANGS).filter((l) => !args.has(`--${l === 'fr' ? 'en' : 'fr'}-only`));
const prev = fs.existsSync(path.join(OUT, 'clips.json')) ? JSON.parse(fs.readFileSync(path.join(OUT, 'clips.json'), 'utf8')) : { clips: [] };
const clips = [], misses = [];
const titleOf = (lang, sp, form) => `File:${LANGS[lang].prefix}${sp.name}-${form}.wav`;
const catOf = new Map(V.map(([k, c]) => [k, c]));
const planned = []; // {lang,key,text,title,speaker,region,locale,tier}

for (const lang of wantLangs) {
  const sps = speakers.filter((s) => s.lang === LANGS[lang].llQ);
  console.log(lang, 'canadian speakers:', sps.length);
  const titles = [];
  for (const [, , forms] of V) for (const f of forms[lang]) for (const s of sps) titles.push(titleOf(lang, s, f));
  const found = await lookup(titles);
  const cover = (s) => V.filter(([, , fm]) => fm[lang].some((f) => found[titleOf(lang, s, f)]?.licence)).length;
  const rank = sps.map((s) => ({ s, q: s.region === 'Québec' ? 1 : 0, c: cover(s) })).sort((a, b) => b.q - a.q || b.c - a.c).map((x) => x.s);
  const missing = new Set();
  for (const [key, , forms] of V) {
    let pick;
    for (const s of rank) {
      for (const f of forms[lang]) {
        const r = found[titleOf(lang, s, f)];
        if (r?.licence && r.url) { pick = { s, f, r }; break; }
      }
      if (pick) break;
    }
    if (!pick) { missing.add(key); continue; }
    planned.push({ lang, key, text: pick.f, title: titleOf(lang, pick.s, pick.f), speaker: pick.s.name, region: pick.s.region, locale: LANGS[lang].locale, tier: 'canada' });
  }
  if (!args.has('--no-fallback') && missing.size) {
    const fb = await fallbackPicks(lang, missing);
    const tl = fb.map((x) => 'File:' + x.row.f);
    await lookup(tl);
    for (const x of fb) {
      const cc = x.sp.geo.cc;
      planned.push({ lang, key: x.key, text: x.row.t, title: 'File:' + x.row.f, speaker: x.sp.name, region: x.sp.geo.label ? `${x.sp.geo.label}${cc ? '' : ''}` : 'unknown', locale: cc ? `${lang}-${cc}` : lang, tier: 'fallback' });
      missing.delete(x.key);
    }
  }
  for (const k of missing) misses.push(`${lang} ${k}`);
}

const foundAll = await lookup([]);
const failedQa = [];
for (const p of planned) {
  const r = foundAll[p.title];
  if (!r?.licence || !r.url) { misses.push(`${p.lang} ${p.key} (no verified licence)`); continue; }
  const id = 'v_' + crypto.createHash('sha1').update(`${p.lang}|${p.key}|${p.title}`).digest('hex').slice(0, 12);
  const dst = path.join(FILES, `${id}.m4a`);
  const pageUrl = 'https://commons.wikimedia.org/wiki/' + encodeURI(p.title.replace(/ /g, '_')).replace(/\?/g, '%3F');
  const entry = {
    id, key: p.key, category: catOf.get(p.key), text: p.text, lang: p.lang, locale: p.locale, speaker: p.speaker,
    speakerRegion: p.region, regionSource: 'speaker self-declared residence on Lingua Libre; accent not verified', tier: p.tier,
    licence: r.licence, shareAlike: r.licence.includes('SA'), modified: true,
    attribution: `"${p.text}" spoken by ${p.speaker}, Lingua Libre / Wikimedia Commons, ${r.licence}${r.licence.startsWith('CC0') ? '' : ', trimmed, loudness-normalised and re-encoded by Numerix'}`,
    sourceUrl: pageUrl, file: `files/${id}.m4a`, duration: null, qa: null,
  };
  if (args.has('--dry')) { clips.push(entry); continue; }
  const raw = path.join(CACHE, `${id}.wav`);
  if (!fs.existsSync(dst) || args.has('--force')) {
    if (!fs.existsSync(raw)) { fs.writeFileSync(raw, Buffer.from(await (await get(r.url)).arrayBuffer())); await sleep(150); }
    if (!convert(raw, dst)) { misses.push(`${p.lang} ${p.key} (ffmpeg fail)`); continue; }
  }
  // QA: duration sanity, silence, clipping (source), peak (output)
  const dur = probeDur(dst), so = stat(dst), sr = fs.existsSync(raw) ? stat(raw) : { h0: 0 };
  const issues = [];
  if (dur < 0.15 || dur > 4) issues.push('duration');
  if (so.mean == null || so.mean < -40) issues.push('too-quiet/silent');
  if (so.max != null && so.max > -0.3) issues.push('output-peak');
  if ((sr.h0 || 0) > 3) issues.push('source-clipped');
  entry.duration = +dur.toFixed(3);
  entry.qa = { pass: issues.length === 0, issues, dur: +dur.toFixed(3), meanDb: so.mean, peakDb: so.max, srcClippedSamples: sr.h0 };
  if (!entry.qa.pass) failedQa.push(entry);
  clips.push(entry);
}

const ran = new Set(wantLangs);
for (const c of prev.clips) if (!ran.has(c.lang)) clips.push(c);
const pass = clips.filter((c) => c.qa?.pass !== false);
let bytes = 0;
const final = pass.filter((c) => { const f = path.join(OUT, c.file); const sz = fs.existsSync(f) ? fs.statSync(f).size : 0; if (bytes + sz > BUDGET) return false; bytes += sz; return true; });
if (!args.has('--dry')) {
  const keep = new Set(final.map((c) => path.basename(c.file)));
  for (const f of fs.readdirSync(FILES)) if (!keep.has(f)) fs.unlinkSync(path.join(FILES, f));
}
const order = (c) => (c.tier === 'canada' ? 0 : 1);
final.sort((a, b) => a.lang.localeCompare(b.lang) || order(a) - order(b) || a.key.localeCompare(b.key, 'en', { numeric: true }));
fs.writeFileSync(path.join(OUT, 'clips.json'), JSON.stringify({
  generated: new Date().toISOString().slice(0, 10), format: 'AAC-LC 64 kbps mono 48 kHz (.m4a), -16 LUFS target, silence trimmed',
  note: 'Human recordings from Lingua Libre via Wikimedia Commons. tier=canada: speaker lists a Canadian residence (locale fr-CA/en-CA, see speakerRegion). tier=fallback: non-Canadian speaker, locale is the real country (fr-FR, en-US...), NOT fr-CA/en-CA. CC BY-SA clips are modified (shareAlike=true): credit speaker, link licence, publish the processed clip under CC BY-SA 4.0 (the SA applies to the clip, not app code).',
  clips: final,
}, null, 1));
fs.writeFileSync(path.join(OUT, 'misses.txt'), misses.join('\n') + '\n');
fs.writeFileSync(path.join(OUT, 'qa-failed.json'), JSON.stringify(failedQa.map((c) => ({ id: c.id, key: c.key, lang: c.lang, speaker: c.speaker, qa: c.qa })), null, 1));
const cnt = {}, cov = {};
for (const c of final) { const k = `${c.lang}/${c.locale}/${c.speakerRegion}/${c.licence}`; cnt[k] = (cnt[k] || 0) + 1; }
for (const l of ['fr', 'en']) { const ks = new Set(final.filter((c) => c.lang === l).map((c) => c.key)); const kc = new Set(final.filter((c) => c.lang === l && c.tier === 'canada').map((c) => c.key)); cov[l] = { any: ks.size, canada: kc.size, of: V.length }; }
console.log(JSON.stringify({ clips: final.length, qaFailed: failedQa.length, misses: misses.length, MB: +(bytes / 1048576).toFixed(2), coverage: cov, breakdown: cnt }, null, 1));
