const test=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const vm=require('node:vm');
function harness(){
 const nodes=new Map(),writes=[],timers=[];let permitted=true,confirmed=true;
 function el(id){if(!nodes.has(id)){const classes=new Set();nodes.set(id,{value:'',checked:false,textContent:'',innerHTML:'',disabled:false,dataset:{},events:{},open:false,classList:{add:n=>classes.add(n),remove:n=>classes.delete(n),contains:n=>classes.has(n)},addEventListener(n,fn){this.events[n]=fn;},showModal(){this.open=true;},close(){this.open=false;},reportValidity(){return true;},focus(){}});}if(id==='functions-tab')nodes.get(id).dataset.canWrite=String(permitted);return nodes.get(id);}
 const spec={name:'Hello',runtime:'python',code:'def handler(event, context): return event',timeout_seconds:30,memory_mb:256,cpu_millis:500,concurrency:1,schedule:'',timezone:'UTC',schedule_enabled:false,http_enabled:true};
 const fn={id:'function-a',server_id:'a',revision:2,draft:{...spec},paused:false,deployed:{id:'version-a',revision:1,spec:{...spec},code_sha256:'abc123',created_at:'2026-10-18T00:00:00Z'}};
 const run={id:'run-a',revision:1,kind:'invoke',trigger:'manual',status:'running',logs:'console output',duration_ms:120,started_at:'2026-10-18T00:00:00Z'};
 const App={config:{SERVER_ID:'a'},Navigation:{current:'functions'},UI:{toast(){}},API:{get:async endpoint=>({data:endpoint.endsWith('/runs')?[run]:endpoint.endsWith('/versions')?[fn.deployed]:[fn]})}};
 for(const [method,upper]of[['post','POST'],['put','PUT'],['delete','DELETE']])App.API[method]=async(endpoint,body)=>{writes.push({method:upper,endpoint,body});return {data:endpoint.endsWith('/http-token')?{token:'secret-token',path:'/api/function-triggers/a/function-a'}:endpoint.endsWith('/run')||endpoint.endsWith('/deploy')?run:fn};};
 const context=vm.createContext({App,document:{getElementById:el,hidden:false},window:{confirm:()=>confirmed,location:{origin:'https://gopulse.invalid'}},Date,console,setInterval:fn=>{timers.push(fn);return timers.length;},clearInterval(){}});
 vm.runInContext(fs.readFileSync(path.join(__dirname,'../static/js/app/functions.js'),'utf8'),context);
 return {ui:App.Functions,App,el,writes,timers,fn,run,allow:v=>permitted=v,confirm:v=>confirmed=v};
}
test('functions poll only their active view and discard late navigation results',async()=>{
 const h=harness();await h.ui.load();await h.ui.load();assert.equal(h.timers.length,1);h.App.Navigation.current='overview';let reads=0;h.App.API.get=async()=>{reads++;return {data:[]};};h.timers[0]();assert.equal(reads,0);h.ui.deactivate();assert.equal(h.ui.timer,null);
 const late=harness();let resolve;late.App.API.get=()=>new Promise(r=>resolve=r);const pending=late.ui.load();late.ui.deactivate();resolve({data:[late.fn]});await pending;assert.equal(late.el('fn-list').innerHTML,'');
});
test('function viewer reads history without mutation controls or write access',async()=>{
 const h=harness();h.allow(false);await h.ui.load();h.ui.openEditor();await h.ui.save();for(const action of ['edit','deploy','run','pause','delete','token','cancel'])await h.ui.action(action,h.fn.id,h.run.id);assert.equal(h.writes.length,0);assert.doesNotMatch(h.ui.card(h.fn),/>Deploy draft<|>Edit draft<|>Delete</);h.ui.selected=h.fn.id;await h.ui.details();assert.doesNotMatch(h.el('fn-runs').innerHTML,/>Cancel</);
});
test('function source names, IDs, errors and logs cannot inject HTML',async()=>{
 const h=harness();await h.ui.load();assert.doesNotMatch(h.ui.card({...h.fn,id:'x" onclick="evil',draft:{...h.fn.draft,name:'<img src=x>'}}),/<img|data-id="x" onclick=/);h.ui.selected=h.fn.id;await h.ui.details();h.run.logs='<script>evil()</script>';h.run.error='<img onerror=x>';h.ui.renderLogs();assert.equal(h.el('fn-logs').textContent,h.run.logs);assert.equal(h.el('fn-log-error').textContent,h.run.error);
});
test('create and edit save drafts with code, triggers and numerical limits',async()=>{
 const h=harness();await h.ui.load();h.ui.openEditor();h.el('fn-name').value='Hello';await h.ui.save();const created=h.writes[0];assert.equal(created.endpoint,'/functions');assert.equal(created.body.memory_mb,256);assert.equal(created.body.concurrency,1);assert.match(created.body.code,/def handler/);assert.equal(created.body.http_enabled,false);h.ui.openEditor(h.fn);await h.ui.save();assert.equal(h.writes[1].method,'PUT');assert.equal(h.writes[1].endpoint,'/functions/function-a');assert.equal(h.writes[1].body.http_enabled,true);
});
test('deploy, run, pause and cancel use scoped endpoints and deployed version',async()=>{
 const h=harness();await h.ui.load();h.el('fn-event').value='{"value":41}';for(const action of ['deploy','run','pause','cancel'])await h.ui.action(action,h.fn.id,h.run.id);assert.deepEqual(h.writes.map(w=>w.endpoint),['/functions/function-a/deploy','/functions/function-a/run','/functions/function-a/pause','/functions/function-a/runs/run-a/cancel']);assert.equal(h.writes[1].body.value,41);assert.equal(h.writes[2].body.paused,true);assert.equal(h.ui.selectedRun,h.run.id);
});
test('invalid test events and cancelled deletion or token rotation perform no writes',async()=>{
 const h=harness();await h.ui.load();h.el('fn-event').value='not JSON';await h.ui.action('run',h.fn.id);assert.match(h.el('fn-event-error').textContent,/valid JSON/);h.confirm(false);await h.ui.action('delete',h.fn.id);await h.ui.action('token',h.fn.id);assert.equal(h.writes.length,0);
});
test('HTTP token survives polling and clears on navigation, including late responses',async()=>{
 const h=harness();await h.ui.load();h.ui.selected=h.fn.id;let resolve;h.App.API.post=()=>new Promise(r=>resolve=r);let action=h.ui.action('token',h.fn.id);await h.ui.refresh();resolve({data:{token:'secret',path:'/trigger'}});await action;assert.equal(h.el('fn-token-value').value,'secret');assert.equal(h.el('fn-trigger-url').value,'https://gopulse.invalid/trigger');h.ui.deactivate();assert.equal(h.el('fn-token-value').value,'');action=h.ui.action('token',h.fn.id);h.ui.deactivate();resolve({data:{token:'late-secret',path:'/trigger'}});await action;assert.equal(h.el('fn-token-value').value,'');
});
test('save failure preserves editor and releases busy state',async()=>{
 const h=harness();await h.ui.load();h.ui.openEditor(h.fn);h.App.API.put=async()=>{throw Error('Validation failed');};await h.ui.save();assert.equal(h.el('fn-editor').open,true);assert.equal(h.el('fn-form-error').textContent,'Validation failed');assert.equal(h.ui.busy,false);assert.equal(h.el('fn-save').disabled,false);
});
