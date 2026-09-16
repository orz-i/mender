import { createConsoleIdentityClient } from '@mender/api-client';
import { createCommercialClient, type CommercialJSON } from '@mender/api-client/commercial';
import { commands, type ResultRecord } from '../domain/workflow';
import type { Gateway } from '../application/gateway';
const labels: Record<string,string> = {"workspace_id":"工作区","publisher_id":"发布者","plugin_id":"插件","version":"版本","revision":"精确版本","expected_revision":"期望版本","state":"服务端状态","reason":"理由","id":"记录ID","approval_id":"审批ID","target_revision":"目标版本","requester_user_id":"申请人","reviewer_user_id":"复核人","display_name":"名称","manifest_sha256":"清单SHA256","parameters_sha256":"精确参数SHA256","ready":"预检就绪","code":"代码","amount_micro":"金额（micro）","currency":"币种","net_billed_micro":"净账单（micro）","pending_reconcile_count":"待对账Run数","difference_micro":"差额（micro）","frozen":"已冻结","replay":"幂等重放","intent_id":"支付意图ID","mode":"模式","journal_id":"Journal ID","expires_at":"到期时间","consumed_at":"消费时间","scopes":"授权范围"};
function csrf() { const raw = document.cookie.split(';').map(part=>part.trim()).find(part=>part.startsWith('mender_csrf=')); return raw ? decodeURIComponent(raw.slice('mender_csrf='.length)) : ''; }
function present(data: CommercialJSON, operation: string): ResultRecord[] {
  const records: ResultRecord[] = [];
  function add(value: CommercialJSON, group: string) {
    if(Array.isArray(value)){ value.forEach(item=>add(item,group)); return; }
    if(!value || typeof value !== 'object') return;
    const facts: ResultRecord['facts'] = [], inputs: Record<string,string> = {};
    for(const [key,item] of Object.entries(value)) {
      if(Array.isArray(item) && key!=='capabilities' && key!=='scopes'){ item.forEach(entry=>add(entry,key)); continue; }
      if(key==='plugin_version' && item && typeof item==='object'){add(item,key);continue;}
      const string = typeof item==='string' ? item : item===null ? '—' : JSON.stringify(item,null,2);
      facts.push({label:labels[key]??key,value:string});
      if(typeof item==='string') inputs[key]=item;
      if(key==='manifest')inputs.manifest=JSON.stringify(item,null,2);
    }
    if(!facts.length) return;
    if(inputs.revision)inputs.expected_revision=inputs.revision;
    if(inputs.id){
      if(operation.includes('incident'))inputs.incident_id=inputs.id;
      else if(operation==='support.activate'||operation==='support.revoke')inputs.grant_id=inputs.id;
      else if(operation.startsWith('approval.')||operation.startsWith('plugin.')||operation.startsWith('support.')) inputs.approval_id=inputs.id;
    }
    if(inputs.journal_id)inputs.billing_journal_id=inputs.journal_id;
    records.push({key:group+':'+records.length,title:String(value.display_name??value.plugin_id??value.workspace_id??value.provider_id??value.id??value.release_id??group),state:String(value.state??value.disposition??(typeof value.ready==='boolean'?(value.ready?'预检通过':'预检未通过'):'')),facts,inputs});
  }
  add(data,'结果');return records;
}
export function createGateway(): Gateway {
  const client=createCommercialClient(); const identity=createConsoleIdentityClient();
  return { session: signal=>identity.getSession(signal), async execute(command,values,signal) {
    if(!(commands as readonly string[]).includes(command))throw new Error('该工作台未开放此操作');
    let input: Record<string,CommercialJSON>={...values};
    if(Object.hasOwn(input,'manifest')){
      let parsed: unknown;try { parsed=JSON.parse(values.manifest!); } catch { throw new Error('Manifest不是有效JSON'); }
      if(!parsed||typeof parsed!=='object'||Array.isArray(parsed))throw new Error('Manifest必须是JSON对象');
      input={...(parsed as Record<string,CommercialJSON>),workspace_id:values.workspace_id!};
    }
    for(const key of ['ttl_seconds','observation_seconds'])if(Object.hasOwn(input,key)){
      if(!/^[1-9]\d{0,5}$/.test(values[key]??''))throw new Error('时间必须是有界整数秒');input[key]=Number(values[key]);
    }
    if(Object.hasOwn(input,'scopes'))input.scopes=values.scopes!.split(',').map(s=>s.trim());
    const result=await client.call(command,input,csrf(),signal);
    return {records:present(result,command),note:'已返回服务端事实。列表为有界快照；写入没有自动重试，刷新列表核对最终状态。'};
  }};
}
