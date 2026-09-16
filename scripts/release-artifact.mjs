import { readFileSync, statSync } from 'node:fs';
import { inventory, signRelease, verifyRelease } from './lib/release-artifact.mjs';

// No implicit key generation, writes, deployment or shell commands. Redirect
// stdout to a chosen output file only after reviewing the explicit inputs.
const read = (path, limit = 1024 * 1024) => {
  if (!path || !statSync(path).isFile() || statSync(path).size > limit) throw new Error('INPUT_FILE_LIMIT');
  return readFileSync(path);
};
try {
  const [command, ...args] = process.argv.slice(2);
  let result;
  if (command === 'inventory' && args.length >= 2) result = inventory(process.cwd(), args.slice(1), args[0]);
  else if (command === 'sign' && args.length === 2) {
    const mode = statSync(args[1]).mode;
    if (process.platform !== 'win32' && (mode & 0o077) !== 0) throw new Error('PRIVATE_KEY_FILE_PERMISSIONS');
    result = signRelease(process.cwd(), JSON.parse(read(args[0])), read(args[1], 16384));
  } else if (command === 'verify' && args.length === 2) result = verifyRelease(process.cwd(), JSON.parse(read(args[0])), read(args[1], 16384));
  else throw new Error('USAGE: inventory REVISION FILE... | sign MANIFEST PRIVATE_KEY_FILE | verify ENVELOPE TRUSTED_PUBLIC_KEY_FILE');
  process.stdout.write(`${JSON.stringify(result, null, 2)}\n`);
} catch (error) {
  // Filesystem paths, PEM contents and OpenSSL diagnostics are not printed.
  console.error(/^[A-Z0-9_: .|]+$/u.test(error.message) ? error.message : 'RELEASE_VERIFICATION_FAILED');
  process.exitCode = 1;
}
