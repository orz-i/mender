import { createHash, createPrivateKey, createPublicKey, sign, verify } from 'node:crypto';
import { lstatSync, readFileSync, realpathSync } from 'node:fs';
import { isAbsolute, relative, resolve } from 'node:path';

const sha256 = (bytes) => createHash('sha256').update(bytes).digest('hex');
const fail = (condition, code) => { if (!condition) throw new Error(code); };
const keys = (value, allowed) => value && typeof value === 'object' && !Array.isArray(value)
  && Object.keys(value).sort().join('|') === [...allowed].sort().join('|');

// Deliberately restrict release input. Keys, .env, sessions, node_modules and
// arbitrary workspace files cannot accidentally be included in a release.
export function artifactPath(path) {
  fail(typeof path === 'string' && path.length <= 512 && !isAbsolute(path)
    && /^[a-zA-Z0-9_./-]+$/u.test(path), 'INVALID_ARTIFACT_PATH');
  const parts = path.split('/');
  fail(parts.every((p) => p !== '' && p !== '.' && p !== '..' && !p.endsWith('.')
    && !/^(con|prn|aux|nul|com\d|lpt\d)(\.|$)/iu.test(p)), 'INVALID_ARTIFACT_PATH');
  fail(/^(frontend\/apps\/(console|admin)\/dist\/|contracts\/|docs\/skills\/mender\/|dist\/)/u.test(path)
    || ['pnpm-lock.yaml', 'backend/go.mod', 'backend/go.sum'].includes(path), 'ARTIFACT_ROOT_NOT_ALLOWED');
  fail(!parts.some((p) => p.startsWith('.') || /(?:secret|credential|private[-_]?key)/iu.test(p))
    && !/\.(?:pem|key|p12|pfx|env)$/iu.test(path), 'SENSITIVE_ARTIFACT_PATH');
  return path;
}

function fileBytes(root, path) {
  artifactPath(path);
  const base = realpathSync(root);
  let current = base;
  for (const part of path.split('/')) {
    current = resolve(current, part);
    fail(!lstatSync(current).isSymbolicLink(), 'ARTIFACT_SYMLINK');
  }
  const local = relative(base, realpathSync(current));
  fail(!isAbsolute(local) && local !== '..' && !local.startsWith('../') && !local.startsWith('..\\'), 'ARTIFACT_ESCAPE');
  const info = lstatSync(current);
  fail(info.isFile() && info.size <= 64 * 1024 * 1024, 'ARTIFACT_FILE_LIMIT');
  const bytes = readFileSync(current);
  fail(bytes.length === info.size, 'ARTIFACT_CHANGED_DURING_READ');
  return bytes;
}

export function inventory(root, paths, revision, now = new Date()) {
  fail(Array.isArray(paths) && paths.length > 0 && paths.length <= 512, 'ARTIFACT_COUNT');
  fail(typeof revision === 'string' && /^[a-f0-9]{40}$/u.test(revision), 'INVALID_SOURCE_REVISION');
  const seen = new Set(); let total = 0;
  const files = [...paths].sort().map((path) => {
    artifactPath(path);
    fail(!seen.has(path.toLowerCase()), 'DUPLICATE_ARTIFACT'); seen.add(path.toLowerCase());
    const bytes = fileBytes(root, path); total += bytes.length;
    fail(total <= 256 * 1024 * 1024, 'ARTIFACT_TOTAL_LIMIT');
    return { path, size_bytes: bytes.length, sha256: sha256(bytes) };
  });
  return { schema_version: 1, source_revision: revision, created_at: now.toISOString(),
    expires_at: new Date(now.getTime() + 24 * 60 * 60 * 1000).toISOString(), files };
}

function payloadBytes(root, payload, now) {
  fail(keys(payload, ['schema_version', 'source_revision', 'created_at', 'expires_at', 'files']) && payload.schema_version === 1, 'INVALID_RELEASE_PAYLOAD');
  const at = Date.parse(payload.created_at), expiry = Date.parse(payload.expires_at);
  fail(Number.isFinite(at) && Number.isFinite(expiry) && new Date(at).toISOString() === payload.created_at
    && new Date(expiry).toISOString() === payload.expires_at && at <= now.getTime() + 300000
    && expiry > now.getTime() && expiry > at && expiry - at <= 7 * 24 * 60 * 60 * 1000, 'RELEASE_TIME_WINDOW');
  fail(Array.isArray(payload.files) && payload.files.every((f) => keys(f, ['path', 'size_bytes', 'sha256'])), 'INVALID_RELEASE_FILES');
  const actual = inventory(root, payload.files.map((f) => f.path), payload.source_revision, new Date(at));
  fail(payload.files.every((f, i) => f.path === actual.files[i].path && f.size_bytes === actual.files[i].size_bytes
    && f.sha256 === actual.files[i].sha256), 'ARTIFACT_DIGEST_MISMATCH');
  // Stable serialization independent of JSON object key order.
  return Buffer.from(JSON.stringify({ schema_version: 1, source_revision: payload.source_revision,
    created_at: payload.created_at, expires_at: payload.expires_at, files: actual.files }));
}

export function signRelease(root, payload, privatePEM, now = new Date()) {
  const bytes = payloadBytes(root, payload, now);
  const key = createPrivateKey(privatePEM);
  fail(key.asymmetricKeyType === 'ed25519', 'ED25519_KEY_REQUIRED');
  const publicDER = createPublicKey(key).export({ type: 'spki', format: 'der' });
  return { algorithm: 'Ed25519', public_key_sha256: sha256(publicDER), payload,
    signature: sign(null, bytes, key).toString('base64') };
}

export function verifyRelease(root, envelope, trustedPublicPEM, now = new Date()) {
  fail(keys(envelope, ['algorithm', 'public_key_sha256', 'payload', 'signature']) && envelope.algorithm === 'Ed25519', 'INVALID_RELEASE_ENVELOPE');
  const key = createPublicKey(trustedPublicPEM);
  fail(key.asymmetricKeyType === 'ed25519', 'ED25519_KEY_REQUIRED');
  fail(sha256(key.export({ type: 'spki', format: 'der' })) === envelope.public_key_sha256, 'UNTRUSTED_RELEASE_KEY');
  fail(typeof envelope.signature === 'string' && /^[A-Za-z0-9+/]{86}==$/u.test(envelope.signature), 'INVALID_RELEASE_SIGNATURE');
  const bytes = payloadBytes(root, envelope.payload, now);
  fail(verify(null, bytes, key, Buffer.from(envelope.signature, 'base64')), 'RELEASE_SIGNATURE_MISMATCH');
  return { status: 'verified', source_revision: envelope.payload.source_revision,
    artifact_count: envelope.payload.files.length, public_key_sha256: envelope.public_key_sha256,
    expires_at: envelope.payload.expires_at };
}
