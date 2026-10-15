// Global inventories use existing APIs, which filter servers and enforce ownership.
// Resource management continues in its existing, explicitly scoped screen.
App.Workspace = {
    servers: [], rows: [], errors: [], loading: false,

    serverURL(serverID, view, resourceID = '', panel = '') {
        const params = new URLSearchParams({view});
        if (resourceID) params.set('resource', resourceID);
        if (panel) params.set('panel', panel);
        return `/servers/${encodeURIComponent(serverID)}/dashboard?${params}`;
    },

    element(tag, text, className) {
        const element = document.createElement(tag);
        if (text !== undefined) element.textContent = text;
        if (className) element.className = className;
        return element;
    },

    async read(path) {
        const value = await App.API.get(path);
        if (!value.success) throw new Error(value.error || value.message || 'The inventory request failed.');
        return value.data || [];
    },

    async bounded(items, work) {
        let next = 0;
        await Promise.all(Array.from({length: Math.min(4, items.length)}, async () => {
            while (next < items.length) await work(items[next++]);
        }));
    },

    row(item, server, kind) {
        const view = {application:'projects', database:'databases', domain:'domains', 'load balancer':'load-balancers'}[kind];
        const status = item.status || (kind === 'domain' ? (item.ssl ? 'TLS configured' : 'HTTP configured') : 'Unknown');
        return {id:item.id, serverID:server.id, server:server.name, name:item.name || item.domain || 'Unnamed resource',
            kind, detail:item.type || item.domain || '', status,
            href:this.serverURL(server.id, view, kind === 'domain' ? '' : item.id)};
    },

    matches(row, query, serverID) {
        return (!serverID || row.serverID === serverID) &&
            [row.name, row.server, row.kind, row.detail, row.status].join(' ').toLowerCase().includes(query.toLowerCase().trim());
    },

    async load() {
        if (this.loading) return;
        this.loading = true;
        const refresh = document.getElementById('workspace-refresh');
        const create = document.getElementById('workspace-create');
        if (refresh) { refresh.disabled = true; refresh.textContent = 'Refreshing…'; }
        if (create) create.disabled = true;
        this.errors = [];
        const rows = [], projects = [];
        let appCount = 0, databaseCount = 0;
        try {
            this.servers = await this.read('/servers');
            const section = this.section;
            const kinds = section === 'networking' ? [['domains','domain'],['load-balancers','load balancer']] :
                section === 'databases' ? [['databases','database']] :
                section === 'overview' ? [['projects','application'],['databases','database']] : [['projects','application']];
            await this.bounded(this.servers, async server => {
                for (const [path, kind] of kinds) {
                    try {
                        const resources = await this.read(`/servers/${encodeURIComponent(server.id)}/${path}`);
                        if (kind === 'application') appCount += resources.length;
                        if (kind === 'database') databaseCount += resources.length;
                        for (const item of resources) {
                            if (section === 'activity') projects.push({item,server});
                            else if (section !== 'overview' || kind === 'application') rows.push(this.row(item, server, kind));
                        }
                    } catch (error) { this.errors.push(`Could not load ${path} on ${server.name}. ${error.message}`); }
                }
            });
            if (section === 'activity') {
                await this.bounded(projects, async ({item,server}) => {
                    try {
                        const attempts = await this.read(`/servers/${encodeURIComponent(server.id)}/projects/${encodeURIComponent(item.id)}/deployments?limit=20`);
                        for (const attempt of attempts) rows.push({name:item.name,serverID:server.id,server:server.name,kind:'deployment',
                            detail:attempt.commit_sha ? attempt.commit_sha.slice(0,8) : attempt.source, status:attempt.status,
                            time:attempt.queued_at, href:this.serverURL(server.id,'projects',item.id,'releases')});
                    } catch (error) { this.errors.push(`Could not load deployment history for ${item.name}. ${error.message}`); }
                });
                try {
                    const operations = await this.read('/operations');
                    for (const op of operations) {
                        const server = this.servers.find(item => item.id === op.server_id);
                        if (!server) continue;
                        rows.push({name:op.operation,serverID:server.id,server:server.name,kind:'operation', detail:'Your infrastructure operation',
                            status:op.status,time:op.created_at,operationID:op.operation_id});
                    }
                } catch (error) { this.errors.push(`Could not load your operations. ${error.message}`); }
                rows.sort((a,b) => new Date(b.time) - new Date(a.time));
            } else rows.sort((a,b) => a.name.localeCompare(b.name));
            this.rows = rows;
            this.fillServerFilter();
            for (const [id, count] of [['workspace-app-count',appCount],['workspace-server-count',this.servers.length],['workspace-db-count',databaseCount]]) {
                const element = document.getElementById(id);
                if (element) element.textContent = this.errors.length ? `${count} (partial)` : count;
            }
            this.render();
        } catch (error) {
            this.errors.push(error.message);
            // Preserve previous inventory if the refresh failed; never turn a failed read into empty success.
            this.render();
        } finally {
            const alert = document.getElementById('workspace-error');
            alert.hidden = this.errors.length === 0;
            alert.textContent = this.errors.join(' ');
            if (refresh) { refresh.disabled = false; refresh.textContent = 'Refresh'; }
            if (create) create.disabled = false;
            this.loading = false;
        }
    },

    fillServerFilter() {
        const select = document.getElementById('workspace-server-filter');
        const previous = select.value || new URLSearchParams(window.location.search).get('server') || '';
        select.replaceChildren(this.element('option','All permitted servers'));
        select.options[0].value = '';
        for (const server of this.servers) { const option = this.element('option',server.name); option.value = server.id; select.append(option); }
        select.value = this.servers.some(server => server.id === previous) ? previous : '';
    },

    render() {
        const results = document.getElementById('workspace-results');
        const query = document.getElementById('workspace-search').value;
        const serverID = document.getElementById('workspace-server-filter').value;
        const rows = this.rows.filter(row => this.matches(row,query,serverID));
        results.replaceChildren();
        if (!rows.length) {
            const empty = this.element('div',undefined,'workspace-empty');
            empty.append(this.element('h3',this.errors.length ? 'Inventory could not be fully loaded' : query || serverID ? 'No matching resources' : this.servers.length ? 'No resources here yet' : 'Connect your first server','text-lg font-semibold'));
            empty.append(this.element('p',this.errors.length ? 'Use Refresh to try again. Existing resources may still be available on their server.' : query || serverID ? 'Change your search or server filter.' : this.servers.length ? 'Open a server to create or manage a resource.' : 'A server provides the infrastructure for your applications and databases.'));
            const link = this.element('a','Open servers','workspace-button secondary mt-5'); link.href = '/servers'; empty.append(link);
            results.append(empty); return;
        }
        const wrap = this.element('div',undefined,'workspace-table-wrap');
        const table = this.element('table',undefined,'workspace-table');
        const caption = this.element('caption',`${rows.length} resources shown`,'sr-only'); table.append(caption);
        const head = this.element('thead'), header = this.element('tr');
        for (const title of ['Resource','Server','Type','Reported state',this.section === 'activity' ? 'Started / queued' : 'Details','Open']) { const th=this.element('th',title); th.scope='col'; header.append(th); }
        head.append(header); table.append(head);
        const body = this.element('tbody');
        for (const row of rows) {
            const tr = this.element('tr');
            tr.append(this.element('td',row.name),this.element('td',row.server),this.element('td',row.kind));
            const state=this.element('td');state.append(this.element('span',row.status,'workspace-state'));tr.append(state);
            tr.append(this.element('td',this.section === 'activity' ? new Date(row.time).toLocaleString() : row.detail || '—'));
            const action=this.element('td');
            if (row.operationID) {
                const button=this.element('button','Inspect','workspace-button secondary');
                button.addEventListener('click',()=>{const root=App.Infrastructure.dialog('Operation details','<p>Loading…</p>');App.Infrastructure.track(row.operationID,root);});action.append(button);
            } else { const link=this.element('a',this.section === 'activity' ? 'Inspect' : 'Manage');link.href=row.href;link.setAttribute('aria-label',`Open ${row.name} on ${row.server}`);action.append(link); }
            tr.append(action);body.append(tr);
            ['Resource','Server','Type','Reported state',this.section === 'activity' ? 'Started / queued' : 'Details','Open'].forEach((label,index)=>{tr.children[index].dataset.label=label;});
        }
        table.append(body);wrap.append(table);results.append(wrap);
    },

    openPicker(view) {
        const servers = this.servers.filter(server => view !== 'projects' || !server.is_db_only);
        if (!servers.length) { window.location.assign('/servers'); return; }
        const select=document.getElementById('workspace-target-server');select.replaceChildren();
        for (const server of servers) {const option=this.element('option',server.name);option.value=server.id;select.append(option);}
        const filtered=document.getElementById('workspace-server-filter').value;
        if (servers.some(server=>server.id===filtered)) select.value=filtered;
        this.pickerView=view;document.getElementById('workspace-server-picker').showModal();
    },

    init() {
        const page=document.getElementById('workspace-page');
        if (!page || page.dataset.section === 'settings') return;
        this.section=page.dataset.section;
        const params=new URLSearchParams(window.location.search);
        document.getElementById('workspace-search').value=params.get('q') || '';
        for (const id of ['workspace-search','workspace-server-filter']) document.getElementById(id).addEventListener(id.endsWith('search') ? 'input' : 'change',()=>{
            const url=new URL(window.location.href);
            for (const [key,value] of [['q',document.getElementById('workspace-search').value],['server',document.getElementById('workspace-server-filter').value]]) {if(value)url.searchParams.set(key,value);else url.searchParams.delete(key);}
            history.replaceState({},'',url);this.render();
        });
        document.getElementById('workspace-refresh').addEventListener('click',()=>this.load());
        document.getElementById('workspace-create')?.addEventListener('click',event=>this.openPicker(event.currentTarget.dataset.view));
        document.getElementById('workspace-picker-cancel').addEventListener('click',()=>document.getElementById('workspace-server-picker').close());
        document.getElementById('workspace-picker-form').addEventListener('submit',event=>{
            event.preventDefault();const serverID=document.getElementById('workspace-target-server').value;
            if (!this.servers.some(server=>server.id===serverID)) return;
            const url=this.serverURL(serverID,this.pickerView);
            window.location.assign(url + (this.pickerView === 'domains' ? '' : '&action=create'));
        });
        document.getElementById('workspace-operations')?.addEventListener('click',()=>App.Infrastructure.operations());
        document.getElementById('workspace-ai')?.addEventListener('click',()=>App.Infrastructure.agentToken());
        this.load();
    }
};
