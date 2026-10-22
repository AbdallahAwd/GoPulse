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

App.Infrastructure.agentToken = function() { window.location.assign('/agents'); };
