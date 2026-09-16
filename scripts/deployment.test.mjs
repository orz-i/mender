import assert from 'node:assert/strict';
import test from 'node:test';
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, rmSync, symlinkSync } from 'node:fs';
import { resolve, join } from 'node:path';
import { tmpdir } from 'node:os';
import { validateConfig, scopedPath, nginxConfig, fileInventory, capabilities } from './lib/deployment.mjs';

const config=()=>({version:1,environment:'local',instance:'mender_fixture',console_origin:'https://console.localhost:18443',admin_origin:'https://admin.localhost:18443',http_port:18000,https_port:18443});
test('local configuration is explicit and production cannot inherit fixture identity or missing credentials',()=>{
 assert.equal(validateConfig(config()).environment,'local');
 for(const mutate of [c=>c.environment='production',c=>c.extra='ignored?',c=>c.console_origin='http://console.localhost:18443',c=>c.admin_origin=c.console_origin,c=>c.oidc_issuer='http://idp.localhost',c=>c.instance='mender_bad;rm',c=>c.https_port=0,c=>c.https_port=c.http_port]){const c=config();mutate(c);assert.throws(()=>validateConfig(c));}
});
test('deployment tests are not optional silently-skipped scripts and production example fails until configured',()=>{
 const p=JSON.parse(readFileSync('package.json','utf8'));assert.ok(p.scripts.test.includes('scripts/deployment.test.mjs'));
 assert.equal(p.scripts['test:deployment'],'node scripts/test-deployment.mjs');
 assert.throws(()=>validateConfig(JSON.parse(readFileSync('deploy/production.example.json','utf8'))));
});
test('deployment output never traverses, follows symlinks or leaves its dedicated workspace root',()=>{
 const root=mkdtempSync(join(tmpdir(),'mender-path-'));
 try{for(const p of ['../x','/x','C:/x','.local/deploy/../x','.local//x','.local/deploy\\x'])assert.throws(()=>scopedPath(root,p,'.local/deploy/'));
  assert.equal(scopedPath(root,'.local/deploy/safe','.local/deploy/'),resolve(root,'.local/deploy/safe'));
  mkdirSync(join(root,'.local'));symlinkSync(join(root,'.local'),join(root,'alias'),'junction');assert.throws(()=>scopedPath(root,'alias/new'));
 }finally{rmSync(root,{recursive:true,force:true});}
});
test('reverse proxy preserves auth/API/MCP paths, disables retries/buffering, rejects unknown hosts and never logs query tokens',()=>{
 const text=nginxConfig(config());assert.match(text,/\(api\|auth\|mcp\)/u);assert.match(text,/proxy_buffering off/u);assert.match(text,/proxy_next_upstream off/u);
 assert.match(text,/Host \$http_host/u);assert.match(text,/ssl_reject_handshake on/u);assert.match(text,/try_files \$uri =404/u);assert.match(text,/access_log off/u);
 assert.doesNotMatch(text,/log_format[^;]*\$(request_uri|args|http_authorization)/u);
 assert.match(readFileSync('frontend/apps/console/vite.config.ts','utf8'),/'\/auth': target/u);
 assert.match(text,/fastcgi_temp_path \/tmp\/fastcgi_temp/u);
 assert.match(text,/uwsgi_temp_path \/tmp\/uwsgi_temp/u);
 assert.match(text,/scgi_temp_path \/tmp\/scgi_temp/u);
 assert.match(text,/listen 127\.0\.0\.1:8081/u);
});
test('byte inventory detects edits and does not silently refresh a supplied inventory',()=>{
 const root=mkdtempSync(join(tmpdir(),'mender-inventory-'));try{writeFileSync(join(root,'app'),'one');const a=fileInventory(root);writeFileSync(join(root,'app'),'two');assert.notDeepEqual(fileInventory(root),a);assert.equal(a.length,1);}finally{rmSync(root,{recursive:true,force:true});}
});
test('every generated role maps to one distinct existing capability and environment key',()=>{
 assert.equal(new Set(capabilities.map(c=>c[0])).size,capabilities.length);assert.equal(new Set(capabilities.map(c=>c[1])).size,capabilities.length);
 const operator=readFileSync('backend/internal/bootstrap/operator.go','utf8');for(const [grant,key]of capabilities){assert.ok(operator.includes(`"grant-${grant}"`));assert.match(key,/^MENDER_[A-Z_]+DATABASE_URL$|^MENDER_DATABASE_URL$/u);}
});
test('release Dockerfile contains no secret input, compiler, root runtime or unpinned fallback',()=>{
 const text=readFileSync('deploy/Dockerfile','utf8');assert.match(text,/FROM scratch AS runtime/u);assert.match(text,/USER 65532:65532/u);assert.match(text,/ARG TRUST_IMAGE\n/u);assert.doesNotMatch(text,/COPY \. |ARG .*PASSWORD|ENV .*SECRET/u);
});
