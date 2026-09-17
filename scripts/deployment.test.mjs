import assert from 'node:assert/strict';
import test from 'node:test';
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, rmSync, symlinkSync } from 'node:fs';
import { resolve, join } from 'node:path';
import { tmpdir } from 'node:os';
import { spawnSync } from 'node:child_process';
import { parse } from 'yaml';
import { validateConfig, scopedPath, nginxConfig, fileInventory, capabilities } from './lib/deployment.mjs';
import { readinessOptions, waitForIngress } from './lib/deployment-readiness.mjs';

const config=()=>({version:1,environment:'local',instance:'mender_fixture',console_origin:'https://console.localhost:18443',admin_origin:'https://admin.localhost:18443',http_port:18000,https_port:18443});
test('local configuration is explicit and production cannot inherit fixture identity or missing credentials',()=>{
 assert.equal(validateConfig(config()).environment,'local');
 for(const mutate of [c=>c.environment='production',c=>c.extra='ignored?',c=>c.console_origin='http://console.localhost:18443',c=>c.admin_origin=c.console_origin,c=>c.oidc_issuer='http://idp.localhost',c=>c.instance='mender_bad;rm',c=>c.https_port=0,c=>c.https_port=c.http_port]){const c=config();mutate(c);assert.throws(()=>validateConfig(c));}
});
test('embedded local OIDC fixture is explicit, isolated and never valid for production',()=>{
 const demo=JSON.parse(readFileSync('deploy/local.demo.example.json','utf8'));
 assert.equal(validateConfig(demo).local_oidc_fixture,true);assert.equal(demo.local_demo_fixture,true);assert.equal(demo.oidc_client_id,'mender-local-demo');
 for(const mutate of [c=>c.environment='production',c=>c.oidc_client_id='other-client',c=>c.oidc_client_secret_file='.local/secret',c=>c.local_oidc_host_gateway=true,c=>c.oidc_issuer='https://login.example.test:19443',c=>c.oidc_issuer='https://idp.localhost:18443',c=>c.worker_workspaces=[]]){const c=structuredClone(demo);mutate(c);assert.throws(()=>validateConfig(c));}
});
test('local product demo is explicit and wires only a loopback reviewed provider fixture',()=>{
 const source=readFileSync('scripts/lib/deployment.mjs','utf8');
 assert.match(source,/local_demo_fixture/u);assert.match(source,/MENDER_REVIEWED_PROVIDER_IDS:'provider_local_demo'/u);
 assert.match(source,/MENDER_REVIEWED_EGRESS_ALLOWED_HOSTS:'127\.0\.0\.1'/u);assert.match(source,/MENDER_REVIEWED_EGRESS_ALLOW_LOOPBACK:'true'/u);
 assert.match(source,/network_mode:'service:worker'/u);assert.match(source,/MENDER_LOCAL_PROVIDER_ENABLED:'true'/u);
 const build=readFileSync('scripts/deploy.mjs','utf8');assert.match(build,/localidp','localprovider'/u);
 const production=JSON.parse(readFileSync('deploy/production.example.json','utf8'));production.local_demo_fixture=true;assert.throws(()=>validateConfig(production),/local product demo fixture/iu);
});
test('readiness checks the actual two TLS ingresses and only retries bounded GET probes',async()=>{
 const c={...config(),web_certificate_file:'operator-owned.pem'};let now=0;const seen=[];
 await waitForIngress(c,'.',{now:()=>now,pause:async(ms)=>{now+=ms;},probe:async(options)=>{seen.push(options);return now>=2000;}});
 assert.equal(seen.length,6);assert.deepEqual([...new Set(seen.map(x=>x.hostname))].sort(),['admin.localhost','console.localhost']);
 for(const options of seen){assert.equal(options.method,'GET');assert.equal(options.path,'/readyz');assert.equal(options.rejectUnauthorized,true);assert.equal(options.agent,false);assert.equal(options.headers.Authorization,undefined);}
 const production=readinessOptions('https://console.mender.org',{environment:'production'},'.');assert.equal(production.lookup,undefined);assert.equal(production.ca,undefined);assert.equal(production.rejectUnauthorized,true);
});
test('unready ingress or invalid TLS never becomes deployment success',async()=>{
 const c={...config(),web_certificate_file:'operator-owned.pem'};let now=0;
 await assert.rejects(waitForIngress(c,'.',{now:()=>now,pause:async(ms)=>{now+=ms;},probe:async()=>false}),/did not become ready/u);
 assert.equal(now,60000);
 await assert.rejects(waitForIngress(c,'.',{probe:async()=>{throw Error('TLS rejected');}}),/TLS rejected/u);
});
test('help is a no-side-effect command and documents explicit pnpm script invocation',()=>{
 const out=spawnSync(process.execPath,['scripts/deploy.mjs','--help'],{encoding:'utf8',timeout:10000});
 assert.equal(out.status,0,out.stderr);assert.match(out.stdout,/Mender deployment CLI/u);assert.match(out.stdout,/pnpm run deploy/u);assert.equal(out.stderr,'');
 const guide=readFileSync('docs/engineering/production-deployment.md','utf8');assert.doesNotMatch(guide,/pnpm deploy\s+(build|prepare|preflight|up|stop|backup|operator|status|switch-release)/u);
});
test('advertised UI origins must use the actual published TLS port',()=>{
 for(const mutate of [c=>c.console_origin='https://console.localhost',c=>c.admin_origin='https://admin.localhost:18444',c=>c.https_port=18445]){const c=config();mutate(c);assert.throws(()=>validateConfig(c),/origin port/u);}
});
test('deployment tests are not optional silently-skipped scripts and production example fails until configured',()=>{
 const p=JSON.parse(readFileSync('package.json','utf8'));assert.ok(p.scripts.test.includes('scripts/deployment.test.mjs'));
 assert.equal(p.scripts['test:deployment'],'node scripts/test-deployment.mjs');
 const steps=parse(readFileSync('.github/workflows/ci.yml','utf8')).jobs.check.steps;
 assert.ok(steps.some(step=>step.run==='pnpm run deploy build dist/releases/ci'));
 assert.ok(steps.some(step=>step.run==='pnpm test:deployment dist/releases/ci'));
 assert.ok(steps.every(step=>!/(?:^|\n)\s*pnpm deploy(?:\s|$)/u.test(step.run||'')));
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
