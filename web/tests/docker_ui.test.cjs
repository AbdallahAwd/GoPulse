const test=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const vm=require('node:vm');
const flush=async()=>{await Promise.resolve();await Promise.resolve();await Promise.resolve();};
function harness(){
    const nodes=new Map(),reads=[],writes=[],toasts=[],handlers={},stream=['mock output\n'];let installed=true;
    function el(id){
        if(!nodes.has(id)){
            const classes=new Set(['hidden']),attributes={};
            nodes.set(id,{id,value:'',textContent:'',innerText:'',innerHTML:'',files:[],dataset:{},style:{},children:[],disabled:false,open:false,
                classList:{add:(...v)=>v.forEach(x=>classes.add(x)),remove:(...v)=>v.forEach(x=>classes.delete(x)),contains:x=>classes.has(x),replace:(a,b)=>{classes.delete(a);classes.add(b);}},
                setAttribute:(k,v)=>attributes[k]=v,getAttribute:k=>attributes[k],querySelector:()=>el('panel'),hasChildNodes(){return this.children.length>0;},appendChild(n){this.children.push(n);},insertBefore(n){this.children.unshift(n);},focus(){},addEventListener:(n,cb)=>{el(id).events??={};el(id).events[n]=cb;}});
        }
        return nodes.get(id);
    }
    const navigation=['containers','images','volumes','networks'].map(key=>{const node=el(key+'-nav');node.dataset.target=key+'-view';return node;});
    const App={config:{SERVER_ID:'server',API_BASE:'/api'},state:{lastContainers:[],lastImages:[],lastVolumes:[]},UI:{el,toast:(...args)=>toasts.push(args),confirm:async()=>true},
        API:{get:async url=>{reads.push(url);return {success:true,data:url==='/system/check/docker'?{installed}:url.includes('/logs')?{logs:[{timestamp:'2026-10-13T00:00:00Z',message:'info <script>safe</script>'}]}:[]};},post:async(url,body)=>{writes.push({method:'POST',url,body});return {success:true};},delete:async url=>{writes.push({method:'DELETE',url});return {success:true};}}};
    const document={getElementById:el,querySelector:s=>s.includes('run-image')?el('run-image-submit'):null,querySelectorAll:s=>s==='.docker-nav-btn'?navigation:s==='.docker-view'?['containers','images','volumes','networks'].map(k=>el(k+'-view')):[],addEventListener:(name,cb)=>handlers[name]=cb,createElement:()=>el('row-'+Math.random())};
    const context=vm.createContext({App,document,window:{},console:{error(){}},TextDecoder,setTimeout:fn=>fn(),fetch:async(url,opts={})=>{
        if(opts.method==='POST')writes.push({method:'POST',url,body:opts.body?JSON.parse(opts.body):undefined});else reads.push(url);
        let index=0;
        return {ok:true,
            json:async()=>({success:true,data:{path:'/srv',files:[{name:'folder',is_dir:true},{name:'Dockerfile',is_dir:false}]}}),
            body:{getReader:()=>({read:async()=>index<stream.length?{done:false,value:new TextEncoder().encode(stream[index++])}:{done:true}})}
        };
    },FileReader:class{readAsText(file){this.onload({target:{result:file.content}});}}});
    for(const name of ['docker','containers','images','volumes','networks'])vm.runInContext(fs.readFileSync(path.join(__dirname,'../static/js/app',name+'.js'),'utf8'),context);
    return {ui:App.Docker,App,el,reads,writes,toasts,context,document,navigation,stream,setInstalled:value=>installed=value};
}
test('Docker section navigation retains existing views, installation checks and read loaders',async()=>{
    const {ui,el,reads,writes,navigation}=harness();
    for(const key of ['containers','images','volumes','networks']){
        el('docker-manage-menu').open=true;ui.switchView(key+'-view');await flush();
        assert.equal(el(key+'-view').classList.contains('hidden'),false);
        assert.equal(navigation.find(n=>n.dataset.target===key+'-view').getAttribute('aria-pressed'),'true');
        assert.equal(el('docker-manage-menu').open,false);assert.ok(reads.includes('/'+key));
    }
    assert.equal(el('docker-manage-label').textContent,'Networks');assert.equal(writes.length,0);
});
test('missing Docker still shows installation state without installing automatically',async()=>{
    const {ui,el,setInstalled,writes}=harness();setInstalled(false);await ui.refresh();
    assert.equal(el('docker-not-installed').classList.contains('hidden'),false);assert.equal(el('docker-content').classList.contains('hidden'),true);assert.equal(writes.length,0);
});
test('container cards preserve state-specific lifecycle actions, logs and button IDs',()=>{
    const {ui,el}=harness();ui.Containers.render([{id:'abc123',name:'api <script>',image:'example/app:latest',state:'running',status:'Up 2 hours'},{id:'xyz123',name:'worker',image:'alpine:latest',state:'exited',status:'Exited (0)'}]);
    const html=el('containers-grid').innerHTML;
    for(const id of ['btn-stop-abc123','btn-start-xyz123','btn-restart-abc123','btn-delete-abc123'])assert.ok(html.includes(id));
    assert.match(html,/Containers.logs\(&quot;abc123&quot;\)/);assert.match(html,/Up 2 hours/);assert.match(html,/api &lt;script&gt;/);assert.doesNotMatch(html,/<script>/);assert.equal(el('docker-containers-count').textContent,'2 containers');
});
test('container lifecycle requests and confirmed removal keep their API methods and endpoints',async()=>{
    const {ui,App,writes}=harness();
    for(const action of ['start','stop','restart'])await ui.Containers.action('abc123',action);
    assert.deepEqual(writes.map(w=>w.url),['/containers/abc123/start','/containers/abc123/stop','/containers/abc123/restart']);
    App.UI.confirm=async()=>false;await ui.Containers.action('abc123','delete');assert.equal(writes.length,3);
    App.UI.confirm=async()=>true;await ui.Containers.action('abc123','delete');assert.equal(writes[3].method,'DELETE');assert.equal(writes[3].url,'/containers/abc123');
});
test('container logs and refresh remain read-only and escape remote output',async()=>{
    const {ui,App,el,reads,writes}=harness();App.state.lastContainers=[{id:'abc123',name:'api'}];ui.Containers.logs('abc123');await flush();
    assert.equal(el('docker-logs-title').textContent,'api');assert.match(el('docker-logs-output').innerHTML,/&lt;script&gt;/);assert.doesNotMatch(el('docker-logs-output').innerHTML,/<script>/);
    ui.Containers.refreshLogs();await flush();assert.deepEqual(reads,['/containers/abc123/logs','/containers/abc123/logs']);assert.equal(writes.length,0);ui.Containers.closeLogs();assert.equal(ui.Containers.currentLogsContainerId,null);
});
for(const reference of ['nginx:alpine','registry.example.com:5000/team/app:v2','team/app:latest'])test(`image reference ${reference} survives display and Run dialog selection`,()=>{
    const {ui,el}=harness();ui.Images.render([{id:'sha256:abcdef123456',repo_tags:[reference],size:1048576}]);
    const html=el('images-tbody').innerHTML;assert.ok(html.includes(ui.handlerArgument(reference)));assert.match(html,/1.00 MB/);assert.match(html,/sha256:abcde/);
    ui.Run.openModal(reference);assert.equal(el('run-image-name').value,reference);assert.equal(ui.Run.currentImage,reference);
});
test('untagged images can run by their full image ID',()=>{
    const {ui,el}=harness();ui.Images.render([{id:'sha256:untagged123',repo_tags:[],size:0}]);assert.match(el('images-tbody').innerHTML,/Untagged image/);assert.ok(el('images-tbody').innerHTML.includes(ui.handlerArgument('sha256:untagged123')));
});
test('Run submit uses its stable ID and unchanged image/name/ports payload',async()=>{
    const {ui,el,writes}=harness();ui.Run.openModal('registry.example.com:5000/team/app:v2');el('run-container-name').value='web';el('run-container-ports').value='8080:80,8443:443';
    await ui.Run.submit();assert.equal(writes.length,1);assert.equal(writes[0].url,'/servers/server/images/run');assert.equal(writes[0].body.image,'registry.example.com:5000/team/app:v2');assert.equal(writes[0].body.name,'web');assert.equal(writes[0].body.ports,'8080:80,8443:443');assert.equal(el('run-image-submit').disabled,false);assert.equal(ui.activeView,'containers-view');
});
for(const [type,source,expectedURL,expectedBody] of [
    ['dockerfile','path','/images/build',{tag:'app:v1',path:'/srv/app'}],['dockerfile','content','/images/build',{tag:'app:v1',content:'FROM alpine:latest'}],
    ['compose','path','/compose/build',{file:'/srv/app'}],['compose','content','/compose/build',{content:'FROM alpine:latest'}]
])test(`${type} from ${source}: visibility, payload and streamed build output are preserved`,async()=>{
    const {ui,el,writes}=harness();el('build-type').value=type;el('build-source').value=source;el('build-tag').value='app:v1';el('build-path').value='/srv/app';el('build-content').value='FROM alpine:latest';ui.Build.openModal();
    assert.equal(el('build-path-wrapper').classList.contains('hidden'),source==='content');assert.equal(el('build-content-group').classList.contains('hidden'),source!=='content');assert.equal(el('build-tag-group').classList.contains('hidden'),type==='compose');
    await ui.Build.start();assert.equal(writes.length,1);assert.equal(writes[0].url,'/api/servers/server'+expectedURL);assert.deepEqual(writes[0].body,expectedBody);assert.match(el('build-terminal').innerText,/mock output/);assert.match(el('build-terminal').innerText,/Build process completed/);
});
test('build validation sends no request and local file loading preserves pasted content',async()=>{
    const {ui,el,writes}=harness();el('build-type').value='dockerfile';el('build-source').value='path';await ui.Build.start();assert.equal(writes.length,0);el('build-tag').value='app';await ui.Build.start();assert.equal(writes.length,0);
    ui.Build.loadFromFile({files:[{content:'FROM scratch\nCMD ["app"]'}]});assert.equal(el('build-content').value,'FROM scratch\nCMD ["app"]');
});
test('pull image retains registry reference, server endpoint and output stream',async()=>{
    const {ui,el,writes}=harness();ui.Images.openPullModal();el('pull-image-name').value=' registry.example.com:5000/team/app:v2 ';await ui.Images.startPull();assert.equal(writes[0].url,'/api/servers/server/images/pull');assert.equal(writes[0].body.image,'registry.example.com:5000/team/app:v2');assert.match(el('pull-terminal').innerText,/mock output/);assert.match(el('pull-terminal').innerText,/Pull completed successfully/);ui.Images.clearPullTerminal();assert.equal(el('pull-terminal').innerText,'');
});
for(const [module,id,url] of [['Images','sha256:abc','/images/sha256:abc'],['Volumes','db_data','/volumes/db_data'],['Networks','network-id','/networks/network-id']])test(`${module}: removal stays confirmed and uses the same DELETE endpoint`,async()=>{
    const {ui,App,writes}=harness();App.UI.confirm=async()=>false;await ui[module].remove(id);assert.equal(writes.length,0);App.UI.confirm=async()=>true;await ui[module].remove(id);assert.equal(writes[0].method,'DELETE');assert.equal(writes[0].url,url);
});
test('volume and network tables preserve metadata and escape remote names',()=>{
    const {ui,el}=harness();ui.Volumes.render([{name:'db_data',driver:'local',mountpoint:'/var/lib/docker/volumes/db_data/_data'}]);assert.match(el('volumes-tbody').innerHTML,/db_data/);assert.match(el('volumes-tbody').innerHTML,/\/var\/lib\/docker\/volumes\/db_data\/_data/);assert.equal(el('docker-volumes-count').textContent,'1 volume');
    ui.Networks.render([{id:'net-id',name:'<bad>',driver:'bridge',scope:'local'}]);assert.match(el('networks-tbody').innerHTML,/&lt;bad&gt;/);assert.match(el('networks-tbody').innerHTML,/bridge/);assert.match(el('networks-tbody').innerHTML,/local/);
});
test('file picker retains server-scoped reads, directory selection, paths and file mode',async()=>{
    const {ui,el,reads,writes}=harness();ui.FilePicker.open('build-path','directory');await flush();await flush();assert.ok(reads.includes('/api/servers/server/files/list?path=%2F'));assert.equal(ui.FilePicker.currentPath,'/srv');ui.FilePicker.select('folder');assert.equal(el('build-path').value,'/srv/folder');assert.equal(el('build-path-display').textContent,'/srv/folder');
    ui.FilePicker.open('compose-file','file');await flush();await flush();ui.FilePicker.select('Dockerfile');assert.equal(el('compose-file').value,'/srv/Dockerfile');assert.equal(writes.length,0);
});
test('all Docker dialogs keep their field IDs and handlers without CSS-dependent Run lookup',()=>{
    const html=fs.readFileSync(path.join(__dirname,'../templates/dashboard.html'),'utf8');
    for(const id of ['docker-logs-modal','docker-logs-title','docker-logs-output','run-image-modal','run-image-name','run-container-name','run-container-ports','run-image-submit','build-modal','build-type','build-source','build-path','build-path-display','compose-file','compose-file-display','build-tag','build-content','build-file-upload','build-terminal','build-terminal-container','pull-modal','pull-image-name','pull-terminal','file-picker-modal','file-picker-title','file-picker-path','file-picker-loading','file-picker-tbody'])assert.equal((html.match(new RegExp(`id="${id}"`,'g'))||[]).length,1,id);
    assert.match(html,/App.Docker.Run.submit\(\)/);assert.match(html,/App.Docker.Build.start\(\)/);assert.match(html,/App.Docker.Images.startPull\(\)/);
});
