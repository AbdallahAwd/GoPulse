const test=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const vm=require('node:vm');
function harness(){
 const nodes=new Map(),writes=[],reads=[],timers=[];let permitted=true,confirmed=true;
 function el(id){if(!nodes.has(id)){const classes=new Set();nodes.set(id,{value:'',checked:false,textContent:'',innerHTML:'',disabled:false,open:false,dataset:{},events:{},classList:{add:n=>classes.add(n),remove:n=>classes.delete(n),contains:n=>classes.has(n),toggle:(n,on)=>on?classes.add(n):classes.delete(n)},addEventListener(n,fn){this.events[n]=fn;},showModal(){this.open=true;},close(){this.open=false;},focus(){},reportValidity(){return true;}});}if(id==='remote-backups-tab')nodes.get(id).dataset.canWrite=String(permitted);return nodes.get(id);}
 const plan={id:'plan',name:'Daily backup',database_id:'db',destination_id:'b',directory:'/srv/backups',schedule:'0 2 * * *',timezone:'UTC',retention_days:30,keep_last:3,enabled:false,auto_test:true,timeout_seconds:3600,max_size_gb:10,test_memory_mb:1024};
 const App={config:{SERVER_ID:'a'},Navigation:{current:'remote-backups'},UI:{toast(){}},API:{get:async endpoint=>{reads.push(endpoint);if(endpoint.endsWith('/options'))return {data:{databases:[{id:'db',name:'App',engine:'sqlite'}],destinations:[{id:'b',name:'Backup host'}]}};if(endpoint.includes('/preview?'))return {data:{next_runs:['2026-10-19T02:00:00Z']}};if(endpoint.endsWith('/artifacts')||endpoint.endsWith('/runs'))return {data:[]};return {data:[plan]};}}};
 for(const [name,method] of [['post','POST'],['put','PUT'],['delete','DELETE']])App.API[name]=async(endpoint,body)=>{writes.push({method,endpoint,body});return {data:plan};};
 const context=vm.createContext({App,document:{getElementById:el,hidden:false},window:{confirm:()=>confirmed},Date,Intl,console,setInterval:fn=>{timers.push(fn);return timers.length;},clearInterval(){}});
 vm.runInContext(fs.readFileSync(path.join(__dirname,'../static/js/app/remote_backups.js'),'utf8'),context);
 return {ui:App.RemoteBackups,App,el,plan,writes,reads,timers,allow:v=>permitted=v,confirm:v=>confirmed=v};
}
test('remote backups poll only the active view and ignore late navigation responses',async()=>{
 const {ui,reads,timers,App,el}=harness();await ui.load();await ui.load();assert.equal(timers.length,1);App.Navigation.current='overview';timers[0]();assert.equal(reads.length,4);
 ui.deactivate();
 assert.equal(ui.timer,null);assert.ok(el('rb-plans').innerHTML.includes('Daily backup'));
 const h=harness();let done;const pending=new Promise(r=>done=r);h.App.API.get=()=>pending;const load=h.ui.load();h.ui.deactivate();done({data:[]});await load;assert.equal(h.el('rb-plans').innerHTML,'');
});
test('viewer sees history but cannot create, test, cancel, download keys or mutate plans',async()=>{
 const {ui,allow,writes,plan,el}=harness();allow(false);await ui.load();ui.openEditor();await ui.save();for(const action of ['toggle','run','cancel','test','delete','edit'])await ui.action(action,plan.id,'archive');assert.equal(writes.length,0);assert.doesNotMatch(ui.card(plan),/>Run backup<|>Edit</);ui.selected=plan.id;ui.artifacts=[{id:'archive',size:10}];ui.renderDetails();assert.doesNotMatch(el('rb-artifacts').innerHTML,/Download encrypted|data-test=/);
});
test('remote backup names, paths, summaries and errors are escaped',async()=>{
 const {ui,plan,el}=harness();await ui.load();assert.doesNotMatch(ui.card({...plan,name:'<img src=x>',id:'x" onclick="evil'}),/<img|data-id="x" onclick=/);ui.selected=plan.id;ui.artifacts=[{id:'x',path:'<script>evil</script>',sha256:'<img>',test_summary:'<img onerror=x>',test_error:'<script>x</script>'}];ui.runs=[{error:'<img src=x>'}];ui.renderDetails();assert.doesNotMatch(el('rb-artifacts').innerHTML+el('rb-runs').innerHTML,/<script|<img/);
});
test('create and edit send schedule, retention and resource settings through scoped API',async()=>{
 const {ui,el,writes,plan}=harness();await ui.load();ui.openEditor();el('rb-name').value='Daily backup';await ui.save();assert.equal(writes[0].endpoint,'/remote-backups');assert.equal(writes[0].body.auto_test,true);assert.equal(writes[0].body.enabled,false);assert.equal(writes[0].body.test_memory_mb,1024);ui.openEditor(plan);assert.equal(el('rb-destination').disabled,true);assert.equal(el('rb-directory').disabled,true);await ui.save();assert.equal(writes[1].method,'PUT');assert.equal(writes[1].endpoint,'/remote-backups/plan');
});
test('schedule builder produces hourly, daily and weekly cron',async()=>{
 const {ui,el}=harness();await ui.load();el('rb-time').value='03:15';el('rb-timezone').value='UTC';for(const [frequency,expected] of [['hourly','15 * * * *'],['daily','15 3 * * *'],['weekly','15 3 * * 4']]){el('rb-frequency').value=frequency;el('rb-weekday').value='4';ui.buildSchedule();assert.equal(el('rb-schedule').value,expected);}
});
test('backup actions use explicit endpoints and cancelled deletion performs no write',async()=>{
 const {ui,writes,plan,confirm,el}=harness();await ui.load();confirm(false);await ui.action('delete',plan.id);assert.equal(writes.length,0);confirm(true);for(const action of ['toggle','run','cancel','test'])await ui.action(action,plan.id,'archive');assert.deepEqual(writes.map(w=>w.endpoint),['/remote-backups/plan','/remote-backups/plan/run','/remote-backups/plan/cancel','/remote-backups/plan/artifacts/archive/test']);assert.equal(writes[0].body.enabled,true);ui.selected=plan.id;await ui.action('delete',plan.id);assert.equal(ui.selected,null);assert.ok(el('rb-detail').classList.contains('hidden'));
});
test('save failure preserves editor and releases busy state',async()=>{
 const {ui,App,el,plan}=harness();await ui.load();ui.openEditor(plan);App.API.put=async()=>{throw Error('Destination unavailable');};await ui.save();assert.equal(el('rb-editor').open,true);assert.equal(el('rb-form-error').textContent,'Destination unavailable');assert.equal(ui.busy,false);assert.equal(el('rb-save').disabled,false);
});
