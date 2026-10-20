// Builds the Discover media set. node build.mjs
// Reads the reviewed picks (work/photos.json, work/clips.json) and the texts (catalog/*), writes
//   seed/feed-media/*   final files named <id>-<8 hex content hash>.<ext> (immutable URLs)
//   seed/feed/items.json  the 200 items, loaded into SQLite by cmd/feedseed
import { readFileSync, writeFileSync, mkdirSync, rmSync, copyFileSync, readdirSync, statSync, existsSync } from 'node:fs';
import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { createRequire } from 'node:module';
import { join } from 'node:path';
import sharp from 'sharp';
sharp.cache(false);
import { HERE } from './commons.mjs';
import photoRows from './catalog/photos.mjs';
import videoRows from './catalog/videos.mjs';
import animRows from './catalog/anims.mjs';
import videoText from './catalog/videotext.mjs';
import animText from './catalog/animtext.mjs';

const require = createRequire(import.meta.url);
const FFMPEG = require('ffmpeg-static');
const API = join(HERE, '..', '..');
const MEDIA = join(API, 'seed', 'feed-media');
const ITEMS = join(API, 'seed', 'feed', 'items.json');
const W = join(HERE, 'work');
const photos = JSON.parse(readFileSync(join(W, 'photos.json'), 'utf8'));
const clips = JSON.parse(readFileSync(join(W, 'clips.json'), 'utf8'));
const hash8 = (buf) => createHash('sha256').update(buf).digest('hex').slice(0, 8);
const problems = [];

rmSync(MEDIA, { recursive: true, force: true });
mkdirSync(MEDIA, { recursive: true });
mkdirSync(join(API, 'seed', 'feed'), { recursive: true });

const put = (name, ext, buf) => { // writes <name>-<hash>.<ext>, returns the file name
  const f = `${name}-${hash8(buf)}.${ext}`;
  writeFileSync(join(MEDIA, f), buf);
  return f;
};

async function webp(input, w, h, maxKB, q0 = 78, fit = 'cover') {
  for (let q = q0; q >= 40; q -= 6) {
    const buf = await sharp(input).rotate().resize(w, h, { fit, position: 'attention', withoutEnlargement: false }).webp({ quality: q, effort: 5 }).toBuffer();
    if (buf.length <= maxKB * 1024 || q <= 46) return buf;
  }
}
const lqip = async (input) => 'data:image/webp;base64,' + (await sharp(input).rotate().resize(24, 18, { fit: 'cover' }).blur(0.6).webp({ quality: 35 }).toBuffer()).toString('base64');

// ---------- credit helpers ----------
function normLicence(l) {
  const t = (l || '').trim();
  if (/^cc0/i.test(t)) return 'CC0 1.0';
  if (/^public domain mark/i.test(t)) return 'Public Domain Mark 1.0';
  if (/^public domain$/i.test(t)) return 'Public domain';
  const m = t.match(/^CC BY (\d\.\d)/i);
  return m ? `CC BY ${m[1]}` : t;
}
const licUrl = (l, given) => (given && /^https:/.test(given) ? given
  : l === 'CC0 1.0' ? 'https://creativecommons.org/publicdomain/zero/1.0/'
  : l === 'Public Domain Mark 1.0' ? 'https://creativecommons.org/publicdomain/mark/1.0/'
  : l.startsWith('CC BY ') ? `https://creativecommons.org/licenses/by/${l.slice(6)}/` : '');
const clean = (s) => (s || '').replace(/\(?https?:\/\/\S+\)?/g, '').replace(/\S+@\S+/g, '').replace(/\s+/g, ' ').replace(/^(user:|author:|by)\s*/i, '').replace(/[,;:(\s/]+$/, '').trim().slice(0, 90);
const CREATOR_FIX = { 'sea-otter': 'gails_pictures (Flickr)', flamingos: 'FreeSwissVideo' };
function credit(id, pick, titleFr) {
  const licence = normLicence(pick.licence);
  let creator = clean(pick.artist) || CREATOR_FIX[id] || ''; // the Commons author field held only a profile URL
  if (!creator && /^NASA/i.test(pick.credit || pick.source || '')) creator = 'NASA';
  if (!creator) creator = /public|cc0/i.test(licence) ? 'Unknown author' : '';
  if (!creator) problems.push(`${id}: CC BY item without an author name`);
  const t = (pick.title || '').replace(/^File:/, '').replace(/\.[a-z0-9]{3,4}$/i, '').trim();
  const host = (() => { try { return new URL(pick.page).hostname.replace(/^www\./, ''); } catch { return ''; } })();
  const attribution = licence.startsWith('CC BY')
    ? `“${t}” by ${creator}, ${licence}, via ${host}`
    : `“${t}”, ${creator}, ${licence}, via ${host}`;
  const page = (pick.page || '').replace(/^http:/, 'https:');
  return { source_url: page, creator, licence, licence_url: licUrl(licence, pick.licenceUrl), attribution };
}

const items = [];
const sizes = { photo: 0, animation: 0, video: 0 };
const add = (it, files) => {
  it.tags = [it.topic, it.world, it.kind, `grade-${it.grade_min}`];
  for (const f of files) sizes[it.kind] += statSync(join(MEDIA, f)).size;
  items.push(it);
};

// ---------- photos ----------
for (const r of photoRows) {
  const [id, , world, grade, topic, fr, en] = r;
  const p = photos[id];
  if (!p?.pick) { problems.push(`${id}: no photo picked`); continue; }
  const src = join(W, 'photos', id + '.jpg');
  if (!existsSync(src)) { problems.push(`${id}: source file missing`); continue; }
  const big = await webp(src, 960, 720, 130);
  const small = await webp(src, 480, 360, 45, 72);
  const f1 = put(id, 'webp', big), f2 = put(`${id}-s`, 'webp', small);
  add({
    id, kind: 'photo', world, topic, grade_min: grade,
    fr: { title: fr[0], caption: fr[1], alt: fr[0], question: fr[2], answer: fr[3] },
    en: { title: en[0], caption: en[1], alt: en[0], question: en[2], answer: en[3] },
    src: f1, src_small: f2, width: 960, height: 720, lqip: await lqip(src), ...credit(id, p.pick),
  }, [f1, f2]);
}

// ---------- animations and videos ----------
function vtt(lang, title, fact, dur) {
  const t = (s) => { const ms = Math.round(s * 1000); const p = (n, w = 2) => String(n).padStart(w, '0'); return `00:${p(Math.floor(ms / 60000) % 60)}:${p(Math.floor(ms / 1000) % 60)}.${p(ms % 1000, 3)}`; };
  const a = Math.min(5, dur * 0.4);
  const sentences = fact.match(/[^.!?]+[.!?]+/g)?.map((s) => s.trim()) || [fact];
  const cues = [[0.3, a, title]];
  const rest = dur - a - 0.2;
  sentences.forEach((s, i) => cues.push([a + (rest * i) / sentences.length, a + (rest * (i + 1)) / sentences.length - 0.2, s]));
  return `WEBVTT\n\n` + cues.map((c, i) => `${i + 1}\n${t(c[0])} --> ${t(c[1])}\n${c[2]}\n`).join('\n');
}
async function clipItem(row, text, kind) {
  const [id, world, grade, topic, fr, en] = text;
  const info = clips[id];
  if (!info?.done) { problems.push(`${id}: clip missing`); return; }
  const mp4 = readFileSync(join(W, 'clips', id + '.mp4'));
  const f1 = put(id, 'mp4', mp4);
  const tmp = execFileSync(FFMPEG, ['-v', 'error', '-ss', String(Math.min(info.dur * 0.35, 4).toFixed(2)), '-i', join(W, 'clips', id + '.mp4'), '-frames:v', '1', '-f', 'image2pipe', '-vcodec', 'mjpeg', '-q:v', '2', '-'], { maxBuffer: 1 << 26 }); // frame as a JPEG buffer
  const poster = await webp(tmp, info.w, info.h, 70, 72, 'fill');
  const f2 = put(`${id}-poster`, 'webp', poster);
  const it = {
    id, kind, world, topic, grade_min: grade,
    fr: { title: fr[0], caption: fr[1], alt: fr[0], question: fr[2], answer: fr[3] },
    en: { title: en[0], caption: en[1], alt: en[0], question: en[2], answer: en[3] },
    duration_ms: Math.round(info.dur * 1000), src: f1, poster: f2, width: info.w, height: info.h, lqip: await lqip(tmp),
    ...(info.audio ? { has_audio: true } : {}), ...credit(id, info.pick),
  };
  const files = [f1, f2];
  if (kind === 'video') {
    it.captions = [];
    for (const [lang, label, x] of [['fr-CA', 'Français', fr], ['en-CA', 'English', en]]) {
      const f = put(`${id}-${lang.slice(0, 2)}`, 'vtt', Buffer.from(vtt(lang, x[0], x[1], info.dur)));
      it.captions.push({ lang, label, src: f });
      files.push(f);
    }
  }
  add(it, files);
}
const textOf = (arr) => new Map(arr.map((r) => [r[0], r]));
const vt = textOf(videoText), at = textOf(animText);
for (const r of animRows) if (at.has(r.id)) await clipItem(r, at.get(r.id), 'animation'); else problems.push(`${r.id}: no animation text`);
for (const r of videoRows) if (vt.has(r.id)) await clipItem(r, vt.get(r.id), 'video'); else problems.push(`${r.id}: no video text`);

writeFileSync(ITEMS, JSON.stringify(items, null, 1) + '\n');
const mb = (n) => (n / 1048576).toFixed(1);
const count = (k) => items.filter((i) => i.kind === k).length;
console.log(`items ${items.length}: photo ${count('photo')} (${mb(sizes.photo)} MB), animation ${count('animation')} (${mb(sizes.animation)} MB), video ${count('video')} (${mb(sizes.video)} MB), total ${mb(sizes.photo + sizes.animation + sizes.video)} MB, files ${readdirSync(MEDIA).length}`);
if (problems.length) { console.log('PROBLEMS:\n' + problems.join('\n')); process.exitCode = 1; }
