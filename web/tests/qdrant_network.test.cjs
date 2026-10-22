const test=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
const path=require('node:path');
const flush=()=>new Promise(resolve=>setImmediate(resolve));

function harness(canWrite=true) {
    const requests=[],tracked=[],confirmations=[],nodes=new Map(),historyEntries=[];
    function element(id='') {
        const parts={},classes=new Set();
        return {id,innerHTML:'',textContent:'',value:'',hidden:false,disabled:false,isConnected:true,dataset:{},children:[],elements:{},
            classList:{add:(...v)=>v.forEach(x=>classes.add(x)),remove:(...v)=>v.forEach(x=>classes.delete(x)),toggle:(v,on)=>on?classes.add(v):classes.delete(v),contains:v=>classes.has(v)},
            setAttribute(){},append(...items){this.children.push(...items);},prepend(item){this.children.unshift(item);},querySelector(s){return parts[s]??=element();},querySelectorAll(){return [];},addEventListener(){},focus(){},reset(){}};
    }
    function el(id){if(!nodes.has(id))nodes.set(id,element(id));return nodes.get(id);}
    el('databases-tab').dataset.canWrite=String(canWrite);
    const window={location:new URL('http://panel.test/servers/source/dashboard?view=databases')};
    const history={pushState(_a,_b,url){historyEntries.push(String(url));window.location=new URL(String(url),window.location.href);},replaceState(_a,_b,url){window.location=new URL(String(url),window.location.href);}};
    const database={id:'vectors',name:'Semantic search',type:'qdrant',status:'Running'};
    const config={bind_address:'0.0.0.0',host_port:6333,grpc_port:6334,version:'v1.19.0',storage_path:'/var/lib/gopulse/databases/vectors'};
    const App={config:{SERVER_ID:'source',SERVER_HOST:'db.example.test'},UI:{confirm:async message=>{confirmations.push(message);return true;},prompt:async()=>'',toast(){}},Databases:{load(){},showListView(){App.Qdrant.deactivate();}}};
    const document={createElement:()=>element(),getElementById:el};
    const context=vm.createContext({App,document,window,history,URL,URLSearchParams,encodeURIComponent,console,navigator:{clipboard:{writeText:async()=>{}}}});
    for(const file of ['infrastructure','qdrant','navigation'])vm.runInContext(fs.readFileSync(path.join(__dirname,'../static/js/app',file+'.js'),'utf8'),context);
    App.Infrastructure.api=async(url,options)=>{
        requests.push({url,options});
        if(options)return {operation_id:'network-op'};
        if(url.endsWith('/config'))return config;
        if(url.endsWith('/credentials'))return {api_key:'test-key'};
        if(url.endsWith('/diagnostics'))return {healthy:true,logs:'safe logs',stats:'10 MB'};
        if(url.endsWith('/snapshots'))return {result:[]};
        if(url.endsWith('/collections'))return {result:{collections:[{name:'documents'},{name:'products'}]}};
        if(url.includes('/collections/'))return {result:{points_count:12,indexed_vectors_count:10,status:'green',config:{params:{vectors:{size:384,distance:'Cosine'}}}}};
        return options?{operation_id:'network-op'}:database;
    };
    App.Infrastructure.track=async id=>tracked.push(id);
    App.Infrastructure.dialog=()=>element();
    return {App,el,requests,tracked,confirmations,window,historyEntries,database,config};
}
test('Qdrant endpoints use the selected server for wildcard bindings and preserve specific bindings',()=>{
    const {App}=harness();
    assert.equal(App.Qdrant.endpoint({bind_address:'0.0.0.0'},6333),'http://db.example.test:6333');
    assert.equal(App.Qdrant.endpoint({bind_address:'127.0.0.1'},16333),'http://127.0.0.1:16333');
    App.config.SERVER_HOST='2001:db8::1';
    assert.equal(App.Qdrant.endpoint({bind_address:'0.0.0.0'},6334),'http://[2001:db8::1]:6334');
});
test('Manage opens a full vector workbench and makes the resource addressable',async()=>{
    const {App,el,window,database}=harness();
    await App.Qdrant.manage('vectors',database);
    assert.equal(el('db-list-view').classList.contains('hidden'),true);
    assert.equal(el('qdrant-manage-view').classList.contains('hidden'),false);
    assert.match(el('qdrant-manage-view').innerHTML,/Application connection/);
    assert.equal(window.location.searchParams.get('resource'),'vectors');
    assert.equal(window.location.searchParams.get('panel'),'overview');
    assert.equal(el('vector-count').textContent,2);
    assert.equal(el('vector-health').textContent,'Connected');
});
test('tab links survive reload and Back clears the selected resource',async()=>{
    const {App,window,database,historyEntries,el}=harness();
    window.location.search='?view=databases&resource=vectors&panel=settings';
    await App.Qdrant.manage('vectors',database);
    assert.equal(App.Qdrant.current.panel,'settings');
    App.Qdrant.switchPanel('connection');
    assert.equal(window.location.searchParams.get('panel'),'connection');
    el('vector-api-key').value='secret';
    App.Qdrant.back();
    assert.equal(window.location.searchParams.has('resource'),false);
    assert.equal(window.location.searchParams.has('panel'),false);
    assert.equal(el('vector-api-key').value,'');
    assert.equal(App.Qdrant.current,null);
    assert.equal(historyEntries.length,2);
});
test('collection discovery stays separate from slow diagnostics and errors leave settings usable',async()=>{
    const {App,el,database,config,requests}=harness();
    let reject;
    App.Infrastructure.api=async url=>{requests.push({url});if(url.endsWith('/config'))return config;return new Promise((_r,j)=>reject=j);};
    const pending=App.Qdrant.manage('vectors',database);await flush();
    assert.match(el('qdrant-manage-view').innerHTML,/Network access/);
    assert.equal(requests.some(r=>r.url.endsWith('/diagnostics')),false);
    reject(Error('SSH unavailable'));await pending;
    assert.equal(el('vector-health').textContent,'Unavailable');
    App.Qdrant.switchPanel('settings');
    assert.equal(el('vector-panel-settings').classList.contains('hidden'),false);
    assert.match(el('vector-error').textContent,/SSH unavailable/);
});
test('network changes submit the reviewed settings to the original server and track completion',async()=>{
    const {App,el,requests,tracked,confirmations}=harness();
    App.config.SERVER_ID='other';
    const values={bind_address:'0.0.0.0',host_port:16333,grpc_port:16334};
    await App.Qdrant.configureNetwork('vectors',values,el('root'),el('button'),'source');
    assert.equal(requests[0].url,'/servers/source/databases/vectors/qdrant/network');
    assert.equal(requests[0].options.method,'PUT');
    assert.deepEqual(JSON.parse(requests[0].options.body),values);
    assert.match(confirmations[0],/restart briefly/);
    assert.deepEqual(tracked,['network-op']);
    assert.equal(el('button').disabled,false);
});
test('canceling network changes performs no writes and errors release controls',async()=>{
    const {App,el,requests}=harness();
    App.UI.confirm=async()=>false;
    await App.Qdrant.configureNetwork('vectors',{},el('root'),el('button'),'source');assert.equal(requests.length,0);
    App.UI.confirm=async()=>true;
    App.Infrastructure.api=async()=>{throw Error('port already allocated');};
    await App.Qdrant.configureNetwork('vectors',{},el('root'),el('button'),'source');
    assert.match(el('vector-error').textContent,/port already allocated/);assert.equal(el('button').disabled,false);
});
test('API keys are fetched only on reveal and cleared on tab changes',async()=>{
    const {App,el,requests,database}=harness();
    await App.Qdrant.manage('vectors',database);
    assert.equal(requests.some(r=>r.url.endsWith('/credentials')),false);
    App.Qdrant.switchPanel('connection');await App.Qdrant.revealKey();
    assert.equal(requests.at(-1).url,'/servers/source/databases/vectors/credentials');
    assert.equal(el('vector-api-key').value,'test-key');assert.equal(el('vector-api-key').hidden,false);
    App.Qdrant.switchPanel('overview');assert.equal(el('vector-api-key').value,'');assert.equal(el('vector-api-key').hidden,true);
});
test('viewers get read-only vector management without mutation or credential controls',async()=>{
    const {App,el,requests,database}=harness(false);
    await App.Qdrant.manage('vectors',database);
    assert.doesNotMatch(el('qdrant-manage-view').innerHTML,/data-q-action="(reveal|remove|new-collection|create-snapshot)"/);
    await App.Qdrant.revealKey();
    await App.Qdrant.action({dataset:{qAction:'stop'}},App.Qdrant.current);
    assert.equal(requests.some(r=>r.options||r.url.endsWith('/credentials')),false);
});
test('leaving the workbench discards late credential and collection responses',async()=>{
    const {App,el,database}=harness();await App.Qdrant.manage('vectors',database);
    App.Qdrant.switchPanel('connection');let resolve;
    App.Infrastructure.api=()=>new Promise(r=>resolve=r);
    const pending=App.Qdrant.revealKey();App.Qdrant.deactivate();resolve({api_key:'do-not-display'});await pending;
    assert.equal(el('vector-api-key').value,'');assert.equal(el('vector-api-key').hidden,true);
    const previous=el('vector-count').textContent;
    const opening=App.Qdrant.manage('vectors',database);await flush();App.Qdrant.deactivate();resolve({});await opening;
    assert.equal(el('vector-count').textContent,previous);
});
test('collection names are escaped and search filters without remote requests',async()=>{
    const {App,el,requests,database}=harness();await App.Qdrant.manage('vectors',database);
    App.Qdrant.current.collections=[{name:'<img src=x onerror=alert(1)>'},{name:'Documents'}];
    App.Qdrant.renderCollections(App.Qdrant.current);
    assert.doesNotMatch(el('vector-collections').innerHTML,/<img /);
    assert.match(el('vector-collections').innerHTML,/&lt;img/);
    const reads=requests.length;
    el('vector-search').value=' DOC ';App.Qdrant.renderCollections(App.Qdrant.current);
    assert.match(el('vector-collections').innerHTML,/Documents/);assert.doesNotMatch(el('vector-collections').innerHTML,/&lt;img/);
    assert.equal(requests.length,reads);
});
test('collection details show real vector metrics and snapshot reads stay collection-scoped',async()=>{
    const {App,el,requests,database}=harness();await App.Qdrant.manage('vectors',database);
    await App.Qdrant.openCollection('documents');
    assert.match(el('vector-collection-detail').innerHTML,/384 dimensions/);
    assert.match(el('vector-collection-detail').innerHTML,/Cosine/);
    assert.match(el('vector-collection-detail').innerHTML,/12/);
    el('vector-snapshot-collection').value='documents';await App.Qdrant.loadSnapshots();
    assert.equal(requests.at(-1).url,'/servers/source/databases/vectors/qdrant/collections/documents/snapshots');
});
test('collection creation submits vector dimensions and distance to the existing API',async()=>{
    const {App,el,requests,database}=harness();await App.Qdrant.manage('vectors',database);
    const form=el('create-form');form.elements={name:{value:'articles'},size:{value:'768'},distance:{value:'Dot'}};
    await App.Qdrant.createCollection(form,App.Qdrant.current);
    const write=requests.find(r=>r.options?.method==='POST');
    assert.equal(write.url,'/servers/source/databases/vectors/qdrant/collections');
    assert.deepEqual(JSON.parse(write.options.body),{name:'articles',size:768,distance:'Dot'});
    assert.equal(App.Qdrant.current.collection,'articles');
});
test('snapshot restores require typed collection confirmation and preserve the selected snapshot',async()=>{
    const {App,el,requests,tracked,database}=harness();await App.Qdrant.manage('vectors',database);
    el('vector-snapshot-collection').value='documents';
    const button={dataset:{qAction:'restore',name:'documents.snapshot'},disabled:false};
    App.UI.prompt=async()=> 'wrong';
    await App.Qdrant.action(button,App.Qdrant.current);
    assert.equal(requests.some(r=>r.options),false);
    App.UI.prompt=async()=> 'documents';
    await App.Qdrant.action(button,App.Qdrant.current);
    const write=requests.find(r=>r.options);
    assert.equal(write.url,'/servers/source/databases/vectors/qdrant/collections/documents/snapshots/restore?confirm=documents');
    assert.deepEqual(JSON.parse(write.options.body),{filename:'documents.snapshot'});
    assert.deepEqual(tracked,['network-op']);assert.equal(button.disabled,false);
});
test('returning to Connection does not reveal a key requested before leaving the tab',async()=>{
    const {App,el,database}=harness();await App.Qdrant.manage('vectors',database);
    App.Qdrant.switchPanel('connection');let resolve;
    App.Infrastructure.api=()=>new Promise(r=>resolve=r);
    const pending=App.Qdrant.revealKey();
    App.Qdrant.switchPanel('overview');App.Qdrant.switchPanel('connection');
    resolve({api_key:'late-key'});await pending;
    assert.equal(el('vector-api-key').hidden,true);assert.equal(el('vector-api-key').value,'');
});
