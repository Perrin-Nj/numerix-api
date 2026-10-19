// Contact sheets for visual review: node tools/assets/sheet.mjs  -> tools/assets/.cache/sheets/*.png (gitignored)
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import sharp from 'sharp';

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..');
const SEED = path.join(ROOT, 'seed', 'assets');
const OUT = path.join(ROOT, 'tools', 'assets', '.cache', 'sheets');
fs.mkdirSync(OUT, { recursive: true });
const manifest = JSON.parse(fs.readFileSync(path.join(ROOT, 'tools', 'assets', 'manifest.json'), 'utf8'));
const worlds = Object.keys(manifest.worlds);
const W = 300, H = 190;
const label = (t) => Buffer.from(`<svg width="${W}" height="22"><rect width="${W}" height="22" fill="#000" fill-opacity=".65"/><text x="4" y="16" font-size="13" fill="#fff" font-family="Arial">${t.replace(/&/g, '&amp;').replace(/</g, '&lt;')}</text></svg>`);

for (let g = 0; g < worlds.length; g += 5) {
  const group = worlds.slice(g, g + 5);
  const comps = [];
  group.forEach((w, r) => {
    const list = JSON.parse(fs.readFileSync(path.join(SEED, `${w}.json`), 'utf8'));
    list.forEach((e, c) => {
      const f = e.variants.find((v) => v.w === 480).file;
      comps.push({ input: path.join(SEED, 'files', f), left: c * W, top: r * H, resizeTo: [W - 4, H - 4], text: `${w} ${e.role === 'hero' ? 'H' : c} ${e.source_name} ${e.licence} ${e.id.slice(2, 8)}` });
    });
  });
  const layers = [];
  for (const c of comps) {
    const img = await sharp(c.input).resize(c.resizeTo[0], c.resizeTo[1], { fit: 'contain', background: '#222' }).toBuffer();
    layers.push({ input: img, left: c.left, top: c.top }, { input: label(c.text), left: c.left, top: c.top });
  }
  await sharp({ create: { width: W * 6, height: H * group.length, channels: 3, background: '#222' } }).composite(layers).png().toFile(path.join(OUT, `sheet${g / 5 + 1}.png`));
}
// sprites sheet
const sp = JSON.parse(fs.readFileSync(path.join(SEED, 'sprites.json'), 'utf8'));
const S = 110, cols = 12, layers = [];
for (const [i, e] of sp.entries()) {
  const img = await sharp(path.join(SEED, 'files', e.variants[0].file)).resize(S - 10, S - 30).toBuffer();
  layers.push({ input: img, left: (i % cols) * S, top: Math.floor(i / cols) * S }, { input: Buffer.from(`<svg width="${S}" height="16"><text x="2" y="12" font-size="11" fill="#000" font-family="Arial">${e.alt_en.replace('3D icon: ', '')}</text></svg>`), left: (i % cols) * S, top: Math.floor(i / cols) * S + S - 20 });
}
await sharp({ create: { width: S * cols, height: S * Math.ceil(sp.length / cols), channels: 3, background: '#fff' } }).composite(layers).png().toFile(path.join(OUT, 'sprites.png'));
console.log('sheets written to', OUT);
