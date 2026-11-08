import fs from 'node:fs';
import http from 'node:http';
import crypto from 'node:crypto';
import {spawn} from 'node:child_process';
const root=process.env.SURGE_QC_WORK||'F:/swe/work/surge-delivery-qc', bin=process.env.SURGE_QC_BINARY||root+'/surge-review.exe';
const payload=crypto.createHash('sha256').update('surge-independent-review').digest();
const data=Buffer.alloc(16*1024*1024);for(let i=0;i<data.length;i++)data[i]=(payload[i%32]+(i>>>8)*29+(i>>>16)*43)%251;
const hash=b=>crypto.createHash('sha256').update(b).digest('hex');

const denied=[];
const denyProxy=http.createServer((req,res)=>{denied.push({method:req.method,url:req.url,status:403});res.writeHead(403);res.end('QC_PUBLIC_NETWORK_DENIED');});
denyProxy.on('connect',(req,socket)=>{denied.push({method:'CONNECT',url:req.url,status:403});socket.end('HTTP/1.1 403 Forbidden\r\nConnection: close\r\n\r\n');});
await new Promise(r=>denyProxy.listen(0,'127.0.0.1',r));
const denyUrl='http://127.0.0.1:'+denyProxy.address().port;
const restrictedEnv={...process.env,HTTP_PROXY:denyUrl,HTTPS_PROXY:denyUrl,ALL_PROXY:denyUrl,http_proxy:denyUrl,https_proxy:denyUrl,all_proxy:denyUrl,NO_PROXY:'127.0.0.1,localhost',no_proxy:'127.0.0.1,localhost'};

const results=[], requests=[];let mode='normal',cut=false,active=0,maxActive=0,seq=0;
const delay=ms=>new Promise(r=>setTimeout(r,ms));
const server=http.createServer(async(req,res)=>{
 const m=/^bytes=(\d+)-(\d*)$/.exec(req.headers.range||'');const a=m?Number(m[1]):0,b=m&&m[2]?Number(m[2]):data.length-1;
 const probe=m&&a===0&&b===0;
 const record={seq:++seq,mode,at:new Date().toISOString(),range:req.headers.range||null,a,b,probe,sent:0};requests.push(record);
 if(!probe){active++;maxActive=Math.max(maxActive,active);res.on('close',()=>{active--;record.closed=new Date().toISOString();});}
 if(a>b||b>=data.length){res.writeHead(416);res.end();return;}
 const noRange=mode==='norange';const unknown=mode==='unknown';const bad=mode.startsWith('bad')&&!probe&&a>=8*1024*1024;
 const status=noRange||(!probe&&bad&&mode==='bad200')?200:206;
 res.setHeader('Accept-Ranges',noRange?'none':'bytes');
 if(!unknown)res.setHeader('Content-Length',String(status===200?data.length:b-a+1));
 if(status===206)res.setHeader('Content-Range',`bytes ${a+(bad&&mode==='badstart'?1:0)}-${b+(bad&&mode==='badend'?1:0)}/${unknown?'*':data.length+(bad&&mode==='badtotal'?1:0)}`);
 if(bad)await delay(200); // let a preceding valid window commit bytes before contract failure
 res.writeHead(status);
 if(probe||noRange||bad){const bytes=status===200?data:data.subarray(a,b+1);record.sent=bytes.length;res.end(bytes);return;}
 if(mode==='short'&&!cut){cut=true;const n=Math.min(70001,b-a);record.short=true;record.sent=n;res.write(data.subarray(a,a+n));await delay(50);res.destroy();return;}
 if(mode==='blocked'){
  const n=Math.min(128*1024,b-a+1);record.sent=n;res.write(data.subarray(a,a+n));
  if(n===b-a+1){res.end();return;}
  // Real HTTP connection remains blocked until client timeout cancels it.
  return;
 }
 if(mode==='slow'){
  for(let off=a;off<=b&&!res.destroyed;off+=65536){const end=Math.min(off+65536,b+1);record.sent+=end-off;res.write(data.subarray(off,end));await delay(210);}res.end();return;
 }
 record.sent=b-a+1;res.end(data.subarray(a,b+1));
});
await new Promise(r=>server.listen(0,'127.0.0.1',r));const url=`http://127.0.0.1:${server.address().port}/source.bin`;
function envFor(name){const d=root+'/qc-fixtures-v1/'+name;fs.mkdirSync(d,{recursive:true});return {...restrictedEnv,APPDATA:d+'/appdata',XDG_CONFIG_HOME:d+'/config',XDG_STATE_HOME:d+'/state',XDG_RUNTIME_DIR:d+'/runtime',NO_COLOR:'1'};}
async function run(name,args,{env=envFor(name),exe=bin}={}){
 const from=requests.length,start=new Date().toISOString();const result=await new Promise((resolve,reject)=>{
  const p=spawn(exe,args,{cwd:root,env,windowsHide:true});let stdout='',stderr='';const timer=setTimeout(()=>{p.kill();reject(new Error(name+' exceeded 90 seconds'));},90000);
  p.stdout.on('data',x=>stdout+=x);p.stderr.on('data',x=>stderr+=x);p.on('error',reject);p.on('close',code=>{clearTimeout(timer);let receipt;try{receipt=JSON.parse(stdout);}catch{}resolve({name,args,pid:p.pid,code,stdout,stderr,receipt});});
 });console.log(name+" exit="+result.code);result.started=start;result.ended=new Date().toISOString();result.requests=requests.slice(from);results.push(result);return result;
}
function args(name,ranges,extra=[]){const dir=root+'/qc-fixtures-v1/'+name+'/downloads';fs.mkdirSync(dir,{recursive:true});return ['fetch',url,'--json','--progress','plain','--color','never','--output',dir,'--filename','result.bin',...(ranges===null?[]:['--byte-ranges',ranges]),...extra];}
function check(r,name,ok,detail=''){r.checks??={};r.checks[name]={ok:!!ok,detail};if(!ok)console.log('ASSERTION FAIL',r.name,name,detail);}
function artifact(r,name,expected){const file=root+'/qc-fixtures-v1/'+name+'/downloads/result.bin';const bytes=fs.existsSync(file)?fs.readFileSync(file):null;r.artifact=bytes?{size:bytes.length,sha256:hash(bytes)}:null;check(r,'exactArtifact',r.code===0&&r.receipt?.ok&&bytes?.equals(expected),JSON.stringify(r.artifact));if(bytes){const i=r.receipt.items[0];check(r,'compactReceipt',i.downloaded_bytes===String(expected.length)&&i.total_bytes===String(expected.length)&&i.artifact?.size_bytes===String(expected.length));}}
const plan=[[8*1024*1024+17,3*1024*1024+23],[1024,2*1024*1024+7],[8*1024*1024+17,3*1024*1024+23]],spec=plan.map(x=>x.join(':')).join(','),expected=Buffer.concat(plan.map(([a,n])=>data.subarray(a,a+n)));
function windowsOnly(r){check(r,'requestsWithinSelectedWindows',r.requests.every(q=>q.probe||q.range&&plan.some(([a,n])=>q.a>=a&&q.b<a+n)));}
try{
 for(const scheme of ['http','https']){
  const before=denied.length;
  const probe=await run('deny-public-'+scheme,['fetch',scheme+'://example.com/qc-public-denial','--json','--byte-ranges','0:2','--timeout','2s']);
  check(probe,'realChildPublicDenied',denied.length>before&&probe.code!==0&&!probe.receipt?.ok&&!probe.receipt?.items?.some(i=>i.artifact),JSON.stringify(denied.slice(before)));
 }
 mode='slow';maxActive=0;let r=await run('ordered',args('ordered',spec));artifact(r,'ordered',expected);windowsOnly(r);r.maxConcurrentDataRequests=maxActive;check(r,'concurrent',maxActive>1);check(r,'liveProgress',r.stderr.includes('downloading')&&/8[.,]0.*MiB/.test(r.stderr));
 mode='short';cut=false;r=await run('short',args('short',spec));artifact(r,'short',expected);windowsOnly(r);const first=r.requests.find(x=>x.short);check(r,'shortActuallyTriggered',first?.sent===70001);check(r,'retryOnlyRemaining',first&&r.requests.some(q=>q.a===first.a+70001&&q.b===first.b));
 for(let trial=1;trial<=3;trial++){
  const name='resume'+trial,env=envFor(name);mode='blocked';const paused=await run(name+'-pause',args(name,spec,['--timeout','1200ms']),{env});const item=paused.receipt?.items?.[0];
  check(paused,'confirmedCheckpoint',paused.code!==0&&paused.receipt?.checkpoint_confirmed===true&&Number(item?.downloaded_bytes)>0&&Number(item?.downloaded_bytes)<expected.length&&item?.total_bytes===String(expected.length)&&!item?.artifact);
  const file=root+'/qc-fixtures-v1/'+name+'/downloads/result.bin';check(paused,'unpublished',!fs.existsSync(file));
  mode='normal';const resumed=await run(name+'-new-process',['fetch','--resume',paused.receipt?.session_id||'missing','--json','--progress','plain'],{env});artifact(resumed,name,expected);windowsOnly(resumed);
  check(resumed,'independentProcess',resumed.pid!==paused.pid);const transferred=resumed.requests.filter(x=>!x.probe).reduce((n,q)=>n+q.sent,0);resumed.previousDownloaded=Number(item?.downloaded_bytes);resumed.transferred=transferred;check(resumed,'reusesExactlyConfirmedBytes',transferred===expected.length-Number(item?.downloaded_bytes));
 }
 mode='normal';const invalid=['','x:2','-1:2','0:0','0:-1','0:9223372036854775808','9223372036854775808:1','0:9223372036854775807,0:1','0:1,','16777215:2'];
 for(let n=0;n<invalid.length;n++){const name='invalid'+n;r=await run(name,args(name,invalid[n]));check(r,'rejected',r.code!==0&&!r.receipt?.ok&&!r.receipt?.items?.some(i=>i.artifact));check(r,'noWorkFile',fs.readdirSync(root+'/qc-fixtures-v1/'+name+'/downloads').length===0);if(n<invalid.length-1)check(r,'preNetworkRejection',r.requests.length===0);}
 const combos=[['--host','http://127.0.0.1:1'],[url],['--resume','12345678-1234-1234-1234-123456789012']];
 for(let n=0;n<combos.length;n++){const name='combination'+n;r=await run(name,args(name,'0:2',combos[n]));check(r,'preNetworkRejection',r.code===2&&r.requests.length===0&&fs.readdirSync(root+'/qc-fixtures-v1/'+name+'/downloads').length===0);}
 r=await run('mirrors',['fetch',url+','+url,'--byte-ranges','0:2','--json']);check(r,'preNetworkRejection',r.code===2&&r.requests.length===0);
 for(const m of ['norange','unknown']){mode=m;r=await run(m,args(m,'4:8'));check(r,'probeRejection',r.code!==0&&!r.receipt?.ok&&!r.receipt?.items?.some(i=>i.artifact)&&fs.readdirSync(root+'/qc-fixtures-v1/'+m+'/downloads').length===0);}
 mode='normal';r=await run('whole',args('whole',null));artifact(r,'whole',data);
 mode='norange';r=await run('fallback',args('fallback',null));artifact(r,'fallback',data);check(r,'singleStreamFallback',r.requests.some(q=>!q.range));
 mode='normal';r=await run('sha-correct',args('sha-correct',null,['--sha256',hash(data)]));artifact(r,'sha-correct',data);
 r=await run('sha-wrong',args('sha-wrong',null,['--sha256','0'.repeat(64)]));check(r,'hashFailureUnpublished',r.code!==0&&!r.receipt?.ok&&!fs.existsSync(root+'/qc-fixtures-v1/sha-wrong/downloads/result.bin')&&!r.receipt?.items?.[0]?.artifact);
 const sentinel=Buffer.from('do not overwrite');const d=root+'/qc-fixtures-v1/protect/downloads';fs.mkdirSync(d,{recursive:true});fs.writeFileSync(d+'/result.bin',sentinel);r=await run('protect',args('protect',null));check(r,'sentinelPreserved',fs.readFileSync(d+'/result.bin').equals(sentinel));
 mode='blocked';const oldenv=envFor('old-checkpoint');const old=await run('baseline-pause',args('old-checkpoint',null,['--timeout','1200ms']),{env:oldenv,exe:process.env.SURGE_BASELINE_BINARY||'F:/swe/099926/surge-baseline.exe'});check(old,'oldCheckpoint',old.receipt?.checkpoint_confirmed===true&&Number(old.receipt?.items?.[0]?.downloaded_bytes)>0);
 mode='normal';r=await run('current-resumes-old',['fetch','--resume',old.receipt?.session_id||'missing','--json','--progress','plain'],{env:oldenv});artifact(r,'old-checkpoint',data);check(r,'legacyReusesBytes',r.requests.filter(x=>!x.probe).reduce((n,q)=>n+q.sent,0)===data.length-Number(old.receipt?.items?.[0]?.downloaded_bytes));
}catch(e){results.push({error:e.stack});process.exitCode=1;}finally{
 fs.writeFileSync(root+'/network-restriction.json',JSON.stringify({mechanism:'Production HTTP transport ProxyFromEnvironment; deny-only loopback proxy returns HTTP403 and rejects HTTPS CONNECT, no forwarding; NO_PROXY permits only loopback fixture',proxy:denyUrl,denied,cacheIsolation:'Per case empty APPDATA/XDG_CONFIG_HOME/XDG_STATE_HOME/XDG_RUNTIME_DIR; no author settings or checkpoints',fixture:'Deterministic 16MiB generated in memory from surge-independent-review SHA256; no external distributions',scope:'Every spawned current and baseline fetch child inherits restrictedEnv; source uses loopback HTTP only'},null,2));
 denyProxy.closeAllConnections();await new Promise(r=>denyProxy.close(r));
 server.closeAllConnections();await new Promise(r=>server.close(r));fs.writeFileSync(root+'/verification.json',JSON.stringify({sourceSize:data.length,sourceSha256:hash(data),spec,compactSize:expected.length,compactSha256:hash(expected),results},null,2)+'\n');
 const checks=results.flatMap(r=>Object.entries(r.checks||{}).map(([k,v])=>({case:r.name,assertion:k,...v})));const failures=checks.filter(x=>!x.ok);
 fs.writeFileSync(root+'/verification-summary.txt',`${checks.length-failures.length}/${checks.length} checks passed\n`+failures.map(x=>JSON.stringify(x)).join('\n')+'\n');console.log(fs.readFileSync(root+'/verification-summary.txt','utf8'));if(failures.length)process.exitCode=1;
}

