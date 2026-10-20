// Contact sheets for visual review. node sheet.mjs <dir> <name> id1,id2,...   (files work/<dir>/<id>.jpg)
// Output work/sheets/<name>-<n>.jpg, 5 columns x 4 rows (20 tiles) per sheet.
import sharp from 'sharp';
import { join } from 'node:path';
import { mkdirSync, existsSync } from 'node:fs';
import { HERE } from './commons.mjs';

const [dir, name, idList] = process.argv.slice(2);
const ids = idList.split(',').filter(Boolean);
const W = 300, H = 190, COLS = 5, ROWS = 4;
mkdirSync(join(HERE, 'work', 'sheets'), { recursive: true });
for (let s = 0; s * COLS * ROWS < ids.length; s++) {
  const part = ids.slice(s * COLS * ROWS, (s + 1) * COLS * ROWS);
  const layers = [];
  for (let i = 0; i < part.length; i++) {
    const f = join(HERE, 'work', dir, part[i] + '.jpg');
    if (!existsSync(f)) continue;
    const x = (i % COLS) * W, y = Math.floor(i / COLS) * H;
    layers.push({ input: await sharp(f).resize(W - 4, H - 22, { fit: 'cover' }).toBuffer(), left: x + 2, top: y + 2 });
    const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${W}" height="20"><text x="4" y="15" font-size="13" font-family="Arial" fill="#111">${part[i]}</text></svg>`;
    layers.push({ input: Buffer.from(svg), left: x, top: y + H - 20 });
  }
  const out = join(HERE, 'work', 'sheets', `${name}-${s + 1}.jpg`);
  await sharp({ create: { width: W * COLS, height: H * ROWS, channels: 3, background: '#fff' } }).composite(layers).jpeg({ quality: 70 }).toFile(out);
  console.log(out);
}
