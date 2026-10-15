/* Shared operation feedback and Qdrant workflows; no raw terminal commands. */
App.Infrastructure = {
    escape(value) { return String(value ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])); },
    dialog(title, html) {
        document.getElementById('infrastructure-dialog')?.remove();
        const root = document.createElement('div'); root.id = 'infrastructure-dialog';
        root.className = 'ui-dialog-overlay fixed inset-0 bg-black/80 z-50 flex items-center justify-center p-4';
        root.innerHTML = `<div role="dialog" aria-modal="true" aria-label="${this.escape(title)}" class="bg-gray-800 text-white rounded-xl p-6 w-full max-w-xl max-h-[90vh] overflow-y-auto"><div class="flex justify-between mb-4"><h3 class="text-xl font-bold">${this.escape(title)}</h3><button type="button" aria-label="Close" data-close>×</button></div><div data-body>${html}</div><p data-error role="alert" class="text-red-300 mt-3"></p></div>`;
        root.querySelector('[data-close]').onclick = () => root.remove(); document.body.append(root); return root;
    },
    async api(path, options = {}) {
        const response = await fetch('/api' + path, { ...options, headers: {'Content-Type':'application/json', ...options.headers} });
        let value; try { value = await response.json(); } catch { throw new Error(`Request failed (${response.status})`); }
        if (!response.ok || !value.success) throw new Error(value.error || value.message || `Request failed (${response.status})`);
        return value.data;
    },
    async track(id, root, finished) {
        const body = root.querySelector('[data-body]');
        while (root.isConnected) {
            try {
                const op = await this.api('/operations/' + encodeURIComponent(id));
                body.innerHTML = `<p class="mb-3">${this.escape(op.operation)}: <strong>${this.escape(op.status)}</strong></p><ol class="space-y-2">${op.steps.map((step, index) => `<li>${op.status === 'completed' || index < op.steps.length - 1 ? '✓' : '•'} ${this.escape(step)}</li>`).join('')}</ol>`;
                if (op.status === 'awaiting_approval') {
                    const preview=document.createElement('pre');preview.className='whitespace-pre-wrap text-xs my-3';preview.textContent=JSON.stringify(op.preview,null,2);body.append(preview);
                    body.innerHTML += `<p class="my-4">An AI agent requested this operation on server ${this.escape(op.server_id)}. Review the operation and target before approving.</p><button data-approve class="bg-blue-600 rounded px-4 py-2">Approve operation</button><button data-reject class="ui-button ui-button-secondary ml-2">Reject</button>`;
                    body.querySelector('[data-reject]').onclick=async()=>{try{await this.api('/operations/'+encodeURIComponent(id)+'/reject',{method:'POST',headers:{'X-GoPulse-Human-Approval':'true'}});await this.track(id,root,finished);}catch(e){root.querySelector('[data-error]').textContent=e.message;}};
                    body.querySelector('[data-approve]').onclick = async () => {
                        if (!await App.UI.confirm(`Approve ${op.operation} on ${op.server_id}?`)) return;
                        try { await this.api('/operations/' + id + '/approve', {method:'POST', headers:{'X-GoPulse-Human-Approval':'true'}}); await this.track(id, root, finished); } catch (e) { root.querySelector('[data-error]').textContent = e.message; }
                    }; return;
                }
                if (['completed','failed','interrupted','expired','rejected'].includes(op.status)) {
                    if (op.error) root.querySelector('[data-error]').textContent = op.error;
                    if (op.result) { const result = document.createElement('pre'); result.className = 'text-xs whitespace-pre-wrap mt-4'; result.textContent = JSON.stringify(op.result, null, 2); body.append(result); }
                    finished?.(op); return;
                }
            } catch (e) { root.querySelector('[data-error]').textContent = e.message; return; }
            await new Promise(resolve => setTimeout(resolve, 1500));
        }
    },
    async operations() {
        const root = this.dialog('Operations and approvals', '<p>Loading…</p>');
        try { const ops = await this.api('/operations'); const body = root.querySelector('[data-body]'); body.replaceChildren();
            for (const op of ops) { const button = document.createElement('button'); button.className = 'block text-left w-full border-b border-gray-700 py-3'; button.textContent = `${op.operation} · ${op.server_id} · ${op.status}`; button.onclick = () => this.track(op.operation_id, root); body.append(button); }
            if (!ops.length) body.textContent = 'No operations yet.';
        } catch(e) {root.querySelector('[data-error]').textContent = e.message;}
    }
};

App.Qdrant = {
    install() { App.Databases.openProvisionModal('qdrant'); },
    async manage(id) {
        const I=App.Infrastructure; const path=`/servers/${App.config.SERVER_ID}/databases/${encodeURIComponent(id)}`;
        const root=I.dialog('Manage Qdrant','<p>Loading authenticated status…</p>');
        try {
            const config=await I.api(path+'/qdrant/config');let data;try{data=await I.api(path+'/qdrant/diagnostics');}catch(e){root.querySelector('[data-error]').textContent=e.message;data={healthy:false,config,stats:'Unavailable',logs:'Unavailable',collections:null};} const body=root.querySelector('[data-body]');
            body.innerHTML=`<p>${data.healthy?'Authenticated connection verified':'Unavailable or starting'} · ${I.escape(data.config.version)}</p><p class="text-sm my-2">${I.escape(data.config.bind_address)}:${data.config.host_port} · ${I.escape(data.config.storage_path)}</p><div data-actions class="flex gap-3 my-4"></div><h4>Collections</h4><div data-collections class="my-3"></div><form class="space-y-2"><label>New collection<input name="name" required pattern="[A-Za-z0-9][A-Za-z0-9_-]{0,63}" class="block w-full bg-gray-900 p-2 rounded"></label><label>Vector dimensions<input name="size" type="number" value="384" min="1" max="65536" required class="block w-full bg-gray-900 p-2 rounded"></label><label>Distance<select name="distance" class="block bg-gray-900 p-2 rounded"><option>Cosine</option><option>Dot</option><option>Euclid</option><option>Manhattan</option></select></label><button class="bg-blue-600 rounded px-4 py-2">Create collection</button></form><details class="mt-4"><summary>Resource usage and logs</summary><pre data-stats class="text-xs whitespace-pre-wrap"></pre><pre data-logs class="text-xs whitespace-pre-wrap"></pre></details>`;
            body.querySelector('[data-stats]').textContent=data.stats;body.querySelector('[data-logs]').textContent=data.logs;
            for (const action of ['start','stop','restart']) {const button=document.createElement('button');button.className='bg-gray-700 rounded px-3 py-2';button.textContent=action;button.onclick=async()=>{if(action!=='start'&&!await App.UI.confirm(`${action} Qdrant?`))return;button.disabled=true;try{await I.api(path+'/'+action,{method:'POST'});await this.manage(id);}catch(e){root.querySelector('[data-error]').textContent=e.message;button.disabled=false;}};body.querySelector('[data-actions]').append(button);}
            const remove=document.createElement('button');remove.textContent='Remove service';remove.className='text-red-300';remove.onclick=async()=>{if(!await App.UI.confirm('Remove Qdrant? Persistent data will be kept.'))return;try{await I.api(path+'?confirm='+encodeURIComponent(id),{method:'DELETE'});root.remove();App.Databases.load();}catch(e){root.querySelector('[data-error]').textContent=e.message;}};body.querySelector('[data-actions]').append(remove);
            for(const collection of data.collections?.result?.collections || []) {const row=document.createElement('div');row.className='flex justify-between py-2';const info=document.createElement('button');info.textContent=collection.name;info.onclick=async()=>{try{const result=await I.api(path+'/qdrant/collections/'+encodeURIComponent(collection.name));const pre=document.createElement('pre');pre.className='text-xs whitespace-pre-wrap';pre.textContent=JSON.stringify(result,null,2);I.dialog('Collection '+collection.name,'').querySelector('[data-body]').append(pre);}catch(e){root.querySelector('[data-error]').textContent=e.message;}};const del=document.createElement('button');del.textContent='Delete';del.className='text-red-300';del.onclick=async()=>{if((await App.UI.prompt('Type the collection name to permanently delete it'))!==collection.name)return;try{await I.api(path+'/qdrant/collections/'+encodeURIComponent(collection.name)+'?confirm='+encodeURIComponent(collection.name),{method:'DELETE'});this.manage(id);}catch(e){root.querySelector('[data-error]').textContent=e.message;}};const snapshots=document.createElement('button');snapshots.textContent='Snapshots';snapshots.onclick=()=>this.snapshots(id,collection.name);row.append(info,snapshots,del);body.querySelector('[data-collections]').append(row);}
            body.querySelector('form').onsubmit=async event=>{event.preventDefault();const f=event.target;f.querySelector('button').disabled=true;try{await I.api(path+'/qdrant/collections',{method:'POST',body:JSON.stringify({name:f.elements.name.value,size:Number(f.elements.size.value),distance:f.elements.distance.value})});this.manage(id);}catch(e){root.querySelector('[data-error]').textContent=e.message;f.querySelector('button').disabled=false;}};
        } catch(e){root.querySelector('[data-error]').textContent=e.message;}
    }
};

App.Infrastructure.agentToken = function() { window.location.assign('/agents'); };

App.Qdrant.snapshots = async function(id, collection) {
    const I=App.Infrastructure, path=`/servers/${App.config.SERVER_ID}/databases/${encodeURIComponent(id)}/qdrant/collections/${encodeURIComponent(collection)}/snapshots`;
    const root=I.dialog('Snapshots: '+collection,'<p>Loading…</p>');
    try {const data=await I.api(path);const body=root.querySelector('[data-body]');body.replaceChildren();const create=document.createElement('button');create.textContent='Create snapshot';create.className='bg-blue-600 rounded px-4 py-2 mb-4';create.onclick=async()=>{create.disabled=true;try{await I.api(path,{method:'POST'});this.snapshots(id,collection);}catch(e){root.querySelector('[data-error]').textContent=e.message;create.disabled=false;}};body.append(create);
      for(const snapshot of data.result||[]){const row=document.createElement('div');row.className='flex justify-between py-3';const name=document.createElement('span');name.textContent=snapshot.name;const restore=document.createElement('button');restore.textContent='Restore';restore.className='text-red-300';restore.onclick=async()=>{if((await App.UI.prompt('Restoring replaces collection data. Type the collection name to confirm'))!==collection)return;try{const op=await I.api(path+'/restore?confirm='+encodeURIComponent(collection),{method:'POST',body:JSON.stringify({filename:snapshot.name})});await I.track(op.operation_id,root);}catch(e){root.querySelector('[data-error]').textContent=e.message;}};row.append(name,restore);body.append(row);}
    } catch(e){root.querySelector('[data-error]').textContent=e.message;}
};
