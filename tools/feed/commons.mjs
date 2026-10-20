// Wikimedia Commons helper for the feed tools. Keyless. Node 22, no dependencies.
// Licence gate: CC0, public domain, CC BY only (no SA, NC, ND).
import { readFileSync, writeFileSync, existsSync, mkdirSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

export const HERE = dirname(fileURLToPath(import.meta.url));
const CACHE = join(HERE, '.cache');
mkdirSync(CACHE, { recursive: true });
const UA = 'NumerixFeedTools/1.0 (https://github.com/Perrin-Nj; wandjiperrin@gmail.com) node';
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

export async function api(params) {
  const qs = new URLSearchParams({ format: 'json', formatversion: '2', origin: '*', ...params }).toString();
  const key = join(CACHE, createHash('sha1').update(qs).digest('hex') + '.json');
  if (existsSync(key)) return JSON.parse(readFileSync(key, 'utf8'));
  for (let i = 0; i < 4; i++) {
    let r;
    try { r = await fetch('https://commons.wikimedia.org/w/api.php?' + qs, { headers: { 'User-Agent': UA } }); } catch { await sleep(2000 * (i + 1)); continue; }
    if (r.status === 429 || r.status >= 500) { await sleep(2000 * (i + 1)); continue; }
    const j = await r.json();
    writeFileSync(key, JSON.stringify(j));
    await sleep(150);
    return j;
  }
  throw new Error('commons api failed ' + qs);
}

const OK = /^(cc0|cc0 1\.0|public domain|pd[- ]|pdm|no restrictions|cc[- ]by[- ](1\.0|2\.0|2\.5|3\.0|4\.0)(\s|$)|attribution)/i;
const BAD = /(\bsa\b|share|\bnc\b|\bnd\b|non-?commercial|gfdl|fair use|copyrighted|gpl)/i;
export function licenceOk(name) {
  const n = (name || '').trim();
  return !!n && OK.test(n) && !BAD.test(n);
}
const strip = (h) => (h || '').replace(/<[^>]+>/g, '').replace(/&amp;/g, '&').replace(/&quot;/g, '"').replace(/&#039;/g, "'").replace(/\s+/g, ' ').trim();

function toCand(p) {
  const ii = (p.imageinfo || [])[0];
  if (!ii) return null;
  const m = ii.extmetadata || {};
  const lic = m.LicenseShortName?.value || '';
  return {
    title: p.title,
    page: 'https://commons.wikimedia.org/wiki/' + encodeURIComponent(p.title.replace(/ /g, '_')),
    url: ii.url, thumb: ii.thumburl, mime: ii.mime, w: ii.width, h: ii.height, size: ii.size, dur: ii.duration || 0,
    licence: lic, licenceUrl: m.LicenseUrl?.value || '',
    artist: strip(m.Artist?.value), credit: strip(m.Credit?.value),
    desc: strip(m.ImageDescription?.value).slice(0, 200),
    cats: strip(m.Categories?.value).slice(0, 300),
    ok: licenceOk(lic),
  };
}

// filetype: bitmap | video | drawing | audio
export async function search(q, { filetype = 'bitmap', limit = 12, thumbw = 640 } = {}) {
  const j = await api({
    action: 'query', generator: 'search', gsrsearch: `${q} filetype:${filetype}`, gsrnamespace: '6', gsrlimit: String(limit),
    prop: 'imageinfo', iiprop: 'url|size|mime|extmetadata|mediatype|timestamp', iiurlwidth: String(thumbw),
  });
  const pages = (j.query?.pages || []).sort((a, b) => (a.index || 0) - (b.index || 0));
  const out = [];
  for (const p of pages) {
    const c = toCand(p);
    if (c) out.push(c);
  }
  // duration needs a second property read for video
  return out;
}

export async function byTitle(title, thumbw = 640) {
  const j = await api({
    action: 'query', titles: title, prop: 'imageinfo', iiprop: 'url|size|mime|extmetadata|mediatype', iiurlwidth: String(thumbw),
  });
  const p = (j.query?.pages || [])[0];
  return p && !p.missing ? toCand(p) : null;
}

export async function download(url, dest) {
  if (existsSync(dest)) return dest;
  for (let i = 0; i < 4; i++) {
    let r;
    try { r = await fetch(url, { headers: { 'User-Agent': UA } }); } catch { await sleep(3000 * (i + 1)); continue; }
    if (r.status === 429 || r.status >= 500) { await sleep(3000 * (i + 1)); continue; }
    if (!r.ok) throw new Error(r.status + ' ' + url);
    mkdirSync(dirname(dest), { recursive: true });
    writeFileSync(dest, Buffer.from(await r.arrayBuffer()));
    await sleep(200);
    return dest;
  }
  throw new Error('download failed ' + url);
}

// Candidates from an English Wikipedia article: lead image first, then the other images of the page.
export async function wpImages(article, thumbw = 1100) {
  const qs = new URLSearchParams({ action: 'query', format: 'json', formatversion: '2', titles: article, redirects: '1', prop: 'pageimages|images', piprop: 'name', imlimit: '60' });
  const r = await fetch('https://en.wikipedia.org/w/api.php?' + qs, { headers: { 'User-Agent': UA } });
  const j = await r.json();
  const p = (j.query?.pages || [])[0] || {};
  const names = [];
  if (p.pageimage) names.push(p.pageimage);
  for (const im of p.images || []) {
    const n = im.title.replace(/^File:/, '');
    if (/\.(jpe?g|png)$/i.test(n) && !names.includes(n) && !p.pageimage?.includes(n)) names.push(n);
  }
  await sleep(150);
  const out = [];
  for (let i = 0; i < names.length && i < 14; i += 10) {
    const titles = names.slice(i, i + 10).map((n) => 'File:' + n.replace(/_/g, ' ')).join('|');
    const jj = await api({ action: 'query', titles, prop: 'imageinfo', iiprop: 'url|size|mime|extmetadata|mediatype', iiurlwidth: String(thumbw) });
    const byT = new Map((jj.query?.pages || []).map((pg) => [pg.title, pg]));
    for (const n of names.slice(i, i + 10)) {
      const pg = byT.get('File:' + n.replace(/_/g, ' '));
      const c = pg && !pg.missing ? toCand(pg) : null;
      if (c) out.push(c);
    }
  }
  return out;
}

export async function categoryFiles(cat, thumbw = 1100, limit = 30) {
  const j = await api({ action: 'query', generator: 'categorymembers', gcmtitle: 'Category:' + cat, gcmtype: 'file', gcmlimit: String(limit), prop: 'imageinfo', iiprop: 'url|size|mime|extmetadata|mediatype', iiurlwidth: String(thumbw) });
  return (j.query?.pages || []).map(toCand).filter(Boolean);
}

// Resolve "Prefix of a file name*" to the first matching Commons file title.
export async function resolvePrefix(prefix) {
  const j = await api({ action: 'query', list: 'allpages', apnamespace: '6', apprefix: prefix, aplimit: '5' });
  return j.query?.allpages?.[0]?.title || null;
}
