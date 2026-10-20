import v from './catalog/videos.mjs'; import vt from './catalog/videotext.mjs'; import a from './catalog/anims.mjs'; import at from './catalog/animtext.mjs'; import p from './catalog/photos.mjs';
const ids=(r)=>r.map(x=>Array.isArray(x)?x[0]:x.id);
const diff=(x,y)=>[ids(x).filter(i=>!ids(y).includes(i)), ids(y).filter(i=>!ids(x).includes(i))];
console.log('videos',v.length,vt.length,diff(v,vt)); console.log('anims',a.length,at.length,diff(a,at)); console.log('photos',p.length);
