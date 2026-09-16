import assert from 'node:assert/strict';
import { generateKeyPairSync } from 'node:crypto';
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { test } from 'node:test';
import { artifactPath, inventory, signRelease, verifyRelease } from './lib/release-artifact.mjs';

const at = new Date('2026-09-16T00:00:00.000Z');
const revision = 'a'.repeat(40);
function fixture(t) {
  const root = mkdtempSync(join(tmpdir(), 'mender-release-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  mkdirSync(join(root, 'dist'));
  writeFileSync(join(root, 'dist', 'app.js'), 'export const version=1;\n');
  writeFileSync(join(root, 'pnpm-lock.yaml'), 'lockfileVersion: 9\n');
  const keys = generateKeyPairSync('ed25519', { privateKeyEncoding: { type: 'pkcs8', format: 'pem' }, publicKeyEncoding: { type: 'spki', format: 'pem' } });
  const payload = inventory(root, ['pnpm-lock.yaml', 'dist/app.js'], revision, at);
  const signed = signRelease(root, payload, keys.privateKey, at);
  return { root, keys, payload, signed };
}
test('real Ed25519 verification binds file bytes and externally supplied trusted public key', (t) => {
  const f = fixture(t);
  assert.equal(verifyRelease(f.root, f.signed, f.keys.publicKey, at).artifact_count, 2);
  assert.equal(f.signed.signature.length, 88);
  assert.ok(!JSON.stringify(f.signed).includes('PRIVATE KEY'));
});
test('artifact drift is rejected rather than silently refreshing hashes', (t) => {
  const f = fixture(t); writeFileSync(join(f.root, 'dist', 'app.js'), 'tampered');
  assert.throws(() => verifyRelease(f.root, f.signed, f.keys.publicKey, at), /DIGEST/);
});
test('wrong signer and modified signatures do not pass', (t) => {
  const f = fixture(t), other = fixture(t);
  assert.throws(() => verifyRelease(f.root, f.signed, other.keys.publicKey, at), /UNTRUSTED/);
  const copy = structuredClone(f.signed); copy.signature = Buffer.alloc(64, 0).toString('base64');
  assert.throws(() => verifyRelease(f.root, copy, f.keys.publicKey, at), /SIGNATURE/);
});
test('revision substitution and embedded self-approved key are rejected', (t) => {
  const f = fixture(t), copy = structuredClone(f.signed); copy.payload.source_revision = 'b'.repeat(40);
  assert.throws(() => verifyRelease(f.root, copy, f.keys.publicKey, at), /SIGNATURE/);
  assert.throws(() => verifyRelease(f.root, { ...f.signed, trusted_key: f.keys.publicKey }, f.keys.publicKey, at), /ENVELOPE/);
});
test('expiry, invalid time and unknown fields fail closed', (t) => {
  const f = fixture(t);
  assert.throws(() => verifyRelease(f.root, f.signed, f.keys.publicKey, new Date('2026-09-18T00:00:00Z')), /TIME/);
  assert.throws(() => signRelease(f.root, { ...f.payload, expires_at: 'invalid' }, f.keys.privateKey, at), /TIME/);
  assert.throws(() => signRelease(f.root, { ...f.payload, approved: true }, f.keys.privateKey, at), /PAYLOAD/);
});
test('empty, duplicate and case-colliding artifact inventories fail', (t) => {
  const f = fixture(t);
  assert.throws(() => inventory(f.root, [], revision, at), /COUNT/);
  assert.throws(() => inventory(f.root, ['dist/app.js', 'dist/app.js'], revision, at), /DUPLICATE/);
  assert.throws(() => inventory(f.root, ['dist/app.js', 'dist/APP.js'], revision, at), /DUPLICATE|ENOENT/);
});
test('paths reject traversal, absolute paths, alternate streams, device names and secrets', () => {
  for (const path of ['../outside', '/tmp/test', 'C:/secret', 'dist/../file', 'dist/file:stream', 'dist/CON', 'dist/a.', 'dist//a', '.env', 'dist/private-key.txt', 'contracts/token.pem', 'docs/session/file.md', 'node_modules/a.js']) assert.throws(() => artifactPath(path));
});
test('signing does not create or modify repository artifacts or keys', (t) => {
  const f = fixture(t), before = readFileSync(join(f.root, 'dist', 'app.js'));
  signRelease(f.root, f.payload, f.keys.privateKey, at);
  assert.deepEqual(readFileSync(join(f.root, 'dist', 'app.js')), before);
});
