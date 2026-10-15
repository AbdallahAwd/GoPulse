const test=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const vm=require('node:vm');
function harness({deferTimers=false}={}){
    const timers=new Map();let timerSequence=0;
    const elements=new Map(),writes=[],reads=[],toasts=[],handlers={},copied=[];
    function el(id){
        if(!elements.has(id)){
            const classes=new Set(['hidden']),attrs={};
            elements.set(id,{id,value:'',innerHTML:'',innerText:'Create application',textContent:'',checked:false,files:[],style:{},disabled:false,children:[],
                classList:{add:(...vs)=>vs.forEach(v=>classes.add(v)),remove:(...vs)=>vs.forEach(v=>classes.delete(v)),contains:v=>classes.has(v),toggle:(v,on)=>{if(on??!classes.has(v))classes.add(v);else classes.delete(v);}},
                setAttribute:(k,v)=>attrs[k]=v,getAttribute:k=>attrs[k],querySelector:()=>el('inner'),querySelectorAll:()=>[],focus(){},select(){},remove(){},dispatchEvent(){},setSelectionRange(){},contains:()=>false});
        }
        return elements.get(id);
    }
    const App={config:{SERVER_ID:'server'},state:{},UI:{toast:(...args)=>toasts.push(args),confirm:async()=>true},Files:{list:p=>reads.push('files:'+p)},
        API:{get:async url=>{reads.push(url);return {success:true,data:[]};},post:async(url,body)=>{writes.push({method:'POST',url,body});return {success:true,data:{id:'created'}};},delete:async url=>{writes.push({method:'DELETE',url});return {success:true};},upload:async(url,body,progress)=>{writes.push({method:'UPLOAD',url,body});progress?.(57.6);return {success:true};}}};
    const document={getElementById:el,querySelector:()=>null,querySelectorAll:s=>s==='.tech-card'?['nextjs','nodejs','go','laravel'].map(k=>el('tech-'+k)):[],
        createElement:()=>el('clipboard-input'),body:{appendChild(){}},execCommand:()=>true,addEventListener:(name,cb)=>handlers[name]=cb};
    const context=vm.createContext({App,document,navigator:{clipboard:{writeText:async t=>copied.push(t)}},FormData,console:{log(){},error(){}},setTimeout:(fn,ms)=>{if(!deferTimers)return fn();const id=++timerSequence;timers.set(id,{fn,ms});return id;},clearTimeout:id=>timers.delete(id),requestAnimationFrame:fn=>fn(),Event:class{}});
    vm.runInContext(fs.readFileSync(path.join(__dirname,'../static/js/app/projects.js'),'utf8'),context);
    return {ui:App.Projects,App,el,writes,reads,toasts,document,context,handlers,copied,timers};
}
const project={id:'app-go',name:'API',type:'go',path:'/srv/api',port:8080,host_port:9000,status:'Stopped',last_build:'2026-10-12T18:00:00Z',last_deploy:'2026-10-12T19:00:00Z',source_type:'git',build_type:'process',env_vars:{TOKEN:'private-secret'},restart_on_boot:true,webhook_enabled:true};
test('compact card preserves every action and displays metadata without environment secrets',()=>{
    const {ui}=harness();const html=ui.renderProjectCard(project);
    for(const name of ['showProjectOps','showEnvModal','openFiles','showWebhookModal','editProject','copyProjectPath'])assert.match(html,new RegExp(`App.Projects.${name}\\('app-go'\\)`));
    for(const action of ['start','stop','build','delete'])assert.match(html,new RegExp(`action\\('app-go', '${action}', this\\)`));
    assert.match(html,/8080 → 9000/);assert.match(html,/Restart on boot/);assert.match(html,/Webhook enabled/);assert.match(html,/app-env-count">1/);
    assert.doesNotMatch(html,/private-secret/);
    const unbuilt=ui.renderProjectCard({...project,last_build:'0001-01-01T00:00:00Z'});
    assert.match(unbuilt,/Build application/);assert.doesNotMatch(unbuilt,/action\('app-go', '(?:start|stop)'/);
    const escaped=ui.renderProjectCard({...project,name:'<script>alert(1)</script>',path:'/srv/<img src=x>'});
    assert.doesNotMatch(escaped,/<script>/);assert.match(escaped,/&lt;script&gt;/);assert.match(escaped,/&lt;img src=x&gt;/);
});
test('inventory counts and combined filters still reflect all saved applications',()=>{
    const {ui,el,writes}=harness();const list=[project,{...project,id:'app-node',name:'Worker',type:'nodejs',status:'Running',last_build:'',restart_on_boot:false,webhook_enabled:false}];
    ui.renderList(list);assert.match(el('projects-list').innerHTML,/2<\/strong> applications/);assert.match(el('projects-list').innerHTML,/1<\/strong> running/);
    ui.setFilter('type','go');ui.setFilter('status','stopped');ui.setFilter('query','9000');
    assert.equal(ui.getFilteredProjects(list).length,1);assert.match(el('projects-list').innerHTML,/1 of 2 applications/);
    ui.setFilter('query','no match');assert.match(el('projects-list').innerHTML,/No matching applications/);
    ui.resetFilters();assert.equal(ui.getFilteredProjects(list).length,2);assert.equal(writes.length,0);
    ui.renderList([]);assert.match(el('projects-list').innerHTML,/showCreateModal/);
});
const defaults={nextjs:['npm install && npm run build','npm start'],nodejs:['npm install','npm start'],go:['go build -o app','./app'],laravel:['composer install --no-dev --optimize-autoloader','php artisan serve --port=8000 --host=0.0.0.0']};
for(const [tech,commands] of Object.entries(defaults)){
    test(`${tech}: technology defaults, upload payload, custom commands and progress stay intact`,async()=>{
        const {ui,el,writes}=harness();ui.selectTech(tech);
        assert.equal(el('selected-tech').value,tech);assert.equal(el('tech-'+tech).getAttribute('aria-pressed'),'true');
        assert.equal(el('project-build-cmd').value,commands[0]);assert.equal(el('project-start-cmd').value,commands[1]);
        el('project-name').value='New application';el('project-port').value='4200';el('project-build-cmd').value='custom build';el('project-start-cmd').value='custom start';el('project-restart-on-boot').checked=true;
        el('project-file').files=[new Blob(['archive'],{type:'application/zip'})];
        await ui.createProject();
        const body=writes[0].body;assert.equal(writes[0].url,'/servers/server/projects');
        assert.equal(body.type,tech);assert.equal(body.port,4200);assert.equal(body.host_port,4200);
        assert.equal(body.build_command,'custom build');assert.equal(body.start_command,'custom start');assert.equal(body.restart_on_boot,true);assert.equal(body.source_type,'upload');assert.equal(body.build_type,'process');
        assert.equal(writes[1].url,'/servers/server/projects/upload?project_id=created');assert.equal(writes[1].body.get('project_id'),'created');assert.equal(await writes[1].body.get('file').text(),'archive');
        assert.equal(el('project-upload-bar').style.width,'57.6%');assert.equal(el('project-upload-percent').textContent,'58%');assert.equal(el('create-project-btn-submit').disabled,false);
    });
    test(`${tech}: repository creation keeps its distinct endpoint and field names`,async()=>{
        const {ui,el,writes}=harness();ui.selectTech(tech);el('project-name').value='Repository app';el('project-repo-url').value='https://github.com/example/project.git';el('project-restart-on-boot').checked=false;
        await ui.createProject();assert.equal(writes.length,1);assert.equal(writes[0].url,'/github/clone');
        const body=writes[0].body;assert.equal(body.server_id,'server');assert.equal(body.type,tech);assert.equal(body.port,3000);assert.equal(body.build_cmd,commands[0]);assert.equal(body.start_cmd,commands[1]);assert.equal(body.repo_url,'https://github.com/example/project.git');assert.equal(body.restart_on_boot,false);
    });
}
test('creation validation and API failures keep the dialog retryable',async()=>{
    const {ui,App,el,writes,toasts}=harness();await ui.createProject();assert.equal(writes.length,0);assert.match(toasts.at(-1)[1],/required/);
    ui.selectTech('go');el('project-name').value='Failed app';App.API.post=async()=>({success:false,error:'Remote unavailable'});
    await ui.createProject();assert.equal(el('create-project-btn-submit').disabled,false);assert.match(toasts.at(-1)[1],/Remote unavailable/);
});
test('lifecycle and deletion use the same endpoints; cancellation issues no write',async()=>{
    const {ui,App,writes}=harness();
    for(const action of ['start','stop','build'])await ui.action('app-go',action);
    assert.deepEqual(writes.map(w=>w.url),['/servers/server/projects/app-go/start','/servers/server/projects/app-go/stop','/servers/server/projects/app-go/build']);
    App.UI.confirm=async()=>false;await ui.action('app-go','delete');assert.equal(writes.length,3);
    App.UI.confirm=async()=>true;await ui.action('app-go','delete');assert.equal(writes.at(-1).method,'DELETE');assert.equal(writes.at(-1).url,'/servers/server/projects/app-go');
});
test('file navigation and project path copy keep server context, including HTTP clipboard fallback',async()=>{
    const {ui,App,copied,reads,context,document}=harness();ui.list=[project];ui.openFiles('app-go');assert.equal(App.state.currentPath,'/srv/api');assert.deepEqual(reads,['files:/srv/api']);
    ui.copyProjectPath('app-go');await Promise.resolve();assert.deepEqual(copied,['/srv/api']);
    context.navigator.clipboard=undefined;let fallbackCalls=0;document.execCommand=()=>{fallbackCalls++;return true;};ui.copyProjectPath('app-go');assert.equal(fallbackCalls,1);
});
test('create dialog keeps all input IDs, technology choices, archive types and handlers',()=>{
    const html=fs.readFileSync(path.join(__dirname,'../templates/dashboard.html'),'utf8');
    for(const id of ['create-project-modal','selected-tech','project-name','project-port','project-build-cmd','project-start-cmd','project-restart-on-boot','project-repo-url','project-file','project-file-label','project-upload-progress','project-upload-bar','project-upload-percent','create-project-btn-submit'])assert.equal((html.match(new RegExp(`id="${id}"`,'g'))||[]).length,1,id);
    assert.match(html,/accept="\.zip,\.tar.gz"/);
    for(const tech of Object.keys(defaults))assert.match(html,new RegExp(`selectTech\\('${tech}'\\)`));
    for(const method of ['createProject','closeCreateModal','openGitHubModal','handleFileSelect'])assert.match(html,new RegExp(`App.Projects.${method}\\(`));
});
const flushOps=async()=>{await Promise.resolve();await Promise.resolve();await Promise.resolve();};
test('management navigation keeps all six sections; opening Overview issues no runtime write',async()=>{
    const {ui,el,writes,reads}=harness({deferTimers:true});ui.list=[project];ui.showProjectOps(project.id);
    const html=el('project-ops-content').innerHTML;
    for(const tab of ['overview','releases','logs','health','automation','integrations'])assert.match(html,new RegExp(`switchOpsTab\\('${tab}'\\)`));
    assert.match(html,/data-ops-tab="overview" aria-pressed="true"/);
    assert.match(html,/project-ops-panel/);assert.match(html,/closeProjectOps/);
    for(const action of ['start','stop','build'])assert.match(html,new RegExp(`action\\('app-go', '${action}', this\\)`));
    assert.deepEqual(reads,[]);assert.deepEqual(writes,[]);
});
test('overview retains process, port mapping, source, dates and exact escaped commands',()=>{
    const {ui}=harness();const html=ui.renderOverviewPanel({...project,process_id:'process-123',source_url:'https://example.test/repo.git',build_command:'echo "<tag>&amp;"',start_command:'./app --bind 0.0.0.0'});
    for(const text of ['8080','9000','process-123','https://example.test/repo.git','/srv/api','Last build','Last deploy','Build type'])assert.ok(html.includes(text),text);
    assert.match(html,/echo &quot;&lt;tag&gt;&amp;amp;&quot;/);assert.match(html,/\.\/app --bind 0\.0\.0\.0/);
    assert.doesNotMatch(html,/private-secret/);
});
for(const [tab,pathTail] of [['releases','/ops/releases'],['logs','/ops/logs?tail=200'],['health','/ops/health'],['automation','/ops/automation'],['integrations','/ops/integrations']])test(`${tab}: switching, caching and refresh retain server-scoped read endpoints`,async()=>{
    const {ui,el,writes,reads}=harness({deferTimers:true});ui.list=[project];ui.showProjectOps(project.id);
    ui.switchOpsTab(tab);await flushOps();assert.deepEqual(reads,[`/servers/server/projects/app-go${pathTail}`]);
    assert.match(el('project-ops-content').innerHTML,new RegExp(`data-ops-tab="${tab}" aria-pressed="true"`));
    ui.switchOpsTab('overview');ui.switchOpsTab(tab);await flushOps();assert.equal(reads.length,1);
    ui.refreshOpsTab();await flushOps();assert.equal(reads.length,2);assert.equal(reads[1],reads[0]);assert.equal(writes.length,0);
});
test('release history displays backend events safely and retains redeploy eligibility',()=>{
    const {ui}=harness({deferTimers:true});const upload={...project,source_type:'upload',source_url:''};
    ui.opsData[`${project.id}:releases`]={git:{is_repository:false,branch:'main',commit:'abcdef',remote:'https://example.test/app.git'},releases:[{title:'<script>bad()</script>',type:'deploy',status:'failed',detail:'error <img src=x>',timestamp:'2026-10-13T00:00:00Z'}]};
    let html=ui.renderReleasesPanel(upload);
    assert.match(html,/redeployProject\('app-go', this\)" class="console-button" disabled/);
    assert.match(html,/main/);assert.match(html,/abcdef/);assert.match(html,/https:\/\/example.test\/app.git/);
    assert.match(html,/&lt;script&gt;/);assert.match(html,/&lt;img src=x&gt;/);assert.doesNotMatch(html,/<script>|<img src=x>/);
    html=ui.renderReleasesPanel(project);assert.doesNotMatch(html,/redeployProject[^>]+disabled/);
    ui.opsData[`${project.id}:releases`].git.is_repository=true;html=ui.renderReleasesPanel(upload);assert.doesNotMatch(html,/redeployProject[^>]+disabled/);
    assert.match(html,/refreshOpsTab/);assert.match(html,/action\('app-go', 'build', this\)/);
});
test('queued deployment polling remains read-only and stops after completion or dialog closure',async()=>{
    const {ui,App,el,reads,writes,timers}=harness({deferTimers:true});ui.list=[project];ui.showProjectOps(project.id);ui.activeOpsTab='releases';
    ui.opsData[`${project.id}:releases`]={releases:[{status:'queued',title:'Deployment queued'}]};
    ui.renderReleasesPanel(project);ui.renderReleasesPanel(project);assert.equal(timers.size,1);
    const timer=timers.get(ui.deploymentPollTimers[project.id]);assert.equal(timer.ms,5000);
    App.API.get=async url=>{reads.push(url);return {success:true,data:{releases:[{status:'completed',title:'Deployment done'}]}};};
    timer.fn();await flushOps();assert.deepEqual(reads,['/servers/server/projects/app-go/ops/releases']);assert.equal(ui.deploymentPollTimers[project.id],undefined);assert.equal(writes.length,0);
    ui.opsData[`${project.id}:releases`]={releases:[{status:'running'}]};ui.renderReleasesPanel(project);
    const closedTimer=timers.get(ui.deploymentPollTimers[project.id]);el('project-ops-modal').classList.add('hidden');closedTimer.fn();await flushOps();assert.equal(reads.length,1);
});
test('logs retain raw output, source, timestamp, refresh and exact clipboard content',async()=>{
    const {ui,el,copied,context,document}=harness({deferTimers:true});ui.list=[project];ui.currentOpsProjectID=project.id;
    const raw='line one\n<script>alert(1)</script> &amp; <value>\nline three';
    ui.opsData[`${project.id}:logs`]={raw,source:'pm2',fetched_at:'2026-10-13T00:00:00Z'};
    const html=ui.renderLogsPanel(project);assert.match(html,/pm2 output/);assert.match(html,/Fetched/);assert.match(html,/refreshOpsTab/);assert.match(html,/copyCurrentLogs/);assert.match(html,/&lt;script&gt;/);assert.doesNotMatch(html,/<script>/);
    ui.copyCurrentLogs();await flushOps();assert.equal(copied[0],raw);
    context.navigator.clipboard=undefined;let copiedByFallback='';document.execCommand=()=>{copiedByFallback=el('clipboard-input').value;return true;};ui.copyCurrentLogs();assert.equal(copiedByFallback,raw);
});
test('failed read sections show escaped errors and can retry without lifecycle writes',async()=>{
    const {ui,App,el,writes}=harness({deferTimers:true});ui.list=[project];ui.showProjectOps(project.id);
    App.API.get=async()=>({success:false,message:'Remote <server> unavailable'});ui.switchOpsTab('logs');await flushOps();
    assert.match(el('project-ops-content').innerHTML,/Remote &lt;server&gt; unavailable/);assert.match(el('project-ops-content').innerHTML,/Retry/);
    App.API.get=async()=>({success:true,data:{raw:'Recovered output',source:'docker'}});ui.refreshOpsTab();await flushOps();assert.match(el('project-ops-content').innerHTML,/Recovered output/);assert.equal(writes.length,0);
});
test('redeploy retains its payload, cache invalidation and button restoration using mocked APIs',async()=>{
    const {ui,App,el,writes}=harness({deferTimers:true});ui.list=[project];ui.currentOpsProjectID=project.id;ui.activeOpsTab='releases';
    for(const tab of ['releases','health','logs'])ui.opsData[`${project.id}:${tab}`]={old:true};
    ui.load=async()=>{};ui.loadOpsTab=async()=>{};
    const button=el('redeploy-button');button.innerHTML='Redeploy';await ui.redeployProject(project.id,button);
    assert.equal(writes[0].url,'/servers/server/projects/app-go/redeploy');assert.equal(JSON.stringify(writes[0].body),'{}');
    for(const tab of ['releases','health','logs'])assert.equal(ui.opsData[`${project.id}:${tab}`],undefined);
    assert.equal(button.disabled,false);assert.equal(button.innerHTML,'Redeploy');
    App.API.post=async()=>{throw new Error('Deployment rejected');};await ui.redeployProject(project.id,button);assert.equal(button.disabled,false);assert.equal(button.innerHTML,'Redeploy');
});
test('management rerenders preserve keyboard focus on section tabs and a stable dialog label',()=>{
    const {ui,el,document}=harness({deferTimers:true});let focused=0;
    document.activeElement={closest:()=>({dataset:{opsTab:'logs'}})};
    el('inner').focus=()=>focused++;
    ui.activeOpsTab='logs';ui.renderOperationsModal(project);
    assert.equal(focused,1);assert.match(el('project-ops-content').innerHTML,/id="project-ops-title"/);
});
