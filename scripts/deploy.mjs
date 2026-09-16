import { spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { readFileSync, writeFileSync, mkdirSync, existsSync, cpSync, copyFileSync, openSync, closeSync, readSync, renameSync } from 'node:fs';
import { join, relative, sep, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { parse, stringify } from 'yaml';
import { scopedPath, validateConfig, validateWebCertificate, fileInventory, writeDeployment } from './lib/deployment.mjs';
import { inventory, verifyRelease } from './lib/release-artifact.mjs';

const root=fileURLToPath(new URL('../',import.meta.url));
const hash=b=>createHash('sha256').update(b).digest('hex');
function run(exe,args,options={}){
 const p=spawnSync(exe,args,{cwd:root,shell:false,windowsHide:true,encoding:'utf8',timeout:600000,maxBuffer:8*1024*1024,...options});
 if(p.status!==0){if(options.publicOutput)process.stderr.write((p.stderr||'').slice(-8000));throw Error(`${exe} ${args[0]} failed (exit ${p.status}); no deployment success asserted`);}
 return (p.stdout||'').trim();
}
function pnpm(script){const a=process.platform==='win32'?['-NoLogo','-NoProfile','-NonInteractive','-Command',`pnpm ${script}`]:[script];return run(process.platform==='win32'?'pwsh':'pnpm',a,{publicOutput:true});}
function identity(){const groups=['backend','frontend','scripts','deploy','package.json','pnpm-lock.yaml','toolchain.versions.json'];const names=run('git',['ls-files','--cached','--others','--exclude-standard','-z','--',...groups]).split('\0').filter(Boolean).sort();return {head:run('git',['rev-parse','HEAD']),source_sha256:hash(JSON.stringify([...new Set(names)].map(p=>[p,hash(readFileSync(join(root,p)))])))};}
function imageInfo(name){const data=JSON.parse(run('docker',['image','inspect',name]));if(data.length!==1)throw Error('Image identity missing');return data[0];}
export function verifyBuiltRelease(dir){
 const release=JSON.parse(readFileSync(join(dir,'release.json'),'utf8'));
 if(release.version!==1||!release.source?.source_sha256||release.platform!=='linux/amd64')throw Error('Invalid built release');
 const actual=fileInventory(join(dir,'context'));if(JSON.stringify(actual)!==JSON.stringify(release.files))throw Error('Built release bytes drifted');
 for(const name of ['runtime_image','web_image'])if(!/^sha256:[a-f0-9]{64}$/.test(release[name])||imageInfo(release[name]).Id!==release[name])throw Error('Built image is unavailable or substituted');
 if(!/^postgres@sha256:[a-f0-9]{64}$/.test(release.postgres_image))throw Error('PostgreSQL must be digest pinned');
 return release;
}
function build(output){
 const dir=scopedPath(root,output,'dist/releases/');if(existsSync(dir))throw Error('Release output already exists; never overwrite a release');
 pnpm('check:toolchain');pnpm('build');
 const source=identity();const nginx=imageInfo('nginx:1.30.5-alpine');const pg=imageInfo('postgres:18.6');
 const pinned=(info,prefix)=>{const v=info.RepoDigests?.find(x=>x.startsWith(prefix+'@sha256:'));if(!v)throw Error('Official base image digest unavailable');return v;};
 const trust=pinned(nginx,'nginx');const postgres=pinned(pg,'postgres');
 const context=join(dir,'context');mkdirSync(join(context,'bin'),{recursive:true});
 for(const name of ['api','worker','operator','provision','healthcheck','mender']){
  run('go',['build','-trimpath','-buildvcs=false','-ldflags=-s -w','-o',join(context,'bin',name),`./cmd/${name}`],{cwd:join(root,'backend'),env:{...process.env,GOOS:'linux',GOARCH:'amd64',CGO_ENABLED:'0'},publicOutput:true});
 }
 cpSync(join(root,'frontend/apps/console/dist'),join(context,'console'),{recursive:true});cpSync(join(root,'frontend/apps/admin/dist'),join(context,'admin'),{recursive:true});
 copyFileSync(join(root,'deploy/Dockerfile'),join(context,'Dockerfile'));
 const after=identity();if(after.head!==source.head||after.source_sha256!==source.source_sha256)throw Error('Source changed during build');
 const tag=source.source_sha256.slice(0,16);
 for(const target of ['runtime','web']){run('docker',['build','--platform','linux/amd64','--target',target,'--build-arg',`TRUST_IMAGE=${trust}`,'--label',`io.mender.source=${source.source_sha256}`,'-t',`mender-${target}:${tag}`,context],{publicOutput:true});}
 const release={version:1,platform:'linux/amd64',created_at:new Date().toISOString(),source,runtime_image:imageInfo(`mender-runtime:${tag}`).Id,web_image:imageInfo(`mender-web:${tag}`).Id,postgres_image:postgres,trust_image:trust,files:fileInventory(context)};
 writeFileSync(join(dir,'release.json'),JSON.stringify(release,null,2));
 const manifestPath=relative(root,join(dir,'release.json')).split(sep).join('/');
 writeFileSync(join(dir,'signing-inventory.json'),JSON.stringify(inventory(root,[manifestPath],source.head),null,2));
 console.log(`Built ${output}; source ${source.source_sha256}; all application runtime images use exact IDs. Sign signing-inventory.json before production up.`);
}
function prepare(configFile,releasePath,output){
 const c=validateConfig(JSON.parse(readFileSync(scopedPath(root,configFile),'utf8')));const release=verifyBuiltRelease(scopedPath(root,releasePath,'dist/releases/'));
 const dir=scopedPath(root,output,'.local/deploy/');if(existsSync(dir))throw Error('Deployment exists; refusing secret/PKI regeneration');mkdirSync(dir,{recursive:true,mode:0o700});
 run(process.execPath,['scripts/backend.mjs','run','./cmd/provision','local-pki',relative(join(root,'backend'),join(dir,'pki'))],{publicOutput:true});
 writeDeployment(root,dir,c,release);
 console.log(`Prepared ${output}; no service started. OIDC configured=${Boolean(c.oidc_issuer)}. Keep pki/ca.key offline; never mount it into runtime containers.`);
}
function readDeployment(input){
 const dir=scopedPath(root,input,'.local/deploy/');const d=JSON.parse(readFileSync(join(dir,'deployment.json'),'utf8'));validateConfig(d.config);const c=parse(readFileSync(join(dir,'compose.yaml'),'utf8'));
 if(c.name!==d.config.instance||!/^([a-f0-9]{32})$/.test(d.ownership||''))throw Error('Compose ownership mismatch');
 if(!Array.isArray(d.controls)||d.controls.length!==3||d.controls.map(x=>x.path).join('|')!=='compose.yaml|nginx.conf|database-entrypoint.sh')throw Error('Deployment control inventory is missing');
 for(const entry of d.controls)if(hash(readFileSync(join(dir,entry.path)))!==entry.sha256)throw Error('Deployment control drift requires explicit review; not automatically accepted');
 for(const s of Object.values(c.services))if(s.labels?.['io.mender.ownership']!==d.ownership)throw Error('Service ownership label differs');
 const ids=run('docker',['ps','-aq','--filter',`label=com.docker.compose.project=${c.name}`]).split(/\s+/).filter(Boolean);
 if(ids.length){const containers=JSON.parse(run('docker',['inspect',...ids]));for(const x of containers)if(x.Config?.Labels?.['io.mender.ownership']!==d.ownership)throw Error('Project name belongs to another existing deployment; refusing to mutate it');}
 const volume=c.name+'_pgdata';const volumes=run('docker',['volume','ls','-q','--filter',`name=^${volume}$`]).split(/\s+/).filter(Boolean);
 if(volumes.includes(volume)){const v=JSON.parse(run('docker',['volume','inspect',volume]))[0];if(v.Labels?.['io.mender.ownership']!==d.ownership)throw Error('Persistent volume belongs to another deployment');}
 return {dir,d,c};
}
function compose(dir,args,options){return run('docker',['compose','-f',join(dir,'compose.yaml'),...args],options);}
function verifyProductionSignature(releases,signed,keyFile){
 if(!signed||!keyFile)throw Error('Production requires --signed ENVELOPE and --trusted-key independently configured public key');
 const envelope=JSON.parse(readFileSync(scopedPath(root,signed),'utf8'));verifyRelease(root,envelope,readFileSync(scopedPath(root,keyFile)));
 const expected=relative(root,join(releases,'release.json')).split(sep).join('/');if(!envelope.payload.files.some(x=>x.path===expected))throw Error('Signature does not cover this exact release manifest');
}
function preflight(input,signed,keyFile){
 const {dir,d,c}=readDeployment(input);
 if(!Array.isArray(d.controls)||d.controls.length!==3||d.controls.map(x=>x.path).join('|')!=='compose.yaml|nginx.conf|database-entrypoint.sh')throw Error('Deployment control inventory is missing');
 for(const entry of d.controls)if(hash(readFileSync(join(dir,entry.path)))!==entry.sha256)throw Error('Deployment control drift requires explicit review; not automatically accepted');
 const releases=scopedPath(root,d.release_path||findReleasePath(d.release),'dist/releases/');const release=verifyBuiltRelease(releases);
 if(JSON.stringify(release)!==JSON.stringify(d.release))throw Error('Deployment release identity drift');
 for(const name of ['api-console','api-admin','worker','provision','operator'])if(c.services[name]?.image!==release.runtime_image)throw Error('Compose runtime image does not match the verified release');
 if(c.services.web?.image!==release.web_image||c.services.database?.image!==release.postgres_image)throw Error('Compose web/database image does not match the verified release');
 validateWebCertificate(readFileSync(join(dir,'secrets/web_certificate')),readFileSync(join(dir,'secrets/web_private_key')),[new URL(d.config.console_origin).hostname,new URL(d.config.admin_origin).hostname]);
 if(d.config.environment==='production'||signed||keyFile){
  verifyProductionSignature(releases,signed,keyFile);
 }
 for(const name of ['api-console','api-admin','worker','web']){
  const s=c.services[name];if(!s||s.read_only!==true||s.privileged||s.network_mode||s.user==='0:0'||!s.cap_drop.includes('ALL')||!s.security_opt.includes('no-new-privileges:true'))throw Error('Runtime hardening drift');
  if(s.secrets.includes('admin_dsn')||s.secrets.includes('db_password')||s.secrets.includes('role_plan'))throw Error('Administrator secrets must not reach runtimes');
 }
 if(c.services.database.ports||c.services['api-console'].ports||c.services['api-admin'].ports||!c.networks.private.internal)throw Error('Database/API port exposure forbidden');
 for(const s of Object.values(c.services)){if(s.environment?.MENDER_PAYMENT_MODE&&s.environment.MENDER_PAYMENT_MODE!=='sandbox')throw Error('Live payment not implemented');for(const n of s.secrets||[])if(!c.secrets[n]||!existsSync(join(dir,c.secrets[n].file)))throw Error('Required mounted secret missing');}
 compose(dir,['config','--quiet']);console.log('Preflight passed: immutable release, separate runtime roles, mounted secrets, private database, configured TLS. This does not attest business approval.');return {dir,d,c};
}
function findReleasePath(release){const dir=`dist/releases/${release.source.source_sha256.slice(0,16)}`;if(existsSync(join(root,dir,'release.json')))return dir;throw Error('Deployment release_path required');}
function hashFile(path){const h=createHash('sha256');const f=openSync(path,'r');const buffer=Buffer.alloc(1024*1024);try{let n;while((n=readSync(f,buffer,0,buffer.length,null))>0)h.update(buffer.subarray(0,n));}finally{closeSync(f);}return h.digest('hex');}
function backup(input,output){
 const {dir,d}=readDeployment(input);const destination=scopedPath(root,output,'.local/backups/');if(existsSync(destination))throw Error('Backup output already exists');mkdirSync(destination,{recursive:true,mode:0o700});
 const file=join(destination,'database.dump');const fd=openSync(file,'wx',0o600);
 try{run('docker',['compose','-f',join(dir,'compose.yaml'),'exec','-T','database','pg_dump','-U','postgres','-d','mender','--format=custom','--no-owner','--no-acl'],{stdio:['ignore',fd,'pipe']});}finally{closeSync(fd);}
 const record={version:1,created_at:new Date().toISOString(),instance:d.config.instance,ownership:d.ownership,release:d.release,sha256:hashFile(file),contains:['database-schema','data'],excludes:['role-passwords','ACL','mounted-secrets','CA-private-key','object-files','external-side-effects'],restoration_approval:'required'};
 writeFileSync(join(destination,'backup.json'),JSON.stringify(record,null,2),{flag:'wx',mode:0o600});console.log('Database backup recorded; preserve protected secrets/role configuration separately. Restore is never automatic.');
}
function switchRelease(input,releasePath,signed,keyFile){
 const {dir,d,c}=readDeployment(input);const nextDir=scopedPath(root,releasePath,'dist/releases/');const next=verifyBuiltRelease(nextDir);
 if(next.postgres_image!==d.release.postgres_image)throw Error('Database image changes require a separately reviewed upgrade; not part of application rollback');
 if(d.config.environment==='production'||signed||keyFile)verifyProductionSignature(nextDir,signed,keyFile);
 const running=run('docker',['ps','-q','--filter',`label=com.docker.compose.project=${c.name}`]);if(running)throw Error('Stop this deployment before switching release; take a backup and review migration compatibility');
 const revision='revision-'+Date.now();const archive=join(dir,revision);mkdirSync(archive,{mode:0o700});for(const name of ['compose.yaml','deployment.json'])copyFileSync(join(dir,name),join(archive,name));
 for(const name of ['api-console','api-admin','worker','provision','operator'])c.services[name].image=next.runtime_image;c.services.web.image=next.web_image;
 const composeText=stringify(c);d.previous_revision=revision;d.release=next;d.release_path=releasePath;d.controls.find(x=>x.path==='compose.yaml').sha256=hash(composeText);
 writeFileSync(join(dir,'compose.yaml.next'),composeText,{mode:0o600,flag:'wx'});writeFileSync(join(dir,'deployment.json.next'),JSON.stringify(d,null,2),{mode:0o600,flag:'wx'});
 renameSync(join(dir,'compose.yaml.next'),join(dir,'compose.yaml'));renameSync(join(dir,'deployment.json.next'),join(dir,'deployment.json'));
 console.log(`Release switched; previous controls preserved in ${revision}. No database migration or process startup performed; run preflight and explicit up next.`);
}
function main(){
 const [command,...args]=process.argv.slice(2);
 if(command==='build'&&args.length===1)return build(args[0]);
 if(command==='prepare'&&args.length===3){prepare(...args);const p=join(scopedPath(root,args[2],'.local/deploy/'),'deployment.json');const d=JSON.parse(readFileSync(p));d.release_path=args[1];writeFileSync(p,JSON.stringify(d,null,2),{mode:0o600});return;}
 if(command==='backup'&&args.length===3&&args[2]==='--apply')return backup(args[0],args[1]);
 if(command==='switch-release'){
  const input=args.shift(),releasePath=args.shift();let signed,key,apply=false,reviewed=false;
  while(args.length){const flag=args.shift();if(flag==='--signed')signed=args.shift();else if(flag==='--trusted-key')key=args.shift();else if(flag==='--apply')apply=true;else if(flag==='--migrations-reviewed')reviewed=true;else throw Error('Unknown switch-release argument');}
  if(!apply||!reviewed)throw Error('switch-release requires --apply and --migrations-reviewed');return switchRelease(input,releasePath,signed,key);
 }
 if(['preflight','up'].includes(command)){
  const input=args.shift();let signed,key;while(args.length){const flag=args.shift();if(flag==='--signed')signed=args.shift();else if(flag==='--trusted-key')key=args.shift();else if(flag==='--apply'&&command==='up')continue;else throw Error('Unknown deployment argument');}
  if(command==='up'&&!process.argv.includes('--apply'))throw Error('up requires explicit --apply; it creates persistent local resources');
  const {dir}=preflight(input,signed,key);if(command==='preflight')return;
  compose(dir,['up','-d','--wait','database']);compose(dir,['run','--rm','--no-deps','provision'],{publicOutput:true});compose(dir,['up','-d','--wait','--wait-timeout','120','api-console','api-admin','worker'],{publicOutput:true});compose(dir,['up','-d','--force-recreate','--wait','--wait-timeout','90','web'],{publicOutput:true});console.log('Stack started with Web readiness verified. Database volume is persistent; no production or G4 approval is implied.');return;
 }
 if(['status','stop'].includes(command)&&args.length===1){const {dir}=readDeployment(args[0]);console.log(compose(dir,command==='status'?['ps']:['stop']));return;}
 if(command==='operator'&&args.length>=2){const {dir}=readDeployment(args.shift());const allowed=['provision-human','provision-platform-staff','issue-key','revoke-key'];if(!allowed.includes(args[0]))throw Error('Use explicit provisioning for schema/role operations');console.log(compose(dir,['run','--rm','--no-deps','operator',...args]));return;}
 throw Error('Usage: deploy build dist/releases/ID | prepare CONFIG RELEASE_DIR .local/deploy/ID | preflight DIR [--signed FILE --trusted-key FILE] | up DIR --apply [--signed FILE --trusted-key FILE] | status DIR | stop DIR | backup DIR .local/backups/ID --apply | switch-release DIR RELEASE_DIR --apply --migrations-reviewed [--signed FILE --trusted-key FILE] | operator DIR provision-human|provision-platform-staff|issue-key|revoke-key ...');
}
if(process.argv[1]&&resolve(process.argv[1])===fileURLToPath(import.meta.url)){try{main();}catch(e){console.error(e.message);process.exitCode=1;}}
