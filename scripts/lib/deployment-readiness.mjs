import { request } from 'node:https';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { setTimeout as sleep } from 'node:timers/promises';

// A container healthcheck cannot prove the published TLS ingress is reachable.
// This probe never authenticates, follows redirects, or calls business tools.
export function readinessOptions(origin, config, directory) {
  const url = new URL(origin);
  if (url.protocol !== 'https:' || url.origin !== origin || !['local', 'production'].includes(config.environment)) {
    throw Error('Invalid HTTPS readiness origin');
  }
  const options = { method: 'GET', path: '/readyz', hostname: url.hostname, port: Number(url.port || 443),
    servername: url.hostname, rejectUnauthorized: true, agent: false,
    headers: { Host: url.host, Accept: 'application/json' } };
  if (config.environment === 'local') {
    // Preserve hostname verification/SNI while directing only this local socket
    // to loopback. Never install this CA into the host trust store.
    options.lookup = (_host, lookupOptions, callback) => {
      if (lookupOptions?.all) callback(null, [{ address: '127.0.0.1', family: 4 }]);
      else callback(null, '127.0.0.1', 4);
    };
    if (!config.web_certificate_file) options.ca = readFileSync(join(directory, 'secrets/db_ca'));
  }
  return options;
}

export function probeReadiness(options, timeoutMS = 3000) {
  return new Promise((resolve, reject) => {
    let settled = false;
    let timer;
    const finish = (error, value) => {
      if (settled) return;
      settled = true; clearTimeout(timer);
      if (error) reject(error); else resolve(value);
    };
    const req = request(options, response => {
      if ([502, 503, 504].includes(response.statusCode)) {
        response.destroy(); finish(null, false); return;
      }
      if (response.statusCode !== 200 || !/^application\/json(?:;|$)/i.test(response.headers['content-type'] || '')) {
        response.destroy(); finish(Error('Ingress returned an invalid readiness response')); return;
      }
      const chunks = []; let size = 0;
      response.on('data', chunk => {
        size += chunk.length;
        if (size > 4096) { response.destroy(); finish(Error('Readiness response exceeded limit')); }
        else chunks.push(chunk);
      });
      response.once('end', () => {
        try {
          const value = JSON.parse(Buffer.concat(chunks).toString('utf8'));
          if (value.service !== 'mender-api' || value.status !== 'ready') throw Error('Wrong readiness identity');
          finish(null, true);
        } catch { finish(Error('Ingress returned an invalid readiness identity')); }
      });
      response.once('error', () => finish(null, false));
    });
    req.once('error', error => {
      if (['ECONNREFUSED', 'ECONNRESET', 'ETIMEDOUT', 'EHOSTUNREACH', 'EAI_AGAIN', 'ENOTFOUND'].includes(error.code)) finish(null, false);
      else finish(Error('HTTPS ingress verification failed; certificate validation was not disabled'));
    });
    timer = setTimeout(() => { finish(null, false); req.destroy(); }, timeoutMS);
    req.end();
  });
}

export async function waitForIngress(config, directory, dependencies = {}) {
  const probe = dependencies.probe || probeReadiness;
  const pause = dependencies.pause || sleep;
  const now = dependencies.now || Date.now;
  const options = [config.console_origin, config.admin_origin].map(origin => readinessOptions(origin, config, directory));
  const deadline = now() + 60000;
  for (let attempt = 0; attempt < 60 && now() < deadline; attempt++) {
    const ready = await Promise.all(options.map(value => probe(value, Math.max(1, Math.min(3000, deadline - now())))));
    if (ready.every(value => value === true)) return;
    if (now() < deadline) await pause(Math.min(1000, deadline - now()));
  }
  throw Error('Published Console/Admin HTTPS ingress did not become ready; deployment success is not asserted');
}
