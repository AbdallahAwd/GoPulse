/* Credential management is exclusively a browser workflow, never an MCP tool. */
App.Agents = {
    credentials:[],configuration:null,busy:false,panel:'connections',client:'claude',secret:'',polling:false,
    scopes:{'files:read':['Read files','File contents can include credentials.'],'logs:read':['Read logs','Application, container, cron, and firewall logs.'],'database:read':['Read database rows','Potentially sensitive records and documents.'],'secrets:read':['Reveal secrets','Environment values; database credentials also require Database management.'],'server:write':['Server connections','Update or unregister permitted servers.'],'projects:write':['Applications','Create, build, deploy, configure, and remove apps.'],'database:write':['Database management','Provision, run queries, restore data, and manage access.'],'docker:write':['Docker','Control containers, images, volumes, and networks.'],'domains:write':['Domains & routing','Configure TLS and load balancers.'],'cron:write':['Cron jobs','Create schedules and execute shell jobs.'],'files:write':['Write files','Write, rename, chmod, archive, and delete files.'],'terminal:write':['Shell commands','Execute commands with the server SSH account’s privileges.'],'services:write':['Services','Restart supported system services.'],'processes:write':['Processes','Terminate running processes.'],'system:write':['Server administration','Install software, manage OS users, and control power.'],'firewall:write':['Firewall','Change network rules and default access policies.'],'user:manage':['Dashboard users','Create and revoke dashboard accounts; global access required.'],'dashboard:admin':['Global administration','Register servers and control the GoPulse host; global access required.']},
    el(id){return document.getElementById(id);},
    escape(value){return App.Infrastructure.escape(value);},
    endpoint(){const url=new URL('/api/mcp',this.configuration?.public_url||window.location.origin);if(url.hostname==='0.0.0.0')url.hostname='127.0.0.1';return url.href;},
    mode(){return this.el('agent-connect-form').querySelector('input[name="agent-mode"]:checked')?.value||'read_only';},
    modeLabel(mode){return ({read_only:'Read-only',supervised:'Supervised',full_control:'Full control'})[mode]||'Unknown';},
    active(credential){return new Date(credential.expires_at)>new Date()&&(!credential.revoked_at||credential.revoked_at.startsWith('0001-'));},
    async api(path,options){return App.Infrastructure.api('/agents'+path,options);},
    init(){
        if(!this.el('agents-page'))return;
        this.el('agent-endpoint').textContent=this.endpoint();
        this.el('agent-copy-endpoint').onclick=()=>this.copy(this.endpoint());
        this.el('agent-connect').onclick=()=>this.open();
        this.el('agents-refresh').onclick=()=>this.load();
        this.el('agents-audit-refresh').onclick=()=>this.audit();
        document.querySelectorAll('[data-agent-panel]').forEach(button=>button.onclick=()=>this.switchPanel(button.dataset.agentPanel));
        this.el('agent-connect-form').onsubmit=event=>{event.preventDefault();this.create();};
        this.el('agent-connect-form').addEventListener('change',()=>this.syncMode());
        this.el('agent-connect-dialog').addEventListener('close',()=>{this.secret='';this.el('agent-issued-token').value='';});
        this.el('agent-connect-dialog').addEventListener('cancel',event=>{if(this.busy)event.preventDefault();});
        this.el('agent-copy-token').onclick=()=>this.copy(this.secret);
        this.el('agent-copy-config').onclick=()=>this.copy(this.el('agent-client-config').textContent);
        document.querySelectorAll('[data-agent-client]').forEach(button=>button.onclick=()=>this.clientConfig(button.dataset.agentClient));
        this.load();
        setInterval(()=>{if(!document.hidden&&!this.polling){this.polling=true;Promise.all([this.load(),this.panel==='activity'?this.audit():Promise.resolve()]).finally(()=>this.polling=false);}},15000);
    },
    async load(){
        try{const [credentials,configuration]=await Promise.all([this.api(''),this.api('/configuration')]);this.credentials=credentials||[];this.configuration=configuration;this.el('agent-endpoint').textContent=this.endpoint();this.el('agents-error').textContent='';this.render();this.renderCapabilities();}
        catch(error){this.el('agents-error').textContent=error.message;}
    },
    render(){
        const root=this.el('agent-connections-list');
        if(!this.credentials.length){root.innerHTML='<div class="agent-empty"><i class="fas fa-robot" aria-hidden="true"></i><h3>No agents connected yet</h3><p>Create a scoped credential, then add GoPulse to your MCP client.</p></div>';return;}
        root.innerHTML=this.credentials.map(item=>{const active=this.active(item),revoked=item.revoked_at&&!item.revoked_at.startsWith('0001-');const servers=item.all_servers?'All current and future servers':`${item.server_ids.length} selected server${item.server_ids.length===1?'':'s'}`;return `<article class="agent-connection-card"><div class="agent-card-heading"><div><span class="agent-card-icon"><i class="fas fa-robot" aria-hidden="true"></i></span><h3>${this.escape(item.name)}</h3></div><span class="agent-credential-status" data-active="${active}">${active?'Active':revoked?'Revoked':'Expired'}</span></div><div class="agent-card-details"><span>${this.escape(this.modeLabel(item.mode))}</span><span>${servers}</span><span>${item.permissions.length} permission scopes</span></div><dl><div><dt>Expires</dt><dd>${this.escape(new Date(item.expires_at).toLocaleString())}</dd></div><div><dt>Last used</dt><dd>${item.last_used_at&&!item.last_used_at.startsWith('0001-')?this.escape(new Date(item.last_used_at).toLocaleString()):'Never'}</dd></div></dl><details><summary>Granted permissions</summary><div class="agent-scope-tags">${item.permissions.length?item.permissions.map(p=>`<span>${this.escape(this.scopes[p]?.[0]||p)}</span>`).join(''):'Inventory reads only'}</div></details>${active?`<button class="console-button secondary agent-revoke" data-agent-id="${this.escape(item.id)}">Revoke access</button>`:''}</article>`;}).join('');
        root.querySelectorAll('[data-agent-id]').forEach(button=>button.onclick=()=>this.revoke(button.dataset.agentId,button));
    },
    renderCapabilities(){const categories=this.configuration?.tool_categories||{};this.el('agent-capabilities').innerHTML=Object.entries(categories).sort(([a],[b])=>a.localeCompare(b)).map(([name,count])=>`<article><h3>${this.escape(name.replace(/_/g,' '))}</h3><span>${Number(count)} ${Number(count)===1?'tool':'tools'}</span></article>`).join('');},
    async switchPanel(panel){if(!['connections','activity','capabilities'].includes(panel))return;this.panel=panel;document.querySelectorAll('[data-agent-panel]').forEach(button=>button.setAttribute('aria-pressed',String(button.dataset.agentPanel===panel)));for(const key of ['connections','activity','capabilities'])this.el('agents-'+key).classList.toggle('hidden',panel!==key);if(panel==='activity')await this.audit();},
    async audit(){try{const items=await this.api('/audit');this.el('agent-audit-list').innerHTML=items.length?items.map(item=>{const id=item.actor.split('|agent:')[1];const credential=this.credentials.find(x=>x.id===id);return `<tr><td>${this.escape(new Date(item.created_at).toLocaleString())}</td><td>${this.escape(credential?.name||item.actor.split('|agent:')[0])}</td><td><code>${this.escape(item.tool)}</code></td><td>${this.escape(this.configuration?.servers.find(x=>x.id===item.server_id)?.name||item.server_id||'Dashboard')}</td><td>${this.escape(item.outcome)}</td><td>${item.operation_id?`<button class="agent-inspect-operation" data-operation-id="${this.escape(item.operation_id)}">Inspect</button>`:'—'}</td></tr>`;}).join(''):'<tr><td colspan="6" class="agent-empty">No agent activity yet.</td></tr>';this.el('agent-audit-list').querySelectorAll('[data-operation-id]').forEach(button=>button.onclick=()=>{const root=App.Infrastructure.dialog('Agent operation','<p>Loading…</p>');App.Infrastructure.track(button.dataset.operationId,root);});}catch(error){this.el('agents-error').textContent=error.message;}},
    open(){
        if(!this.configuration||this.busy)return;
        this.el('agent-connect-form').reset();this.el('agent-form-error').textContent='';this.el('agent-connect-form').classList.remove('hidden');this.el('agent-connection-ready').classList.add('hidden');
        this.el('agent-all-servers-label').classList.toggle('hidden',!this.configuration.can_select_all_servers);
        this.el('agent-server-options').innerHTML=this.configuration.servers.length?this.configuration.servers.map(server=>`<label><input type="checkbox" value="${this.escape(server.id)}" data-agent-server><span><strong>${this.escape(server.name)}</strong><code>${this.escape(server.host)}</code></span></label>`).join(''):'<p class="agent-field-help">Connect a server before creating a scoped credential.</p>';
        this.el('agent-permission-options').innerHTML=this.configuration.permissions.map(permission=>{const [label,description]=this.scopes[permission]||[permission,''];return `<label><input type="checkbox" value="${this.escape(permission)}" data-agent-permission><span><strong>${this.escape(label)}</strong><small>${this.escape(description)}</small></span></label>`;}).join('');
        this.syncMode();this.el('agent-dialog-title').textContent='Connect an agent';this.el('agent-connect-dialog').showModal();this.el('agent-name').focus();
    },
    syncMode(){
        const mode=this.mode(),all=this.el('agent-all-servers').checked;
        const descriptions={read_only:'This agent can inspect authorized resources. No mutation tools are exposed.',supervised:'Every change requires your approval in Activity. Review the exact target and request before approving.',full_control:'This agent can execute permitted changes without dashboard approval, including deletion, deployment, and shell commands when those scopes are granted.'};
        this.el('agent-mode-description').textContent=descriptions[mode];this.el('agent-mode-description').dataset.mode=mode;
        this.el('agent-server-options').querySelectorAll('[data-agent-server]').forEach(input=>input.disabled=all);
        this.el('agent-permission-options').querySelectorAll('[data-agent-permission]').forEach(input=>{const read=input.value.endsWith(':read'),global=['user:manage','dashboard:admin'].includes(input.value);input.disabled=(!read&&mode==='read_only')||(global&&!all);if(input.disabled)input.checked=false;});
    },
    payload(){return {name:this.el('agent-name').value.trim(),mode:this.mode(),expiry_hours:Number(this.el('agent-expiry').value),all_servers:this.el('agent-all-servers').checked,server_ids:this.el('agent-all-servers').checked?[]:Array.from(this.el('agent-server-options').querySelectorAll('[data-agent-server]:checked')).map(input=>input.value),permissions:Array.from(this.el('agent-permission-options').querySelectorAll('[data-agent-permission]:checked')).filter(input=>!input.disabled).map(input=>input.value)};},
    async create(){
        if(this.busy||!this.el('agent-connect-form').reportValidity())return;
        const payload=this.payload();
        if(!payload.all_servers&&!payload.server_ids.length){this.el('agent-form-error').textContent='Choose at least one server.';return;}
        this.busy=true;this.el('agent-create-credential').disabled=true;
        try{const result=await this.api('',{method:'POST',body:JSON.stringify(payload)});this.secret=result.token;this.el('agent-issued-token').value=result.token;this.el('agent-connect-form').classList.add('hidden');this.el('agent-connection-ready').classList.remove('hidden');this.el('agent-dialog-title').textContent='Connect '+payload.name;this.clientConfig('claude');await this.load();}
        catch(error){this.el('agent-form-error').textContent=error.message;}
        finally{this.busy=false;this.el('agent-create-credential').disabled=false;}
    },
    clientConfig(client){
        this.client=client==='codex'?'codex':'claude';
        const endpoint=this.endpoint();
        document.querySelectorAll('[data-agent-client]').forEach(button=>button.setAttribute('aria-pressed',String(button.dataset.agentClient===this.client)));
        this.el('agent-client-help').textContent=this.client==='claude'?'Add this entry to your project .mcp.json, then check /mcp in Claude Code.':'Add this entry to ~/.codex/config.toml, then check codex mcp list or /mcp in Codex.';
        this.el('agent-client-config').textContent=this.client==='claude'?JSON.stringify({mcpServers:{gopulse:{type:'http',url:endpoint,headers:{Authorization:'Bearer ${GOPULSE_AGENT_TOKEN}'}}}},null,2):`[mcp_servers.gopulse]\nurl = ${JSON.stringify(endpoint)}\nbearer_token_env_var = "GOPULSE_AGENT_TOKEN"\nstartup_timeout_sec = 20\ntool_timeout_sec = 60\n`;
    },
    async revoke(id,button){const credential=this.credentials.find(x=>x.id===id);if(!credential||!await App.UI.confirm(`Revoke “${credential.name}”? New calls and queued actions will be denied. Already running actions may finish.`,{title:'Revoke agent access',confirmLabel:'Revoke access'}))return;button.disabled=true;try{await this.api('/'+encodeURIComponent(id),{method:'DELETE'});App.UI.toast('success','Agent access revoked.');await this.load();}catch(error){this.el('agents-error').textContent=error.message;button.disabled=false;}},
    close(){if(!this.busy)this.el('agent-connect-dialog').close();},
    async copy(value){
        try{if(navigator.clipboard&&window.isSecureContext)await navigator.clipboard.writeText(value);else{const input=document.createElement('textarea');input.value=value;input.style.position='fixed';input.style.opacity='0';(document.querySelector('dialog[open]')||document.body).append(input);input.select();try{if(!document.execCommand('copy'))throw new Error('Clipboard unavailable. Select and copy the text.');}finally{input.remove();}}App.UI.toast('success','Copied.');}catch(error){App.UI.toast('error',error.message);}
    }
};
