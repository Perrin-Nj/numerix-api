import rows from './catalog/photos.mjs';
console.log(rows.length, rows.map((r) => r[0]).join(','));
