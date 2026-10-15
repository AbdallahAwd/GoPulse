const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
const path=require('node:path');
function load(file,context){vm.runInNewContext(fs.readFileSync(path.join(__dirname,'../static/js',file),'utf8'),context);}
function wizard(){
 const context={App:{config:{SERVER_ID:'entry'},LoadBalancers:{load(){}},Infrastructure:{}}};
 load('app/infrastructure.js',context);load('app/load_balancer_wizard.js',context);
 const w=context.App.LoadBalancerWizard, button={disabled:false};let error='';
 w.root={querySelector(){return button;}};w.capture=()=>{};w.render=()=>{};w.error=value=>{error=value;};
 w.state={server_id:'entry',name:'Test',domain:'example.test',port:80,backendPort:3000,ssl:false,algorithm:'round-robin',health_check_path:'/',health_check_interval:10};
 return {App:context.App,w,button,error:()=>error};
}
test('wizard rejects empty backends and invalid traffic before deployment',async()=>{
 const {App,w,error}=wizard();let calls=0;App.Infrastructure.api=async()=>{calls++;};
 w.step=1;await w.next();assert.equal(w.step,1);assert.match(error(),/backend/);
 w.step=2;w.state.port=NaN;await w.next();assert.equal(w.step,2);assert.match(error(),/valid ports/);assert.equal(calls,0);
});
test('wizard submits all selected backends once and tracks actual operation',async()=>{
 const {App,w,button}=wizard();w.step=5;w.selected=['b','c'];let resolve,requests=[],tracked=[];
 App.Infrastructure.api=(url,options)=>{requests.push({url,payload:JSON.parse(options.body)});return new Promise(r=>resolve=r);};
 App.Infrastructure.track=async id=>{tracked.push(id);};
 const pending=w.next();await w.next();assert.equal(requests.length,1);assert.equal(button.disabled,true);
 assert.equal(requests[0].url,'/servers/entry/load-balancers');assert.deepEqual(requests[0].payload.backends.map(b=>b.target_server_id),['b','c']);
 resolve({operation_id:'operation'});await pending;assert.deepEqual(tracked,['operation']);
});
test('failed submission exposes error and allows a retry',async()=>{
 const {App,w,button,error}=wizard();w.step=5;w.selected=['b'];
 App.Infrastructure.api=async()=>{throw new Error('permission revoked');};
 await w.next();assert.equal(error(),'permission revoked');assert.equal(button.disabled,false);assert.equal(w.submitting,false);
});
test('display escaping treats remote names as text',()=>{
 const {App}=wizard();assert.equal(App.Infrastructure.escape('<img src=x onerror="run()">'), '&lt;img src=x onerror=&quot;run()&quot;&gt;');
});
test('login page does not open a metrics socket or dereference absent indicators',()=>{
 let connections=0;const context={window:{location:{protocol:'http:',host:'localhost'}},document:{getElementById(){return null;},querySelector(){return null;}},WebSocket:function(){connections++;}};
 load('websocket.js',context);assert.equal(connections,0);context.updateWSStatus('error');
});
