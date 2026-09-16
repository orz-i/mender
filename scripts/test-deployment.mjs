// Explicit local-only smoke: real Linux images + nginx TLS + PostgreSQL + signed
// OIDC protocol simulator + Chromium. Never included in the production image.
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { randomBytes, generateKeyPairSync, sign, createHash } from 'node:crypto';
import { createServer } from 'node:https';
import { createServer as netServer } from 'node:net';
import { mkdirSync, readFileSync, writeFileSync, existsSync } from 'node:fs';
import { join } from 'node:path';
import { chromium, expect } from '@playwright/test';
import { parse, stringify } from 'yaml';
import { signRelease } from './lib/release-artifact.mjs';

const releaseDir=process.argv[2];if(!/^dist\/releases\/[a-zA-Z0-9_-]+$/.test(releaseDir||''))throw Error('Usage: pnpm test:deployment dist/releases/ID');
const suffix=randomBytes(5).toString('hex');const instance='mender_test_'+suffix;
const fixture=`.tmp/deploy-fixture-${suffix}`,dir=`.local/deploy/${instance}`;mkdirSync(fixture,{recursive:true,mode:0o700});
const results=[];let browser,idp,started=false;let current='prepare';
async function run(exe,args,ok=true,input){return await new Promise((res,rej)=>{const child=spawn(exe,args,{shell:false,windowsHide:true,env:{...process.env,PLAYWRIGHT_BROWSERS_PATH:process.env.PLAYWRIGHT_BROWSERS_PATH||'.tmp/playwright-browsers'}});let out='',err='';const timer=setTimeout(()=>child.kill(),240000);child.stdin.end(input);child.stdout.on('data',b=>out+=b);child.stderr.on('data',b=>err+=b);child.once('error',rej);child.once('exit',code=>{clearTimeout(timer);if(ok&&code!==0){writeFileSync(join(fixture,'command-failure.txt'),err.replace(/postgres(?:ql)?:\/\/\S+/g,'[redacted]'));rej(Error(`${exe} ${args[0]} failed (${code}); details in fixture command-failure.txt`));}else res({code,out,err});});});}
async function node(args,ok=true){return run(process.execPath,args,ok);}
async function compose(args,ok=true){return run('docker',['compose','-f',`${dir}/compose.yaml`,...args],ok);}
async function port(){return new Promise((res,rej)=>{const s=netServer();s.once('error',rej);s.listen(0,'127.0.0.1',()=>{const p=s.address().port;s.close(()=>res(p));});});}
async function step(name,fn){current=name;const start=Date.now();await fn();results.push({name,result:'passed',duration_ms:Date.now()-start});console.log(`PASS: ${name}`);}
const [httpsPort,httpPort]=await Promise.all([port(),port()]);
try{
 await step('generate owned local PKI without changing OS trust',async()=>{
  await node(['scripts/backend.mjs','run','./cmd/provision','local-pki','../'+fixture+'/pki']);
 });
 const secret=randomBytes(32).toString('hex');writeFileSync(join(fixture,'oidc-client.txt'),secret,{mode:0o600});
 const keys=generateKeyPairSync('rsa',{modulusLength:2048});const jwk=keys.publicKey.export({format:'jwk'});jwk.kid='fixture-key';jwk.alg='RS256';jwk.use='sig';
 const codes=new Map();let issuer;const clientID='mender-deployment-test';
 idp=createServer({key:readFileSync(join(fixture,'pki/web.key')),cert:readFileSync(join(fixture,'pki/web.crt'))},async(req,res)=>{
  try{const u=new URL(req.url,issuer);const json=value=>{res.setHeader('Content-Type','application/json');res.end(JSON.stringify(value));};
   if(u.pathname==='/.well-known/openid-configuration')return json({issuer,authorization_endpoint:issuer+'/authorize',token_endpoint:issuer+'/token',jwks_uri:issuer+'/keys',response_types_supported:['code'],subject_types_supported:['public'],id_token_signing_alg_values_supported:['RS256']});
   if(u.pathname==='/keys')return json({keys:[jwk]});
   if(u.pathname==='/authorize'){
    assert.equal(u.searchParams.get('client_id'),clientID);assert.equal(u.searchParams.get('code_challenge_method'),'S256');
    const redirect=u.searchParams.get('redirect_uri');assert.ok([`https://console.localhost:${httpsPort}/auth/callback`,`https://admin.localhost:${httpsPort}/auth/callback`].includes(redirect));
    if(!u.searchParams.has('account')){res.setHeader('Content-Type','text/html');res.end('<!doctype html><title>Local OIDC fixture</title><h1>Local OIDC fixture</h1>'+['maker','checker','bad_nonce'].map(a=>`<a href="${u.pathname+u.search.replaceAll('&','&amp;')}&amp;account=${a}">Sign in ${a}</a>`).join('<br>'));return;}
    const account=u.searchParams.get('account');assert.ok(['maker','checker','bad_nonce'].includes(account));const code=randomBytes(24).toString('hex');codes.set(code,{account,nonce:u.searchParams.get('nonce'),challenge:u.searchParams.get('code_challenge'),redirect});const dest=new URL(redirect);dest.searchParams.set('code',code);dest.searchParams.set('state',u.searchParams.get('state'));res.writeHead(302,{Location:dest.href});res.end();return;
   }
   if(u.pathname==='/token'&&req.method==='POST'){
    let body='';for await(const b of req){body+=b;if(body.length>8192)throw Error('body limit');}const form=new URLSearchParams(body);
    const basic='Basic '+Buffer.from(`${clientID}:${secret}`).toString('base64');assert.ok(req.headers.authorization===basic||(form.get('client_id')===clientID&&form.get('client_secret')===secret));
    const c=codes.get(form.get('code'));assert.ok(c);codes.delete(form.get('code'));assert.equal(form.get('redirect_uri'),c.redirect);assert.equal(createHash('sha256').update(form.get('code_verifier')).digest('base64url'),c.challenge);
    const now=Math.floor(Date.now()/1000);const b64=v=>Buffer.from(JSON.stringify(v)).toString('base64url');const unsigned=b64({alg:'RS256',kid:jwk.kid,typ:'JWT'})+'.'+b64({iss:issuer,aud:clientID,sub:c.account==='bad_nonce'?'maker':c.account,nonce:c.account==='bad_nonce'?'invalid-nonce':c.nonce,iat:now,exp:now+120});
    return json({access_token:'fixture-not-a-provider-credential',token_type:'Bearer',expires_in:120,id_token:unsigned+'.'+sign('RSA-SHA256',Buffer.from(unsigned),keys.privateKey).toString('base64url')});
   }
   res.writeHead(404);res.end();
  }catch{res.writeHead(400);res.end('fixture request rejected');}
 });
 await new Promise(res=>idp.listen(0,'0.0.0.0',res));issuer=`https://idp.localhost:${idp.address().port}`;
 const config={version:1,environment:'local',instance,console_origin:`https://console.localhost:${httpsPort}`,admin_origin:`https://admin.localhost:${httpsPort}`,http_port:httpPort,https_port:httpsPort,oidc_issuer:issuer,oidc_client_id:clientID,oidc_client_secret_file:fixture+'/oidc-client.txt',oidc_ca_file:fixture+'/pki/ca.crt',local_oidc_host_gateway:true};
 writeFileSync(join(fixture,'config.json'),JSON.stringify(config));
 await step('prepare persistent configuration with separate credentials and HTTPS database verification',async()=>{await node(['scripts/deploy.mjs','prepare',fixture+'/config.json',releaseDir,dir]);});
 started=true;
 await step('start built read-only non-root images and explicit least-privilege provisioning',async()=>{await node(['scripts/deploy.mjs','up',dir,'--apply']);});
 await step('repeat provisioning without resetting credentials or generating data',async()=>{await compose(['run','--rm','--no-deps','provision']);});
 await step('deployment preflight rejects untrusted release signatures and changed runtime images',async()=>{
  const pair=generateKeyPairSync('ed25519');const pub=pair.publicKey.export({type:'spki',format:'pem'});const priv=pair.privateKey.export({type:'pkcs8',format:'pem'});
  const payload=JSON.parse(readFileSync(join(releaseDir,'signing-inventory.json')));const envelope=signRelease(process.cwd(),payload,priv);
  writeFileSync(join(fixture,'release-envelope.json'),JSON.stringify(envelope));writeFileSync(join(fixture,'trusted.pem'),pub);const wrong=generateKeyPairSync('ed25519').publicKey.export({type:'spki',format:'pem'});writeFileSync(join(fixture,'wrong.pem'),wrong);
  const bad=await node(['scripts/deploy.mjs','preflight',dir,'--signed',fixture+'/release-envelope.json','--trusted-key',fixture+'/wrong.pem'],false);assert.notEqual(bad.code,0);
  await node(['scripts/deploy.mjs','preflight',dir,'--signed',fixture+'/release-envelope.json','--trusted-key',fixture+'/trusted.pem']);
  const file=join(dir,'compose.yaml'),meta=join(dir,'deployment.json');const original=readFileSync(file),oldMeta=readFileSync(meta);
  try{const c=parse(original.toString());c.services['api-console'].image='sha256:'+'0'.repeat(64);const changed=stringify(c);writeFileSync(file,changed);const d=JSON.parse(oldMeta);d.controls.find(x=>x.path==='compose.yaml').sha256=createHash('sha256').update(changed).digest('hex');writeFileSync(meta,JSON.stringify(d));assert.notEqual((await node(['scripts/deploy.mjs','preflight',dir],false)).code,0);}finally{writeFileSync(file,original);writeFileSync(meta,oldMeta);}
 });
 for(const user of ['maker','checker']){
  await node(['scripts/deploy.mjs','operator',dir,'provision-human','--user',`user_${user}`,'--display-name',user,'--issuer',issuer,'--oidc-subject',user,'--workspace','ws_local','--membership-role','owner']);
  await node(['scripts/deploy.mjs','operator',dir,'provision-platform-staff','--user',`user_${user}`,'--platform-role',user==='maker'?'operator':'reviewer']);
 }
 browser=await chromium.launch({args:['--host-resolver-rules=MAP *.localhost 127.0.0.1']});
 const context=await browser.newContext({ignoreHTTPSErrors:true,viewport:{width:1440,height:960}});const page=await context.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));
 await step('real Console login via TLS discovery, code, PKCE, nonce, JWKS and server session',async()=>{
  await page.goto(config.console_origin+'/workspaces');await expect(page.getByRole('heading',{name:'登录 Console',exact:true})).toBeVisible();await page.getByRole('link',{name:'使用 OIDC 登录'}).click();await expect(page.getByRole('heading',{name:'Local OIDC fixture'})).toBeVisible();await page.getByRole('link',{name:'Sign in maker',exact:true}).click();await expect(page.getByRole('heading',{name:'工作空间',exact:true})).toBeVisible();await expect(page.getByRole('heading',{name:'ws_local',exact:true})).toBeVisible();
  const cookies=await context.cookies(config.console_origin);const session=cookies.find(c=>c.name==='mender_session');assert.ok(session?.httpOnly&&session.secure);assert.ok(!cookies.some(c=>/oidc-client|access_token/.test(c.name)));
  await page.screenshot({path:join(fixture,'console-login.png'),fullPage:false});
 });
 await step('distinct Admin origin is not authenticated by Console cookie; independent login lands safely',async()=>{
  await page.goto(config.admin_origin+'/');const before=await page.evaluate(async()=> (await fetch('/api/console/v1/session')).status);assert.equal(before,401);
  await page.goto(config.admin_origin+'/auth/login');await page.getByRole('link',{name:'Sign in checker',exact:true}).click();await expect(page).toHaveURL(config.admin_origin+'/');await page.goto(config.admin_origin+'/support-approvals');await expect(page.locator('.workbench-identity')).toBeVisible();await page.screenshot({path:join(fixture,'admin-login.png'),fullPage:false});
 });
 await step('API restart preserves server-side browser session',async()=>{await compose(['restart','api-console','api-admin']);await compose(['up','-d','--wait','--wait-timeout','90','api-console','api-admin']);await page.goto(config.console_origin+'/');const status=await page.evaluate(async()=> (await fetch('/api/console/v1/session')).status);assert.equal(status,200);});
 await step('database restart preserves identities and sessions with the same volume and TLS key',async()=>{await compose(['restart','database']);await compose(['up','-d','--wait','--wait-timeout','90','database','api-console','api-admin']);await page.goto(config.console_origin+'/workspaces');await expect(page.getByRole('heading',{name:'ws_local',exact:true})).toBeVisible();});
 await step('actual protected backup is readable by pg_restore and release switching preserves volume and secrets',async()=>{
  const dest='.local/backups/'+instance;await node(['scripts/deploy.mjs','backup',dir,dest,'--apply']);const bytes=readFileSync(join(dest,'database.dump'));assert.equal(bytes.subarray(0,5).toString(),'PGDMP');const meta=JSON.parse(readFileSync(join(dest,'backup.json')));assert.equal(meta.sha256,createHash('sha256').update(bytes).digest('hex'));
  const listed=await run('docker',['compose','-f',`${dir}/compose.yaml`,'exec','-T','database','pg_restore','--list'],true,bytes);assert.match(listed.out,/identity/);
  const keyBefore=readFileSync(join(dir,'secrets/dsn_runtime'));
  assert.notEqual((await node(['scripts/deploy.mjs','switch-release',dir,releaseDir,'--apply','--migrations-reviewed'],false)).code,0);
  await node(['scripts/deploy.mjs','stop',dir]);await node(['scripts/deploy.mjs','switch-release',dir,releaseDir,'--apply','--migrations-reviewed']);await node(['scripts/deploy.mjs','up',dir,'--apply']);assert.deepEqual(readFileSync(join(dir,'secrets/dsn_runtime')),keyBefore);await page.goto(config.console_origin+'/workspaces');await expect(page.getByRole('heading',{name:'ws_local',exact:true})).toBeVisible();
 });
 await step('nonce tampering rejects login; no forged session is issued',async()=>{const other=await browser.newContext({ignoreHTTPSErrors:true});const p=await other.newPage();await p.goto(config.console_origin+'/auth/login');await p.getByRole('link',{name:'Sign in bad_nonce',exact:true}).click();await expect(p.locator('body')).toContainText('OIDC_LOGIN_FAILED');assert.ok(!(await other.cookies()).some(c=>c.name==='mender_session'));await other.close();});
 await step('logout invalidates persistent session and mobile page remains meaningful',async()=>{await page.goto(config.console_origin+'/workspaces');await page.getByRole('button',{name:'退出登录',exact:true}).click();await expect(page.getByRole('heading',{name:'登录 Console',exact:true})).toBeVisible();await page.setViewportSize({width:390,height:844});await page.screenshot({path:join(fixture,'console-logged-out-mobile.png'),fullPage:false});assert.deepEqual(errors,[]);});
}catch(e){results.push({name:current,result:'failed',error:e.message});console.error(e.message);if(started){const r=await compose(['logs','--no-color','--tail','50'],false);writeFileSync(join(fixture,'stack-failure.txt'),(r.out+r.err).replace(/postgres(?:ql)?:\/\/\S+/g,'[redacted]'));}process.exitCode=1;
}finally{
 await browser?.close();await new Promise(res=>idp?idp.close(res):res());
 if(started&&existsSync(join(dir,'deployment.json'))){const d=JSON.parse(readFileSync(join(dir,'deployment.json')));assert.equal(d.config.instance,instance);assert.match(instance,/^mender_test_[a-f0-9]{10}$/);await compose(['down','--volumes','--remove-orphans']);}
 writeFileSync(join(fixture,'results.json'),JSON.stringify({scope:'local-owned-production-image-stack',external_idp:false,external_psp:false,chromium_local_ca_exception:true,production_trust_modified:false,results,resources_removed:started},null,2));console.log('Evidence: '+fixture+'/results.json');
}
