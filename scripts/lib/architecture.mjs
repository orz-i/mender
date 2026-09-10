import ts from 'typescript';

const pureLayers = new Set(['domain', 'application']);
const ambientNames = new Set(['fetch', 'window', 'document', 'localStorage', 'sessionStorage', 'globalThis', 'process', 'XMLHttpRequest', 'WebSocket']);

export function classify(file) {
  const parts = file.replaceAll('\\', '/').split('/');
  if (parts[0] !== 'frontend') return {};
  if (parts[1] === 'packages') return { package: parts[2], kind: 'package' };
  if (parts[1] !== 'apps') return {};
  const result = { app: parts[2], kind: 'app' };
  if (parts[3] === 'src' && parts[4] === 'modules') {
    result.module = parts[5];
    result.layer = /^index\.[cm]?[jt]sx?$/.test(parts[6] ?? '') ? 'public' : parts[6];
  }
  return result;
}

function boundaryProblem(source, target) {
  const from = classify(source);
  const to = classify(target);
  if (from.app && to.app && from.app !== to.app) return 'Console and Admin cannot import each other';
  if (from.kind === 'package' && to.kind === 'app') return 'shared packages cannot depend on applications';
  if (from.package === 'ui' && to.package && to.package !== 'ui') return 'shared UI cannot depend on application packages';
  if (!from.module) return null;
  if (from.layer === 'presentation' && to.package === 'api-client') return 'presentation uses application ports, not the transport client';
  if (pureLayers.has(from.layer)) {
    const allowed = from.layer === 'domain' ? ['domain'] : ['domain', 'application'];
    if (from.app !== to.app || from.module !== to.module || !allowed.includes(to.layer)) {
      return `${from.layer} may only import its own inward layers`;
    }
  } else if (to.module && (to.app !== from.app || to.module !== from.module) && to.layer !== 'public') {
    return 'cross-module imports must use the target module public index';
  } else if (from.layer === 'infrastructure' && from.module === to.module && ['presentation', 'public'].includes(to.layer)) {
    return 'infrastructure cannot depend on presentation or its re-export facade';
  } else if (from.layer === 'presentation' && from.module === to.module && ['infrastructure', 'public'].includes(to.layer)) {
    return 'presentation receives ports from app composition, not concrete infrastructure';
  }
  return null;
}

function owningPackage(file, packages) {
  return packages.find((pkg) => file === pkg.root || file.startsWith(`${pkg.root}/`));
}

function exported(pkg, specifier) {
  const key = specifier === pkg.name ? '.' : `.${specifier.slice(pkg.name.length)}`;
  const exp = pkg.exports;
  if (typeof exp === 'string') return key === '.';
  if (!exp || typeof exp !== 'object') return false;
  if (!Object.keys(exp).some((name) => name.startsWith('.'))) return key === '.';
  return Object.keys(exp).some((pattern) => {
    if (exp[pattern] == null) return false;
    if (pattern === key) return true;
    const star = pattern.indexOf('*');
    return star >= 0 && key.startsWith(pattern.slice(0, star)) && key.endsWith(pattern.slice(star + 1));
  });
}

// resolve() is injected: the CLI uses TypeScript's actual tsconfig/module resolver.
// A null target means an external library, not permission to import it into pure layers.
export function analyzeFrontend(files, { resolve, packages = [] }) {
  const problems = [];
  const graph = new Map();
  const known = new Set(files.map((file) => file.path));
  for (const file of files) {
    const ast = ts.createSourceFile(file.path, file.content, ts.ScriptTarget.Latest, true,
      file.path.endsWith('.tsx') || file.path.endsWith('.jsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS);
    const from = classify(file.path);
    const owner = owningPackage(file.path, packages);
    const edges = new Set();
    graph.set(file.path, edges);
    const report = (node, rule, detail) => problems.push({
      file: file.path, line: ast.getLineAndCharacterOfPosition(node.getStart(ast)).line + 1, rule, detail,
    });
    for (const diagnostic of ast.parseDiagnostics) {
      problems.push({ file: file.path, line: ast.getLineAndCharacterOfPosition(diagnostic.start ?? 0).line + 1,
        rule: 'TS_PARSE', detail: ts.flattenDiagnosticMessageText(diagnostic.messageText, '\n') });
    }
    if (from.module && !['domain', 'application', 'infrastructure', 'presentation', 'public'].includes(from.layer)) {
      report(ast, 'TS_LAYER', 'module source must have an explicit layer or public index');
    }
    const dependency = (node, specifier) => {
      const pkg = packages.find((candidate) => specifier === candidate.name || specifier.startsWith(`${candidate.name}/`));
      if (pkg) {
        if (!exported(pkg, specifier)) report(node, 'TS_EXPORTS', `not an exported workspace entry: ${specifier}`);
        if (owner && owner.name !== pkg.name && !owner.dependencies?.[pkg.name]?.startsWith('workspace:')) {
          report(node, 'TS_WORKSPACE', `declare ${pkg.name} with workspace: in ${owner.name}`);
        }
      }
      const target = resolve(file.path, specifier);
      if (target && known.has(target)) edges.add(target);
      if (target && owner) {
        const targetOwner = owningPackage(target, packages);
        if (targetOwner && targetOwner.name !== owner.name && (!pkg || pkg.name !== targetOwner.name)) {
          report(node, 'TS_PACKAGE_ENTRY', 'cross-package access must use the declared workspace export');
        }
      }
      if (target) {
        const reason = boundaryProblem(file.path, target);
        if (reason) report(node, 'TS_BOUNDARY', `${specifier}: ${reason}`);
      } else if (pureLayers.has(from.layer)) {
        report(node, 'TS_PURE_IMPORT', `${from.layer} cannot import external or unresolved module ${specifier}`);
      } else if (specifier.startsWith('.') || specifier.startsWith('@mender/')) {
        report(node, 'TS_UNRESOLVED', `local/workspace import could not be resolved: ${specifier}`);
      }
    };
    const visit = (node) => {
      if ((ts.isImportDeclaration(node) || ts.isExportDeclaration(node)) && node.moduleSpecifier && ts.isStringLiteralLike(node.moduleSpecifier)) {
        dependency(node, node.moduleSpecifier.text);
      } else if (ts.isImportTypeNode(node) && ts.isLiteralTypeNode(node.argument) && ts.isStringLiteralLike(node.argument.literal)) {
        dependency(node, node.argument.literal.text);
      } else if (ts.isImportEqualsDeclaration(node) && ts.isExternalModuleReference(node.moduleReference)) {
        report(node, 'TS_REQUIRE', 'CommonJS import-equals is not allowed in the frontend ESM workspace');
      } else if (ts.isCallExpression(node) && (node.expression.kind === ts.SyntaxKind.ImportKeyword || (ts.isIdentifier(node.expression) && node.expression.text === 'require'))) {
        const arg = node.arguments[0];
        if (!arg || !ts.isStringLiteralLike(arg)) report(node, 'TS_DYNAMIC_IMPORT', 'non-literal module loading requires a separately reviewed boundary');
        else dependency(node, arg.text);
      }
      if (pureLayers.has(from.layer)) {
        if (ts.isJsxElement(node) || ts.isJsxSelfClosingElement(node) || ts.isJsxFragment(node)) report(node, 'TS_JSX', 'JSX belongs in presentation');
        if (ts.isIdentifier(node) && ambientNames.has(node.text)) {
          const parent = node.parent;
          const isPropertyName = (ts.isPropertyAccessExpression(parent) && parent.name === node)
            || ((ts.isPropertyAssignment(parent) || ts.isPropertySignature(parent)) && parent.name === node);
          if (!isPropertyName) report(node, 'TS_AMBIENT_IO', `inject a port instead of referencing ${node.text}`);
        }
      }
      ts.forEachChild(node, visit);
    };
    visit(ast);
  }
  const complete = new Set();
  const active = new Set();
  const walk = (name, trail) => {
    if (active.has(name)) {
      problems.push({ file: name, line: 1, rule: 'TS_CYCLE', detail: [...trail, name].join(' -> ') });
      return;
    }
    if (complete.has(name)) return;
    active.add(name);
    for (const target of graph.get(name) ?? []) walk(target, [...trail, name]);
    active.delete(name);
    complete.add(name);
  };
  for (const name of graph.keys()) walk(name, []);
  return problems;
}
