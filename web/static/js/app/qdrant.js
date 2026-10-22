// Vector database workbench. Metadata, credentials, and remote reads have separate lifetimes.
App.Qdrant = {
    current: null, generation: 0,
    panels: ['overview','collections','connection','snapshots','settings','logs'],
    el(id) { return document.getElementById(id); },
    escape(value) { return App.Infrastructure.escape(value); },
    canWrite() { return this.el('databases-tab')?.dataset.canWrite === 'true'; },
    install() { App.Databases.openProvisionModal('qdrant'); },
    endpoint(config, port) {
        const host = config.bind_address === '0.0.0.0' ? App.config.SERVER_HOST : config.bind_address;
        const address = host.includes(':') && !host.startsWith('[') ? `[${host}]` : host;
        return `http://${address}:${port}`;
    },
    valid(state) { return this.current === state && this.generation === state.generation && App.config.SERVER_ID === state.server; },
    deactivate() {
        ++this.generation;
        this.clearKey();
        if(this.current&&App.Navigation?.current==='databases') {
            const title=this.el('server-section-title');if(title)title.textContent='Databases';
            const description=this.el('server-section-description');if(description)description.textContent='Manage database instances, services, and backups on this server.';
            document.title=`GoPulse - ${this.el('current-server-id')?.dataset.name||'Server'} - Databases`;
        }
        this.current = null;
        this.el('qdrant-manage-view')?.classList.add('hidden');
    },
    clearKey() {
        if(this.current)this.current.keyRequest=(this.current.keyRequest||0)+1;
        const key=this.el('vector-api-key');
        if(key) { key.value='';key.hidden=true; }
        const button=this.el('vector-reveal');if(button)button.textContent='Show API key';
    },
    address(panel, replace=false) {
        if(!this.current)return;
        const url=new URL(window.location.href);
        url.searchParams.set('view','databases');url.searchParams.set('resource',this.current.id);
        url.searchParams.set('panel',panel);url.searchParams.delete('action');url.hash='';
        if(url.href!==window.location.href)history[replace?'replaceState':'pushState']({},'',url);
    },
    back() {
        const url=App.Navigation.sectionURL('databases');history.pushState({},'',url);
        App.Databases.showListView();
    },
    error(message='') {
        const box=this.el('vector-error');if(box){box.textContent=message;box.hidden=!message;}
    },
    async manage(id, database) {
        this.deactivate();
        const state={id,server:App.config.SERVER_ID,generation:this.generation,config:null,collections:null,collection:null,panel:'overview',collectionRequest:0,detailRequest:0,snapshotRequest:0};
        this.current=state;
        state.path=`/servers/${encodeURIComponent(state.server)}/databases/${encodeURIComponent(id)}`;
        const root=this.el('qdrant-manage-view');
        for(const name of ['db-list-view','db-manage-view','baas-manage-view'])this.el(name)?.classList.add('hidden');
        root.classList.remove('hidden');
        root.innerHTML='<div class="vector-loading" role="status">Opening vector database…</div>';
        try {
            if(!database)database=await App.Infrastructure.api(state.path);
            const config=await App.Infrastructure.api(state.path+'/qdrant/config');
            if(!this.valid(state))return;
            state.db=database;state.config=config;
            this.render(state);
            const title=this.el('server-section-title');if(title)title.textContent='Vector database';
            const description=this.el('server-section-description');if(description)description.textContent='Manage collections, application connections, and your Qdrant instance.';
            document.title=`GoPulse - ${database.name} - Vector database`;
            const requested=new URLSearchParams(window.location.search).get('panel');
            this.switchPanel(this.panels.includes(requested)?requested:'overview',false);
            this.address(state.panel,true);
            await this.refreshCollections(state);
        } catch(e) {
            if(!this.valid(state))return;
            root.innerHTML='<div class="vector-empty"><i class="fas fa-circle-exclamation" aria-hidden="true"></i><h3>Unable to open this database</h3><p data-vector-error></p><button class="console-button secondary" data-q-action="back">Back to databases</button><button class="console-button" data-q-action="retry">Retry</button></div>';
            root.querySelector('[data-vector-error]').textContent=e.message;
            root.onclick=event=>{const action=event.target.closest('[data-q-action]')?.dataset.qAction;if(action==='back')this.back();if(action==='retry')this.manage(id,database);};
        }
    },
    button(action,label,icon='',extra='') {
        return `<button type="button" class="console-button secondary ${extra}" data-q-action="${action}">${icon?`<i class="fas ${icon}" aria-hidden="true"></i>`:''}${label}</button>`;
    },
    render(state) {
        const e=value=>this.escape(value),cfg=state.config,write=this.canWrite();
        const root=this.el('qdrant-manage-view');
        root.innerHTML=`<div class="db-resource-back"><button type="button" data-q-action="back"><i class="fas fa-arrow-left" aria-hidden="true"></i>All databases</button><span>/</span><span>Vector database</span></div>
            <header class="vector-header"><div class="vector-identity"><div class="vector-mark"><i class="fas fa-cubes" aria-hidden="true"></i></div><div><div class="vector-eyebrow">QDRANT · VECTOR DATABASE</div><h2>${e(state.db.name)}</h2><p>${e(App.config.SERVER_HOST)} <span>·</span> <span id="vector-saved-status">${e(state.db.status||'Unknown')} (saved state)</span></p></div></div><div class="vector-header-actions">${this.button('refresh','Refresh','fa-rotate')}${write?`<details class="db-tools-menu"><summary>Instance<i class="fas fa-chevron-down" aria-hidden="true"></i></summary><div class="db-tools-popover">${['start','stop','restart'].map(action=>`<button type="button" data-q-action="${action}"><i class="fas fa-${action==='start'?'play':action==='stop'?'stop':'rotate'}" aria-hidden="true"></i><span>${action[0].toUpperCase()+action.slice(1)} instance</span></button>`).join('')}<button type="button" data-q-action="remove" class="db-menu-danger"><i class="fas fa-trash" aria-hidden="true"></i><span>Remove service<small>Keep persistent data</small></span></button></div></details>`:''}</div></header>
            <nav class="vector-tabs" aria-label="Vector database sections">${this.panels.map(panel=>`<button type="button" data-q-action="panel" data-panel="${panel}" aria-pressed="false">${panel[0].toUpperCase()+panel.slice(1)}</button>`).join('')}</nav>
            <p id="vector-error" class="vector-error" role="alert" hidden></p>
            <div id="vector-panel-overview" class="vector-panel"><div class="vector-metrics"><article><span>Collections</span><strong id="vector-count">—</strong><small>Available in this instance</small></article><article><span>Connection</span><strong id="vector-health" class="vector-metric-text">Checking…</strong><small>Authenticated API check</small></article><article><span>Qdrant version</span><strong class="vector-metric-text">${e(cfg.version)}</strong><small>Configured image version</small></article><article><span>Network access</span><strong class="vector-metric-text">${cfg.bind_address==='0.0.0.0'?'External':cfg.bind_address.startsWith('127.')?'Local only':'Specific interface'}</strong><small>${e(cfg.bind_address)}</small></article></div>
                <div class="vector-overview-grid"><section class="vector-card"><div class="vector-card-heading"><div><h3>Your collections</h3><p>Organize vectors for search and retrieval.</p></div>${this.button('all-collections','View all','fa-arrow-right')}</div><div id="vector-recent" class="vector-loading">Loading collections…</div></section><section class="vector-card"><div class="vector-card-heading"><div><h3>Connect your application</h3><p>Your REST endpoint and authentication.</p></div><i class="fas fa-plug" aria-hidden="true"></i></div><label class="vector-label">HTTP endpoint</label><div class="vector-endpoint"><code>${e(this.endpoint(cfg,cfg.host_port))}</code>${this.button('copy-http','Copy','fa-copy')}</div><p class="vector-help">Send your API key in the <code>api-key</code> header.</p>${this.button('connection','Connection details','fa-arrow-right')}</section></div>
            </div>
            <div id="vector-panel-collections" class="vector-panel hidden"><section class="vector-card"><div class="vector-card-heading"><div><h3>Collections</h3><p>Vector spaces for your application's data.</p></div>${write?this.button('new-collection','Create collection','fa-plus'):''}</div><div id="vector-create" hidden>${write?`<form id="vector-create-form" class="vector-form"><label>Collection name<input name="name" required pattern="[A-Za-z0-9][A-Za-z0-9_-]{0,63}" placeholder="e.g. documents"></label><label>Vector dimensions<input name="size" type="number" value="384" min="1" max="65536" required></label><label>Distance metric<select name="distance"><option>Cosine</option><option>Dot</option><option>Euclid</option><option>Manhattan</option></select></label><div class="vector-form-actions"><button class="console-button" type="submit">Create collection</button>${this.button('cancel-create','Cancel')}</div></form>`:''}</div><label class="vector-search"><i class="fas fa-search" aria-hidden="true"></i><input id="vector-search" type="search" placeholder="Search collections…" aria-label="Search collections"></label><div id="vector-collections" class="vector-loading">Loading collections…</div></section><section id="vector-collection-detail" class="vector-card" hidden></section></div>
            <div id="vector-panel-connection" class="vector-panel hidden"><section class="vector-card"><div class="vector-card-heading"><div><h3>Application connection</h3><p>Use the server address and API key in your Qdrant client.</p></div></div><dl class="vector-connection-grid"><div><dt>HTTP / REST</dt><dd>${e(this.endpoint(cfg,cfg.host_port))}</dd></div><div><dt>gRPC</dt><dd>${cfg.grpc_port?e(this.endpoint(cfg,cfg.grpc_port)):'Not published'}</dd></div></dl><div class="vector-form-actions">${this.button('copy-http','Copy HTTP endpoint','fa-copy')}${cfg.grpc_port?this.button('copy-grpc','Copy gRPC endpoint','fa-copy'):''}</div><div class="vector-key-area"><label class="vector-label" for="vector-api-key">API key</label><p class="vector-help">Use the <code>api-key</code> header for every authenticated request.</p>${write?`<div class="vector-form-actions"><button id="vector-reveal" type="button" class="console-button secondary" data-q-action="reveal">Show API key</button></div><input id="vector-api-key" aria-label="Qdrant API key" type="text" readonly autocomplete="off" hidden>`:'<p class="vector-help">Ask a database manager for application credentials.</p>'}</div><label class="vector-label">Example request</label><p class="vector-help">Set QDRANT_API_KEY in your environment before running this request.</p><pre class="vector-code">${e('curl '+this.endpoint(cfg,cfg.host_port)+'/collections \\\n  -H "api-key: ${QDRANT_API_KEY:?Set your Qdrant API key}"')}</pre><p class="vector-help">Allow the selected ports in your host and cloud firewalls. Use TLS or a trusted network to protect traffic and credentials.</p></section></div>
            <div id="vector-panel-snapshots" class="vector-panel hidden"><section class="vector-card"><div class="vector-card-heading"><div><h3>Collection snapshots</h3><p>Snapshots stay on the server. Restore replaces the selected collection's data.</p></div>${write?this.button('create-snapshot','Create snapshot','fa-plus'):''}</div><label class="vector-label" for="vector-snapshot-collection">Collection</label><select id="vector-snapshot-collection"><option value="">Select a collection</option></select><div id="vector-snapshots" class="vector-empty"><p>Select a collection to view snapshots.</p></div></section></div>
            <div id="vector-panel-settings" class="vector-panel hidden"><section class="vector-card"><div class="vector-card-heading"><div><h3>Network access</h3><p>Choose how applications reach this database.</p></div></div><form id="vector-network-form" class="vector-form"><label>Bind address<input name="bind" value="${e(cfg.bind_address)}" required ${write?'':'disabled'}><small>0.0.0.0 allows external apps. 127.0.0.1 allows local access only.</small></label><label>HTTP port<input name="http" type="number" min="1024" max="65535" value="${cfg.host_port}" required ${write?'':'disabled'}></label><label>gRPC port<input name="grpc" type="number" min="0" max="65535" value="${cfg.grpc_port||0}" required ${write?'':'disabled'}><small>Set to 0 to disable gRPC.</small></label>${write?'<div class="vector-form-actions"><button class="console-button" type="submit">Apply network settings</button><span class="vector-help">Restarts briefly; preserves data and API key.</span></div>':''}</form></section><section class="vector-card"><div class="vector-card-heading"><div><h3>Deployment</h3><p>Persistent storage and configured version.</p></div></div><dl class="vector-connection-grid"><div><dt>Version</dt><dd>${e(cfg.version)}</dd></div><div><dt>Storage directory</dt><dd>${e(cfg.storage_path)}</dd></div></dl></section></div>
            <div id="vector-panel-logs" class="vector-panel hidden"><section class="vector-card"><div class="vector-card-heading"><div><h3>Resource usage & logs</h3><p>Live diagnostics from the server. API keys are redacted.</p></div>${this.button('diagnostics','Refresh diagnostics','fa-rotate')}</div><p id="vector-diagnostics-status" class="vector-help">Open this section to load diagnostics.</p><label class="vector-label">Resource usage</label><pre id="vector-stats" class="vector-code">Not loaded</pre><label class="vector-label">Recent logs</label><pre id="vector-logs" class="vector-code vector-log-output">Not loaded</pre></section></div>`;
        root.onclick=event=>{const button=event.target.closest('[data-q-action]');if(button&&!button.disabled)this.action(button,state);};
        this.el('vector-search').oninput=()=>this.renderCollections(state);
        this.el('vector-snapshot-collection').onchange=()=>this.loadSnapshots();
        this.el('vector-create-form')?.addEventListener('submit',event=>{event.preventDefault();this.createCollection(event.target,state);});
        this.el('vector-network-form').onsubmit=event=>{event.preventDefault();if(!write)return;const f=event.target;this.configureNetwork(state.id,{bind_address:f.elements.bind.value.trim(),host_port:Number(f.elements.http.value),grpc_port:Number(f.elements.grpc.value)},root,f.querySelector('button'),state.server);};
    },
    switchPanel(panel,push=true) {
        const state=this.current;if(!state||!this.panels.includes(panel))return;
        this.clearKey();state.panel=panel;
        for(const name of this.panels)this.el('vector-panel-'+name)?.classList.toggle('hidden',name!==panel);
        this.el('qdrant-manage-view').querySelectorAll('[data-panel]').forEach(button=>button.setAttribute('aria-pressed',String(button.dataset.panel===panel)));
        if(push)this.address(panel);
        if(panel==='logs')this.diagnostics(state);
        if(panel==='snapshots'&&this.el('vector-snapshot-collection').value)this.loadSnapshots();
    },
    async refreshCollections(state=this.current) {
        if(!state)return;
        const request=++state.collectionRequest;
        try {
            const result=await App.Infrastructure.api(state.path+'/qdrant/collections');
            if(!this.valid(state)||request!==state.collectionRequest)return;
            state.collections=result.result?.collections||[];
            this.el('vector-health').textContent='Connected';this.el('vector-health').dataset.state='healthy';
            this.el('vector-count').textContent=state.collections.length;
            this.renderCollections(state);
            const select=this.el('vector-snapshot-collection'),selected=select.value;
            select.innerHTML='<option value="">Select a collection</option>'+state.collections.map(c=>`<option value="${this.escape(c.name)}">${this.escape(c.name)}</option>`).join('');
            if(state.collections.some(c=>c.name===selected))select.value=selected;
        } catch(e) {
            if(!this.valid(state)||request!==state.collectionRequest)return;
            state.collections=null;this.el('vector-count').textContent='—';
            this.el('vector-health').textContent='Unavailable';this.el('vector-health').dataset.state='error';
            for(const id of ['vector-recent','vector-collections'])this.el(id).innerHTML='<div class="vector-empty"><i class="fas fa-plug-circle-exclamation" aria-hidden="true"></i><h4>Collections unavailable</h4><p>Check the server connection, then refresh.</p></div>';
            this.error(e.message);
        }
    },
    renderCollections(state) {
        const query=(this.el('vector-search').value||'').trim().toLowerCase();
        const all=state.collections||[],items=all.filter(c=>c.name.toLowerCase().includes(query));
        const rows=collections=>collections.map(c=>`<div class="vector-collection-row"><span class="vector-collection-icon"><i class="fas fa-layer-group" aria-hidden="true"></i></span><button type="button" data-q-action="collection" data-name="${this.escape(c.name)}"><strong>${this.escape(c.name)}</strong><small>Vector collection</small></button>${this.buttonForCollection(c.name)}</div>`).join('');
        this.el('vector-recent').innerHTML=all.length?rows(all.slice(0,4)):'<div class="vector-empty"><i class="fas fa-layer-group" aria-hidden="true"></i><h4>Your first vector space</h4><p>Create a collection to organize embeddings for search.</p>'+ (this.canWrite()?this.button('new-collection','Create collection','fa-plus'):'')+'</div>';
        this.el('vector-collections').innerHTML=items.length?rows(items):`<div class="vector-empty"><h4>${query?'No matching collections':'No collections yet'}</h4><p>${query?'Try a different collection name.':'Create a collection to get started.'}</p></div>`;
    },
    buttonForCollection(name) { return `<button class="vector-row-link" type="button" data-q-action="collection" data-name="${this.escape(name)}" aria-label="Open ${this.escape(name)}"><i class="fas fa-arrow-up-right-from-square" aria-hidden="true"></i></button>`; },
    async openCollection(name) {
        const state=this.current;if(!state)return;
        this.switchPanel('collections');state.collection=name;
        const request=++state.detailRequest,detail=this.el('vector-collection-detail');detail.hidden=false;detail.innerHTML='<p class="vector-loading">Loading collection details…</p>';
        try {
            const data=await App.Infrastructure.api(state.path+'/qdrant/collections/'+encodeURIComponent(name));
            if(!this.valid(state)||request!==state.detailRequest)return;
            const result=data.result||{},vectors=result.config?.params?.vectors;
            const spaces=vectors?.size?[['Default vector',vectors]]:Object.entries(vectors||{});
            detail.innerHTML=`<div class="vector-card-heading"><div><h3>${this.escape(name)}</h3><p>Collection details</p></div><div class="vector-form-actions">${this.button('collection-snapshots','Snapshots','fa-camera')}${this.canWrite()?this.button('delete-collection','Delete','fa-trash','vector-danger'):''}</div></div><dl class="vector-connection-grid"><div><dt>Points</dt><dd>${this.escape(result.points_count??'Unavailable')}</dd></div><div><dt>Indexed vectors</dt><dd>${this.escape(result.indexed_vectors_count??'Unavailable')}</dd></div><div><dt>Status</dt><dd>${this.escape(result.status??'Unavailable')}</dd></div></dl><h4 class="vector-label">Vector configuration</h4>${spaces.length?spaces.map(([label,v])=>`<div class="vector-space"><strong>${this.escape(label)}</strong><span>${this.escape(v.size)} dimensions</span><span>${this.escape(v.distance)}</span></div>`).join(''):'<p class="vector-help">No dense vector configuration returned.</p>'}`;
        } catch(e){if(this.valid(state)&&request===state.detailRequest){detail.textContent=e.message;this.error(e.message);}}
    },
    async createCollection(form,state) {
        if(!this.canWrite()||!this.valid(state))return;
        const button=form.querySelector('button[type="submit"]');if(button.disabled)return;button.disabled=true;this.error();
        const name=form.elements.name.value;
        try {
            await App.Infrastructure.api(state.path+'/qdrant/collections',{method:'POST',body:JSON.stringify({name,size:Number(form.elements.size.value),distance:form.elements.distance.value})});
            if(!this.valid(state))return;
            form.reset();this.el('vector-create').hidden=true;await this.refreshCollections(state);if(this.valid(state))await this.openCollection(name);
        }catch(e){if(this.valid(state))this.error(e.message);}finally{button.disabled=false;}
    },
    async revealKey() {
        const state=this.current;if(!state||!this.canWrite())return;
        const key=this.el('vector-api-key'),button=this.el('vector-reveal');if(!key.hidden){this.clearKey();return;}
        if(button.disabled)return;button.disabled=true;
        const request=state.keyRequest=(state.keyRequest||0)+1;
        try{const result=await App.Infrastructure.api(state.path+'/credentials');if(this.valid(state)&&state.panel==='connection'&&request===state.keyRequest){key.value=result.api_key;key.hidden=false;button.textContent='Hide API key';}}
        catch(e){if(this.valid(state))this.error(e.message);}finally{button.disabled=false;}
    },
    async diagnostics(state=this.current) {
        if(!state||state.diagnosticsLoading)return;
        state.diagnosticsLoading=true;this.el('vector-diagnostics-status').textContent='Loading live diagnostics…';
        try{const data=await App.Infrastructure.api(state.path+'/qdrant/diagnostics');if(!this.valid(state))return;this.el('vector-stats').textContent=typeof data.stats==='string'?data.stats:JSON.stringify(data.stats,null,2);this.el('vector-logs').textContent=data.logs||'No recent logs.';this.el('vector-diagnostics-status').textContent=data.healthy?'Authenticated connection verified.':'Database unavailable or starting.';}
        catch(e){if(this.valid(state)){this.el('vector-diagnostics-status').textContent='Diagnostics unavailable.';this.error(e.message);}}
        finally{state.diagnosticsLoading=false;}
    },
    async loadSnapshots() {
        const state=this.current;if(!state)return;
        const name=this.el('vector-snapshot-collection').value,request=++state.snapshotRequest;
        const list=this.el('vector-snapshots');list.innerHTML='<p class="vector-help">'+(name?'Loading snapshots…':'Select a collection to view snapshots.')+'</p>';if(!name)return;
        try {
            const data=await App.Infrastructure.api(state.path+'/qdrant/collections/'+encodeURIComponent(name)+'/snapshots');
            if(!this.valid(state)||request!==state.snapshotRequest)return;
            list.innerHTML=(data.result||[]).map(snapshot=>`<div class="vector-snapshot-row"><div><strong>${this.escape(snapshot.name)}</strong><small>${this.escape(snapshot.creation_time||'Collection snapshot')}${Number.isFinite(snapshot.size)?' · '+this.escape((snapshot.size/1048576).toFixed(1))+' MB':''}</small></div>${this.canWrite()?`<button type="button" class="console-button secondary" data-q-action="restore" data-name="${this.escape(snapshot.name)}">Restore</button>`:''}</div>`).join('')||'<div class="vector-empty"><h4>No snapshots yet</h4><p>Create a snapshot of this collection before making major changes.</p></div>';
        } catch(e){if(this.valid(state)&&request===state.snapshotRequest){list.textContent='Snapshots unavailable.';this.error(e.message);}}
    },
    async configureNetwork(id,values,root,button,serverID) {
        if(button.disabled)return;
        if(!await App.UI.confirm(`Apply Qdrant network settings?\n\nBind: ${values.bind_address}\nHTTP: ${values.host_port}\ngRPC: ${values.grpc_port||'disabled'}\n\nThe service will restart briefly. Data and the API key will be preserved.`))return;
        button.disabled=true;
        try {
            const I=App.Infrastructure,op=await I.api(`/servers/${encodeURIComponent(serverID)}/databases/${encodeURIComponent(id)}/qdrant/network`,{method:'PUT',body:JSON.stringify(values)});
            const progress=I.dialog('Updating Qdrant network','<p>Applying network settings…</p>');
            await I.track(op.operation_id,progress,result=>{if(App.config.SERVER_ID!==serverID)return;if(this.current?.id===id&&result?.status==='completed')this.manage(id,this.current.db);else App.Databases.load();});
        }catch(e){this.error(e.message);}finally{button.disabled=false;}
    },
    async action(button,state) {
        if(!this.valid(state))return;
        const action=button.dataset.qAction,name=button.dataset.name;
        if(action==='back'){this.back();return;}
        if(action==='panel'){this.switchPanel(button.dataset.panel);return;}
        if(action==='connection'){this.switchPanel('connection');return;}
        if(action==='all-collections'){this.switchPanel('collections');return;}
        if(action==='collection'){await this.openCollection(name);return;}
        if(action==='collection-snapshots'){this.switchPanel('snapshots');this.el('vector-snapshot-collection').value=state.collection;await this.loadSnapshots();return;}
        if(action==='new-collection'){if(this.canWrite()){this.switchPanel('collections');this.el('vector-create').hidden=false;this.el('vector-create-form').elements.name.focus();}return;}
        if(action==='cancel-create'){this.el('vector-create').hidden=true;return;}
        if(action==='refresh'){this.error();button.disabled=true;try{await this.refreshCollections(state);}finally{button.disabled=false;}return;}
        if(action==='reveal'){await this.revealKey();return;}
        if(action==='diagnostics'){await this.diagnostics(state);return;}
        if(action.startsWith('copy-')){try{await navigator.clipboard.writeText(this.endpoint(state.config,action==='copy-http'?state.config.host_port:state.config.grpc_port));App.UI.toast('success','Endpoint copied.');}catch(e){this.error('Unable to copy. Select and copy the endpoint instead.');}return;}
        if(!this.canWrite())return;
        button.disabled=true;this.error();
        try {
            if(['start','stop','restart'].includes(action)){
                if(!await App.UI.confirm(`${action[0].toUpperCase()+action.slice(1)} this Qdrant instance?`))return;
                await App.Infrastructure.api(state.path+'/'+action,{method:'POST'});if(this.valid(state)){this.el('vector-saved-status').textContent=action==='stop'?'Stopped':'Running';await this.refreshCollections(state);}
            }else if(action==='remove'){
                if(!await App.UI.confirm('Remove this Qdrant service? Persistent data will be kept.'))return;
                await App.Infrastructure.api(state.path+'?confirm='+encodeURIComponent(state.id),{method:'DELETE'});if(this.valid(state))this.back();
            }else if(action==='delete-collection'){
                if(await App.UI.prompt('Type the collection name to permanently delete it')!==state.collection)return;
                await App.Infrastructure.api(state.path+'/qdrant/collections/'+encodeURIComponent(state.collection)+'?confirm='+encodeURIComponent(state.collection),{method:'DELETE'});if(this.valid(state)){this.el('vector-collection-detail').hidden=true;++state.detailRequest;state.collection=null;await this.refreshCollections(state);}
            }else if(action==='create-snapshot'){
                const collection=this.el('vector-snapshot-collection').value;if(!collection){this.error('Select a collection first.');return;}
                await App.Infrastructure.api(state.path+'/qdrant/collections/'+encodeURIComponent(collection)+'/snapshots',{method:'POST'});if(this.valid(state))await this.loadSnapshots();
            }else if(action==='restore'){
                const collection=this.el('vector-snapshot-collection').value;
                if(await App.UI.prompt('Restoring replaces collection data. Type the collection name to confirm')!==collection)return;
                const op=await App.Infrastructure.api(state.path+'/qdrant/collections/'+encodeURIComponent(collection)+'/snapshots/restore?confirm='+encodeURIComponent(collection),{method:'POST',body:JSON.stringify({filename:name})});
                const progress=App.Infrastructure.dialog('Restoring snapshot','<p>Preparing restore…</p>');await App.Infrastructure.track(op.operation_id,progress,()=>{if(this.valid(state))this.refreshCollections(state);});
            }
        }catch(e){if(this.valid(state))this.error(e.message);}finally{button.disabled=false;}
    }
};
