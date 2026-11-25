import { execFileSync } from 'node:child_process';
import { existsSync, readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { dirname, join, relative, resolve, sep } from 'node:path';
import { fileURLToPath } from 'node:url';
import { build } from 'vite';

const [cli, native, output, menu] = process.argv.slice(2).map(path => resolve(path));
if (!cli || !native || !output) {
  throw new Error('Usage: node tools/bundled-notices.mjs CLI NATIVE OUTPUT [MENU]');
}

const root = fileURLToPath(new URL('../', import.meta.url));
process.chdir(root);

const groups = new Map();
const noticeName = /^(?:licen[cs]e|copying|copyright|notice|patents)(?:$|[._-]|Notice)/i;
const projectModule = 'github.com/vadim82/panora';

function groupFor(label) {
  if (!groups.has(label)) groups.set(label, new Map());
  return groups.get(label);
}

function collectNotices(label, base, start = base) {
  const files = groupFor(label);
  let directory = start;
  while (directory === base || directory.startsWith(base + sep)) {
    for (const entry of readdirSync(directory, { withFileTypes: true })) {
      if (!entry.isFile() || !noticeName.test(entry.name)) continue;
      const path = join(directory, entry.name);
      const name = relative(base, path);
      if (!files.has(name)) files.set(name, readFileSync(path, 'utf8'));
    }
    if (directory === base) break;
    directory = dirname(directory);
  }
}

function go(args, env = process.env) {
  return execFileSync('go', args, { encoding: 'utf8', env }).trimEnd();
}

const goRoot = go(['env', 'GOROOT']);
const goVersion = go(['env', 'GOVERSION']);
const sdkLabel = `Go toolchain ${goVersion}`;
collectNotices(sdkLabel, goRoot);
const archGo = goRoot === '/usr/lib/go';
// Arch installs LICENSE separately and can omit PATENTS. The pinned
// upstream copy is byte-identical for Go 1.27.3 and 1.27.4.
const verifiedPatentsVersion = ['go1.27.3', 'go1.27.4'].includes(goVersion.split('-')[0]);
const sdkFallbacks = {
  LICENSE: archGo ? '/usr/share/licenses/go/LICENSE' : null,
  PATENTS: archGo && verifiedPatentsVersion
    ? new URL('./notices/go1.27.4.PATENTS', import.meta.url)
    : null,
};
for (const [name, fallback] of Object.entries(sdkFallbacks)) {
  const sdkPath = join(goRoot, name);
  const source = existsSync(sdkPath) ? sdkPath : fallback;
  if (!source || !existsSync(source)) throw new Error(`Missing ${sdkLabel} ${name}`);
  const contents = readFileSync(source, 'utf8');
  if (!contents.trim()) throw new Error(`Empty ${sdkLabel} ${name}`);
  groupFor(sdkLabel).set(name, contents);
}
const packageFormat = '{{.Dir}}\t{{with .Module}}{{.Path}}\t{{.Version}}\t{{.Dir}}{{end}}';

function collectGo(binary, args, target) {
  const info = JSON.parse(go(['version', '-m', '-json', binary]));
  if (!info.GoVersion || info.GoVersion.split(' ')[0] !== goVersion) {
    throw new Error(`Use ${info.GoVersion} to collect notices for ${binary}`);
  }
  const settings = Object.fromEntries(info.Settings.map(({ Key, Value }) => [Key, Value]));
  const env = { ...process.env };
  for (const key of ['GOOS', 'GOARCH', 'CGO_ENABLED']) {
    if (!settings[key]) throw new Error(`Missing ${key} build setting for ${binary}`);
    env[key] = settings[key];
  }
  const tags = settings['-tags'] ? ['-tags', settings['-tags']] : [];
  const modules = new Set(
    info.Deps.filter(({ Path }) => Path !== projectModule).map(({ Path, Version }) => `${Path}@${Version}`),
  );
  const packages = go([...args, 'list', '-deps', '-f', packageFormat, ...tags, target], env).split('\n');
  for (const entry of packages) {
    const [directory, module, version, moduleRoot] = entry.split('\t');
    if (!module) {
      if (directory.startsWith(join(goRoot, 'src') + sep)) {
        collectNotices(sdkLabel, goRoot, directory);
      }
      continue;
    }
    const dependency = `${module}@${version}`;
    if (modules.has(dependency)) collectNotices(`Go ${dependency}`, moduleRoot, directory);
  }
  for (const dependency of modules) {
    if (!groups.get(`Go ${dependency}`)?.size) throw new Error(`Missing Go notices for ${dependency}`);
  }
}

collectGo(cli, ['-C', '..'], './cmd/panora');
collectGo(native, [], '.');
if (menu) collectGo(menu, [], './cmd/menu-bar');

function packageDirectory(id) {
  const marker = '/node_modules/';
  const index = id.lastIndexOf(marker);
  if (index < 0) return null;
  const prefix = id.slice(0, index + marker.length);
  const parts = id.slice(prefix.length).split('/');
  return prefix + parts.slice(0, parts[0].startsWith('@') ? 2 : 1).join('/');
}

const require = createRequire(import.meta.url);
const vitePackage = require.resolve('vite/package.json');
const requireFromVite = createRequire(vitePackage);
const bundledHelpers = new Map([
  ['\0vite/modulepreload-polyfill.js', dirname(vitePackage)],
  ['\0rolldown/runtime.js', dirname(requireFromVite.resolve('rolldown/package.json'))],
]);

const rendered = new Map();
await build({
  build: { write: false },
  plugins: [
    {
      name: 'panora-bundled-notices',
      generateBundle(_options, bundle) {
        for (const item of Object.values(bundle)) {
          if (item.type !== 'chunk') continue;
          for (const [id, module] of Object.entries(item.modules)) {
            if (module.renderedLength <= 0) continue;
            const directory = id.startsWith('\0') ? bundledHelpers.get(id) : packageDirectory(id);
            if (directory) rendered.set(id, directory);
          }
        }
      },
    },
  ],
});

if (!rendered.size) throw new Error('No third-party frontend modules found in the production bundle');
for (const [id, directory] of rendered) {
  const { name, version } = JSON.parse(readFileSync(join(directory, 'package.json'), 'utf8'));
  const dependency = `${name}@${version}`;
  const helper = bundledHelpers.has(id);
  const label = `${helper ? 'Frontend helper' : 'Frontend'} ${dependency}`;
  collectNotices(label, directory, helper ? directory : dirname(id.split('?')[0]));
  if (!groupFor(label).size) throw new Error(`Missing frontend notices for ${dependency}`);
}

const text = ['Panora bundled third-party notices', ''];
for (const label of [...groups.keys()].sort()) {
  const files = groupFor(label);
  for (const name of [...files.keys()].sort()) {
    text.push(`----- ${label} ${name} -----`, files.get(name), '');
  }
}
writeFileSync(output, text.join('\n'));
