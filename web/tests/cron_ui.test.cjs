const test=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const vm=require('node:vm');
function harness(){
    const nodes=new Map(),reads=[],writes=[],toasts=[],intervals=[],previews=[],timers=[];let allowed=true,confirm=true;
    function el(id){if(!nodes.has(id)){const classes=new Set();nodes.set(id,{id,value:'',checked:false,disabled:false,required:false,textContent:'',innerHTML:'',dataset:{},open:false,style:{},events:{},
        classList:{add:x=>classes.add(x),remove:x=>classes.delete(x),contains:x=>classes.has(x),toggle:(x,on)=>on?classes.add(x):classes.delete(x)},
        addEventListener(name,callback){this.events[name]=callback;},setAttribute(name,value){this[name]=value;},
        querySelectorAll(){return id==='cron-frequency-options'?frequencies:[];},showModal(){this.open=true;},close(){this.open=false;this.events.close?.();},focus(){},reportValidity(){return true;},append(){},select(){},remove(){}});}return nodes.get(id);}
    const frequencies=['minutes','hourly','daily','weekly','monthly','custom'].map(f=>{const n=el('frequency-'+f);n.dataset.frequency=f;return n;});
    const job={id:'job',name:'Daily report',command:'printf ok',schedule:'0 9 * * *',timezone:'UTC',working_dir:'/srv/app',timeout_seconds:300,enabled:true,next_run_at:'2026-10-14T09:00:00Z'};
    const run={id:'run',command:'printf ok',status:'succeeded',started_at:'2026-10-13T09:00:00Z',duration_ms:123,trigger:'manual',actor:'alice',exit_code:0,output:'<script>safe</script>\n'};
    let jobs=[job],runs=[run];
    const App={config:{API_BASE:'/api'},Navigation:{current:'cron'},UI:{toast:(...args)=>toasts.push(args),confirm:async()=>confirm},API:{
        url:p=>'/servers/server'+p,
        get:async url=>{reads.push(url);return {success:true,data:url.includes('/runs/')?run:url.includes('/runs')?runs:jobs};},
        post:async(url,body)=>{writes.push({method:'POST',url,body});return {success:true,data:run};},
        put:async(url,body)=>{writes.push({method:'PUT',url,body});return {success:true};},
        delete:async url=>{writes.push({method:'DELETE',url});return {success:true};}}};
    Object.defineProperty(el('cron-tab').dataset,'canWrite',{get:()=>String(allowed)});
    el('cron-filter').value='all';el('cron-timezone').value='UTC';
    const document={getElementById:el,activeElement:null,hidden:false,createElement:()=>el('clipboard-fallback'),execCommand:()=>true};
    const context=vm.createContext({App,document,window:{isSecureContext:false},navigator:{},Intl,URLSearchParams,console,
        setInterval:fn=>{intervals.push(fn);return intervals.length;},setTimeout:fn=>{timers.push(fn);return timers.length;},clearTimeout(){},
        fetch:async url=>{previews.push(url);return {ok:true,json:async()=>({success:true,data:{next_runs:['2026-10-14T09:00:00Z']}})};}});
    vm.runInContext(fs.readFileSync(path.join(__dirname,'../static/js/app/cron.js'),'utf8'),context);
    return {ui:App.Cron,App,el,job,run,reads,writes,toasts,intervals,previews,context,frequencies,allow:v=>allowed=v,confirm:v=>confirm=v,jobs:v=>jobs=v,runs:v=>runs=v};
}
const plain=value=>JSON.parse(JSON.stringify(value));
test('Cron initialization, list and background refresh perform reads only',async()=>{
    const {ui,reads,writes,intervals,App}=harness();await ui.load();ui.init();assert.equal(intervals.length,1);assert.deepEqual(reads,['/cron']);assert.equal(writes.length,0);
    App.Navigation.current='docker';intervals[0]();assert.equal(reads.length,1);
    App.Navigation.current='cron';intervals[0]();await Promise.resolve();assert.equal(reads.length,2);assert.equal(writes.length,0);
});
test('schedule builder generates expressions for each visual frequency',()=>{
    const {ui,el}=harness();ui.openEditor();el('cron-time').value='14:35';
    const cases=[['minutes','*/5 * * * *'],['hourly','0 * * * *'],['daily','35 14 * * *'],['weekly','35 14 * * 1'],['monthly','35 14 1 * *']];
    for(const [frequency,expression]of cases){ui.setFrequency(frequency);assert.equal(el('cron-schedule').value,expression);}
    el('cron-interval').value='1';ui.setFrequency('minutes');assert.equal(el('cron-schedule').value,'* * * * *');
    el('cron-weekday').value='0';ui.setFrequency('weekly');assert.equal(el('cron-schedule').value,'35 14 * * 0');
    el('cron-monthday').value='28';ui.setFrequency('monthly');assert.equal(el('cron-schedule').value,'35 14 28 * *');
    assert.match(el('cron-schedule-summary').textContent,/day 28/);
});
test('editing restores visual controls without altering advanced cron rules',()=>{
    const {ui,el,job}=harness();
    const cases=[['*/15 * * * *','minutes'],['* * * * *','minutes'],['23 * * * *','hourly'],['35 14 * * *','daily'],['35 14 * * 0','weekly'],['35 14 28 * *','monthly'],['0 9 * * MON-FRI','custom'],['0 9 1 JAN *','custom']];
    for(const [expression,frequency]of cases){ui.openEditor({...job,schedule:expression});assert.equal(ui.frequency,frequency);assert.equal(el('cron-schedule').value,expression);assert.equal(el('cron-advanced-schedule').open,frequency==='custom');}
});
test('visual selection updates pressed state and hides irrelevant timing controls',()=>{
    const {ui,el,frequencies}=harness();ui.openEditor();ui.setFrequency('weekly');assert.equal(el('cron-weekday-field').classList.contains('hidden'),false);assert.equal(el('cron-time-field').classList.contains('hidden'),false);assert.equal(el('cron-interval-field').classList.contains('hidden'),true);
    assert.equal(frequencies.find(x=>x.dataset.frequency==='weekly')['aria-pressed'],'true');
    ui.setFrequency('custom');assert.equal(el('cron-advanced-schedule').open,true);assert.equal(el('cron-time').required,false);
});
test('editor previews schedule using a server-scoped read and saves exact fields',async()=>{
    const {ui,el,writes,previews}=harness();ui.openEditor();await ui.preview();assert.ok(previews[0].startsWith('/api/servers/server/cron/preview?'));assert.equal(writes.length,0);
    el('cron-name').value='Report';el('cron-command').value='printf ok';el('cron-directory').value='/srv';el('cron-timeout').value='120';el('cron-timezone').value='UTC';el('cron-time').value='08:15';ui.setFrequency('daily');await ui.save();
    assert.deepEqual(plain(writes[0]),{method:'POST',url:'/cron',body:{name:'Report',schedule:'15 8 * * *',timezone:'UTC',command:'printf ok',working_dir:'/srv',timeout_seconds:120,enabled:true}});assert.equal(el('cron-editor').open,false);
});
test('existing job updates preserve its advanced schedule and paused state',async()=>{
    const {ui,el,job,writes}=harness();ui.openEditor({...job,schedule:'0 9 * * MON-FRI',enabled:false});await ui.save();
    assert.equal(writes[0].method,'PUT');assert.equal(writes[0].url,'/cron/job');assert.equal(writes[0].body.schedule,'0 9 * * MON-FRI');assert.equal(writes[0].body.enabled,false);assert.equal(el('cron-save').disabled,false);
});
test('viewer has history but cannot edit, run, toggle, cancel or delete',async()=>{
    const {ui,job,allow,writes}=harness();allow(false);ui.jobs=[job];const row=ui.row(job);assert.match(row,/data-action="history"/);assert.doesNotMatch(row,/data-action="edit"/);
    ui.openEditor();for(const action of ['edit','run','pause','resume','cancel','delete'])await ui.action(action,'job');assert.equal(writes.length,0);
});
test('cancelled execution and deletion confirmations produce no writes',async()=>{
    const {ui,job,confirm,writes}=harness();ui.jobs=[job];confirm(false);for(const action of ['run','delete','cancel'])await ui.action(action,'job');assert.equal(writes.length,0);assert.equal(ui.pending.size,0);
});
test('management controls use the scoped API and keep run-once independent of schedule',async()=>{
    const {ui,job,writes}=harness();ui.jobs=[job];for(const action of ['pause','resume','run','cancel','delete'])await ui.action(action,'job');
    assert.deepEqual(writes.map(x=>[x.method,x.url]),[['PUT','/cron/job/enabled'],['PUT','/cron/job/enabled'],['POST','/cron/job/run'],['POST','/cron/job/cancel'],['DELETE','/cron/job']]);assert.equal(writes[0].body.enabled,false);assert.equal(writes[1].body.enabled,true);
});
test('job data is escaped and running jobs expose cancellation',()=>{
    const {ui,job}=harness();const row=ui.row({...job,name:'<img src=x>',command:'echo "<script>"',id:'id" onmouseover="evil',last_run:{status:'running'}});assert.doesNotMatch(row,/<img|<script|data-id="id" on/);assert.match(row,/&lt;script&gt;/);assert.match(row,/data-action="cancel"/);assert.doesNotMatch(row,/data-action="delete"/);
});
test('search and filters match name, command, enabled state and failures',()=>{
    const {ui,job}=harness();assert.equal(ui.matches(job,' REPORT ','all'),true);assert.equal(ui.matches(job,'printf','enabled'),true);assert.equal(ui.matches(job,'','paused'),false);assert.equal(ui.matches({...job,last_run:{status:'timed_out'}},'','failed'),true);assert.equal(ui.matches(job,'nomatch','all'),false);
});
test('history pagination and output inspection use read endpoints and text rendering',async()=>{
    const {ui,job,run,el,reads,writes}=harness();ui.init();ui.openHistory(job);await ui.loadHistory();await ui.inspectRun('run');assert.ok(reads.includes('/cron/job/runs?limit=25&offset=0'));assert.ok(reads.includes('/cron/job/runs/run'));assert.equal(el('cron-run-output').textContent,run.output);assert.equal(el('cron-run-output').innerHTML,'');assert.equal(writes.length,0);assert.equal(el('cron-runs-prev').disabled,true);assert.equal(el('cron-runs-next').disabled,true);
    await ui.copyOutput();assert.equal(el('clipboard-fallback').value,run.output);ui.closeHistory();assert.equal(ui.historyID,null);
});
test('save errors leave editor open with feedback and release busy state',async()=>{
    const {ui,el,App}=harness();ui.openEditor();App.API.post=async()=>{throw new Error('Invalid schedule');};await ui.save();assert.equal(el('cron-editor').open,true);assert.equal(el('cron-form-error').textContent,'Invalid schedule');assert.equal(ui.saving,false);assert.equal(el('cron-save').disabled,false);
});
test('server navigation, template and scripts expose Cron jobs',()=>{
    const dashboard=fs.readFileSync(path.join(__dirname,'../templates/dashboard.html'),'utf8');const base=fs.readFileSync(path.join(__dirname,'../templates/base.html'),'utf8');const navigation=fs.readFileSync(path.join(__dirname,'../static/js/app/navigation.js'),'utf8');
    for(const id of ['cron-tab','cron-editor','cron-history','cron-frequency-options','cron-time','cron-weekday','cron-monthday','cron-command','cron-enabled','cron-runs'])assert.match(dashboard,new RegExp(`id="${id}"`));
    assert.match(base,/app\/cron.js/);assert.match(navigation,/cron: 'Cron jobs'/);assert.match(navigation,/cron: \(\) => App.Cron.load\(\)/);
});
test('inventory uses readable schedule labels and preview changes clear stale dates',()=>{
    const {ui,el,job}=harness();
    assert.equal(ui.scheduleLabel('30 2 * * *'),'Daily at 02:30');
    assert.equal(ui.scheduleLabel('*/15 * * * *'),'Every 15 minutes');
    assert.equal(ui.scheduleLabel('0 3 * * 0'),'Sunday at 03:00');
    assert.equal(ui.scheduleLabel('0 9 * * MON-FRI'),'0 9 * * MON-FRI');
    assert.match(ui.row(job),/Daily at 09:00/);
    el('cron-preview').textContent='Old dates';ui.queuePreview();assert.equal(el('cron-preview').textContent,'Updating upcoming runs…');
});
