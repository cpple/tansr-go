// GO-02：同一真实 Serve 源码的便携验收宿主。只写显式指定的归档目录；
// 不修改 CLI、不发布 bundle、不读取环境凭据。Go SDK 消费者不需要 Node。
import { createHash } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { mkdir, readFile, realpath, writeFile } from 'node:fs/promises';
import { isBuiltin, createRequire } from 'node:module';
import { dirname, isAbsolute, join, relative, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const integrationRoot = dirname(fileURLToPath(import.meta.url));
const goRoot = resolve(integrationRoot, '..');
const args = process.argv.slice(2);
const options = new Map();
for (let i = 0; i < args.length; i += 2) {
  if (!['--cli-root', '--source-commit', '--out'].includes(args[i]) || !args[i + 1] || options.has(args[i])) {
    throw new Error('usage: node integration/build-serve-fixture.mjs --cli-root CLI_ROOT --source-commit FULL_SHA --out ARCHIVE_DIRECTORY');
  }
  options.set(args[i], args[i + 1]);
}
if (options.size !== 3 || !/^[a-f0-9]{40}$/.test(options.get('--source-commit') ?? '')) throw new Error('A full reviewed source commit and all three arguments are required');
const cliRoot = await realpath(resolve(options.get('--cli-root')));
const outputRoot = resolve(options.get('--out'));
const expectedCommit = options.get('--source-commit');
const digest = bytes => createHash('sha256').update(bytes).digest('hex');
const slash = path => path.replaceAll('\\', '/');
const within = (parent, child) => { const path = relative(parent, child); return path === '' || (!isAbsolute(path) && path !== '..' && !path.startsWith(`..${process.platform === 'win32' ? '\\' : '/'}`)); };
if (within(cliRoot, outputRoot) || within(goRoot, outputRoot)) throw new Error('Build outputs must live in the task archive, outside both source repositories');
const git = (...command) => execFileSync('git', command, { cwd: cliRoot, encoding: 'utf8', windowsHide: true }).trim();
if (git('rev-parse', 'HEAD') !== expectedCommit) throw new Error('CLI source HEAD does not match --source-commit');
if (git('status', '--porcelain', '--untracked-files=no')) throw new Error('CLI tracked tree must be clean before making cross-platform evidence');
const sourceTree = git('rev-parse', 'HEAD^{tree}');

// Frozen source inputs are checked before bundling. A newer implementation
// commit may fix behavior but must not silently change the language SDK contract.
const lockPath = join(goRoot, 'contract', 'LOCK.json');
const lockBytes = await readFile(lockPath);
const lock = JSON.parse(lockBytes);
const rows = lock.files;
if (!Array.isArray(rows) || rows.length !== 39) throw new Error('Expected the reviewed 39-file SDK2/UAPI freeze lock');
for (const row of rows) {
  const source = row.source ?? row.sourcePath;
  if (typeof source !== 'string') throw new Error('Unsupported contract lock source entry');
  const actual = await readFile(join(cliRoot, source));
  if (digest(actual) !== row.sha256) throw new Error(`Frozen contract source differs: ${source}`);
}

const requireCLI = createRequire(join(cliRoot, 'package.json'));
const esbuild = requireCLI('esbuild');
// Reuse CLI's single-file ESM profile, including original text/catalog loaders,
// CJS shims and dependency plugins. Only entrypoint and output location change.
const { tansrdEsmProfile } = await import(pathToFileURL(join(cliRoot, 'build', 'config.mjs')).href);
const sourceProfile = tansrdEsmProfile({ isRelease: false, channel: 'dev' });
const fixturePath = join(integrationRoot, 'serve-fixture.mjs');
const fixtureBytes = await readFile(fixturePath);
const moduleInputs = new Set();
let source = fixtureBytes.toString('utf8').replace(/\bload\('([^']+)'\)/g, (_match, name) => {
  if (!/^(packages|tests)\/[A-Za-z0-9_./-]+\.ts$/.test(name) || name.includes('..')) throw new Error(`Unexpected fixture import: ${name}`);
  moduleInputs.add(name);
  return `import(${JSON.stringify(slash(join(cliRoot, name)))})`;
});
if (moduleInputs.size !== 10) throw new Error(`The reviewed real source module set changed (${moduleInputs.size}); review fixture input closure`);
source = source.replace(/^const load = relative => import\(pathToFileURL\(join\(cliRoot, relative\)\)\.href\);\r?\n/m, '');
const manifestStatement = "const contract = JSON.parse(await readFile(join(cliRoot, 'packages/server/contract/api-manifest.json'), 'utf8'));";
if (!source.includes(manifestStatement) || /\bload\(/.test(source)) throw new Error('Fixture load/manifest anchors changed; do not emit a partial source bundle');
source = source.replace(manifestStatement, `import contract from ${JSON.stringify(slash(join(cliRoot, 'packages/server/contract/api-manifest.json')))};`);
const executableName = 'serve-fixture.mjs';
const outputPath = join(outputRoot, executableName);
const { outdir: _outdir, outExtension: _extension, entryPoints: _entries, ...profile } = sourceProfile;
// This existing test helper reads schema-relative defaults at module load. Keep
// exactly the same JSON bytes/parse, but embed that immutable resource so the
// portable bundle does not acquire a fabricated ../../doc tree dependency.
const resourcePath = 'doc/rfc/sdk2-ext-v1.schema.json';
const resourceBytes = await readFile(join(cliRoot, resourcePath));
const helperPath = join(cliRoot, 'tests/sdk2/serve-archive-host.helper.ts');
const resourcePlugin = {
  name: 'go-fixture-frozen-resource',
  setup(build) {
    build.onLoad({ filter: /[\\/]serve-archive-host\.helper\.ts$/ }, async args => {
      if (resolve(args.path) !== resolve(helperPath)) return null;
      const content = await readFile(args.path, 'utf8');
      const anchor = "readFileSync(new URL('../../doc/rfc/sdk2-ext-v1.schema.json', import.meta.url), 'utf8')";
      if (content.split(anchor).length !== 2) throw new Error('Archive helper schema resource anchor changed');
      return { contents: content.replace(anchor, JSON.stringify(resourceBytes.toString('utf8'))), loader: 'ts', resolveDir: dirname(args.path) };
    });
  },
};
const buildOptions = {
  ...profile,
  absWorkingDir: cliRoot,
  stdin: { contents: source, resolveDir: integrationRoot, sourcefile: 'go-integration-fixture.mjs', loader: 'js' },
  outfile: outputPath,
  sourcemap: false,
  splitting: false,
  write: false,
  logLevel: 'warning',
  plugins: [...profile.plugins, resourcePlugin],
  // Match the original tsx test bootstrap; these are test-host metadata only.
  define: { ...profile.define, __VERSION__: JSON.stringify('0.0.0-dev'), __CHANNEL__: JSON.stringify('dev') },
};
const result = await esbuild.build(buildOptions);
if (result.outputFiles.length !== 1) throw new Error('Portable test host must remain a single emitted module');
const external = [...new Set(Object.values(result.metafile.outputs).flatMap(output => output.imports.filter(item => item.external).map(item => item.path)))].sort();
if (external.some(name => !isBuiltin(name))) throw new Error(`Non-builtin runtime dependency escaped bundling: ${external.filter(name => !isBuiltin(name)).join(', ')}`);

const inputs = [];
const virtualInputs = [];
for (const [name, details] of Object.entries(result.metafile.inputs).sort(([a], [b]) => a.localeCompare(b))) {
  if (name === 'go-integration-fixture.mjs' || resolve(cliRoot, name) === join(integrationRoot, 'go-integration-fixture.mjs') || /^[a-z][a-z0-9-]+:/.test(name) && !/^[A-Za-z]:[\\/]/.test(name)) {
    virtualInputs.push({ name, bytes: details.bytes, imports: details.imports });
    continue;
  }
  const path = resolve(cliRoot, name);
  const bytes = await readFile(path);
  inputs.push({ path: slash(relative(cliRoot, path)), bytes: bytes.length, sha256: digest(bytes) });
}
// Plugins are tracked by source tree identity; explicitly hash their files and
// package manifests too, so a consumer can inspect the build inputs without Git.
const trackedBuild = git('ls-files', '--', 'build', 'package.json', 'pnpm-lock.yaml', 'pnpm-workspace.yaml', 'tsconfig.json').split(/\r?\n/).filter(Boolean);
const buildInputs = [];
for (const name of trackedBuild) {
  const bytes = await readFile(join(cliRoot, name));
  buildInputs.push({ path: name, bytes: bytes.length, sha256: digest(bytes) });
}
const scriptBytes = await readFile(fileURLToPath(import.meta.url));
const output = result.outputFiles[0].contents;
const metadata = {
  format: 'tansr-go-serve-fixture-v1',
  generatedAt: new Date().toISOString(),
  purpose: 'Private portable integration host; not a Go consumer dependency or a distributable SDK payload',
  source: { repository: 'cpple/tansr', commit: expectedCommit, tree: sourceTree, trackedClean: true, moduleInputs: [...moduleInputs].sort() },
  tools: { node: process.version, esbuild: esbuild.version, sourceProfile: 'build/config.mjs#tansrdEsmProfile(dev)' },
  fixture: { path: 'integration/serve-fixture.mjs', bytes: fixtureBytes.length, sha256: digest(fixtureBytes), transformedInputSha256: digest(Buffer.from(source)) },
  builder: { path: 'integration/build-serve-fixture.mjs', bytes: scriptBytes.length, sha256: digest(scriptBytes) },
  freezeLock: { path: 'contract/LOCK.json', bytes: lockBytes.length, sha256: digest(lockBytes), checkedSourceFiles: rows.length },
  output: { file: executableName, bytes: output.length, sha256: digest(output), externalBuiltins: external },
  inputs, buildInputs, virtualInputs,
  embeddedRuntimeResources: [{ path: resourcePath, bytes: resourceBytes.length, sha256: digest(resourceBytes),
    consumer: 'tests/sdk2/serve-archive-host.helper.ts', transformation: 'same UTF-8 file content embedded in original JSON.parse expression' }],
};
if (git('rev-parse', 'HEAD') !== expectedCommit || git('status', '--porcelain', '--untracked-files=no')) throw new Error('CLI source changed while building; discard candidate');
await mkdir(outputRoot, { recursive: true });
await writeFile(outputPath, output, { flag: 'wx' });
await writeFile(join(outputRoot, 'serve-fixture.provenance.json'), JSON.stringify(metadata, null, 2) + '\n', { flag: 'wx' });
console.log(JSON.stringify({ ...metadata.output, sourceCommit: expectedCommit, inputFiles: inputs.length, archive: slash(outputRoot) }));
