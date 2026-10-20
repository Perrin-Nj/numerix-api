// Openverse (https://api.openverse.org) keyless search, CC0 / PDM / CC BY only. Cached, throttled (anonymous limit is low).
import { readFileSync, writeFileSync, existsSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { join } from 'node:path';
import { HERE } from './commons.mjs';

const UA = 'NumerixFeedTools/1.0 (wandjiperrin@gmail.com)';
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const LIC = { cc0: 'CC0 1.0', pdm: 'Public Domain Mark 1.0', by: 'CC BY' };

export async function openverse(q, { extension = 'jpg', pages = 1 } = {}) {
  const out = [];
  for (let page = 1; page <= pages; page++) {
    const qs = new URLSearchParams({ q, license: 'cc0,pdm,by', category: 'photograph', extension, aspect_ratio: 'wide', size: 'large', mature: 'false', page_size: '20', page: String(page) }).toString();
    const key = join(HERE, '.cache', 'ov-' + createHash('sha1').update(qs).digest('hex') + '.json');
    let j;
    if (existsSync(key)) j = JSON.parse(readFileSync(key, 'utf8'));
    else {
      for (let i = 0; i < 6; i++) {
        await sleep(3300);
        const r = await fetch('https://api.openverse.org/v1/images/?' + qs, { headers: { 'User-Agent': UA } });
        if (r.status === 429) { await sleep(20000); continue; }
        if (!r.ok) throw new Error('openverse ' + r.status);
        j = await r.json();
        writeFileSync(key, JSON.stringify(j));
        break;
      }
      if (!j) throw new Error('openverse throttled: ' + q);
    }
    for (const r of j.results || []) {
      const lic = r.license === 'by' ? `CC BY ${r.license_version}` : LIC[r.license] || r.license;
      out.push({
        title: (r.title || q).replace(/\s+/g, ' ').trim(), page: r.foreign_landing_url, url: r.url, thumb: r.thumbnail, mime: 'image/jpeg',
        w: r.width, h: r.height, licence: lic, licenceUrl: r.license_url, artist: r.creator || '', credit: r.source, source: r.source,
        desc: '', cats: (r.tags || []).map((t) => t.name).join(' '), ok: ['cc0', 'pdm', 'by'].includes(r.license) && !r.mature,
      });
    }
  }
  return out;
}

// NASA Image and Video Library (https://images-api.nasa.gov), keyless. NASA media is generally not copyrighted;
// see https://www.nasa.gov/nasa-brand-center/images-and-media/ . Illustrations and artist concepts are skipped.
export async function nasa(q) {
  const qs = new URLSearchParams({ q, media_type: 'image', page_size: '30' }).toString();
  const key = join(HERE, '.cache', 'nasa-' + createHash('sha1').update(qs).digest('hex') + '.json');
  let j;
  if (existsSync(key)) j = JSON.parse(readFileSync(key, 'utf8'));
  else {
    await sleep(600);
    const r = await fetch('https://images-api.nasa.gov/search?' + qs, { headers: { 'User-Agent': UA } });
    if (!r.ok) throw new Error('nasa ' + r.status);
    j = await r.json();
    writeFileSync(key, JSON.stringify(j));
  }
  const out = [];
  for (const it of j.collection?.items || []) {
    const d = it.data?.[0];
    if (!d || /illustration|artist|concept|animation|simulation|diagram|logo|poster/i.test(`${d.title} ${d.description}`)) continue;
    const big = (it.links || []).find((l) => /~large\.jpg$/.test(l.href)) || (it.links || []).find((l) => /~medium\.jpg$/.test(l.href));
    if (!big || (big.width || 0) < 900 || (big.height || 0) < 600) continue;
    out.push({
      title: d.title, page: 'https://images.nasa.gov/details/' + d.nasa_id, url: big.href, mime: 'image/jpeg', w: big.width, h: big.height,
      licence: 'Public domain', licenceUrl: 'https://www.nasa.gov/nasa-brand-center/images-and-media/', artist: d.photographer || d.secondary_creator || 'NASA',
      credit: 'NASA', source: 'nasa', desc: '', cats: (d.keywords || []).join(' '), ok: true,
    });
  }
  return out;
}
