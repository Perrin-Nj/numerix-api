// node framesheet.mjs <name> id1,id2,...  : one row per clip (4 frames), 6 clips per sheet -> work/sheets/<name>-<n>.jpg
import sharp from 'sharp';
import { join } from 'node:path';
import { mkdirSync, existsSync } from 'node:fs';
import { HERE } from './commons.mjs';

const [name, list] = process.argv.slice(2);
const ids = list.split(',').filter(Boolean);
const FW = 300, FH = 170, LAB = 18, ROWS = 6;
mkdirSync(join(HERE, 'work', 'sheets'), { recursive: true });
for (let s = 0; s * ROWS < ids.length; s++) {
  const part = ids.slice(s * ROWS, (s + 1) * ROWS);
  const layers = [];
  for (let r = 0; r < part.length; r++) {
    const y = r * (FH + LAB);
    for (let i = 0; i < 4; i++) {
      const f = join(HERE, 'work', 'frames', `${part[r]}-${i}.jpg`);
      if (existsSync(f)) layers.push({ input: await sharp(f).resize(FW - 2, FH, { fit: 'cover' }).toBuffer(), left: i * FW, top: y });
    }
    layers.push({ input: Buffer.from(`<svg xmlns="http://www.w3.org/2000/svg" width="${FW * 4}" height="${LAB}"><text x="4" y="13" font-size="13" font-family="Arial" fill="#111">${part[r]}</text></svg>`), left: 0, top: y + FH });
  }
  const out = join(HERE, 'work', 'sheets', `${name}-${s + 1}.jpg`);
  await sharp({ create: { width: FW * 4, height: (FH + LAB) * ROWS, channels: 3, background: '#fff' } }).composite(layers).jpeg({ quality: 70 }).toFile(out);
  console.log(out);
}
