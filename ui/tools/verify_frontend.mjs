import assert from 'node:assert/strict';
import { readFileSync, readdirSync, existsSync } from 'node:fs';
import { resolve } from 'node:path';

// Validate what Wails will embed, rather than only testing source components.
const directory = resolve('frontend/dist');
const html = readFileSync(resolve(directory, 'index.html'), 'utf8');
assert.match(html, /Content-Security-Policy/);
assert.match(html, /script-src 'self';/);
assert.match(html, /style-src 'self';/);
assert.doesNotMatch(html, /unsafe-inline|unsafe-eval|localhost|127\.0\.0\.1|\/src\//);
for (const script of html.matchAll(/<script\b([^>]*)>([\s\S]*?)<\/script>/g)) {
  assert.equal(script[2].trim(), '', 'production scripts must be external');
  assert.match(script[1], /src="\.\/assets\//);
}
const paths = [...html.matchAll(/(?:src|href)="(\.\/assets\/[^"?]+)"/g)].map(match => match[1]);
assert.ok(paths.length >= 2, 'the native entry needs bundled script and stylesheet assets');
for (const path of paths) {
  const target = resolve(directory, path);
  assert.ok(target.startsWith(directory + '/'), 'asset must stay in the build');
  assert.ok(existsSync(target), `missing embedded asset ${path}`);
}
for (const filename of readdirSync(resolve(directory, 'assets'))) {
  assert.doesNotMatch(filename, /coast|sunset|fixture/, 'development assets must not ship');
  if (filename.endsWith('.js')) {
    assert.doesNotMatch(readFileSync(resolve(directory, 'assets', filename), 'utf8'), /Development preview · Simulated data|demo-folder|browser-preview/);
  }
}
console.log('Embedded frontend: external assets, strict CSP and no development fixtures.');
