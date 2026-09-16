// Called only by the explicit disposable-PostgreSQL --browser harness.
// No mocked business API: the browser uses real handlers, roles, sessions and DB.
import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { chromium, expect } from '@playwright/test';

const adminURL=process.env.MENDER_S4_ADMIN_URL;
const consoleURL=process.env.MENDER_S4_CONSOLE_URL;
for(const endpoint of [adminURL,consoleURL]) {
  const url=new URL(endpoint);assert.equal(url.protocol,'http:');assert.equal(url.hostname,'127.0.0.1');
}
const credentials=JSON.parse(process.env.MENDER_S4_TEST_SESSIONS??'null');
assert.ok(credentials?.maker?.session&&credentials?.checker?.session&&credentials?.support?.session);
const output='.tmp/s4-browser';await mkdir(output,{recursive:true});
const browser=await chromium.launch();const pages=[],errors=[],results=[];
const workspace='ws_s4_browser';let currentStep='startup';
async function page(role,app='admin') {
 const url=app==='admin'?adminURL:consoleURL;
 const context=await browser.newContext({viewport:{width:1440,height:1000},locale:'zh-CN'});
 const c=credentials[role];await context.addCookies([{name:'mender_session',value:c.session,url,httpOnly:true,sameSite:'Lax'},{name:'mender_csrf',value:c.csrf,url,sameSite:'Lax'}]);
 const p=await context.newPage();p.on('pageerror',error=>errors.push(error.message));pages.push(p);return p;
}
async function open(p,app,path,heading,scope=workspace) {
 await p.goto((app==='admin'?adminURL:consoleURL)+path);await expect(p.getByRole('heading',{name:heading,exact:true})).toBeVisible();
 await expect(p.locator('.workbench-identity')).toBeVisible();
 if(scope!==null){await p.locator('input[name="workspace"]').fill(scope);await p.getByRole('button',{name:'切换工作区'}).click();}
}
async function select(p,command,fields={}) {
 await p.getByLabel('选择操作').selectOption(command);
 for(const [name,value] of Object.entries(fields)) {
  const input=p.locator(`.workbench-form [id$="-${name}"]`);
  await expect(input).toBeVisible();const tag=await input.evaluate(el=>el.tagName);
  if(tag==='SELECT')await input.selectOption(value);else await input.fill(value);
 }
}
async function request(p,command,fields={},mutation=false,status=200) {
 await select(p,command,fields);
 if(mutation){await p.getByRole('button',{name:'核对操作',exact:true}).click();await expect(p.getByRole('dialog')).toBeVisible();await p.getByRole('checkbox').check();}
 const pending=p.waitForResponse(r=>r.url().includes('/api/')&&!r.url().endsWith('/session')&&r.request().method()===(mutation?(command.includes('update')?'PUT':'POST'):'GET'));
 await p.getByRole('button',{name:mutation?'确认执行':'读取记录',exact:true}).click();
 const response=await pending;
 assert.equal(response.status(),status,`${command} unexpected status`);
 if(status>=400){await expect(p.getByRole('alert')).toBeVisible();return null;}
 await expect(p.locator('.workbench-results [role="status"]')).toContainText('已返回服务端事实');
 // Read actual rendered, contract-validated user facts rather than Chromium's
 // evictable Network.getResponseBody cache. No API or app state is mocked.
 const rows=await p.locator('.workbench-record').evaluateAll(cards=>cards.map(card=>Object.fromEntries([...card.querySelectorAll('.fact-grid > div')].map(row=>[row.querySelector('dt').textContent,row.querySelector('dd').textContent]))));
 const aliases={'工作区':'workspace_id','发布者':'publisher_id','插件':'plugin_id','版本':'version','精确版本':'revision','服务端状态':'state','理由':'reason','记录ID':'id','审批ID':'approval_id','目标版本':'target_revision','申请人':'requester_user_id','复核人':'reviewer_user_id','名称':'display_name','清单SHA256':'manifest_sha256','精确参数SHA256':'parameters_sha256','预检就绪':'ready','代码':'code','金额（micro）':'amount_micro','币种':'currency','净账单（micro）':'net_billed_micro','待对账Run数':'pending_reconcile_count','差额（micro）':'difference_micro','已冻结':'frozen','幂等重放':'replay','支付意图ID':'intent_id','模式':'mode','Journal ID':'journal_id','到期时间':'expires_at','消费时间':'consumed_at','授权范围':'scopes'};
 const records=rows.map(row=>Object.fromEntries(Object.entries(row).map(([label,value])=>{
   const key=aliases[label]??label;
   return [key,['frozen','ready','replay','owned'].includes(key)?value==='true':['parameters','manifest','scopes'].includes(key)?JSON.parse(value):value==='—'?null:value];
 })));
 if(command==='version.submit'||command==='version.publish')return {approval_id:records.find(row=>row.approval_id)?.approval_id,plugin_version:records.find(row=>row.manifest)};
 if(['plugin.reviews','approval.list','platform.workspaces','platform.providers','platform.incidents','platform.audit','support.runs','payment.intents','payment.callbacks'].includes(command))return records;
 return records[0]??{};
}
async function choose(p,id) {const card=p.locator('.workbench-record').filter({hasText:id}).first();await expect(card).toBeVisible();await card.getByRole('button',{name:'用于表单'}).click();}
async function step(name,fn){currentStep=name;const start=Date.now();await fn();results.push({name,result:'passed',duration_ms:Date.now()-start});console.log('PASS: '+name);}
try {
 const publisher=await page('maker','console'),maker=await page('maker'),checker=await page('checker'),support=await page('support'),viewer=await page('viewer');
 let pluginApproval,grantID,financeApproval;
 const target={plugin_id:'example.browser',version:'1.0.0'};
 const manifest=JSON.stringify({apiVersion:'mender.io/plugin/v1alpha1',plugin_id:'example.browser',version:'1.0.0',publisher_id:'publisher_browser',display_name:'浏览器验收能力',description:'真实HTTP与隔离数据库，不进行外部供应商调用。',capabilities:[{kind:'agent',deployment_revision:'deploy_s4a_agent'}]},null,2);
 await step('S4-09 publisher draft → real preflight → submit; no implicit publication',async()=>{
  await open(publisher,'console','/publisher','发布者工作台');
  await request(publisher,'publisher.snapshot');
  await request(publisher,'publisher.create',{publisher_id:'publisher_browser',display_name:'浏览器发布者'},true,201);
  await request(publisher,'plugin.create',{publisher_id:'publisher_browser',plugin_id:'example.browser'},true,201);
  const draft=await request(publisher,'version.create',{manifest},true,201);assert.equal(draft.state,'draft');
  assert.equal((await request(publisher,'version.preflight',target,true)).ready,true);
  const submitted=await request(publisher,'version.submit',target,true,202);assert.equal(submitted.plugin_version.state,'submitted');pluginApproval=submitted.approval_id;
 });
 await step('S4-10 independent plugin review, self-review denied, immutable publication',async()=>{
  await open(maker,'admin','/release-management','插件审核与发布');await request(maker,'plugin.reviews');await choose(maker,pluginApproval);
  await request(maker,'plugin.approve',{note:'self review must fail'},true,403);
  await open(checker,'admin','/release-management','插件审核与发布');await request(checker,'plugin.reviews');await choose(checker,pluginApproval);
  assert.equal((await request(checker,'plugin.approve',{note:'独立复核精确版本与能力范围'},true)).state,'approved');
  const published=await request(publisher,'version.publish',target,true);assert.equal(published.plugin_version.state,'published');
  await request(publisher,'version.update',{manifest},true,409);
  await publisher.screenshot({path:output+'/publisher-desktop.png',fullPage:true});
 });
 await step('S4-10 platform freeze/unfreeze with real revision conflict and denied viewer',async()=>{
  await open(maker,'admin','/platform-operations','平台运营与异常',null);
  const workspaces=await request(maker,'platform.workspaces');const initial=workspaces.find(w=>w.workspace_id==='ws_s4_freeze');assert.ok(initial);
  const first=await request(maker,'workspace.freeze',{workspace_id:'ws_s4_freeze',expected_revision:initial.revision,reason:'browser incident freeze'},true);assert.equal(first.frozen,true);
  await request(maker,'workspace.unfreeze',{workspace_id:'ws_s4_freeze',expected_revision:initial.revision,reason:'stale request rejected'},true,409);
  const restored=await request(maker,'workspace.unfreeze',{workspace_id:'ws_s4_freeze',expected_revision:first.revision,reason:'incident resolved'},true);assert.equal(restored.frozen,false);
  await open(viewer,'admin','/platform-operations','平台运营与异常',null);await request(viewer,'platform.workspaces',{},false,403);
 });
 await step('S4-10 provider incidents and safe bounded audit export',async()=>{
  const event=await request(maker,'incident.create',{target_kind:'provider',target_id:'provider_s4a_agent',severity:'warning',code:'browser.review',reason:'合成故障，未调用供应商'},true,201);
  assert.equal(event.state,'open');assert.equal((await request(maker,'incident.resolve',{incident_id:event.id,expected_revision:event.revision,resolution:'本地验收已确认恢复'},true)).state,'resolved');
  await request(maker,'platform.audit');const download=maker.waitForEvent('download');await maker.getByRole('button',{name:'导出本页安全 JSON'}).click();assert.equal((await download).suggestedFilename(),'mender-current-page.json');
  await maker.screenshot({path:output+'/platform-desktop.png',fullPage:true});
 });
 await step('S4-10 JIT independent review/activation/read/revocation with no target membership',async()=>{
  await open(support,'admin','/support-approvals','危险审批与临时支持');
  const requestResult=await request(support,'support.request',{ttl_seconds:'900',reason:'只读支持浏览器验收'},true,201);const id=requestResult.id;assert.equal(requestResult.target_version,'');
  await open(checker,'admin','/support-approvals','危险审批与临时支持');await request(checker,'approval.get',{approval_id:id});await choose(checker,id);
  await request(checker,'support.approve',{note:'独立复核只读范围与900秒有效期'},true);
  const grant=await request(support,'support.activate',{approval_id:id},true,201);grantID=grant.id;
  const runs=await request(support,'support.runs');assert.ok(runs.some(r=>r.id==='run_s4_billing'));
  await request(support,'support.revoke',{grant_id:grantID,reason:'不能自行授予复核权限'},true,403);
  await request(checker,'support.revoke',{grant_id:grantID,reason:'独立复核人确认支持结束'},true);
  await request(support,'support.runs',{},false,403);
 });
 await step('S4-11 refund approval + immutable supplemental journal + idempotent replay',async()=>{
  await open(maker,'admin','/billing','账单与模拟支付');
  const summary=await request(maker,'billing.summary',{currency:'USD'});assert.equal(summary.charged_micro,'100');
  const binding={business_key:'browser_refund',basis_kind:'usage_settlement',basis_id:'run_s4_billing',direction:'credit',amount_micro:'40',currency:'USD',reason:'浏览器验收部分退款'};
  const r=await request(maker,'approval.commerce',{action:'commerce.refund',...binding,ttl_seconds:'900'},true,201);financeApproval=r.id;
  await request(checker,'approval.get',{approval_id:financeApproval});await choose(checker,financeApproval);
  await request(checker,'approval.approve',{note:'确认原Run、40micro和同一业务键'},true);
  const refundFields={business_key:binding.business_key,basis_id:binding.basis_id,amount_micro:'40',currency:'USD',reason:binding.reason,approval_id:financeApproval};
  const journal=await request(maker,'billing.refund',refundFields,true);assert.equal(journal.replay,false);
  assert.equal((await request(maker,'billing.refund',refundFields,true)).replay,true);
  const updated=await request(maker,'billing.summary',{currency:'USD'});assert.equal(updated.refunded_micro,'40');assert.equal(updated.net_billed_micro,'60');
  await maker.screenshot({path:output+'/billing-desktop.png',fullPage:true});
 });
 await step('S4-11 sandbox intent remains pending; no redirect/DOM callback can settle money',async()=>{
  const account={provider_id:'sandbox_browser',provider_account_id:'account_browser'};
  const created=await request(maker,'payment.create',{business_key:'browser_payment',...account,purpose:'collect_charge',billing_journal_id:process.env.MENDER_S4_CHARGE_ID,amount_micro:'100',currency:'USD'},true,201);
  const records=await request(maker,'payment.intents',account);assert.equal(records.find(v=>v.id===created.intent_id).state,'pending');
  const reconciliation=await request(maker,'payment.reconciliation',{...account,currency:'USD'});assert.equal(reconciliation.settled_collection_micro,'0');assert.equal(reconciliation.pending_intent_count,'1');
 });
 await step('responsive identity, accessible confirmation cancel and scope isolation',async()=>{
  await maker.setViewportSize({width:390,height:844});
  await maker.screenshot({path:output+'/billing-mobile.png',fullPage:true});
  assert.ok(await maker.locator('html').evaluate(el=>el.scrollWidth)<=391,'horizontal overflow');
  await select(maker,'payment.create',{business_key:'not_sent',provider_id:'sandbox_browser',provider_account_id:'account_browser',purpose:'collect_charge',billing_journal_id:process.env.MENDER_S4_CHARGE_ID,currency:'USD',amount_micro:'100'});
  await maker.getByRole('button',{name:'核对操作',exact:true}).click();await expect(maker.getByRole('dialog')).toBeVisible();await maker.keyboard.press('Escape');await expect(maker.getByRole('dialog')).not.toBeVisible();
  await maker.locator('input[name="workspace"]').fill('ws_s4_other');await maker.getByRole('button',{name:'切换工作区'}).click();await expect(maker.locator('.workbench-record')).toHaveCount(0);
  await maker.screenshot({path:output+'/billing-mobile-empty.png',fullPage:true});
 });
 assert.deepEqual(errors,[],'unexpected browser page errors');
 console.log('PASS: real Chromium workbenches, real HTTP authorization and isolated PostgreSQL; external IdP/PSP not certified.');
} catch(error) {
 results.push({name:currentStep,result:'failed',message:error instanceof Error?error.message:String(error)});
 for(let i=0;i<pages.length;i++)await pages[i].screenshot({path:`${output}/failure-${i}.png`,fullPage:true}).catch(()=>{});
 console.error(`FAIL: ${currentStep}: ${error instanceof Error?error.message:String(error)}`);process.exitCode=1;
} finally {
 await writeFile(output+'/results.json',JSON.stringify({scope:'real-browser-real-http-isolated-postgres',external_idp:false,external_psp:false,steps:results,page_errors:errors},null,2));await browser.close();
}
