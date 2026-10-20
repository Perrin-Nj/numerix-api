// Video and animation pipeline. node clips.mjs <videos|anims> [--only=id,id]
// download (Commons transcode 480p when the original is large) -> cut -> H.264 MP4 under a size budget -> 4 review frames.
// Output: work/clips/<id>.mp4, work/frames/<id>-<n>.jpg, work/clips.json (size, duration, size, licence data).
import { readFileSync, writeFileSync, existsSync, mkdirSync, statSync } from 'node:fs';
import { execFileSync } from 'node:child_process';
import { join } from 'node:path';
import { createRequire } from 'node:module';
import { HERE, byTitle, download, resolvePrefix } from './commons.mjs';

const require = createRequire(import.meta.url);
const FFMPEG = require('ffmpeg-static');
const FFPROBE = require('ffprobe-static').path;
const kind = process.argv[2];
const only = (process.argv.find((a) => a.startsWith('--only='))?.slice(7) || '').split(',').filter(Boolean);
const rows = (await import(kind === 'videos' ? './catalog/videos.mjs' : './catalog/anims.mjs')).default;
const W = join(HERE, 'work');
for (const d of ['src', 'clips', 'frames']) mkdirSync(join(W, d), { recursive: true });
const OUT = join(W, 'clips.json');
const info = existsSync(OUT) ? JSON.parse(readFileSync(OUT, 'utf8')) : {};
const UA = 'NumerixFeedTools/1.0 (wandjiperrin@gmail.com)';

const probe = (f) => JSON.parse(execFileSync(FFPROBE, ['-v', 'error', '-print_format', 'json', '-show_format', '-show_streams', f]).toString());
const ff = (args) => execFileSync(FFMPEG, ['-v', 'error', '-y', ...args], { stdio: ['ignore', 'inherit', 'inherit'] });

async function source(c) {
  const u = new URL(c.url);
  u.search = '';
  const parts = u.pathname.split('/');
  const name = parts.pop(), h2 = parts.pop(), h1 = parts.pop();
  const ext = name.split('.').pop().toLowerCase();
  const dest = (e) => join(W, 'src', c.title.replace(/[^a-z0-9]+/gi, '_').slice(5, 60) + '.' + e);
  if (kind === 'videos' && c.size > 25e6) {
    for (const suf of ['.480p.vp9.webm', '.480p.webm', '.360p.vp9.webm']) {
      const t = `https://upload.wikimedia.org/wikipedia/commons/transcoded/${h1}/${h2}/${name}/${name}${suf}`;
      const r = await fetch(t, { method: 'HEAD', headers: { 'User-Agent': UA } });
      if (r.status === 200) return download(t, dest('webm'));
    }
  }
  if (c.size > 80e6) throw new Error('source too large and no transcode: ' + c.title);
  return download(u.toString(), dest(ext));
}

for (const r of rows) {
  if (only.length && !only.includes(r.id)) continue;
  if (info[r.id]?.done && !only.length) continue;
  const title = r.file.endsWith('*') ? await resolvePrefix(r.file.slice(0, -1)) : 'File:' + r.file;
  const c = title ? await byTitle(title) : null;
  if (!c || !c.ok) { console.log('SKIP (missing or licence)', r.id, r.file, c?.licence); continue; }
  const src = await source(c);
  const p = probe(src);
  const v = p.streams.find((s) => s.codec_type === 'video');
  const hasAudio = p.streams.some((s) => s.codec_type === 'audio');
  const total = parseFloat(p.format.duration) || c.dur;
  const start = r.start || 0;
  let dur = Math.min(r.dur || 20, total - start);
  const out = join(W, 'clips', r.id + '.mp4');
  const audio = !!r.audio && hasAudio;
  const loop = kind === 'anims' && dur < 6 ? Math.ceil(6 / dur) - 1 : 0; // short loops play 2 to 3 times in the file
  if (loop) dur = dur * (loop + 1);
  const H = kind === 'anims' ? Math.min(540, v.height) : 480;
  const budget = kind === 'anims' ? 300 : 780; // KB
  const vkbps = Math.max(120, Math.floor((budget * 8) / dur - (audio ? 48 : 0)));
  const vf = `scale=-2:${H % 2 ? H - 1 : H}:flags=lanczos,fps=${kind === 'anims' ? 20 : 24},format=yuv420p`;
  const args = [...(loop ? ['-stream_loop', String(loop)] : []), ...(start ? ['-ss', String(start)] : []), '-i', src, '-t', dur.toFixed(2), '-vf', vf,
    '-c:v', 'libx264', '-preset', 'slow', '-profile:v', 'main', '-crf', '27', '-maxrate', vkbps + 'k', '-bufsize', vkbps * 2 + 'k', '-movflags', '+faststart',
    ...(audio ? ['-af', 'loudnorm=I=-26:TP=-3:LRA=7,volume=0.8', '-c:a', 'aac', '-b:a', '48k', '-ac', '1'] : ['-an']), out];
  ff(args);
  const q = probe(out);
  const qv = q.streams.find((s) => s.codec_type === 'video');
  const size = statSync(out).size;
  for (let i = 0; i < 4; i++) {
    ff(['-ss', ((parseFloat(q.format.duration) * (0.1 + i * 0.27))).toFixed(2), '-i', out, '-frames:v', '1', '-vf', 'scale=320:-2', join(W, 'frames', `${r.id}-${i}.jpg`)]);
  }
  info[r.id] = { done: true, pick: c, size, dur: parseFloat(q.format.duration), w: qv.width, h: qv.height, audio, srcDur: total };
  console.log(`ok ${r.id} ${(size / 1024) | 0}KB ${info[r.id].dur.toFixed(1)}s ${qv.width}x${qv.height}${audio ? ' audio' : ''} <- ${c.licence}`);
  writeFileSync(OUT, JSON.stringify(info, null, 1));
}
