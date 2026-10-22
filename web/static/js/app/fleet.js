// Server-first inventory. Counts and workload groups use permission-scoped saved
// resources, never fabricated live health or inferred network connections.
App.Fleet = {
    servers: [], loading: false, loaded: false, view: 'all', layout: 'grid',
    roleInfo: {
        edge: {label:'Load balancers', icon:'fa-network-wired', hint:'Traffic entry points', view:'load-balancers'},
        apps: {label:'Applications', icon:'fa-cubes', hint:'Application workloads', view:'projects'},
        data: {label:'Databases', icon:'fa-database', hint:'Data & storage', view:'databases'},
        general: {label:'General purpose', icon:'fa-server', hint:'No saved workloads', view:'overview'}
    },
    roles(server) {
        const roles = [];
        if (server.counts?.edge > 0) roles.push('edge');
        if (server.counts?.apps > 0) roles.push('apps');
        if (server.is_db_only || server.counts?.data > 0) roles.push('data');
        if (!roles.length && ['apps','data','edge'].every(key => Number.isInteger(server.counts?.[key]))) roles.push('general');
        return roles;
    },
    matches(server, query, role) {
        return (!role || this.roles(server).includes(role)) &&
            [server.name, server.host, ...(server.tags || [])].join(' ').toLowerCase().includes(query.trim().toLowerCase());
    },
    node(tag, text, className) { return App.Workspace.element(tag, text, className); },
    icon(name) { const icon=this.node('i',undefined,`fas ${name}`); icon.setAttribute('aria-hidden','true'); return icon; },
    link(server, view = 'overview') { return App.Workspace.serverURL(server.id, view); },
    async load() {
        const root=document.getElementById('fleet-page');
        if (!root || this.loading) return;
        this.loading=true;
        const refresh=document.getElementById('fleet-refresh'), errorBox=document.getElementById('fleet-error');
        refresh.disabled=true;
        document.getElementById('server-list').setAttribute('aria-busy','true');
        errorBox.hidden=true;
        const errors=[];
        try {
            const raw=await App.Workspace.read('/servers/inventory');
            // Keep only display fields; SSH keys/passwords never enter card state.
            const servers=raw.map(server=>({id:server.id,name:server.name,host:server.host,port:server.port,user:server.user,
                auth_type:server.auth_type,tags:server.tags || [],is_db_only:server.is_db_only,counts:server.counts || {}}));
            this.servers=servers;
            this.loaded=true;
            this.summary(); this.render();
        } catch (error) {
            errors.push(`Unable to refresh servers. ${error.message}`);
            if (!this.loaded) {
                const list=document.getElementById('server-list'); list.replaceChildren();
                const empty=this.node('div',undefined,'fleet-empty');
                empty.append(this.icon('fa-triangle-exclamation'),this.node('h2','Your servers could not be loaded'),this.node('p','Use Refresh to try again.'));
                list.append(empty);
                document.getElementById('fleet-result-count').textContent='Server inventory unavailable';
            }
        } finally {
            errorBox.hidden=!errors.length;
            errorBox.textContent=errors.length ? `${errors.join(' ')} ${this.loaded ? 'Available data is shown; missing counts are marked with a dash.' : ''}` : '';
            refresh.disabled=false;
            document.getElementById('server-list').setAttribute('aria-busy','false');
            this.loading=false;
        }
    },
    summary() {
        for (const id of ['stat-servers','fleet-tab-count','rail-server-count']) {
            const target=document.getElementById(id); if (target) target.textContent=this.servers.length;
        }
        for (const [key,id] of [['apps','stat-projects'],['data','stat-databases'],['edge','stat-balancers']]) {
            const known=this.servers.filter(server=>Number.isInteger(server.counts[key]));
            const count=known.reduce((sum,server)=>sum+server.counts[key],0);
            const target=document.getElementById(id);
            target.textContent=!this.servers.length ? '0' : !known.length ? '—' : known.length < this.servers.length ? `${count}+` : count;
            target.title=known.length < this.servers.length ? 'Partial inventory; some counts are unavailable.' : 'Saved resources; not a live health check.';
        }
    },
    card(server) {
        const card=this.node('article',undefined,'fleet-card'), top=this.node('div',undefined,'fleet-card-top');
        const badge=this.node('span',undefined,'fleet-server-icon');badge.append(this.icon('fa-server'));top.append(badge);
        const identity=this.node('div',undefined,'fleet-server-identity'), title=this.node('h2'), link=this.node('a',server.name);
        link.href=this.link(server);title.append(link);identity.append(title);
        const host=this.node('div',undefined,'fleet-host');host.append(this.node('span',`${server.host}:${server.port || 22}`));
        const copy=this.node('button'); copy.type='button';copy.setAttribute('aria-label',`Copy host for ${server.name}`);copy.title='Copy host';copy.append(this.icon('fa-copy'));
        copy.addEventListener('click',event=>copyHost(event,server.host));host.append(copy);identity.append(host);top.append(identity);
        if (document.getElementById('fleet-page').dataset.canWrite==='true') {
            const verify=this.node('button',undefined,'fleet-delete fleet-verify');verify.type='button';verify.setAttribute('aria-label',`Verify SSH host key for ${server.name}`);verify.title='Verify SSH key';verify.append(this.icon('fa-shield-halved'));
            verify.addEventListener('click',()=>App.SSHTrust.verifyServer(server,verify));top.append(verify);
            const remove=this.node('button',undefined,'fleet-delete');remove.type='button';remove.setAttribute('aria-label',`Remove ${server.name}`);remove.title='Remove server';remove.append(this.icon('fa-trash-can'));
            remove.addEventListener('click',()=>deleteServer(server.id));top.append(remove);
        }
        card.append(top);
        const tags=this.node('div',undefined,'fleet-tags');
        for (const tag of server.tags.slice(0,3)) tags.append(this.node('span',tag,/^(prod|production)$/i.test(tag) ? 'fleet-tag' : 'fleet-tag neutral'));
        if (!server.tags.length) tags.append(this.node('span','Untagged','fleet-tag neutral'));
        if (server.is_db_only) tags.append(this.node('span','Database focus','fleet-tag neutral'));
        card.append(tags);
        const workloads=this.node('div',undefined,'fleet-workloads');
        for (const key of ['apps','data','edge']) {
            const info=this.roleInfo[key], count=server.counts[key];
            const focusedAway=server.is_db_only && key!=='data';
            const resource=this.node(focusedAway ? 'span' : 'a');
            if (!focusedAway) resource.href=this.link(server,info.view);
            resource.setAttribute('aria-label',`${info.label} on ${server.name}`);
            resource.append(this.icon(info.icon),this.node('strong',Number.isInteger(count) ? count : '—'),this.node('span',{apps:'apps',data:'databases',edge:'balancers'}[key]));workloads.append(resource);
        }
        card.append(workloads);
        const footer=this.node('div',undefined,'fleet-card-footer'), auth=this.node('span');
        auth.append(this.icon(server.auth_type==='key' ? 'fa-key' : 'fa-lock'),this.node('span',server.auth_type==='key' ? 'SSH key' : 'Password auth'));
        const manage=this.node('a','Manage server','fleet-manage');manage.href=this.link(server);manage.append(this.icon('fa-arrow-up-right-from-square'));footer.append(auth,manage);card.append(footer);
        return card;
    },
    roleBoard(servers,root) {
        for (const key of ['edge','apps','data','general']) {
            const selected=servers.filter(server=>this.roles(server).includes(key));
            if (key==='general' && !selected.length) continue;
            const info=this.roleInfo[key], column=this.node('section',undefined,'fleet-role-column'), heading=this.node('div',undefined,'fleet-role-heading');
            const icon=this.node('span',undefined,`fleet-role-icon ${key}`);icon.append(this.icon(info.icon));
            const text=this.node('div');text.append(this.node('h2',`${info.label} · ${selected.length}`),this.node('small',info.hint));heading.append(icon,text);column.append(heading);
            for (const server of selected) {
                const link=this.node('a',undefined,'fleet-role-node');link.href=this.link(server,info.view);
                link.append(this.icon('fa-server'));const identity=this.node('div');identity.append(this.node('strong',server.name),this.node('code',server.host));link.append(identity,this.node('span','↗'));column.append(link);
            }
            if (!selected.length) column.append(this.node('p','No saved workloads','fleet-role-no-nodes'));
            root.append(column);
        }
        const unknown=servers.filter(server=>!this.roles(server).length);
        if (unknown.length) {
            const note=this.node('div',undefined,'fleet-empty');note.append(this.node('h2','Workload information unavailable'),this.node('p',`${unknown.length} server(s) have incomplete resource counts. Open All servers to manage them.`));root.append(note);
        }
    },
    render() {
        const root=document.getElementById('server-list');
        if (!root) return;
        const query=document.getElementById('fleet-search').value, role=document.getElementById('fleet-role-filter').value;
        const servers=this.servers.filter(server=>this.matches(server,query,role));
        root.replaceChildren();root.className=this.view==='roles' ? 'fleet-grid fleet-role-board' : `fleet-grid ${this.layout==='list' ? 'list' : ''}`;
        if (!servers.length) {
            const empty=this.node('div',undefined,'fleet-empty');
            empty.append(this.icon('fa-server'),this.node('h2',query || role ? 'No matching servers' : 'Your infrastructure starts here'),this.node('p',query || role ? 'Try a different name, host, tag, or workload.' : 'Connect a server to deploy applications, manage databases, and route traffic.'));
            const action=this.node('button',query || role ? 'Clear filters' : 'Connect your first server','console-button');
            if (query || role) action.addEventListener('click',()=>{document.getElementById('fleet-search').value='';document.getElementById('fleet-role-filter').value='';this.update();});
            else action.addEventListener('click',()=>openAddServerModal());
            if (query || role || document.getElementById('fleet-page').dataset.canWrite==='true') empty.append(action);
            root.append(empty);
        } else if (this.view==='roles') this.roleBoard(servers,root);
        else for (const server of servers) root.append(this.card(server));
        document.getElementById('fleet-result-count').textContent=`Showing ${servers.length} of ${this.servers.length} servers`;
        for (const [id,active] of [['fleet-all-view',this.view==='all'],['fleet-role-view',this.view==='roles'],['fleet-grid-view',this.layout==='grid'],['fleet-list-view',this.layout==='list']]) document.getElementById(id).setAttribute('aria-pressed',String(active));
        document.querySelector('.fleet-layout-toggle').hidden=this.view==='roles';
    },
    update() {
        const url=new URL(window.location.href);
        for (const [key,value] of [['q',document.getElementById('fleet-search').value],['role',document.getElementById('fleet-role-filter').value],['layout',this.layout==='list' ? 'list' : ''],['group',this.view==='roles' ? 'workload' : '']]) {
            if (value) url.searchParams.set(key,value);else url.searchParams.delete(key);
        }
        history.replaceState({},'',url);this.render();
    },
    async init() {
        const switcher=document.getElementById('console-server-switcher');
        if (switcher) {
            switcher.addEventListener('change',()=>window.location.assign(App.Workspace.serverURL(switcher.value,'overview')));
            try {
                const servers=await App.Workspace.read('/servers');
                switcher.replaceChildren();
                for (const server of servers) {const option=this.node('option',server.name);option.value=server.id;switcher.append(option);}
                switcher.value=App.config.SERVER_ID;
            } catch { switcher.title='Server list unavailable; return to All servers to retry.'; }
            return;
        }
        if (!document.getElementById('fleet-page')) return;
        const params=new URLSearchParams(window.location.search);
        document.getElementById('fleet-search').value=params.get('q') || '';
        document.getElementById('fleet-role-filter').value=params.get('role') || '';
        this.layout=params.get('layout')==='list' ? 'list' : 'grid';this.view=params.get('group')==='workload' ? 'roles' : 'all';
        document.getElementById('fleet-search').addEventListener('input',()=>this.update());
        document.getElementById('fleet-role-filter').addEventListener('change',()=>this.update());
        for (const [id,view] of [['fleet-all-view','all'],['fleet-role-view','roles'],['fleet-explore-roles','roles']]) document.getElementById(id).addEventListener('click',()=>{this.view=view;this.update();});
        for (const [id,layout] of [['fleet-grid-view','grid'],['fleet-list-view','list']]) document.getElementById(id).addEventListener('click',()=>{this.layout=layout;this.update();});
        document.getElementById('fleet-refresh').addEventListener('click',()=>this.load());
        this.load();
    }
};
