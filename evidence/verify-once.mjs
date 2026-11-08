import fs from 'node:fs';
import http from 'node:http';
import crypto from 'node:crypto';
import {spawn} from 'node:child_process';
const root='F:/swe/work/surge-delivery-review', bin=root+'/surge-review.exe';
const payload=crypto.createHash('sha256').update('surge-independent-review').digest();
const data=Buffer.alloc(16*1024*1024);for(let i=0;i<data.length;i++)data[i]=(payload[i%32]+(i>>>8)*29+(i>>>16)*43)%251;
const hash=b=>crypto.createHash('sha256').update(b).digest('hex');
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
 record.status=status;record.contentRange=res.getHeader("Content-Range")??null;res.writeHead(status);
 if(probe||noRange||bad){const bytes=status===200?data:data.subarray(a,b+1);record.sent=bytes.length;res.end(bytes);if(bad)mode="normal";return;}
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
function envFor(name){const d=root+'/fixtures-contract/'+name;fs.mkdirSync(d,{recursive:true});return {...process.env,APPDATA:d+'/appdata',XDG_CONFIG_HOME:d+'/config',XDG_STATE_HOME:d+'/state',XDG_RUNTIME_DIR:d+'/runtime',NO_COLOR:'1'};}
async function run(name,args,{env=envFor(name),exe=bin}={}){
 const from=requests.length,start=new Date().toISOString();const result=await new Promise((resolve,reject)=>{
  const p=spawn(exe,args,{cwd:root,env,windowsHide:true});let stdout='',stderr='';const timer=setTimeout(()=>{p.kill();reject(new Error(name+' exceeded 90 seconds'));},90000);
  p.stdout.on('data',x=>stdout+=x);p.stderr.on('data',x=>stderr+=x);p.on('error',reject);p.on('close',code=>{clearTimeout(timer);let receipt;try{receipt=JSON.parse(stdout);}catch{}resolve({name,args,pid:p.pid,code,stdout,stderr,receipt});});
 });console.log(name+" exit="+result.code);result.started=start;result.ended=new Date().toISOString();result.requests=requests.slice(from);results.push(result);return result;
}
function args(name,ranges,extra=[]){const dir=root+'/fixtures-contract/'+name+'/downloads';fs.mkdirSync(dir,{recursive:true});return ['fetch',url,'--json','--progress','plain','--color','never','--output',dir,'--filename','result.bin',...(ranges===null?[]:['--byte-ranges',ranges]),...extra];}
function check(r,name,ok,detail=''){r.checks??={};r.checks[name]={ok:!!ok,detail};if(!ok)console.log('ASSERTION FAIL',r.name,name,detail);}
function artifact(r,name,expected){const file=root+'/fixtures-contract/'+name+'/downloads/result.bin';const bytes=fs.existsSync(file)?fs.readFileSync(file):null;r.artifact=bytes?{size:bytes.length,sha256:hash(bytes)}:null;check(r,'exactArtifact',r.code===0&&r.receipt?.ok&&bytes?.equals(expected),JSON.stringify(r.artifact));if(bytes){const i=r.receipt.items[0];check(r,'compactReceipt',i.downloaded_bytes===String(expected.length)&&i.total_bytes===String(expected.length)&&i.artifact?.size_bytes===String(expected.length));}}
const plan=[[8*1024*1024+17,3*1024*1024+23],[1024,2*1024*1024+7],[8*1024*1024+17,3*1024*1024+23]],spec=plan.map(x=>x.join(':')).join(','),expected=Buffer.concat(plan.map(([a,n])=>data.subarray(a,a+n)));
function windowsOnly(r){check(r,'requestsWithinSelectedWindows',r.requests.every(q=>q.probe||q.range&&plan.some(([a,n])=>q.a>=a&&q.b<a+n)));}
try {
 for(const badmode of ['bad200','badstart','badend','badtotal']) {
  mode=badmode;const r=await run(badmode+'-once',args(badmode+'-once','1024:65536,8388608:65536'));
  const i=r.receipt?.items?.[0];const file=root+'/fixtures-contract/'+badmode+'-once/downloads/result.bin';
  r.finalFileExists=fs.existsSync(file);r.finalHash=r.finalFileExists?hash(fs.readFileSync(file)):null;
  r.expectedHash=hash(Buffer.concat([data.subarray(1024,1024+65536),data.subarray(8388608,8388608+65536)]));
  check(r,'mustRejectAfterAnyContractViolation',r.code!==0&&!r.receipt?.ok&&!i?.artifact&&!r.finalFileExists);
 }
} finally {
 server.closeAllConnections();await new Promise(r=>server.close(r));
 fs.writeFileSync(root+'/verification-once.json',JSON.stringify({results},null,2)+'\n');
 console.log(JSON.stringify(results.map(r=>({name:r.name,code:r.code,ok:r.receipt?.ok,size:r.receipt?.items?.[0]?.artifact?.size_bytes,finalFileExists:r.finalFileExists,requests:r.requests.map(q=>({range:q.range,mode:q.mode,probe:q.probe}))})),null,2));
}

