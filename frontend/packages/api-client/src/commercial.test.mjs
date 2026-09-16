import assert from 'node:assert/strict';
import { test } from 'node:test';
import { createCommercialClient, commercialOperations } from './commercial.ts';
const at='2026-09-16T08:00:00Z';
const frozen=(revision='9007199254740993')=>({workspace_id:'ws_a',frozen:true,revision,reason:'reviewed incident',actor_user_id:'operator_a',created_at:at,updated_at:at});
const target=()=>({workspace_id:'ws_a',expected_revision:'9007199254740992',reason:'reviewed incident'});
const manifest=()=>({apiVersion:'mender.io/plugin/v1alpha1',plugin_id:'example.tool',publisher_id:'publisher_a',version:'1.0.0',display_name:'Example',description:'reviewed capability',capabilities:[{kind:'agent',deployment_revision:'deploy_a'}]});
const pluginVersion=()=>({plugin_id:'example.tool',publisher_id:'publisher_a',version:'1.0.0',revision:'9007199254740993',state:'draft',manifest:manifest(),manifest_sha256:'a'.repeat(64),created_at:at,updated_at:at,submitted_at:null,approved_at:null,published_at:null,deprecated_at:null,disabled_at:null});
const approval=()=>({id:'approval_a',requester_user_id:'maker_a',subject_kind:'platform_staff',subject_id:'maker_a',action:'support.workspace_read',target_kind:'workspace',target_id:'ws_a',target_version:'',parameters:{scopes:['run:read'],ttl_seconds:900},parameters_sha256:'a'.repeat(64),amount_micro:null,currency:null,reason:'support incident',state:'pending',requested_at:at,expires_at:'2026-09-16T08:15:00Z',reviewer_user_id:null,reviewed_at:null,decision_note:'',consumed_at:null});
const reply=(data)=>Response.json({data});

test('commercial client uses same-origin CSRF with exact revision strings and no bearer',async()=>{
 let calls=0;const client=createCommercialClient(async(url,init)=>{
  calls++;assert.equal(url,'/api/admin/v1/platform/workspaces/ws_a/freeze');assert.equal(init.credentials,'same-origin');assert.equal(init.redirect,'error');assert.equal(init.cache,'no-store');
  const headers=new Headers(init.headers);assert.equal(headers.get('Authorization'),null);assert.equal(headers.get('X-Mender-CSRF'),'csrf_a');assert.deepEqual(JSON.parse(init.body),{expected_revision:'9007199254740992',reason:'reviewed incident'});return reply(frozen());
 });assert.equal((await client.call('workspace.freeze',target(),'csrf_a')).revision,'9007199254740993');assert.equal(calls,1);
});
test('write authority, missing CSRF, unknown endpoints and traversal reject before fetch',async()=>{
 let calls=0;const client=createCommercialClient(async()=>{calls++;return reply(frozen());});
 for(const input of [{...target(),actor_user_id:'attacker'},{...target(),frozen:true},{...target(),expected_revision:12},{...target(),workspace_id:'../ws_b'},{...target(),workspace_id:'%2e%2e'},{...target(),workspace_id:'ws_a?other=1'}])await assert.rejects(client.call('workspace.freeze',input,'csrf_a'));
 await assert.rejects(client.call('workspace.freeze',target(),''));await assert.rejects(client.call('constructor',{}));await assert.rejects(client.call('https://evil.test',{}));assert.equal(calls,0);
});
test('403/409 and network uncertainty do not retry or leak arbitrary error bodies',async()=>{
 for(const status of [403,409,503]){let calls=0;const client=createCommercialClient(async()=>{calls++;return Response.json({secret:'not displayed'},{status});});
 await assert.rejects(client.call('approval.approve',{workspace_id:'ws_a',approval_id:'approval_a',note:'reviewed'},'csrf_a'),error=>error.status===status&&!error.message.includes('not displayed'));assert.equal(calls,1);}
 let calls=0;await assert.rejects(createCommercialClient(async()=>{calls++;throw new TypeError('network unavailable');}).call('workspace.freeze',target(),'csrf_a'));assert.equal(calls,1);
});
test('JIT empty target revision and reviewed exact parameter shape are supported',async()=>{
 const input={workspace_id:'ws_a',scopes:['run:read'],ttl_seconds:900,reason:'support incident'};
 const result=await createCommercialClient(async()=>reply(approval())).call('support.request',input,'csrf_a');assert.deepEqual(result.parameters,{scopes:['run:read'],ttl_seconds:900});assert.equal(result.target_version,'');
 for(const mutate of [(v)=>{v.parameters.secret='leak';},(v)=>{v.parameters.scopes=['secret:read'];},(v)=>{v.parameters.ttl_seconds=7200;},(v)=>{v.action='commerce.refund';}]){const value=approval();mutate(value);await assert.rejects(createCommercialClient(async()=>reply(value)).call('support.request',input,'csrf_a'));}
});
test('plugin preflight/submit/publish send empty bodies and extra script fields cannot enter draft',async()=>{
 let calls=0;const client=createCommercialClient(async(url,init)=>{calls++;assert.equal(init.body,undefined);assert.equal(new Headers(init.headers).get('Content-Type'),null);return url.endsWith('preflight')?reply({ready:false,issues:[{code:'NOT_READY',target_id:'deploy_a'}]}):reply({approval_id:'approval_a',plugin_version:pluginVersion()});});
 for(const command of ['version.preflight','version.submit','version.publish'])await client.call(command,{workspace_id:'ws_a',plugin_id:'example.tool',version:'1.0.0'},'csrf_a');
 await assert.rejects(client.call('version.create',{workspace_id:'ws_a',...manifest(),script:'unsafe'},'csrf_a'));assert.equal(calls,3);
});
test('draft create sends exactly the manifest and frozen publishers can be read',async()=>{
 const client=createCommercialClient(async(url,init)=>{assert.equal(url,'/api/console/v1/workspaces/ws_a/publisher/plugins/example.tool/versions');assert.deepEqual(JSON.parse(init.body),manifest());return reply(pluginVersion());});await client.call('version.create',{workspace_id:'ws_a',...manifest()},'csrf_a');
 const snapshot={publishers:[{publisher_id:'publisher_a',display_name:'Frozen',state:'frozen',owned:true,created_at:at,updated_at:at}],plugins:[],plugin_versions:[]};assert.equal((await createCommercialClient(async()=>reply(snapshot)).call('publisher.snapshot',{workspace_id:'ws_a'})).publishers[0].state,'frozen');
});
test('financial approval displays explicit basis, amount and business-key bindings',async()=>{
 const value={...approval(),action:'commerce.refund',target_kind:'billing_refund',target_version:'refund-key',parameters:{basis_id:'run_a',basis_kind:'usage_settlement',business_key:'refund-key',direction:'credit'},amount_micro:'9007199254740993',currency:'USD'};
 const result=await createCommercialClient(async()=>reply([value])).call('approval.list',{workspace_id:'ws_a'});assert.equal(result[0].parameters.business_key,'refund-key');assert.equal(result[0].amount_micro,'9007199254740993');
 value.parameters.token='leak';await assert.rejects(createCommercialClient(async()=>reply([value])).call('approval.list',{workspace_id:'ws_a'}));
});
test('sandbox intent mode is fixed; floats, exponent, leading zero and overflow never send',async()=>{
 const input={workspace_id:'ws_a',business_key:'payment_a',provider_id:'provider_a',provider_account_id:'account_a',mode:'sandbox',purpose:'collect_charge',billing_journal_id:'journal_a',currency:'USD',amount_micro:'9007199254740993'};
 let count=0;const client=createCommercialClient(async()=>{count++;return reply({intent_id:'intent_a',replay:false,mode:'sandbox'});});await client.call('payment.create',input,'csrf_a');
 for(const value of ['0','-1','1.5','1e3','01',1,'9223372036854775808'])await assert.rejects(client.call('payment.create',{...input,amount_micro:value},'csrf_a'));await assert.rejects(client.call('payment.create',{...input,mode:'live'},'csrf_a'));assert.equal(count,1);
});
test('payment metadata is required and browser client exposes no callback write',async()=>{
 const query={workspace_id:'ws_a',provider_id:'provider_a',provider_account_id:'account_a'};
 for(const value of [{data:[]},{data:[],meta:{mode:'live'}},{data:[],meta:{mode:'sandbox',redirect_success:true}}])await assert.rejects(createCommercialClient(async()=>Response.json(value)).call('payment.intents',query));
 const intent={id:'intent_a',business_key:'payment_a',purpose:'collect_charge',billing_journal_id:'journal_a',currency:'USD',amount_micro:'100',state:'pending',revision:'1',created_at:at,updated_at:at,settled_at:null,failed_at:null};
 const result=await createCommercialClient(async()=>Response.json({data:[intent],meta:{mode:'sandbox'}})).call('payment.intents',query);assert.equal(result[0].state,'pending');assert.ok(!Object.keys(commercialOperations).some(key=>key.includes('callback')&&commercialOperations[key].method!=='GET'));
});
test('unknown response keys, huge payloads, wrong content type and numeric revisions fail closed',async()=>{
 for(const item of [{...frozen(),raw_body:'secret'},frozen(Number('9007199254740993'))])await assert.rejects(createCommercialClient(async()=>reply(item)).call('workspace.freeze',target(),'csrf_a'));
 await assert.rejects(createCommercialClient(async()=>new Response('x'.repeat(2*1024*1024+1),{headers:{'content-type':'application/json'}})).call('platform.audit',{}));await assert.rejects(createCommercialClient(async()=>new Response('<html>auth</html>',{headers:{'content-type':'text/html'}})).call('platform.audit',{}));
});
test('incident actions use the real Go target and severity vocabulary',async()=>{
 let calls=0;const client=createCommercialClient(async()=>{calls++;return reply({id:'inc_a',target_kind:'provider',target_id:'provider_a',severity:'warning',code:'provider_unavailable',state:'open',opened_by_user_id:'operator_a',open_reason:'investigate',resolved_by_user_id:null,resolution:'',revision:'1',opened_at:at,updated_at:at,resolved_at:null});});
 const input={target_kind:'provider',target_id:'provider_a',severity:'warning',code:'provider_unavailable',reason:'investigate'};await client.call('incident.create',input,'csrf_a');await assert.rejects(client.call('incident.create',{...input,target_kind:'run'},'csrf_a'));await assert.rejects(client.call('incident.create',{...input,severity:'high'},'csrf_a'));assert.equal(calls,1);
});
