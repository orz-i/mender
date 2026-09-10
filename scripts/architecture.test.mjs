import test from 'node:test';
import assert from 'node:assert/strict';
import ts from 'typescript';
import { analyzeFrontend } from './lib/architecture.mjs';

const prefix = 'frontend/apps/console/src/modules/';
function inspect(content, { source = `${prefix}runs/domain/run.ts`, target = null, other = [], packages = [] } = {}) {
  const files = [{ path: source, content }, ...other];
  return analyzeFrontend(files, { packages, resolve: (_source, specifier) => typeof target === 'function' ? target(_source, specifier) : target });
}
function rejects(rule, content, options) {
  assert.ok(inspect(content, options).some((issue) => issue.rule === rule), `expected ${rule}`);
}

test('pure layers reject transport, framework and reverse dependencies', () => {
  rejects('TS_PURE_IMPORT', "import type { QueryClient } from '@tanstack/react-query';");
  rejects('TS_BOUNDARY', "import type { Run } from '../infrastructure/api';", { target: `${prefix}runs/infrastructure/api.ts` });
  rejects('TS_BOUNDARY', "import type { Policy } from '@alias/other';", { target: `${prefix}billing/domain/policy.ts` });
  rejects('TS_BOUNDARY', "export * from '../presentation/page';", { source: `${prefix}runs/application/query.ts`, target: `${prefix}runs/presentation/page.tsx` });
});

test('type imports, re-exports and static dynamic imports are inspected', () => {
  const target = `${prefix}billing/domain/private.ts`;
  for (const content of ["export type { X } from '@alias/private';", "type X = import('@alias/private').X;", "const x = import('@alias/private');", "const x = require('@alias/private');"]) {
    rejects('TS_BOUNDARY', content, { source: `${prefix}runs/presentation/page.ts`, target });
  }
  rejects('TS_DYNAMIC_IMPORT', 'const x = import(selectedPath);');
  rejects('TS_DYNAMIC_IMPORT', 'const x = require(selectedPath);');
  rejects('TS_REQUIRE', "import x = require('somewhere');");
});

test('ambient effects and JSX cannot enter pure business layers', () => {
  for (const content of ['const send = fetch;', 'const token = window.localStorage;', "const token = globalThis['localStorage'];", 'const env = process.env;', 'const call = new WebSocket(url);']) rejects('TS_AMBIENT_IO', content);
  rejects('TS_JSX', 'export const view = <div />;', { source: `${prefix}runs/domain/run.tsx` });
  assert.deepEqual(inspect('export type Port = { fetch: () => Promise<void> };'), []);
});

test('cross-app private imports and shared UI business imports fail', () => {
  rejects('TS_BOUNDARY', "import { x } from '../../../../admin/src/app/page';", { source: 'frontend/apps/console/src/app/page.tsx', target: 'frontend/apps/admin/src/app/page.tsx' });
  rejects('TS_BOUNDARY', "import { x } from '@alias/api';", { source: 'frontend/packages/ui/src/button.ts', target: 'frontend/packages/api-client/src/index.ts' });
  rejects('TS_BOUNDARY', "import { x } from '@alias/app';", { source: 'frontend/packages/api-client/src/index.ts', target: 'frontend/apps/console/src/app/page.tsx' });
});

test('workspace imports require declared workspace protocol and exported entry', () => {
  const source = 'frontend/apps/console/src/app/page.tsx';
  const packages = [
    { name: '@mender/console', root: 'frontend/apps/console', dependencies: { '@mender/ui': '^1.0.0' } },
    { name: '@mender/ui', root: 'frontend/packages/ui', exports: { '.': './src/index.ts' } },
  ];
  rejects('TS_WORKSPACE', "import { x } from '@mender/ui';", { source, packages, target: 'frontend/packages/ui/src/index.ts' });
  packages[0].dependencies['@mender/ui'] = 'workspace:*';
  rejects('TS_EXPORTS', "import { x } from '@mender/ui/src/private';", { source, packages, target: 'frontend/packages/ui/src/private.ts' });
  rejects('TS_PACKAGE_ENTRY', "import { x } from '../../../packages/ui/src/index';", { source, packages, target: 'frontend/packages/ui/src/index.ts' });
  assert.deepEqual(inspect("import { x } from '@mender/ui';", { source, packages, target: 'frontend/packages/ui/src/index.ts' }), []);
});

test('pure intra-module ports and app composition remain allowed', () => {
  assert.deepEqual(inspect("import type { Run } from '../domain/run';", { source: `${prefix}runs/application/query.ts`, target: `${prefix}runs/domain/run.ts` }), []);
  assert.deepEqual(inspect("import { createGateway } from '../modules/runs';", { source: 'frontend/apps/console/src/app/dependencies.ts', target: `${prefix}runs/index.ts` }), []);
  assert.deepEqual(inspect("import { Card } from '../../other';", { source: `${prefix}runs/presentation/page.tsx`, target: `${prefix}other/index.ts` }), []);
});

test('presentation cannot instantiate its own concrete infrastructure', () => {
  rejects('TS_BOUNDARY', "import { createGateway } from '../infrastructure/gateway';", { source: `${prefix}runs/presentation/page.tsx`, target: `${prefix}runs/infrastructure/gateway.ts` });
  rejects('TS_BOUNDARY', "import { readRun } from '@mender/api-client';", { source: `${prefix}runs/presentation/page.tsx`, target: 'frontend/packages/api-client/src/index.ts' });
});

test('actual TypeScript path-alias resolution cannot hide a cross-app dependency', () => {
  const source = 'frontend/apps/console/src/app/page.ts';
  const target = 'frontend/apps/admin/src/private.ts';
  const root = '/mender-virtual-fixture/';
  const contents = new Map([[root + source, "export type { Secret } from '@admin/private';"], [root + target, 'export type Secret = string;']]);
  const host = {
    fileExists: (name) => contents.has(name.replaceAll('\\', '/')),
    readFile: (name) => contents.get(name.replaceAll('\\', '/')),
    directoryExists: (name) => [...contents.keys()].some((file) => file.startsWith(`${name.replaceAll('\\', '/').replace(/\/$/, '')}/`)),
    getCurrentDirectory: () => root,
  };
  const options = { moduleResolution: ts.ModuleResolutionKind.Bundler, baseUrl: root, paths: { '@admin/*': ['frontend/apps/admin/src/*'] } };
  const result = ts.resolveModuleName('@admin/private', root + source, options, host).resolvedModule;
  assert.equal(result?.resolvedFileName, root + target);
  const issues = analyzeFrontend([{ path: source, content: contents.get(root + source) }], {
    resolve: (file, specifier) => ts.resolveModuleName(specifier, root + file, options, host).resolvedModule?.resolvedFileName.slice(root.length) ?? null,
  });
  assert.ok(issues.some((issue) => issue.rule === 'TS_BOUNDARY'));
});

test('cycles, unresolved local modules, malformed source and unclassified layers fail', () => {
  const a = `${prefix}runs/domain/a.ts`; const b = `${prefix}runs/domain/b.ts`;
  rejects('TS_CYCLE', "export type { B } from './b';", { source: a, target: (source) => source === a ? b : a, other: [{ path: b, content: "export type { A } from './a';" }] });
  rejects('TS_UNRESOLVED', "import { x } from './missing';", { source: `${prefix}runs/presentation/page.tsx` });
  rejects('TS_PARSE', 'export type = ;');
  rejects('TS_LAYER', 'export const x = 1;', { source: `${prefix}runs/helpers/escape.ts` });
});
