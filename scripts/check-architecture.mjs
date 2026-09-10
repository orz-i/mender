import { readFileSync, existsSync } from 'node:fs';
import { resolve, relative, dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import ts from 'typescript';
import { analyzeFrontend } from './lib/architecture.mjs';
import { walkFiles, readJSON } from './lib/files.mjs';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const relativeName = (file) => relative(root, file).replaceAll('\\', '/');
const all = walkFiles(join(root, 'frontend'));
const packages = all.filter((file) => file.endsWith('package.json')).map((file) => {
  const manifest = readJSON(file);
  return { name: manifest.name, root: relativeName(dirname(file)), exports: manifest.exports,
    dependencies: { ...manifest.dependencies, ...manifest.devDependencies, ...manifest.peerDependencies } };
});
const options = new Map();
const host = { ...ts.sys, getCurrentDirectory: () => root };
function compilerOptions(file) {
  const config = ts.findConfigFile(dirname(file), ts.sys.fileExists, 'tsconfig.json');
  if (!config) throw new Error(`No tsconfig for ${relativeName(file)}`);
  if (!options.has(config)) {
    const parsed = ts.getParsedCommandLineOfConfigFile(config, {}, { ...host, onUnRecoverableConfigFileDiagnostic: (error) => {
      throw new Error(ts.flattenDiagnosticMessageText(error.messageText, '\n'));
    } });
    if (!parsed || parsed.errors.length) throw new Error(`Cannot parse ${relativeName(config)}: ${parsed?.errors.map((error) => ts.flattenDiagnosticMessageText(error.messageText, '\n')).join('; ')}`);
    options.set(config, parsed.options);
  }
  return options.get(config);
}

// Stylesheet exports are not JavaScript modules, but still obey workspace/package boundaries.
function resolveStyle(source, specifier) {
  if (!specifier.endsWith('.css')) return null;
  let target;
  if (specifier.startsWith('.')) target = resolve(dirname(source), specifier);
  else {
    const pkg = packages.find((item) => specifier.startsWith(`${item.name}/`));
    const exp = pkg?.exports?.[`.${specifier.slice(pkg.name.length)}`];
    if (typeof exp === 'string') target = resolve(root, pkg.root, exp);
  }
  return target && existsSync(target) ? relativeName(target) : null;
}

const files = all.filter((file) => /\.(?:[cm]?[jt]sx?)$/.test(file)).map((file) => ({ path: relativeName(file), content: readFileSync(file, 'utf8') }));
if (files.length === 0) throw new Error('No frontend source files found; refusing an empty architecture pass');
const problems = analyzeFrontend(files, {
  packages,
  resolve: (source, specifier) => {
    const absolute = resolve(root, source);
    const result = ts.resolveModuleName(specifier, absolute, compilerOptions(absolute), host).resolvedModule;
    if (result) {
      const target = relativeName(result.resolvedFileName);
      return target.startsWith('frontend/') ? target : null;
    }
    return resolveStyle(absolute, specifier);
  },
});
for (const issue of problems) console.error(`${issue.file}:${issue.line} [${issue.rule}] ${issue.detail}`);
if (problems.length) process.exitCode = 1;
else console.log(`Frontend architecture: ${files.length} source files checked; resolved imports, module boundaries and source cycles passed.`);
