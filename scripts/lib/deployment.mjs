import { randomBytes, createHash, X509Certificate, createPrivateKey, createPublicKey } from 'node:crypto';
import { readFileSync, writeFileSync, mkdirSync, existsSync, lstatSync, readdirSync, copyFileSync, chownSync } from 'node:fs';
import { resolve, relative, join, isAbsolute, sep } from 'node:path';
import { stringify } from 'yaml';

export const capabilities = [
 ['runtime','MENDER_DATABASE_URL'], ['browser-session','MENDER_BROWSER_SESSION_DATABASE_URL'],
 ['admission','MENDER_ADMISSION_DATABASE_URL'],['cancellation','MENDER_CANCELLATION_DATABASE_URL'],
 ['catalog-manager','MENDER_CATALOG_MANAGER_DATABASE_URL'],['connection-manager','MENDER_CONNECTION_MANAGER_DATABASE_URL'],
 ['publisher-manager','MENDER_PUBLISHER_MANAGER_DATABASE_URL'],['governance-reviewer','MENDER_GOVERNANCE_REVIEWER_DATABASE_URL'],
 ['governance-policy-manager','MENDER_GOVERNANCE_POLICY_MANAGER_DATABASE_URL'],['governance-execution-confirmer','MENDER_GOVERNANCE_EXECUTION_CONFIRMER_DATABASE_URL'],
 ['release-manager','MENDER_RELEASE_MANAGER_DATABASE_URL'],['dangerous-operation-manager','MENDER_DANGEROUS_OPERATION_MANAGER_DATABASE_URL'],
 ['support-reader','MENDER_SUPPORT_READER_DATABASE_URL'],['platform-admin-manager','MENDER_PLATFORM_ADMIN_MANAGER_DATABASE_URL'],
 ['billing-manager','MENDER_BILLING_MANAGER_DATABASE_URL'],['payment-manager','MENDER_PAYMENT_MANAGER_DATABASE_URL'],
 ['commerce-observer','MENDER_COMMERCE_OBSERVER_DATABASE_URL'],['worker','MENDER_WORKER_DATABASE_URL'],
 ['executor','MENDER_EXECUTOR_DATABASE_URL'],['reconciler','MENDER_RECONCILER_DATABASE_URL'],['settlement','MENDER_SETTLEMENT_DATABASE_URL'],
];
export function scopedPath(root, input, prefix) {
 if(typeof input!=='string'||!input||isAbsolute(input)||input.includes('\\')||input.split('/').some(x=>!x||x==='.'||x==='..')||input.includes(':'))throw Error('Invalid workspace-relative deployment path');
 if(prefix&&!input.startsWith(prefix))throw Error('Output must be inside '+prefix);
 const p=resolve(root,input); if(!p.startsWith(resolve(root)+sep))throw Error('Path escapes workspace');
 let at=resolve(root);for(const part of relative(root,p).split(sep)){at=join(at,part);if(existsSync(at)&&lstatSync(at).isSymbolicLink())throw Error('Linked deployment paths are forbidden');}
 return p;
}
export function validateConfig(c) {
 const allowed=['version','environment','instance','console_origin','admin_origin','oidc_issuer','oidc_client_id','oidc_client_secret_file','oidc_ca_file','local_oidc_host_gateway','local_oidc_fixture','web_certificate_file','web_private_key_file','http_port','https_port','worker_workspaces'];
 if(!c||Object.keys(c).some(k=>!allowed.includes(k))||c.version!==1||!['local','production'].includes(c.environment)||!/^mender_[a-z0-9_]{3,16}$/.test(c.instance))throw Error('Invalid deployment configuration or unknown field');
 for(const k of ['http_port','https_port'])if(!Number.isInteger(c[k])||c[k]<(c.environment==='local'?1024:1)||c[k]>65535)throw Error('Invalid host port; local fixtures use unprivileged ports');
 if(c.http_port===c.https_port)throw Error('HTTP and HTTPS ports must differ');
 if(c.worker_workspaces!==undefined&&(!Array.isArray(c.worker_workspaces)||c.worker_workspaces.length<1||c.worker_workspaces.length>64||new Set(c.worker_workspaces).size!==c.worker_workspaces.length||c.worker_workspaces.some(x=>typeof x!=='string'||!/^[A-Za-z0-9_-]{1,128}$/.test(x))))throw Error('Invalid explicit Worker workspace scope');
 if(c.environment==='production'&&!c.worker_workspaces)throw Error('Production requires explicit Worker workspace scope');
 if(c.local_oidc_host_gateway!==undefined&&(c.local_oidc_host_gateway!==true||c.environment!=='local'||new URL(c.oidc_issuer).hostname!=='idp.localhost'))throw Error('Host-gateway fixture is local-only and restricted to idp.localhost');
 if(c.local_oidc_fixture!==undefined){
  if(c.local_oidc_fixture!==true||c.environment!=='local'||!c.oidc_issuer||new URL(c.oidc_issuer).hostname!=='idp.localhost'||c.local_oidc_host_gateway)throw Error('Embedded local OIDC fixture is local-only and restricted to idp.localhost');
  const port=Number(new URL(c.oidc_issuer).port||443);if(!Number.isInteger(port)||port<1024||port>65535||port===c.http_port||port===c.https_port)throw Error('Embedded local OIDC fixture requires a distinct unprivileged port');
 }
 for(const k of ['console_origin','admin_origin']){
  const u=new URL(c[k]);if(u.protocol!=='https:'||u.origin!==c[k]||!/^([a-z0-9-]+\.)+[a-z0-9-]+$/.test(u.hostname))throw Error('Invalid HTTPS UI origin');
  if(Number(u.port||443)!==c.https_port)throw Error('UI origin port must match the published HTTPS port');
  if(c.environment==='production'&&(u.hostname.endsWith('.localhost')||u.hostname.endsWith('.test')||u.hostname.endsWith('.invalid')||u.hostname.includes('example')))throw Error('Production requires real, distinct reviewed hostnames');
 }
 if(new URL(c.console_origin).hostname===new URL(c.admin_origin).hostname)throw Error('Console and Admin require different hosts, not merely different ports');
 if(c.oidc_issuer){const u=new URL(c.oidc_issuer);if(u.protocol!=='https:'||u.username||u.password||u.search||u.hash)throw Error('Invalid OIDC issuer');}
 if(c.environment==='production'&&(!c.oidc_issuer||!c.oidc_client_id||!c.oidc_client_secret_file||!c.web_certificate_file||!c.web_private_key_file))throw Error('Production requires real OIDC and externally supplied HTTPS certificate/key');
 if(c.oidc_issuer&&!c.oidc_client_id)throw Error('OIDC client configuration is incomplete');
 if(c.oidc_issuer&&!c.local_oidc_fixture&&!c.oidc_client_secret_file)throw Error('OIDC client secret file is required outside the embedded local fixture');
 if(c.local_oidc_fixture&&c.oidc_client_secret_file)throw Error('Embedded local OIDC fixture generates its own deployment-scoped client secret');
 if(c.local_oidc_fixture&&c.oidc_client_id!=='mender-local-demo')throw Error('Embedded local OIDC fixture uses the fixed mender-local-demo client');
 return c;
}
export function validateWebCertificate(certBytes,keyBytes,hosts,now=Date.now()){
 const cert=new X509Certificate(certBytes);const key=createPrivateKey(keyBytes);
 if(Date.parse(cert.validFrom)>now||Date.parse(cert.validTo)<=now+7*86400000)throw Error('TLS certificate must remain valid for at least seven days');
 if(!createPublicKey(key).export({format:'der',type:'spki'}).equals(cert.publicKey.export({format:'der',type:'spki'})))throw Error('TLS certificate and private key do not match');
 for(const host of hosts)if(!cert.checkHost(host))throw Error('TLS certificate does not cover both UI hosts');
}
export function fileInventory(root) {
 const result=[];
 function walk(dir){for(const e of readdirSync(dir,{withFileTypes:true})){const p=join(dir,e.name);if(e.isSymbolicLink())throw Error('Release must contain regular files only');if(e.isDirectory())walk(p);else if(e.isFile()){const b=readFileSync(p);result.push({path:relative(root,p).split(sep).join('/'),size:b.length,sha256:createHash('sha256').update(b).digest('hex')});}}}
 walk(root);return result.sort((a,b)=>a.path.localeCompare(b.path));
}
export function nginxConfig(c){
 const hosts=[new URL(c.console_origin).hostname,new URL(c.admin_origin).hostname];
 const servers=hosts.map((host,i)=>`server {
 listen 8443 ssl; server_name ${host};
 ssl_certificate /run/secrets/web_certificate; ssl_certificate_key /run/secrets/web_private_key;
 ssl_protocols TLSv1.2 TLSv1.3; ssl_session_tickets off;
 add_header X-Content-Type-Options nosniff always; add_header Referrer-Policy no-referrer always;
 add_header X-Frame-Options DENY always;
 add_header Content-Security-Policy "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'" always;
 ${c.environment==='production'?'add_header Strict-Transport-Security "max-age=31536000" always;':''}
 root /srv/${i===0?'console':'admin'};
 location ~ ^/(api|auth|mcp)(/|$) {
  access_log off; proxy_pass http://api-${i===0?'console':'admin'}:18080;
  proxy_http_version 1.1; proxy_set_header Host $http_host;
  proxy_set_header X-Forwarded-Proto https; proxy_set_header X-Forwarded-Host $http_host;
  proxy_set_header X-Forwarded-For $remote_addr; proxy_set_header Forwarded "";
  proxy_buffering off; proxy_request_buffering on; proxy_next_upstream off;
  proxy_connect_timeout 5s; proxy_read_timeout 65s;
 }
 location ~ ^/(healthz|readyz)$ { access_log off; proxy_pass http://api-${i===0?'console':'admin'}:18080; }
 location ~ /\\. { deny all; }
 location /assets/ { try_files $uri =404; expires 1y; }
 location / { expires -1; try_files $uri /index.html; }
}`).join('\n');
 return `pid /tmp/nginx.pid; error_log /dev/stderr warn; worker_processes auto;
events { worker_connections 1024; }
http {
 include /etc/nginx/mime.types; default_type application/octet-stream;
 server_tokens off; client_max_body_size 6m;
 client_body_temp_path /tmp/client_temp; proxy_temp_path /tmp/proxy_temp;
 fastcgi_temp_path /tmp/fastcgi_temp; uwsgi_temp_path /tmp/uwsgi_temp; scgi_temp_path /tmp/scgi_temp;
 log_format safe '$request_method $status $request_time'; access_log /dev/stdout safe;
 server { listen 8080 default_server; server_name _; return 404; }
 server { listen 8443 ssl default_server; ssl_reject_handshake on; }
 server { listen 127.0.0.1:8081; server_name localhost; location = /readyz { access_log off; proxy_pass http://api-console:18080/readyz; } location / { return 404; } }
 server { listen 8080; server_name ${hosts[0]}; return 308 ${c.console_origin}$request_uri; }
 server { listen 8080; server_name ${hosts[1]}; return 308 ${c.admin_origin}$request_uri; }
 ${servers}
}
`;
}

// Only writes to an already new output directory. Existing deployment secrets
// are never regenerated as part of startup, upgrade or an idempotent retry.
export function writeDeployment(root,dir,c,release){
 validateConfig(c);if(!release?.runtime_image||!release?.web_image||!release?.postgres_image)throw Error('A built immutable release is required');
 const secretDir=join(dir,'secrets');mkdirSync(secretDir,{mode:0o700});
 const secret=(name,b)=>{writeFileSync(join(secretDir,name),b,{flag:'wx',mode:0o600});return {file:`./secrets/${name}`};};
 const secrets={};const add=(n,b)=>{secrets[n]=secret(n,b);return n;};
 const baseDSN=(u,p)=>`postgres://${u}:${p}@database:5432/mender?sslmode=verify-full&sslrootcert=/run/secrets/db_ca`;
 add('db_password',randomBytes(32).toString('hex'));
 add('admin_dsn',baseDSN('postgres',readFileSync(join(secretDir,'db_password'),'utf8')));
 add('cursor',randomBytes(32).toString('base64url'));
 add('flow_console',randomBytes(32).toString('base64url'));add('flow_admin',randomBytes(32).toString('base64url'));
 const roles=[];const mappings={};
 for(const [grant,key]of capabilities){const suffix=grant.replaceAll('-','_');const name=(c.instance+'_'+suffix);if(name.length>63)throw Error('Role name too long');const password=randomBytes(32).toString('hex');add('password_'+suffix,password);add('dsn_'+suffix,baseDSN(name,password));roles.push({name,grant,password_file:'/run/secrets/password_'+suffix});mappings[key+'_FILE']='/run/secrets/dsn_'+suffix;}
 add('role_plan',JSON.stringify({version:1,database:'mender',instance:c.instance,roles},null,2));
 add('db_ca',readFileSync(join(dir,'pki/ca.crt')));add('db_certificate',readFileSync(join(dir,'pki/database.crt')));add('db_private_key',readFileSync(join(dir,'pki/database.key')));
 if(c.oidc_issuer)add('oidc_client_secret',c.local_oidc_fixture?randomBytes(32).toString('base64url'):readFileSync(scopedPath(root,c.oidc_client_secret_file)));
 if(c.oidc_ca_file)add('oidc_ca',readFileSync(scopedPath(root,c.oidc_ca_file)));
 const cert=c.web_certificate_file?readFileSync(scopedPath(root,c.web_certificate_file)):readFileSync(join(dir,'pki/web.crt'));
 const key=c.web_private_key_file?readFileSync(scopedPath(root,c.web_private_key_file)):readFileSync(join(dir,'pki/web.key'));
 validateWebCertificate(cert,key,[new URL(c.console_origin).hostname,new URL(c.admin_origin).hostname,...(c.local_oidc_fixture?[new URL(c.oidc_issuer).hostname]:[])]);
 add('web_certificate',cert);add('web_private_key',key);
 const common={GIN_MODE:'release',MENDER_HTTP_ADDR:'0.0.0.0:18080',MENDER_RUN_API_ENABLED:'true',MENDER_RUN_READ_API_ENABLED:'true',MENDER_CURSOR_SIGNING_KEY_FILE:'/run/secrets/cursor',MENDER_PAYMENT_MODE:'sandbox',MENDER_CONSOLE_COOKIE_SECURE:'true',MENDER_RUN_START_API_ENABLED:'true',MENDER_RUN_COORDINATED_CANCEL_ENABLED:'true',MENDER_MCP_GATEWAY_ENABLED:'true',MENDER_MCP_FIXED_TOOLSET_ENABLED:'true'};
 const session={MENDER_CONSOLE_OIDC_ENABLED:'true',MENDER_CONSOLE_OIDC_ISSUER:c.oidc_issuer,MENDER_CONSOLE_OIDC_CLIENT_ID:c.oidc_client_id,MENDER_CONSOLE_OIDC_CLIENT_SECRET_FILE:'/run/secrets/oidc_client_secret'};
 const consoleCaps=['runtime','admission','cancellation','browser-session','catalog-manager','connection-manager','publisher-manager','commerce-observer'];
 const adminCaps=['runtime','admission','cancellation','browser-session','catalog-manager','publisher-manager','governance-reviewer','governance-policy-manager','release-manager','dangerous-operation-manager','support-reader','platform-admin-manager','billing-manager','payment-manager'];
 const consoleEnv={...common,...(c.oidc_issuer?{...session,MENDER_CONSOLE_OIDC_REDIRECT_URL:c.console_origin+'/auth/callback',MENDER_CONSOLE_FLOW_SIGNING_KEY_FILE:'/run/secrets/flow_console',MENDER_CONSOLE_CATALOG_ENABLED:'true',MENDER_CONSOLE_CONNECTIONS_ENABLED:'true',MENDER_CONSOLE_PUBLISHER_ENABLED:'true',MENDER_CONSOLE_USAGE_ENABLED:'true',MENDER_CONSOLE_LAUNCH_DISCOVERY_ENABLED:'true',MENDER_CONSOLE_RUN_DELEGATION_ENABLED:'true',MENDER_CONSOLE_HUMAN_START_ENABLED:'true'}:{})};
 const adminEnv={...common,...(c.oidc_issuer?{...session,MENDER_CONSOLE_OIDC_REDIRECT_URL:c.admin_origin+'/auth/callback',MENDER_CONSOLE_FLOW_SIGNING_KEY_FILE:'/run/secrets/flow_admin',MENDER_CONSOLE_CATALOG_ENABLED:'true',MENDER_CONSOLE_PUBLISHER_ENABLED:'true',MENDER_ADMIN_CATALOG_REVIEW_ENABLED:'true',MENDER_ADMIN_PLUGIN_REVIEW_ENABLED:'true',MENDER_ADMIN_CATALOG_POLICY_ENABLED:'true',MENDER_ADMIN_RELEASE_GOVERNANCE_ENABLED:'true',MENDER_ADMIN_DANGEROUS_OPERATION_ENABLED:'true',MENDER_ADMIN_SUPPORT_ACCESS_ENABLED:'true',MENDER_ADMIN_PLATFORM_OPERATIONS_ENABLED:'true',MENDER_ADMIN_BILLING_ENABLED:'true',MENDER_ADMIN_PAYMENTS_ENABLED:'true'}:{})};
 function api(env,caps,flow){const needed=c.oidc_issuer?caps:['runtime','admission','cancellation'];for(const g of needed){const [,k]=capabilities.find(x=>x[0]===g);env[k+'_FILE']=mappings[k+'_FILE'];}return {image:release.runtime_image,read_only:true,user:'65532:65532',cap_drop:['ALL'],security_opt:['no-new-privileges:true'],restart:'unless-stopped',stop_grace_period:'15s',pids_limit:128,mem_limit:'512m',environment:env,networks:['private','egress'],secrets:['db_ca','cursor',...needed.map(g=>'dsn_'+g.replaceAll('-','_')),...(c.oidc_issuer?['oidc_client_secret',flow]:[])],depends_on:{database:{condition:'service_healthy'}},healthcheck:{test:['CMD','/mender/healthcheck'],interval:'10s',timeout:'4s',retries:6,start_period:'20s'},logging:{driver:'json-file',options:{'max-size':'10m','max-file':'3'}}};}
 const dbScript='#!/bin/sh\nset -eu\ncp /run/secrets/db_certificate /tmp/server.crt\ncp /run/secrets/db_private_key /tmp/server.key\nchown postgres:postgres /tmp/server.crt /tmp/server.key\nchmod 600 /tmp/server.key\nexec docker-entrypoint.sh postgres -c ssl=on -c ssl_cert_file=/tmp/server.crt -c ssl_key_file=/tmp/server.key -c log_statement=none\n';
 writeFileSync(join(dir,'database-entrypoint.sh'),dbScript,{mode:0o600});writeFileSync(join(dir,'nginx.conf'),nginxConfig(c),{mode:0o600});
 const compose={name:c.instance,services:{
  database:{image:release.postgres_image,environment:{POSTGRES_USER:'postgres',POSTGRES_DB:'mender',POSTGRES_PASSWORD_FILE:'/run/secrets/db_password',POSTGRES_INITDB_ARGS:'--auth-host=scram-sha-256'},entrypoint:['sh','/etc/mender/database-entrypoint.sh'],secrets:['db_password','db_certificate','db_private_key'],volumes:['pgdata:/var/lib/postgresql','./database-entrypoint.sh:/etc/mender/database-entrypoint.sh:ro'],tmpfs:['/tmp'],networks:['private'],restart:'unless-stopped',healthcheck:{test:['CMD','pg_isready','-U','postgres','-d','mender'],interval:'2s',timeout:'3s',retries:30},logging:{driver:'json-file',options:{'max-size':'10m','max-file':'3'}}},
  provision:{image:release.runtime_image,entrypoint:['/mender/provision'],command:['bootstrap','--apply','/run/secrets/role_plan'],environment:{MENDER_ADMIN_DATABASE_URL_FILE:'/run/secrets/admin_dsn'},read_only:true,user:'65532:65532',cap_drop:['ALL'],security_opt:['no-new-privileges:true'],profiles:['operator'],networks:['private'],secrets:['admin_dsn','db_ca','role_plan',...roles.map(r=>'password_'+r.grant.replaceAll('-','_'))],depends_on:{database:{condition:'service_healthy'}}},
  operator:{image:release.runtime_image,entrypoint:['/mender/operator'],environment:{MENDER_ADMIN_DATABASE_URL_FILE:'/run/secrets/admin_dsn'},read_only:true,user:'65532:65532',cap_drop:['ALL'],security_opt:['no-new-privileges:true'],profiles:['operator'],networks:['private'],secrets:['admin_dsn','db_ca']},
  'api-console':api(consoleEnv,consoleCaps,'flow_console'), 'api-admin':api(adminEnv,adminCaps,'flow_admin'),
  worker:{image:release.runtime_image,entrypoint:['/mender/worker'],read_only:true,user:'65532:65532',cap_drop:['ALL'],security_opt:['no-new-privileges:true'],restart:'unless-stopped',stop_grace_period:'30s',pids_limit:128,mem_limit:'512m',networks:['private'],environment:{MENDER_WORKER_CONTROL_ENABLED:'true',MENDER_WORKER_DATABASE_URL_FILE:'/run/secrets/dsn_worker',MENDER_WORKER_ID:c.instance+'_worker',MENDER_WORKER_WORKSPACES:'ws_local',MENDER_WORKER_DISPATCH_ENABLED:'false',MENDER_REVIEWED_WORKER_RUNTIME_ENABLED:'false'},secrets:['db_ca','dsn_worker'],depends_on:{database:{condition:'service_healthy'}},logging:{driver:'json-file',options:{'max-size':'10m','max-file':'3'}}},
  web:{image:release.web_image,read_only:true,user:'101:101',cap_drop:['ALL'],security_opt:['no-new-privileges:true'],restart:'unless-stopped',pids_limit:128,mem_limit:'256m',tmpfs:['/tmp'],networks:['private','egress'],ports:[`${c.environment==='production'?'0.0.0.0':'127.0.0.1'}:${c.http_port}:8080`,`${c.environment==='production'?'0.0.0.0':'127.0.0.1'}:${c.https_port}:8443`],secrets:['web_certificate','web_private_key'],volumes:['./nginx.conf:/etc/nginx/nginx.conf:ro'],depends_on:{'api-console':{condition:'service_healthy'},'api-admin':{condition:'service_healthy'}},logging:{driver:'json-file',options:{'max-size':'10m','max-file':'3'}}}
 },secrets,volumes:{pgdata:{labels:{'io.mender.instance':c.instance}}},networks:{private:{internal:true},egress:{}}};
 if(c.local_oidc_fixture){
  const oidcPort=Number(new URL(c.oidc_issuer).port||443);
  compose.services['local-idp']={image:release.runtime_image,entrypoint:['/mender/localidp'],read_only:true,user:'65532:65532',cap_drop:['ALL'],security_opt:['no-new-privileges:true'],restart:'unless-stopped',pids_limit:64,mem_limit:'128m',environment:{MENDER_LOCAL_OIDC_ADDR:`0.0.0.0:${oidcPort}`,MENDER_LOCAL_OIDC_ISSUER:c.oidc_issuer,MENDER_LOCAL_OIDC_CLIENT_ID:c.oidc_client_id,MENDER_LOCAL_OIDC_CLIENT_SECRET_FILE:'/run/secrets/oidc_client_secret',MENDER_LOCAL_OIDC_REDIRECT_URIS:`${c.console_origin}/auth/callback,${c.admin_origin}/auth/callback`,MENDER_LOCAL_OIDC_CERT_FILE:'/run/secrets/web_certificate',MENDER_LOCAL_OIDC_KEY_FILE:'/run/secrets/web_private_key',MENDER_LOCAL_OIDC_CA_FILE:'/run/secrets/db_ca'},secrets:['oidc_client_secret','web_certificate','web_private_key','db_ca'],networks:{private:{aliases:['idp.localhost']}},ports:[`127.0.0.1:${oidcPort}:${oidcPort}`],healthcheck:{test:['CMD','/mender/localidp','health'],interval:'2s',timeout:'3s',retries:30,start_period:'2s'},logging:{driver:'json-file',options:{'max-size':'5m','max-file':'2'}}};
  compose.services['api-console'].depends_on['local-idp']={condition:'service_healthy'};
  compose.services['api-admin'].depends_on['local-idp']={condition:'service_healthy'};
 }
 for(const name of ['api-console','api-admin']){
  if(c.oidc_ca_file){compose.services[name].secrets.push('oidc_ca');compose.services[name].environment.SSL_CERT_FILE='/run/secrets/oidc_ca';}
  else if(c.local_oidc_fixture)compose.services[name].environment.SSL_CERT_FILE='/run/secrets/db_ca';
  if(c.local_oidc_host_gateway)compose.services[name].extra_hosts=['idp.localhost:host-gateway'];
 }
 const uid=process.getuid?.()||65532;const gid=process.getgid?.()||65532;
 const ownership=randomBytes(16).toString('hex');
 for(const service of Object.values(compose.services))service.labels={'io.mender.instance':c.instance,'io.mender.ownership':ownership};
 compose.volumes.pgdata.labels['io.mender.ownership']=ownership;
 compose.services.worker.environment.MENDER_WORKER_WORKSPACES=(c.worker_workspaces||['ws_local']).join(',');
 compose.services.web.healthcheck={test:['CMD','wget','-q','-O','/dev/null','http://127.0.0.1:8081/readyz'],interval:'5s',timeout:'4s',retries:12,start_period:'10s'};
 for(const name of ['api-console','api-admin','provision','operator','worker','web',...(c.local_oidc_fixture?['local-idp']:[])])compose.services[name].user=`${uid}:${gid}`;
 // Local Compose bind-mounted secrets retain host ownership; do not pretend
 // uid/mode in the secrets stanza changes filesystem ownership.
 if(process.platform!=='win32'&&process.getuid()===0){for(const f of readdirSync(secretDir))chownSync(join(secretDir,f),uid,gid);chownSync(join(dir,'nginx.conf'),uid,gid);}
 writeFileSync(join(dir,'compose.yaml'),stringify(compose),{flag:'wx',mode:0o600});
 const controls=['compose.yaml','nginx.conf','database-entrypoint.sh'].map(path=>({path,sha256:createHash('sha256').update(readFileSync(join(dir,path))).digest('hex')}));
 writeFileSync(join(dir,'deployment.json'),JSON.stringify({version:1,config:c,release,controls,ownership,created_at:new Date().toISOString(),oidc_configured:Boolean(c.oidc_issuer),production_approval:'not_attested',worker_egress:false},null,2),{flag:'wx',mode:0o600});
 copyFileSync(join(dir,'pki/ca.crt'),join(dir,'local-ca.crt'));
 return compose;
}
