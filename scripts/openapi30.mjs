// Turns the OpenAPI 3.1 contract into a 3.0.3 copy that oapi-codegen understands.
// Run: node scripts/openapi30.mjs  (reads ../frontend/contracts/openapi.yaml, writes api/openapi30.yaml)
// Changes: type [X,'null'] -> type X + nullable; other type arrays -> no type; const -> enum; examples -> example.
import { readFileSync, writeFileSync } from 'node:fs';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const yaml = require('../../frontend/node_modules/.pnpm/js-yaml@4.3.2/node_modules/js-yaml');

const spec = yaml.load(readFileSync('../frontend/contracts/openapi.yaml', 'utf8'));
spec.openapi = '3.0.3';

function fix(node) {
  if (Array.isArray(node)) return node.forEach(fix);
  if (!node || typeof node !== 'object') return;
  if (Array.isArray(node.type)) {
    const real = node.type.filter((t) => t !== 'null');
    if (real.length < node.type.length) node.nullable = true;
    if (real.length === 1) node.type = real[0];
    else delete node.type; // mixed scalar: left open, the Go type becomes interface{}
  }
  if ('const' in node) {
    node.enum = [node.const];
    delete node.const;
  }
  if (Array.isArray(node.examples) && !('content' in node)) {
    node.example = node.examples[0];
    delete node.examples;
  }
  Object.values(node).forEach(fix);
}
fix(spec);

writeFileSync('api/openapi30.yaml', yaml.dump(spec, { lineWidth: -1, noRefs: true }));
console.log('wrote api/openapi30.yaml');
