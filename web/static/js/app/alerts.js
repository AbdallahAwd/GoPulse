// Server-scoped rules, maintenance silencing and durable delivery status.
App.Alerts = {
    rules: [], events: [], timer: null, generation: 0, initialized: false, loading: false, busy: false, editingID: null,
    el(id) { return document.getElementById(id); },
    canWrite() { return this.el('alerts-tab')?.dataset.canWrite === 'true'; },
    escape(value) { return String(value ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])); },
    date(value) { return !value || String(value).startsWith('0001-') ? '—' : new Date(value).toLocaleString(); },
    init() {
        if (this.initialized || !this.el('alerts-tab')) return;
        this.initialized = true;
        this.el('alerts-refresh').addEventListener('click', () => this.refresh());
        this.el('alerts-create')?.addEventListener('click', () => this.openEditor());
        this.el('alert-form').addEventListener('submit', event => { event.preventDefault(); this.save(); });
        for (const id of ['alert-close','alert-cancel']) this.el(id).addEventListener('click', () => this.closeEditor());
        this.el('alert-metric').addEventListener('change', () => this.metricChanged());
        this.el('alert-reveal').addEventListener('change', event => { this.el('alert-secret').type = event.target.checked ? 'text' : 'password'; });
        this.el('alert-editor').addEventListener('close', () => { this.el('alert-secret').value = ''; this.el('alert-webhook').value = ''; });
        this.el('alerts-rules').addEventListener('click', event => {
            const button = event.target.closest('button[data-action]');
            if (button) this.action(button.dataset.action, button.dataset.id);
        });
    },
    async load() {
        this.init();
        if (!this.timer) this.timer = setInterval(() => {
            if (!document.hidden && App.Navigation.current === 'alerts') this.refresh();
        }, 15000);
        await this.refresh();
    },
    deactivate() { if (this.timer) clearInterval(this.timer); this.timer = null; this.generation++; this.loading = false; this.closeEditor(); },
    async refresh() {
        if (this.loading) return;
        this.loading = true;
        const generation = ++this.generation;
        try {
            const [rules, events] = await Promise.all([App.API.get('/alerts'), App.API.get('/alerts/events')]);
            if (generation !== this.generation) return;
            this.rules = rules.data || []; this.events = events.data || [];
            this.el('alerts-error').textContent = '';
            this.render();
        } catch (error) { if (generation === this.generation) this.el('alerts-error').textContent = error.message; }
        finally { if (generation === this.generation) this.loading = false; }
    },
    deliveryLabel(status) {
        return ({pending:'Queued',retry:'Retry scheduled',delivering:'Sending',delivered:'Delivered',failed:'Failed',silenced:'Silenced',not_configured:'Dashboard only'})[status] || 'Unknown';
    },
    render() {
        const count = state => this.rules.filter(rule => rule.state === state).length;
        this.el('alerts-summary').textContent = `${count('firing')} firing · ${count('pending')} pending · ${count('unknown')} unknown · ${count('healthy')} healthy · ${count('disabled')} disabled`;
        this.el('alerts-rules').innerHTML = this.rules.map(rule => this.card(rule)).join('') || '<div class="monitor-empty"><i class="fas fa-bell" aria-hidden="true"></i><h3>No alert rules yet</h3><p>Create a rule to track resource usage or metrics availability.</p></div>';
        this.el('alerts-events').innerHTML = this.events.map(event => `<tr><td>${this.escape(this.date(event.occurred_at))}</td><td>${this.escape(event.rule_name)}</td><td>${this.escape(event.state)}</td><td>${this.escape(this.deliveryLabel(event.delivery_status))}${event.attempts ? ` · ${Number(event.attempts)} attempt(s)` : ''}${event.last_error ? `<small>${this.escape(event.last_error)}</small>` : ''}</td></tr>`).join('') || '<tr><td colspan="4">No incidents or test notifications recorded.</td></tr>';
    },
    card(rule) {
        const e = value => this.escape(value);
        const state = ['healthy','pending','firing','unknown','disabled'].includes(rule.state) ? rule.state : 'unknown';
        const names = {cpu:'CPU',memory:'Memory',disk:'Highest disk',availability:'Metrics availability'};
        const description = rule.metric === 'availability' ? 'Collection unavailable' : `${names[rule.metric] || rule.metric} above ${rule.threshold}%`;
        const silenced = new Date(rule.silenced_until).getTime() > Date.now();
        const value = rule.last_value == null ? 'Unknown' : rule.metric === 'availability' ? (rule.last_value === 0 ? 'Unavailable' : 'Available') : `${Number(rule.last_value).toFixed(1)}%`;
        const button = (action, label) => `<button class="console-button secondary" data-action="${action}" data-id="${e(rule.id)}" ${this.busy ? 'disabled' : ''}>${label}</button>`;
        const controls = this.canWrite() ? `<div class="monitor-actions">${button('edit','Edit')}${button('toggle',rule.enabled ? 'Disable' : 'Enable')}${button(silenced ? 'unsilence' : 'silence',silenced ? 'End silence' : 'Silence 1h')}${rule.has_webhook ? button('test','Test webhook') : ''}${button('delete','Delete')}</div>` : '';
        return `<article class="monitor-card"><header><h3>${e(rule.name)}</h3><span class="monitor-state ${state}">${state}</span></header><p>${e(description)} for ${Number(rule.duration_seconds)} seconds</p><p class="console-subtle">Latest: ${e(value)} · ${rule.has_webhook ? 'Webhook configured' : 'Dashboard only'}</p>${silenced ? `<p class="monitor-silenced">Notifications silenced until ${e(this.date(rule.silenced_until))}</p>` : ''}${controls}</article>`;
    },
    metricChanged() {
        const availability = this.el('alert-metric').value === 'availability';
        this.el('alert-threshold').disabled = availability;
        this.el('alert-threshold-hint').textContent = availability ? 'Fires when host metrics cannot be collected.' : 'Fires when usage stays strictly above this threshold.';
    },
    openEditor(rule = null) {
        if (!this.canWrite() || this.busy) return;
        this.editingID = rule?.id || null;
        this.el('alert-editor-title').textContent = rule ? 'Edit alert rule' : 'Create alert rule';
        this.el('alert-name').value = rule?.name || '';
        this.el('alert-metric').value = rule?.metric || 'disk';
        this.el('alert-threshold').value = rule?.threshold ?? 85;
        this.el('alert-duration').value = rule?.duration_seconds ?? 300;
        this.el('alert-enabled').checked = rule?.enabled ?? true;
        this.el('alert-webhook').value = rule?.webhook_url || '';
        this.el('alert-secret').value = rule?.webhook_secret || '';
        this.el('alert-secret').type = 'password'; this.el('alert-reveal').checked = false;
        this.el('alert-signing-key').classList.toggle('hidden', !rule?.webhook_secret);
        this.el('alert-form-error').textContent = '';
        this.metricChanged(); this.el('alert-editor').showModal(); this.el('alert-name').focus();
    },
    closeEditor() { this.el('alert-editor')?.close(); },
    input(rule) {
        return {name:rule.name,metric:rule.metric,threshold:rule.threshold,duration_seconds:rule.duration_seconds,enabled:rule.enabled,webhook_url:rule.webhook_url || ''};
    },
    async save() {
        if (!this.canWrite() || this.busy || !this.el('alert-form').reportValidity()) return;
        this.busy = true; this.el('alert-save').disabled = true;
        const input = {name:this.el('alert-name').value,metric:this.el('alert-metric').value,threshold:this.el('alert-metric').value === 'availability' ? 0 : Number(this.el('alert-threshold').value),duration_seconds:Number(this.el('alert-duration').value),enabled:this.el('alert-enabled').checked,webhook_url:this.el('alert-webhook').value};
        try {
            if (this.editingID) await App.API.put(`/alerts/${encodeURIComponent(this.editingID)}`,input);
            else await App.API.post('/alerts',input);
            this.closeEditor(); App.UI.toast('success',input.webhook_url ? 'Alert rule saved. Edit the rule to view its signing secret.' : 'Alert rule saved.');
            await this.refresh();
        } catch (error) { this.el('alert-form-error').textContent = error.message; }
        finally { this.busy = false; this.el('alert-save').disabled = false; this.render(); }
    },
    async action(action,id) {
        if (!this.canWrite() || this.busy) return;
        const rule = this.rules.find(item => item.id === id); if (!rule) return;
        if (action === 'edit') { this.openEditor(rule); return; }
        if (action === 'delete' && !window.confirm(`Delete “${rule.name}” and its event history?`)) return;
        this.busy = true; this.render();
        const path = `/alerts/${encodeURIComponent(id)}`;
        try {
            if (action === 'delete') await App.API.delete(path);
            else if (action === 'toggle') await App.API.put(path,{...this.input(rule),enabled:!rule.enabled});
            else if (action === 'silence' || action === 'unsilence') await App.API.put(`${path}/silence`,{seconds:action === 'silence' ? 3600 : 0});
            else if (action === 'test') { await App.API.post(`${path}/test`,{}); App.UI.toast('success','Test notification queued. Check delivery history for the result.'); }
            await this.refresh();
        } catch (error) { this.el('alerts-error').textContent = error.message; }
        finally { this.busy = false; this.render(); }
    }
};
