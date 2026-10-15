const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const flush = async () => { await Promise.resolve(); await Promise.resolve(); await Promise.resolve(); };
function harness(type = 'postgres') {
    const elements = new Map(), reads = [], writes = [], errors = [], handlers = new Map(), fields = {};
    function element(id) {
        if (!elements.has(id)) {
            const classes = new Set(['hidden']);
            const attributes = new Map();
            elements.set(id, {id, value:'', textContent:'', innerText:'', innerHTML:'', dataset:{}, style:{}, children:[], disabled:false,
                classList:{add:(...values)=>values.forEach(v=>classes.add(v)),remove:(...values)=>values.forEach(v=>classes.delete(v)),contains:v=>classes.has(v)},
                setAttribute:(key,value)=>attributes.set(key,value),getAttribute:key=>attributes.get(key),
                appendChild(node){this.children.push(node);},querySelector:()=>element('nested'),focus(){},remove(){},contains(){return false;}});
        }
        return elements.get(id);
    }
    const database = {id:`db-${type}`,type,name:'Main',username:type === 'postgres' ? 'postgres' : 'root',database_name:'main',host_port:5432,status:'Running',password:''};
    const buttons = ['overview','explorer','console','backups','users','settings'].map(key=>{const b=element(`button-${key}`);b.dataset.dbTab=key;return b;});
    const panels = ['overview','explorer','console','backups','users','settings'].map(key=>element(`db-panel-${key}`));
    const baasButtons = ['overview','guides','env','logs','backups','settings'].map(key=>{const b=element(`baas-button-${key}`);b.dataset.baasTab=key;return b;});
    const baasPanels = ['overview','guides','env','logs','backups','settings'].map(key=>element(`baas-panel-${key}`));
    const App = {config:{SERVER_ID:'server',SERVER_HOST:'db.example.test'},Navigation:{current:'not-databases'},UI:{el:element,confirm:async()=>true,toast:(...args)=>errors.push(args)},Qdrant:{manage:id=>{element('qdrant-managed').value=id;}},
        API:{get:async url=>{reads.push(url);if(url.endsWith('/schemas'))return {success:true,data:['main','other']};if(url.endsWith('/tables'))return {success:true,data:['items']};if(url.includes('/rows?'))return {success:true,data:{columns:['id','name'],rows:[[1,'<script>remote()</script>']],total:101,page:Number(new URL('http://test'+url).searchParams.get('page'))}};if(url.endsWith('/auto-backup'))return {success:true,data:{auto_backup:false,retention_days:7}};if(url.endsWith('/backups')||url.endsWith('/users')||url.endsWith('/databases'))return {success:true,data:[]};return {success:true,data:{...database}};},
             post:async(url,body)=>{writes.push({url,body});return {success:true,data:{type:'table',columns:['value'],rows:[[1]]}};}},};
    App.API.put=async(url,body)=>{writes.push({method:'PUT',url,body});return {success:true};};
    App.API.delete=async url=>{writes.push({method:'DELETE',url});return {success:true};};
    const document = {getElementById:element,createElement:()=>({value:'',textContent:''}),activeElement:null,
        querySelector:selector=>selector.includes('runQuery')?element('db-query-run'):element('generic-button'),
        querySelectorAll:selector=>fields[selector]||({'.db-tab-btn':buttons,'.db-panel-content':panels,'.baas-tab-btn':baasButtons,'.baas-panel-content':baasPanels,'.db-tools-menu':[element('db-administration-menu'),element('baas-administration-menu')]}[selector]||[]),
        addEventListener(name,callback){if(!handlers.has(name))handlers.set(name,[]);handlers.get(name).push(callback);}};
    const context = vm.createContext({App,document,navigator:{},console,FormData,setInterval:()=>1,clearInterval(){},setTimeout:()=>{},encodeURIComponent});
    for (const file of ['database_connection','databases']) vm.runInContext(fs.readFileSync(path.join(__dirname,'../static/js/app',file+'.js'),'utf8'),context);
    return {App,ui:App.Databases,element,reads,writes,errors,buttons,document,handlers,database,fields};
}
for (const type of ['mysql','postgres','sqlite','mongodb','redis']) {
    test(`${type}: query text and selected context reach the unchanged query endpoint`, async () => {
        const {ui,element,writes,errors} = harness(type);
        await ui.showManageView(`db-${type}`);
        assert.equal(errors.length, 0);
        ui.switchManageTab('console');await flush();
        const query = type === 'redis' ? 'SET test:key "hello world"' : type === 'mongodb' ? 'db.items.updateOne({id:1}, {$set:{value:"test"}})' : 'UPDATE items SET value = 7 WHERE id = 1;';
        element('db-console-query').value = query;
        element('db-console-schema-select').value = 'chosen context';
        await ui.runQuery();
        assert.equal(writes.length, 1);
        assert.equal(writes[0].url, `/servers/server/databases/db-${type}/query`);
        assert.equal(writes[0].body.query, query);
        assert.equal(writes[0].body.schema, 'chosen context');
        assert.match(element('db-console-results-container').innerHTML, /<table/);
        assert.equal(element('db-query-result-summary').textContent, 'Completed · 1 row');
        assert.equal(element('db-query-run').disabled, false);
    });
    test(`${type}: explorer expansion, pagination, and row action bindings remain available`, async () => {
        const {ui,element,reads,errors} = harness(type);
        await ui.showManageView(`db-${type}`);await ui.loadSchemas();await ui.toggleSchema('main');
        assert.equal(errors.length, 0);
        assert.match(element('db-explorer-tree').innerHTML, /App\.Databases\.selectTable\('main', 'items'\)/);
        if(type==='redis')assert.equal(element('generic-button').classList.contains('hidden'),true);
        else assert.match(element('db-explorer-tree').innerHTML, /openCreateTableModal/);
        ui.selectTable('main','items');await flush();
        assert.match(reads.at(-1), /\/schemas\/main\/tables\/items\/rows\?page=1&limit=50$/);
        assert.match(element('db-explorer-data').innerHTML, /openEditRowModal/);
        assert.match(element('db-explorer-data').innerHTML, /deleteRow\('main', 'items', 'id', '1'\)/);
        assert.match(element('db-explorer-data').innerHTML, /&lt;script&gt;/);
        ui.explorerNextPage();await flush();assert.match(reads.at(-1), /page=2&limit=50$/);
        ui.explorerPrevPage();await flush();assert.match(reads.at(-1), /page=1&limit=50$/);
        assert.equal(element('db-explorer-prev-btn').disabled,true);
    });
}
test('secondary navigation retains backups, users and settings panels and their requests',async()=>{
    const {ui,element,reads,buttons} = harness();
    await ui.showManageView('db-postgres');
    for (const [tab,label] of [['backups','Backups'],['users','Users'],['settings','Settings']]) {
        element('db-administration-menu').open=true;ui.switchManageTab(tab);await flush();
        assert.equal(element(`db-panel-${tab}`).classList.contains('hidden'),false);
        assert.equal(element('db-administration-menu').open,false);
        assert.equal(element('db-administration-label').textContent,label);
        assert.equal(buttons.find(b=>b.dataset.dbTab===tab).getAttribute('aria-pressed'),'true');
    }
    assert.ok(reads.includes('/servers/server/databases/db-postgres/backups'));
    assert.ok(reads.includes('/servers/server/databases/db-postgres/users'));
    ui.switchManageTab('overview');assert.equal(element('db-administration-label').textContent,'Manage');
    assert.equal(element('db-panel-overview').classList.contains('hidden'),false);
});
for (const type of ['pocketbase','appwrite']) test(`${type}: dedicated views, credentials and backup navigation stay separate from SQL`,async()=>{
    const {ui,element,reads,errors} = harness(type);
    await ui.showManageView(`db-${type}`);
    assert.equal(errors.length,0);
    assert.equal(element('baas-manage-view').classList.contains('hidden'),false);
    assert.equal(element('db-manage-view').classList.contains('hidden'),true);
    ui.switchBaaSTab('env');assert.equal(element('baas-panel-env').classList.contains('hidden'),false);
    assert.equal(element('baas-administration-label').textContent,'Keys & environment');
    ui.switchBaaSTab('backups');await flush();assert.equal(element('baas-panel-backups').classList.contains('hidden'),false);
    assert.ok(reads.includes(`/servers/server/databases/db-${type}/backups`));
    element('baas-log-service-select').value = `gopulse-db-${type}`;
    ui.switchBaaSTab('logs');await flush();
    assert.equal(element('baas-panel-logs').classList.contains('hidden'),false);
    assert.ok(reads.includes(`/containers/gopulse-db-${type}/logs`));
    assert.equal(element('baas-settings-pass').innerText,'••••••••');
});
test('Qdrant still opens its collection and snapshot workflow instead of the SQL workbench',async()=>{
    const {ui,element,reads} = harness('qdrant');
    await ui.showManageView('db-qdrant');
    assert.equal(element('qdrant-managed').value,'db-qdrant');
    assert.deepEqual(reads,['/servers/server/databases/db-qdrant']);
});
test('keyboard execution and raw/error results remain engine-independent',async()=>{
    const {ui,App,element,document,handlers,writes}=harness('redis');
    await ui.showManageView('db-redis');
    App.API.post=async(url,body)=>{writes.push({url,body});return {success:true,data:{type:'raw',data:'PONG'}};};
    document.activeElement=element('db-console-query');element('db-console-query').value='PING';
    let prevented=false;
    for (const handler of handlers.get('keydown')) handler({ctrlKey:true,key:'Enter',preventDefault(){prevented=true;}});
    await flush();assert.equal(prevented,true);assert.equal(writes.length,1);assert.match(element('db-console-results-container').innerHTML,/PONG/);
    App.API.post=async()=>{throw new Error('Remote command rejected');};await ui.runQuery();
    assert.match(element('db-console-results-container').innerHTML,/Remote command rejected/);
    assert.equal(element('db-query-result-summary').textContent,'Failed');
    assert.equal(element('db-query-run').disabled,false);
});
for (const type of ['mysql','postgres','sqlite','mongodb','redis']) test(`${type}: row insertion, editing and deletion retain their payloads and primary key`,async()=>{
    const {ui,element,writes,fields}=harness(type);
    await ui.showManageView(`db-${type}`);ui.selectTable('main','items');await flush();
    const row={columns:['id','name'],values:[1,'Original'],pkCol:'id',pkVal:1};
    ui.openEditRowModal(encodeURIComponent(JSON.stringify(row)));
    if(['mongodb','redis'].includes(type))element('db-edit-row-json').value=JSON.stringify({name:'Changed'});
    else fields['.db-edit-row-field']=[{dataset:{colName:'id'},value:'1'},{dataset:{colName:'name'},value:'Changed'}];
    await ui.submitEditRow();
    assert.equal(writes[0].method,'PUT');
    assert.equal(writes[0].url,`/servers/server/databases/db-${type}/schemas/main/tables/items/rows?pk_col=id&pk_val=1`);
    assert.equal(writes[0].body.data.name,'Changed');
    assert.equal(writes[0].body.data.id,undefined);
    if(['mongodb','redis'].includes(type))element('db-insert-row-json').value=JSON.stringify({name:'New'});
    else fields['.db-insert-row-field']=[{dataset:{colName:'name'},value:'New'}];
    await ui.submitInsertRow();
    assert.equal(writes[1].url,`/servers/server/databases/db-${type}/schemas/main/tables/items/rows`);
    assert.equal(writes[1].body.data.name,'New');
    await ui.deleteRow('main','items','id',1);
    assert.equal(writes[2].method,'DELETE');
    assert.equal(writes[2].url,writes[0].url);
});
test('switching sections preserves the query draft, context, and completed results',async()=>{
    const {ui,element}=harness();
    await ui.showManageView('db-postgres');await ui.loadSchemas();
    element('db-console-query').value='SELECT 1;';element('db-console-schema-select').value='other';await ui.runQuery();
    const results=element('db-console-results-container').innerHTML;
    ui.switchManageTab('settings');ui.switchManageTab('console');
    assert.equal(element('db-console-query').value,'SELECT 1;');
    assert.equal(element('db-console-schema-select').value,'other');
    assert.equal(element('db-console-results-container').innerHTML,results);
});
test('lifecycle actions remain available and use the existing server-scoped endpoints',async()=>{
    const {ui,element,writes}=harness();
    await ui.showManageView('db-postgres');
    assert.match(element('db-manage-actions').innerHTML,/Stop instance/);
    assert.match(element('db-manage-actions').innerHTML,/Restart instance/);
    for(const action of ['start','stop','restart']){await ui.togglePower('db-postgres',action);await flush();}
    assert.deepEqual(writes.map(value=>value.url),['/servers/server/databases/db-postgres/start','/servers/server/databases/db-postgres/stop','/servers/server/databases/db-postgres/restart']);
});
test('dismissed Stop and Restart confirmations cannot change database state',async()=>{
    const {App,ui,writes,element}=harness();await ui.showManageView('db-postgres');
    App.UI.confirm=async()=>false;
    await ui.confirmPower('db-postgres','stop');await ui.confirmPower('db-postgres','restart');
    assert.equal(writes.length,0);
    assert.match(element('db-manage-actions').innerHTML,/App.Databases.confirmPower/);
    assert.doesNotMatch(element('db-manage-actions').innerHTML,/App.Databases.togglePower/);
    App.UI.confirm=async()=>true;await ui.confirmPower('db-postgres','restart');
    assert.equal(writes.length,1);assert.equal(writes[0].url,'/servers/server/databases/db-postgres/restart');
});

// Administration writes are exercised only against in-memory API mocks.
for (const type of ['mysql','postgres','sqlite','mongodb','redis','pocketbase','appwrite']) {
    test(`${type}: backup schedule, files, creation and confirmed restore keep their API contracts`,async()=>{
        const {ui,App,element,writes}=harness(type);
        await ui.showManageView(`db-${type}`);
        const baas=['pocketbase','appwrite'].includes(type),prefix=baas?'baas':'db';
        const metadataRead=App.API.get;
        App.API.get=async url=>{
            if(url.endsWith('/auto-backup'))return {success:true,data:{auto_backup:true,retention_days:14}};
            if(url.endsWith('/backups'))return {success:true,data:[{filename:'archive.sql.gz',size:1048576,created_at:'2026-10-13T00:00:00Z'}]};
            return metadataRead(url);
        };
        if(baas)ui.switchBaaSTab('backups');else ui.switchManageTab('backups');
        await flush();
        assert.equal(element(`${prefix}-auto-backup-toggle`).checked,true);
        assert.equal(element(`${prefix}-auto-backup-retention`).value,14);
        assert.equal(element(`${prefix}-auto-backup-settings`).classList.contains('hidden'),false);
        const row=element(`${prefix}-backups-table-body`).children.at(-1);
        assert.match(row.innerHTML,/archive.sql.gz/);
        assert.match(row.innerHTML,new RegExp(`restoreBackup${baas?'BaaS':''}\\('archive.sql.gz', this\\)`));
        assert.match(row.innerHTML,/\/backups\/download\?filename=archive.sql.gz/);
        element(`${prefix}-auto-backup-toggle`).checked=false;
        element(`${prefix}-auto-backup-retention`).value='0';
        await ui[baas?'saveAutoBackupConfigBaaS':'saveAutoBackupConfig']();
        assert.equal(writes[0].method,'PUT');
        assert.equal(writes[0].url,`/servers/server/databases/db-${type}/auto-backup`);
        assert.equal(writes[0].body.auto_backup,false);
        assert.equal(writes[0].body.retention_days,0);
        await ui[baas?'createBackupBaaS':'createBackup']();
        assert.equal(writes[1].url,`/servers/server/databases/db-${type}/backup`);
        const button=element('restore-button');button.innerHTML='Restore';
        App.UI.confirm=async()=>false;
        await ui[baas?'restoreBackupBaaS':'restoreBackup']('archive.sql.gz',button);
        assert.equal(writes.length,2);
        App.UI.confirm=async()=>true;
        await ui[baas?'restoreBackupBaaS':'restoreBackup']('archive.sql.gz',button);
        assert.equal(writes[2].url,`/servers/server/databases/db-${type}/backups/restore`);
        assert.equal(writes[2].body.filename,'archive.sql.gz');
        assert.equal(button.disabled,false);assert.equal(button.innerHTML,'Restore');
        assert.ok(writes.every(write=>! /\/(?:start|stop|restart)$/.test(write.url)));
    });
}
for (const type of ['mysql','postgres','mongodb']) test(`${type}: users retain permission values, selected database and revoke confirmation`,async()=>{
    const {ui,App,element,writes}=harness(type);await ui.showManageView(`db-${type}`);
    const metadataRead=App.API.get;
    App.API.get=async url=>url.endsWith('/users')?{success:true,data:[{username:'app_user',database:'main',scope:'readWrite'}]}:metadataRead(url);
    ui.switchManageTab('users');await flush();
    const row=element('db-users-table-body').children.at(-1);
    assert.match(row.innerHTML,/Read &amp; write/);
    assert.match(row.innerHTML,/copyUserURI\(this\)/);
    assert.match(row.innerHTML,/deleteUser\('app_user'\)/);
    for (const scope of ['read','readWrite','admin']) {
        element('db-user-username').value='new_user';element('db-user-password').value='test-password';
        element('db-user-scope').value=scope;element('db-user-database').value='selected_database';
        await ui.submitAddUser();
        const write=writes.at(-1);
        assert.equal(write.url,`/servers/server/databases/db-${type}/users`);
        assert.equal(write.body.username,'new_user');assert.equal(write.body.password,'test-password');
        assert.equal(write.body.scope,scope);assert.equal(write.body.database,'selected_database');
    }
    App.UI.confirm=async()=>false;await ui.deleteUser('app_user');assert.equal(writes.length,3);
    App.UI.confirm=async()=>true;await ui.deleteUser('app_user');
    assert.equal(writes.at(-1).method,'DELETE');
    assert.equal(writes.at(-1).url,`/servers/server/databases/db-${type}/users/app_user`);
});
for (const type of ['postgres','pocketbase','appwrite']) test(`${type}: settings keep protected credential reads and deletion volume choice`,async()=>{
    const {ui,App,element,reads,writes}=harness(type);await ui.showManageView(`db-${type}`);
    const baas=type!=='postgres',prefix=baas?'baas':'db';
    assert.equal(element(`${prefix}-settings-pass`).innerText,'••••••••');
    const metadataRead=App.API.get;
    App.API.get=async url=>{
        if(url.endsWith('/credentials')){reads.push(url);return {success:true,data:{password:'protected-secret'}};}
        return metadataRead(url);
    };
    await ui[baas?'toggleBaaSPasswordVisibility':'revealCredentials']();
    assert.ok(reads.includes(`/servers/server/databases/db-${type}/credentials`));
    assert.equal(baas?element('baas-settings-pass').innerText:element('db-settings-pass').textContent,'protected-secret');
    for(const purge of [false,true]){
        await ui.showManageView(`db-${type}`);
        element(`${prefix}-delete-volume-check`).checked=purge;
        App.UI.confirm=async()=>false;
        const before=writes.length;await ui[baas?'deleteBaaSInstance':'deleteDatabase']();assert.equal(writes.length,before);
        App.UI.confirm=async()=>true;await ui[baas?'deleteBaaSInstance':'deleteDatabase']();
        assert.equal(writes.at(-1).url,`/servers/server/databases/db-${type}?delete_volume=${purge}&confirm=db-${type}`);
    }
    assert.ok(writes.every(write=>! /\/(?:start|stop|restart)$/.test(write.url)));
});
for (const type of ['postgres','pocketbase','appwrite']) test(`${type}: backup import retains multipart upload and restores button state`,async()=>{
    const {ui,App,element,writes}=harness(type);await ui.showManageView(`db-${type}`);
    const baas=type!=='postgres';
    App.API.upload=async(url,body,progress)=>{writes.push({url,body});if(progress)progress(50);return {success:true};};
    const input={files:[new Blob(['test dump'],{type:'application/octet-stream'})],value:'archive.gz'};
    const button=element('generic-button');button.innerHTML='Import';
    await ui[baas?'importBackupBaaS':'importBackup']({target:input});
    assert.equal(writes.length,1);assert.equal(writes[0].url,`/servers/server/databases/db-${type}/backups/import`);
    assert.equal(await writes[0].body.get('file').text(),'test dump');
    assert.equal(button.disabled,false);assert.equal(button.innerHTML,'Import');
});
