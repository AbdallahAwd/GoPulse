const test=require('node:test');const assert=require('node:assert/strict');const fs=require('node:fs');const path=require('node:path');const vm=require('node:vm');
function harness(){
    const nodes=new Map(),writes=[],toasts=[],copies=[];let mode='read_only',confirm=true;
    function el(id){if(!nodes.has(id)){const classes=new Set();nodes.set(id,{id,value:'',checked:false,disabled:false,textContent:'',innerHTML:'',dataset:{},style:{},open:false,events:{},classList:{add:v=>classes.add(v),remove:v=>classes.delete(v),contains:v=>classes.has(v),toggle:(v,on)=>on?classes.add(v):classes.delete(v)},querySelectorAll(selector){return id==='agent-server-options'?servers.filter(x=>!selector.includes(':checked')||x.checked):id==='agent-permission-options'?permissions.filter(x=>!selector.includes(':checked')||x.checked):[];},querySelector(){return {value:mode};},addEventListener(k,fn){this.events[k]=fn;},showModal(){this.open=true;},close(){this.open=false;this.events.close?.();},focus(){},select(){},remove(){},reportValidity(){return true;},reset(){}});}return nodes.get(id);}
    const servers=[{value:'a',checked:true,disabled:false}],permissions=[{value:'logs:read',checked:true,disabled:false},{value:'docker:write',checked:false,disabled:false},{value:'dashboard:admin',checked:false,disabled:false}];
    const document={getElementById:el,querySelectorAll:()=>[],querySelector:()=>null,createElement:()=>el('fallback'),body:{append(){}},execCommand(){copies.push(el('fallback').value);return true;},hidden:false};
    const App={UI:{confirm:async()=>confirm,toast:(...x)=>toasts.push(x)},Infrastructure:{escape:v=>String(v??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])),api:async(url,options)=>{if(options?.method)writes.push({url,options});return url==='/agents/configuration'?{servers:[{id:'a',name:'Production',host:'192.0.2.1'}],permissions:permissions.map(x=>x.value),can_select_all_servers:true,tool_categories:{docker:15,cron:9}}:options?.method==='POST'?{token:'TEST-ONLY-CREDENTIAL'}:[];}}};
    const context=vm.createContext({App,document,window:{location:{origin:'https://panel.example'},isSecureContext:false},navigator:{},URL,Date,setInterval(){},console});
    vm.runInContext(fs.readFileSync(path.join(__dirname,'../static/js/app/agents.js'),'utf8'),context);
    return {ui:App.Agents,App,el,permissions,servers,writes,copies,toasts,mode:value=>mode=value,confirm:value=>confirm=value};
}
test('client configurations use correct transports and environment references without embedding credentials',()=>{
    const {ui,el}=harness();ui.secret='PRIVATE';ui.clientConfig('claude');let config=JSON.parse(el('agent-client-config').textContent);assert.equal(config.mcpServers.gopulse.type,'http');assert.equal(config.mcpServers.gopulse.url,'https://panel.example/api/mcp');assert.equal(config.mcpServers.gopulse.headers.Authorization,'Bearer ${GOPULSE_AGENT_TOKEN}');assert.equal(el('agent-client-config').textContent.includes('PRIVATE'),false);
    ui.clientConfig('codex');assert.match(el('agent-client-config').textContent,/bearer_token_env_var = "GOPULSE_AGENT_TOKEN"/);assert.match(el('agent-client-config').textContent,/\[mcp_servers.gopulse\]/);assert.equal(el('agent-client-config').textContent.includes('PRIVATE'),false);
});
test('agent configurations use the applied master domain when it is available',()=>{
    const {ui,el}=harness();ui.configuration={public_url:'https://dashboard.example.com'};ui.clientConfig('codex');assert.match(el('agent-client-config').textContent,/url = "https:\/\/dashboard.example.com\/api\/mcp"/);ui.configuration.public_url='';assert.equal(ui.endpoint(),'https://panel.example/api/mcp');
});
test('read-only mode disables write grants, and global administration requires all-server selection',()=>{
    const {ui,el,permissions,mode}=harness();permissions[1].checked=true;ui.syncMode();assert.equal(permissions[1].disabled,true);assert.equal(permissions[1].checked,false);assert.equal(permissions[0].disabled,false);
    mode('full_control');ui.syncMode();assert.equal(permissions[1].disabled,false);assert.equal(permissions[2].disabled,true);el('agent-all-servers').checked=true;ui.syncMode();assert.equal(permissions[2].disabled,false);assert.match(el('agent-mode-description').textContent,/without dashboard approval/);
});
test('creating a credential sends selected scopes and servers exactly once',async()=>{
    const {ui,el,writes,mode}=harness();mode('supervised');el('agent-name').value='Codex production';el('agent-expiry').value='24';ui.configuration={servers:[]};await ui.create();assert.equal(writes.length,1);assert.equal(writes[0].url,'/agents');const payload=JSON.parse(writes[0].options.body);assert.deepEqual(payload,{name:'Codex production',mode:'supervised',expiry_hours:24,all_servers:false,server_ids:['a'],permissions:['logs:read']});assert.equal(ui.secret,'TEST-ONLY-CREDENTIAL');assert.equal(el('agent-connect-form').classList.contains('hidden'),true);
});
test('missing server selection blocks creation and failed requests release the busy state',async()=>{
    const {ui,el,servers,writes,App}=harness();servers[0].checked=false;await ui.create();assert.equal(writes.length,0);assert.match(el('agent-form-error').textContent,/Choose/);servers[0].checked=true;App.Infrastructure.api=async()=>{throw new Error('Denied');};await ui.create();assert.equal(ui.busy,false);assert.equal(el('agent-create-credential').disabled,false);assert.equal(el('agent-form-error').textContent,'Denied');
});
test('cancelled revocation does not write, and confirmed revocation uses the credential ID',async()=>{
    const {ui,el,writes,confirm}=harness();ui.credentials=[{id:'credential',name:'Claude'}];confirm(false);await ui.revoke('credential',el('revoke'));assert.equal(writes.length,0);confirm(true);await ui.revoke('credential',el('revoke'));assert.equal(writes[0].url,'/agents/credential');assert.equal(writes[0].options.method,'DELETE');
});
test('HTTP clipboard fallback copies the exact configuration or credential',async()=>{
    const {ui,copies}=harness();await ui.copy('<private> & literal');assert.deepEqual(copies,['<private> & literal']);
});
test('connection cards escape names and show expiration/revocation without secrets',()=>{
    const {ui,el}=harness();ui.credentials=[{id:'id',name:'<script>bad</script>',mode:'read_only',server_ids:['a'],permissions:['logs:read'],expires_at:'2099-01-01',revoked_at:'0001-01-01',last_used_at:'0001-01-01'}];ui.render();assert.doesNotMatch(el('agent-connections-list').innerHTML,/<script>/);assert.match(el('agent-connections-list').innerHTML,/&lt;script&gt;/);assert.match(el('agent-connections-list').innerHTML,/Active/);assert.equal(ui.active({...ui.credentials[0],revoked_at:'2026-01-01'}),false);
});
test('global navigation and connection dialog are reachable without server context',()=>{
    const template=fs.readFileSync(path.join(__dirname,'../templates/agents.html'),'utf8');const dashboard=fs.readFileSync(path.join(__dirname,'../../internal/handlers/dashboard.go'),'utf8');const main=fs.readFileSync(path.join(__dirname,'../static/js/app/main.js'),'utf8');
    for(const id of ['agents-page','agent-connect-dialog','agent-connect-form','agent-issued-token','agent-client-config','agent-audit-list'])assert.match(template,new RegExp(`id="${id}"`));assert.match(dashboard,/"agents", "Agents", "\/agents"/);assert.match(main,/App.Agents.init\(\)/);
});
