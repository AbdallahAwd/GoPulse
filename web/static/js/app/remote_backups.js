App.RemoteBackups = {
    plans: [], databases: [], destinations: [], artifacts: [], runs: [], selected: null,
    timer: null, initialized: false, generation: 0, loading: false, busy: false, editing: null,
    el(id) { return document.getElementById(id); },
    canWrite() { return this.el('remote-backups-tab')?.dataset.canWrite === 'true'; },
    escape(value) { return String(value ?? '').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])); },
    date(value) { return !value || String(value).startsWith('0001-') ? '—' : new Date(value).toLocaleString(); },
    bytes(value) { return value >= 1073741824 ? `${(value/1073741824).toFixed(2)} GiB` : value >= 1048576 ? `${(value/1048576).toFixed(1)} MiB` : `${Number(value).toLocaleString()} bytes`; },
    base(id) { return `/remote-backups/${encodeURIComponent(id)}`; },
    href(endpoint) { return `/api/servers/${encodeURIComponent(App.config.SERVER_ID)}${endpoint}`; },
    label(status) { return ({running:'Running',succeeded:'Succeeded',failed:'Failed',cancelled:'Cancelled',timed_out:'Timed out',interrupted:'Interrupted',skipped:'Skipped',cleanup_failed:'Cleanup needs attention',passed:'Passed',not_tested:'Not tested',pending:'Queued'})[status] || 'Unknown'; },
    phaseLabel(phase) { return ({starting:'Starting',dumping:'Creating database dump',encrypting_and_uploading:'Encrypting and uploading',restore_testing:'Testing recovery',retention:'Applying retention',complete:'Complete',skipped:'Skipped'})[phase] || ''; },
    init() {
        if (this.initialized || !this.el('remote-backups-tab')) return;
        this.initialized = true;
        this.el('rb-refresh').addEventListener('click',()=>this.refresh());
        this.el('rb-create')?.addEventListener('click',()=>this.openEditor());
        this.el('rb-form').addEventListener('submit',e=>{e.preventDefault();this.save();});
        for (const id of ['rb-editor-close','rb-editor-cancel']) this.el(id).addEventListener('click',()=>this.closeEditor());
        this.el('rb-detail-close').addEventListener('click',()=>{this.selected=null;this.el('rb-detail').classList.add('hidden');});
        for (const id of ['rb-frequency','rb-time','rb-weekday']) this.el(id).addEventListener('change',()=>this.buildSchedule());
        this.el('rb-schedule').addEventListener('input',()=>{this.el('rb-frequency').value='custom';this.scheduleFields();this.el('rb-preview').textContent='';});
        this.el('rb-schedule').addEventListener('change',()=>this.preview());
        this.el('rb-timezone').addEventListener('change',()=>this.preview());
        this.el('rb-plans').addEventListener('click',e=>{const button=e.target.closest('button[data-action]');if(button)this.action(button.dataset.action,button.dataset.id);});
        this.el('rb-artifacts').addEventListener('click',e=>{const button=e.target.closest('button[data-test]');if(button)this.action('test',this.selected,button.dataset.test);});
    },
    async load() {
        this.init();
        if (!this.timer) this.timer=setInterval(()=>{if(!document.hidden && App.Navigation.current==='remote-backups')this.refresh();},15000);
        await this.refresh();
    },
    deactivate() { if(this.timer)clearInterval(this.timer);this.timer=null;this.generation++;this.loading=false;this.closeEditor(); },
    async refresh() {
        if (this.loading) return;
        this.loading=true;const generation=++this.generation;
        try {
            const [plans,options]=await Promise.all([App.API.get('/remote-backups'),App.API.get('/remote-backups/options')]);
            if (generation!==this.generation) return;
            this.plans=plans.data || [];this.databases=options.data?.databases || [];this.destinations=options.data?.destinations || [];
            this.el('rb-error').textContent='';this.render();
            if(this.selected)await this.loadDetails(generation);
        } catch(error) {if(generation===this.generation)this.el('rb-error').textContent=error.message;}
        finally {if(generation===this.generation)this.loading=false;}
    },
    render() {
        this.el('rb-summary').textContent=`${this.plans.length} plans · ${this.plans.filter(p=>p.enabled).length} scheduled · ${this.plans.filter(p=>p.last_run?.status==='running').length} running`;
        this.el('rb-plans').innerHTML=this.plans.map(p=>this.card(p)).join('') || '<div class="monitor-empty"><i class="fas fa-cloud-arrow-up" aria-hidden="true"></i><h3>No remote backup plans yet</h3><p>Choose a database and a different destination server to keep an encrypted off-server copy.</p></div>';
    },
    card(plan) {
        const e=value=>this.escape(value), running=plan.last_run?.status==='running';
        const database=this.databases.find(d=>d.id===plan.database_id)?.name || plan.database_id;
        const destination=this.destinations.find(d=>d.id===plan.destination_id)?.name || plan.destination_id;
        const button=(action,label,disabled=false)=>`<button class="console-button secondary" data-action="${action}" data-id="${e(plan.id)}" ${disabled || this.busy?'disabled':''}>${label}</button>`;
        const write=this.canWrite() ? `${button('edit','Edit',running)}${button('run','Run backup',running)}${running?button('cancel','Cancel execution'):button('toggle',plan.enabled?'Pause schedule':'Enable schedule')}${!plan.enabled && !running?button('delete','Remove plan'):''}` : '';
        return `<article class="monitor-card"><header><h3>${e(plan.name)}</h3><span class="monitor-state ${running?'pending':plan.enabled?'healthy':'disabled'}">${running?'Running':plan.enabled?'Scheduled':'Paused'}</span></header><p>${e(database)} <span aria-hidden="true">→</span> ${e(destination)}</p><p class="console-subtle">${e(plan.schedule)} · ${e(plan.timezone)}<br>Next backup: ${e(this.date(plan.next_run_at))}<br>Retain ${Number(plan.retention_days)} days, keep at least ${Number(plan.keep_last)} copies · ${plan.auto_test?'Automatic restore tests':'Manual restore tests'}</p>${plan.last_run?`<p>${e(this.label(plan.last_run.status))} · ${e(this.phaseLabel(plan.last_run.phase))}</p>${plan.last_run.error?`<p class="rb-run-error">${e(plan.last_run.error)}</p>`:''}`:''}<div class="monitor-actions">${button('details','Backups & history')}${write}</div></article>`;
    },
    scheduleFields() {
        const frequency=this.el('rb-frequency').value;
        this.el('rb-time-field').classList.toggle('hidden',frequency==='custom');
        this.el('rb-weekday-field').classList.toggle('hidden',frequency!=='weekly');
    },
    buildSchedule() {
        const [hour,minute]=(this.el('rb-time').value || '02:00').split(':').map(Number);
        const frequency=this.el('rb-frequency').value;
        if(frequency==='hourly')this.el('rb-schedule').value=`${minute} * * * *`;
        if(frequency==='daily')this.el('rb-schedule').value=`${minute} ${hour} * * *`;
        if(frequency==='weekly')this.el('rb-schedule').value=`${minute} ${hour} * * ${this.el('rb-weekday').value}`;
        this.scheduleFields();this.preview();
    },
    async preview() {
        const schedule=this.el('rb-schedule').value, timezone=this.el('rb-timezone').value;
        this.el('rb-preview').textContent='Checking upcoming backups…';
        try {
            const response=await App.API.get(`/remote-backups/preview?schedule=${encodeURIComponent(schedule)}&timezone=${encodeURIComponent(timezone)}`);
            if(this.el('rb-schedule').value!==schedule || this.el('rb-timezone').value!==timezone)return;
            this.el('rb-preview').textContent='Next: '+(response.data.next_runs || []).slice(0,3).map(date=>new Date(date).toLocaleString([], {timeZone:timezone})).join(' · ');
        } catch(error) {this.el('rb-preview').textContent=error.message;}
    },
    openEditor(plan=null) {
        if(!this.canWrite() || this.busy)return;
        if(!this.databases.length || !this.destinations.length){this.el('rb-error').textContent='Add a supported database and connect a different destination server before creating a plan.';return;}
        this.editing=plan?.id || null;
        this.el('rb-editor-title').textContent=plan?'Edit backup plan':'Create backup plan';
        this.el('rb-database').innerHTML=this.databases.map(d=>`<option value="${this.escape(d.id)}">${this.escape(d.name)} (${this.escape(d.engine)})</option>`).join('');
        this.el('rb-destination').innerHTML=this.destinations.map(d=>`<option value="${this.escape(d.id)}">${this.escape(d.name)}</option>`).join('');
        this.el('rb-name').value=plan?.name || '';
        this.el('rb-database').value=plan?.database_id || this.databases[0].id;
        this.el('rb-destination').value=plan?.destination_id || this.destinations[0].id;
        this.el('rb-directory').value=plan?.directory || '/srv/gopulse-backups';
        for(const id of ['rb-database','rb-destination','rb-directory'])this.el(id).disabled=!!plan;
        this.el('rb-timezone').value=plan?.timezone || Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
        this.el('rb-schedule').value=plan?.schedule || '0 2 * * *';
        this.el('rb-frequency').value=plan?'custom':'daily';this.el('rb-time').value='02:00';this.el('rb-weekday').value='0';this.scheduleFields();
        this.el('rb-retention').value=plan?.retention_days ?? 30;this.el('rb-keep').value=plan?.keep_last ?? 3;
        this.el('rb-timeout').value=plan?.timeout_seconds ?? 3600;this.el('rb-size').value=plan?.max_size_gb ?? 10;this.el('rb-memory').value=plan?.test_memory_mb ?? 1024;
        this.el('rb-enabled').checked=plan?.enabled ?? false;this.el('rb-auto-test').checked=plan?.auto_test ?? true;
        this.el('rb-form-error').textContent='';this.el('rb-editor').showModal();this.el('rb-name').focus();this.preview();
    },
    closeEditor() {this.el('rb-editor')?.close();},
    input(plan) {return {name:plan.name,database_id:plan.database_id,destination_id:plan.destination_id,directory:plan.directory,schedule:plan.schedule,timezone:plan.timezone,retention_days:plan.retention_days,keep_last:plan.keep_last,enabled:plan.enabled,auto_test:plan.auto_test,timeout_seconds:plan.timeout_seconds,max_size_gb:plan.max_size_gb,test_memory_mb:plan.test_memory_mb};},
    async save() {
        if(!this.canWrite() || this.busy || !this.el('rb-form').reportValidity())return;
        this.busy=true;this.el('rb-save').disabled=true;
        const input={name:this.el('rb-name').value,database_id:this.el('rb-database').value,destination_id:this.el('rb-destination').value,directory:this.el('rb-directory').value,schedule:this.el('rb-schedule').value,timezone:this.el('rb-timezone').value,retention_days:Number(this.el('rb-retention').value),keep_last:Number(this.el('rb-keep').value),enabled:this.el('rb-enabled').checked,auto_test:this.el('rb-auto-test').checked,timeout_seconds:Number(this.el('rb-timeout').value),max_size_gb:Number(this.el('rb-size').value),test_memory_mb:Number(this.el('rb-memory').value)};
        try {
            const result=this.editing?await App.API.put(this.base(this.editing),input):await App.API.post('/remote-backups',input);
            this.selected=result.data.id;this.closeEditor();App.UI.toast('success','Backup plan saved. Download its recovery key and keep a separate safe copy.');await this.refresh();
        } catch(error) {this.el('rb-form-error').textContent=error.message;}
        finally {this.busy=false;this.el('rb-save').disabled=false;this.render();}
    },
    async loadDetails(generation=this.generation) {
        const id=this.selected, plan=this.plans.find(p=>p.id===id);
        if(!plan){this.selected=null;this.el('rb-detail').classList.add('hidden');return;}
        try {
            const [artifacts,runs]=await Promise.all([App.API.get(this.base(id)+'/artifacts'),App.API.get(this.base(id)+'/runs')]);
            if(generation!==this.generation || id!==this.selected)return;
            this.artifacts=artifacts.data || [];this.runs=runs.data || [];
            this.el('rb-detail').classList.remove('hidden');this.el('rb-detail-title').textContent=plan.name;
            this.el('rb-detail-subtitle').textContent=`Encrypted copies in ${plan.directory} · ${plan.auto_test?'Each new backup is automatically restore-tested.':'Restore tests can be started manually.'}`;
            this.el('rb-detail-key').href=this.href(this.base(id)+'/recovery-key');
            this.el('rb-detail-error').textContent='';this.renderDetails();
        } catch(error) {if(generation===this.generation)this.el('rb-detail-error').textContent=error.message;}
    },
    renderDetails() {
        const e=value=>this.escape(value), plan=this.plans.find(p=>p.id===this.selected), running=plan?.last_run?.status==='running';
        this.el('rb-artifacts').innerHTML=this.artifacts.map(a=>`<tr><td>${e(this.date(a.created_at))}<details class="rb-file-details"><summary>Storage details</summary><code>${e(a.path)}</code><code>SHA-256: ${e(a.sha256)}</code></details></td><td>${e(this.bytes(a.size))}${a.pruned?'<small>Removed by retention</small>':''}</td><td>${e(this.label(a.test_status))}${a.test_summary?`<small>${e(a.test_summary)}</small>`:''}${a.test_error?`<small>${e(a.test_error)}</small>`:''}${a.tested_at&&!String(a.tested_at).startsWith('0001-')?`<small>${e(this.date(a.tested_at))}</small>`:''}</td><td>${this.canWrite()&&!a.pruned?`<div class="monitor-actions"><a class="console-button secondary" href="${e(this.href(this.base(this.selected)+'/artifacts/'+encodeURIComponent(a.id)+'/download'))}">Download encrypted</a><button class="console-button secondary" data-test="${e(a.id)}" ${running||this.busy?'disabled':''}>Test restore</button></div>`:'—'}</td></tr>`).join('') || '<tr><td colspan="4">No encrypted backups yet. Run the plan to create and verify the first copy.</td></tr>';
        this.el('rb-runs').innerHTML=this.runs.map(r=>`<tr><td>${e(this.date(r.started_at))}</td><td>${r.kind==='restore_test'?'Restore test':'Backup'}<small>${r.trigger==='scheduled'?'Scheduled':'Manual'}</small></td><td>${e(this.label(r.status))}${r.needs_cleanup?'<small>Cleanup pending</small>':''}</td><td>${e(this.phaseLabel(r.phase))}${r.error?`<small>${e(r.error)}</small>`:''}</td></tr>`).join('') || '<tr><td colspan="4">No executions recorded.</td></tr>';
    },
    async action(action,id,artifactID='') {
        if(this.busy)return;
        const plan=this.plans.find(p=>p.id===id);if(!plan)return;
        if(action==='details'){this.selected=id;await this.loadDetails();return;}
        if(!this.canWrite())return;
        if(action==='edit'){this.openEditor(plan);return;}
        if(action==='delete'&&!window.confirm(`Remove “${plan.name}” and its dashboard history? Encrypted files and the recovery key remain on disk. Download the recovery key before removing the plan.`))return;
        this.busy=true;this.render();
        try {
            if(action==='toggle')await App.API.put(this.base(id),{...this.input(plan),enabled:!plan.enabled});
            else if(action==='run')await App.API.post(this.base(id)+'/run',{});
            else if(action==='cancel')await App.API.post(this.base(id)+'/cancel',{});
            else if(action==='test')await App.API.post(this.base(id)+'/artifacts/'+encodeURIComponent(artifactID)+'/test',{});
            else if(action==='delete'){await App.API.delete(this.base(id));if(this.selected===id){this.selected=null;this.el('rb-detail').classList.add('hidden');}}
            await this.refresh();
        } catch(error) {this.el('rb-error').textContent=error.message;}
        finally {this.busy=false;this.render();if(this.selected)this.renderDetails();}
    }
};
