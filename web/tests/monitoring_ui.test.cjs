const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
function harness() {
    const nodes = new Map(), reads = [], writes = [], intervals = [], toasts = [];
    let permitted = true, confirmed = true;
    function el(id) {
        if (!nodes.has(id)) {
            const classes = new Set();
            nodes.set(id,{id,value:'',checked:false,textContent:'',innerHTML:'',disabled:false,open:false,type:'',dataset:{canWrite:'true'},events:{},style:{},
                classList:{toggle(name,on){if(on)classes.add(name);else classes.delete(name);},contains:name=>classes.has(name)},
                addEventListener(name,fn){this.events[name]=fn;},showModal(){this.open=true;},close(){this.open=false;this.events.close?.();},focus(){},reportValidity(){return true;}});
        }
        if (id === 'alerts-tab') nodes.get(id).dataset.canWrite = String(permitted);
        return nodes.get(id);
    }
    const rule = {id:'r',server_id:'a',name:'Disk space',metric:'disk',threshold:85,duration_seconds:300,enabled:true,webhook_url:'https://hooks.example/events',webhook_secret:'example-signing-key',has_webhook:true,state:'healthy',last_value:60,silenced_until:'0001-01-01T00:00:00Z'};
    const event = {id:'e',rule_name:'Disk space',state:'firing',delivery_status:'retry',attempts:1,occurred_at:'2026-10-17T12:00:00Z',last_error:'HTTP 503'};
    const App = {config:{SERVER_ID:'a'},Navigation:{current:'alerts'},UI:{toast:(...args)=>toasts.push(args)},API:{
        get:async endpoint=>{reads.push(endpoint);return {success:true,data:endpoint.endsWith('/events')?[event]:[rule]};},
        post:async(endpoint,body)=>{writes.push({method:'POST',endpoint,body});return {success:true};},
        put:async(endpoint,body)=>{writes.push({method:'PUT',endpoint,body});return {success:true};},
        delete:async endpoint=>{writes.push({method:'DELETE',endpoint});return {success:true};}}};
    const context = vm.createContext({App,document:{getElementById:el,hidden:false},window:{confirm:()=>confirmed},console,Date,URL,URLSearchParams,
        setInterval:fn=>{intervals.push(fn);return intervals.length;},clearInterval(){}});
    vm.runInContext(fs.readFileSync(path.join(__dirname,'../static/js/app/alerts.js'),'utf8'),context);
    return {ui:App.Alerts,App,el,rule,event,reads,writes,intervals,toasts,allow:value=>permitted=value,confirm:value=>confirmed=value};
}
const plain = value => JSON.parse(JSON.stringify(value));
test('alerts initialize read-only and refresh only in the active view', async()=>{
    const {ui,reads,writes,intervals,App}=harness();await ui.load();await ui.load();
    assert.equal(intervals.length,1);assert.equal(writes.length,0);assert.deepEqual(reads,['/alerts','/alerts/events','/alerts','/alerts/events']);
    App.Navigation.current='overview';intervals[0]();assert.equal(reads.length,4);
    ui.deactivate();assert.equal(ui.timer,null);
});
test('viewer can inspect alerts but cannot edit or send webhooks', async()=>{
    const {ui,allow,rule,writes}=harness();allow(false);await ui.load();
    assert.doesNotMatch(ui.card(rule),/data-action=/);ui.openEditor();
    for(const action of ['edit','toggle','silence','unsilence','test','delete']) await ui.action(action,'r');
    await ui.save();assert.equal(writes.length,0);
});
test('rule and incident fields are escaped and webhook secrets are absent from cards', async()=>{
    const {ui,rule,event,el}=harness();await ui.load();
    const card=ui.card({...rule,id:'x" onclick="evil',name:'<img src=x onerror=evil>'});
    assert.doesNotMatch(card,/<img|data-id="x" onclick=/);assert.match(card,/&lt;img/);
    assert.ok(!card.includes(rule.webhook_secret));assert.ok(!card.includes(rule.webhook_url));
    ui.events=[{...event,rule_name:'<script>alert(1)</script>',last_error:'<img src=x>'}];ui.render();assert.doesNotMatch(el('alerts-events').innerHTML,/<script|<img/);
});
test('create and edit save only configuration through server-scoped API', async()=>{
    const {ui,el,writes,rule}=harness();ui.init();ui.openEditor();
    el('alert-name').value='CPU pressure';el('alert-metric').value='cpu';el('alert-threshold').value='90';el('alert-duration').value='60';el('alert-webhook').value='https://hooks.example/events';await ui.save();
    assert.deepEqual(plain(writes[0]),{method:'POST',endpoint:'/alerts',body:{name:'CPU pressure',metric:'cpu',threshold:90,duration_seconds:60,enabled:true,webhook_url:'https://hooks.example/events'}});
    ui.openEditor(rule);assert.equal(el('alert-secret').value,rule.webhook_secret);assert.equal(el('alert-secret').type,'password');
    await ui.save();assert.equal(writes[1].method,'PUT');assert.equal(writes[1].endpoint,'/alerts/r');assert.equal(Object.hasOwn(writes[1].body,'webhook_secret'),false);
    assert.equal(el('alert-secret').value,'');assert.equal(el('alert-webhook').value,'');
});
test('availability hides percentage requirements and saves threshold zero', async()=>{
    const {ui,el,writes}=harness();ui.init();ui.openEditor();el('alert-name').value='Availability';el('alert-metric').value='availability';ui.metricChanged();assert.equal(el('alert-threshold').disabled,true);await ui.save();assert.equal(writes[0].body.threshold,0);
});
test('management actions preserve destination and explicitly request test delivery',async()=>{
    const {ui,rule,writes}=harness();await ui.load();
    for(const action of ['toggle','silence','unsilence','test','delete']) await ui.action(action,rule.id);
    assert.deepEqual(writes.map(w=>[w.method,w.endpoint]),[['PUT','/alerts/r'],['PUT','/alerts/r/silence'],['PUT','/alerts/r/silence'],['POST','/alerts/r/test'],['DELETE','/alerts/r']]);
    assert.equal(writes[0].body.webhook_url,rule.webhook_url);assert.equal(writes[0].body.enabled,false);assert.equal(writes[1].body.seconds,3600);assert.equal(writes[2].body.seconds,0);
});
test('cancelling deletion and duplicate saves cannot trigger writes',async()=>{
    const {ui,writes,confirm}=harness();await ui.load();confirm(false);await ui.action('delete','r');assert.equal(writes.length,0);ui.busy=true;await ui.action('test','r');await ui.save();assert.equal(writes.length,0);
});
test('save failures keep editor and credentials available for correction',async()=>{
    const {ui,el,rule,App}=harness();ui.init();ui.openEditor(rule);App.API.put=async()=>{throw new Error('Invalid webhook URL');};await ui.save();assert.equal(el('alert-editor').open,true);assert.equal(el('alert-form-error').textContent,'Invalid webhook URL');assert.equal(el('alert-secret').value,rule.webhook_secret);assert.equal(ui.busy,false);assert.equal(el('alert-save').disabled,false);
});
test('late reads after navigation cannot render into a deactivated view',async()=>{
    const {ui,App,el}=harness();let resolve;const pending=new Promise(r=>resolve=r);App.API.get=()=>pending;const load=ui.load();ui.deactivate();resolve({data:[]});await load;assert.equal(el('alerts-rules').innerHTML,'');
});
function charts(){
    const updates=[];const context=vm.createContext({App:{config:{SERVER_ID:'a'},Navigation:{current:'overview'}},document:{getElementById:()=>null},Date,console});
    vm.runInContext(fs.readFileSync(path.join(__dirname,'../static/js/app/charts.js'),'utf8'),context);context.App.Charts.updateOrCreateChart=(id,data,options)=>updates.push({id,data,options});return {ui:context.App.Charts,updates};
}
test('charts leave missing minutes and failed readings as gaps',()=>{
    const {ui,updates}=charts();const point=(timestamp,unavailable=false)=>({timestamp,unavailable,cpu_percent:70,mem_percent:40,disk_percent:80,net_rx_bytes:1024,net_tx_bytes:2048});
    ui.renderCharts([point('2026-10-17T10:00:00Z'),point('2026-10-17T10:02:00Z'),point('2026-10-17T10:03:00Z',true)]);
    assert.deepEqual(plain(updates[0].data.datasets[0].data),[70,null,70,null]);assert.equal(updates[0].options.spanGaps,false);
    assert.deepEqual(plain(updates[3].data.datasets[0].data),[null,null,null,null]);
});
test('network counter resets cannot produce negative throughput',()=>{
    const {ui,updates}=charts();ui.renderCharts([{timestamp:'2026-10-17T10:00:00Z',net_rx_bytes:1000,net_tx_bytes:2000},{timestamp:'2026-10-17T10:01:00Z',net_rx_bytes:100,net_tx_bytes:100}]);
    assert.deepEqual(plain(updates[3].data.datasets[0].data),[null,0]);
});
test('alerts navigation and persistent history ranges are exposed in templates',()=>{
    const read=file=>fs.readFileSync(path.join(__dirname,'..',file),'utf8');
    assert.match(read('static/js/app/navigation.js'),/alerts: \(\) => App.Alerts.load\(\)/);
    assert.match(read('templates/base.html'),/app\/alerts.js/);assert.match(read('templates/dashboard.html'),/template "monitoring-panel"/);
    for(const id of ['alerts-tab','alerts-events','alerts-rules','alert-form','alert-webhook','alert-secret'])assert.ok(read('templates/monitoring.html').includes(`id="${id}"`));
    for(const range of ['7d','30d'])assert.ok(read('templates/dashboard.html').includes(`id="btn-chart-${range}"`));
});
