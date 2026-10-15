/* GoPulse-managed server schedules. Viewing this screen performs only reads. */
App.Cron = {
    jobs: [], initialized: false, loading: false, saving: false, editID: null,
    historyID: null, runID: null, offset: 0, historyGeneration: 0, previewGeneration: 0,
    signature: null, pending: new Set(), timer: null, previewTimer: null,
    historySignature: null,
    frequency: 'daily',

    el(id) { return document.getElementById(id); },
    escape(value) { return String(value ?? '').replace(/[&<>"']/g, ch => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[ch])); },
    canWrite() { return this.el('cron-tab')?.dataset.canWrite === 'true'; },
    endpoint(id = '', suffix = '') { return '/cron' + (id ? '/' + encodeURIComponent(id) : '') + suffix; },
    date(value, timezone) {
        const date = new Date(value);
        if (!value || !Number.isFinite(date.getTime()) || date.getUTCFullYear() < 1970) return '—';
        try { return date.toLocaleString(undefined, {timeZone: timezone || undefined, month:'short', day:'numeric', hour:'2-digit', minute:'2-digit', timeZoneName:'short'}); }
        catch { return date.toLocaleString(); }
    },
    status(run) { return ({succeeded:'Succeeded',failed:'Failed',running:'Running',timed_out:'Timed out',cancelled:'Cancelled',interrupted:'Interrupted',skipped:'Skipped'})[run?.status] || 'Never run'; },
    badge(run) { const state = ['succeeded','failed','running','timed_out','cancelled','interrupted','skipped'].includes(run?.status) ? run.status : 'none'; return `<span class="cron-status" data-status="${state}">${this.status(run)}</span>`; },
    scheduleLabel(expression) {
        const [minute,hour,day,month,weekday] = expression.trim().split(/\s+/);
        const weekdays = ['Sunday','Monday','Tuesday','Wednesday','Thursday','Friday','Saturday'];
        if (hour === '*' && day === '*' && month === '*' && weekday === '*') {
            if (minute === '*' || minute === '*/1') return 'Every minute';
            if (/^\*\/(5|10|15|20|30)$/.test(minute)) return `Every ${minute.slice(2)} minutes`;
            if (/^\d+$/.test(minute) && Number(minute) < 60) return `Hourly at :${minute.padStart(2,'0')}`;
        }
        if (/^\d+$/.test(minute) && Number(minute) < 60 && /^\d+$/.test(hour) && Number(hour) < 24 && month === '*') {
            const time = `${hour.padStart(2,'0')}:${minute.padStart(2,'0')}`;
            if (day === '*' && weekday === '*') return `Daily at ${time}`;
            if (day === '*' && /^[0-6]$/.test(weekday)) return `${weekdays[Number(weekday)]} at ${time}`;
            if (/^\d+$/.test(day) && weekday === '*') return `Day ${day} at ${time}`;
        }
        return expression;
    },
    matches(job, query, filter) {
        if (!`${job.name} ${job.command} ${job.schedule}`.toLowerCase().includes(query.trim().toLowerCase())) return false;
        return filter === 'all' || (filter === 'enabled' && job.enabled) || (filter === 'paused' && !job.enabled) || (filter === 'running' && job.last_run?.status === 'running') || (filter === 'failed' && ['failed','timed_out','interrupted'].includes(job.last_run?.status));
    },
    init() {
        if (this.initialized || !this.el('cron-tab')) return;
        this.initialized = true;
        this.el('cron-refresh').addEventListener('click', () => this.load());
        this.el('cron-create')?.addEventListener('click', () => this.openEditor());
        this.el('cron-search').addEventListener('input', () => this.render());
        this.el('cron-filter').addEventListener('change', () => this.render());
        this.el('cron-jobs').addEventListener('click', event => {
            const button = event.target.closest('button[data-action]');
            if (button && !button.disabled) this.action(button.dataset.action, button.dataset.id);
        });
        this.el('cron-form').addEventListener('submit', event => { event.preventDefault(); this.save(); });
        this.el('cron-frequency-options').addEventListener('click', event => { const button = event.target.closest('button[data-frequency]'); if (button) this.setFrequency(button.dataset.frequency); });
        for (const id of ['cron-time','cron-interval','cron-hour-minute','cron-weekday','cron-monthday']) this.el(id).addEventListener('input', () => this.updateSchedule());
        this.el('cron-schedule').addEventListener('input', () => { this.frequency = 'custom'; this.syncFrequency(); this.updateSummary(); this.queuePreview(); });
        this.el('cron-timezone').addEventListener('input', () => { this.updateSummary(); this.queuePreview(); });
        this.el('cron-preview-refresh').addEventListener('click', () => this.preview());
        this.el('cron-editor').addEventListener('close', () => { ++this.previewGeneration; clearTimeout(this.previewTimer); });
        this.el('cron-history').addEventListener('close', () => { this.historyID = null; this.runID = null; ++this.historyGeneration; });
        this.el('cron-history-refresh').addEventListener('click', () => this.loadHistory());
        this.el('cron-runs').addEventListener('click', event => { const button = event.target.closest('button[data-run-id]'); if (button) this.inspectRun(button.dataset.runId); });
        this.el('cron-runs-prev').addEventListener('click', () => { this.offset = Math.max(0, this.offset - 25); this.loadHistory(); });
        this.el('cron-runs-next').addEventListener('click', () => { this.offset += 25; this.loadHistory(); });
        this.el('cron-copy-output').addEventListener('click', () => this.copyOutput());
        this.timer = setInterval(() => {
            if (document.hidden || App.Navigation.current !== 'cron') return;
            this.load();
            if (this.historyID && this.el('cron-history').open) this.loadHistory();
        }, 5000);
    },
    async load() {
        this.init();
        if (this.loading) return;
        this.loading = true;
        this.el('cron-refresh').disabled = true;
        try {
            const response = await App.API.get(this.endpoint());
            this.jobs = response.data || [];
            this.el('cron-error').textContent = '';
            const signature = JSON.stringify(this.jobs);
            if (signature !== this.signature) { this.signature = signature; this.render(); }
        } catch (error) { this.el('cron-error').textContent = `Unable to load jobs: ${error.message}`; }
        finally { this.loading = false; this.el('cron-refresh').disabled = false; }
    },
    render() {
        const jobs = this.jobs.filter(job => this.matches(job, this.el('cron-search').value, this.el('cron-filter').value));
        const counts = [this.jobs.length, this.jobs.filter(j => j.enabled).length, this.jobs.filter(j => j.last_run?.status === 'running').length, this.jobs.filter(j => ['failed','timed_out','interrupted'].includes(j.last_run?.status)).length];
        this.el('cron-summary').innerHTML = ['Total jobs','Enabled','Running','Last run failed'].map((label,i) => `<span><strong>${counts[i]}</strong>${label}</span>`).join('');
        const active = document.activeElement;
        const focus = active?.dataset.action ? {id:active.dataset.id,action:active.dataset.action} : null;
        this.el('cron-jobs').innerHTML = jobs.length ? jobs.map(job => this.row(job)).join('') : `<tr><td colspan="5" class="cron-empty"><i class="fas fa-clock" aria-hidden="true"></i><h3>${this.jobs.length ? 'No matching jobs' : 'No scheduled jobs yet'}</h3><p>${this.jobs.length ? 'Try another search or status filter.' : 'Create a recurring command to automate work on this server.'}</p></td></tr>`;
        if (focus) Array.from(this.el('cron-jobs').querySelectorAll('button[data-action]')).find(button => button.dataset.id === focus.id && button.dataset.action === focus.action)?.focus();
    },
    row(job) {
        const escape = value => this.escape(value);
        const running = job.last_run?.status === 'running';
        const button = (action,label,icon) => `<button type="button" data-action="${action}" data-id="${escape(job.id)}" class="cron-row-button" ${this.pending.has(job.id) ? 'disabled' : ''} aria-label="${label} ${escape(job.name)}"><i class="fas ${icon}" aria-hidden="true"></i>${label}</button>`;
        return `<tr><td class="cron-job-name"><div><strong>${escape(job.name)}</strong><span class="cron-state-label">${job.enabled ? 'Enabled' : 'Paused'}</span></div><code title="${escape(job.command)}">${escape(job.command.replace(/\s+/g,' '))}</code>${job.working_dir ? `<small>${escape(job.working_dir)}</small>` : ''}</td><td><span title="${escape(job.schedule)}">${escape(this.scheduleLabel(job.schedule))}</span><small>${escape(job.timezone)}</small></td><td>${job.enabled ? escape(this.date(job.next_run_at,job.timezone)) : 'Paused'}</td><td>${this.badge(job.last_run)}<small>${job.last_run ? escape(this.date(job.last_run.started_at,job.timezone)) : 'No execution history'}</small></td><td><div class="cron-row-actions">${button('history','History','fa-clock-rotate-left')}${this.canWrite() ? button('edit','Edit','fa-pen') + `<details class="cron-row-menu"><summary aria-label="Manage ${escape(job.name)}">Manage<i class="fas fa-chevron-down" aria-hidden="true"></i></summary><div>${button(job.enabled ? 'pause' : 'resume',job.enabled ? 'Pause' : 'Resume',job.enabled ? 'fa-pause' : 'fa-play')}${button(running ? 'cancel' : 'run',running ? 'Cancel run' : 'Run once',running ? 'fa-ban' : 'fa-arrow-right')}${running ? '' : button('delete','Delete','fa-trash')}</div></details>` : ''}</div></td></tr>`;
    },
    async action(action, id) {
        const job = this.jobs.find(job => job.id === id);
        if (!job || this.pending.has(id)) return;
        if (action === 'history') { this.openHistory(job); return; }
        if (!this.canWrite()) return;
        if (action === 'edit') { this.openEditor(job); return; }
        if (!['pause','resume','run','cancel','delete'].includes(action)) return;
        this.pending.add(id); this.render();
        try {
            if (action === 'run' && !await App.UI.confirm(`Run “${job.name}” once on this server now? This executes the saved shell command even when its schedule is paused.`, {title:'Run job once',confirmLabel:'Run once',danger:false})) return;
            if (action === 'delete' && !await App.UI.confirm(`Delete “${job.name}” and its execution history?`, {title:'Delete cron job',confirmLabel:'Delete job'})) return;
            if (action === 'cancel' && !await App.UI.confirm(`Request cancellation of “${job.name}”? For remote commands, verify the outcome before retrying.`, {title:'Cancel execution',confirmLabel:'Cancel run'})) return;
            if (action === 'pause' || action === 'resume') await App.API.put(this.endpoint(id,'/enabled'), {enabled:action === 'resume'});
            if (action === 'run') await App.API.post(this.endpoint(id,'/run'), {});
            if (action === 'cancel') await App.API.post(this.endpoint(id,'/cancel'), {});
            if (action === 'delete') await App.API.delete(this.endpoint(id));
            App.UI.toast('success', ({pause:'Schedule paused. Active runs continue.',resume:'Schedule enabled.',run:'Execution started.',cancel:'Cancellation requested.',delete:'Job deleted.'})[action]);
            await this.load();
            if (action === 'run') this.openHistory(job);
        } catch (error) { this.el('cron-error').textContent = error.message; }
        finally { this.pending.delete(id); this.render(); }
    },
    openEditor(job = null) {
        if (!this.canWrite() || this.saving) return;
        this.editID = job?.id || null;
        this.el('cron-editor-title').textContent = job ? 'Edit cron job' : 'Create cron job';
        this.el('cron-save').textContent = job ? 'Save changes' : 'Create job';
        this.el('cron-form-error').textContent = '';
        this.el('cron-name').value = job?.name || '';
        this.el('cron-command').value = job?.command || '';
        this.el('cron-directory').value = job?.working_dir || '';
        this.el('cron-timeout').value = job?.timeout_seconds || 300;
        this.el('cron-schedule').value = job?.schedule || '0 0 * * *';
        this.el('cron-timezone').value = job?.timezone || Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
        this.el('cron-enabled').checked = job?.enabled ?? true;
        this.restoreSchedule(job?.schedule || '0 0 * * *');
        if (!this.el('cron-editor').open) this.el('cron-editor').showModal();
        this.el('cron-name').focus();
        this.preview();
    },
    closeEditor() { if (!this.saving) this.el('cron-editor').close(); },
    setFrequency(value) {
        if (!['minutes','hourly','daily','weekly','monthly','custom'].includes(value)) return;
        this.frequency = value;
        this.syncFrequency();
        this.updateSchedule();
    },
    syncFrequency() {
        this.el('cron-frequency-options').querySelectorAll('button[data-frequency]').forEach(button => button.setAttribute('aria-pressed', String(button.dataset.frequency === this.frequency)));
        const fields = {'cron-interval-field':['minutes'], 'cron-hour-minute-field':['hourly'], 'cron-weekday-field':['weekly'], 'cron-monthday-field':['monthly'], 'cron-time-field':['daily','weekly','monthly']};
        for (const [id,modes] of Object.entries(fields)) this.el(id).classList.toggle('hidden', !modes.includes(this.frequency));
        this.el('cron-time').required = ['daily','weekly','monthly'].includes(this.frequency);
        this.el('cron-hour-minute').disabled = this.frequency !== 'hourly';
        this.el('cron-hour-minute').required = this.frequency === 'hourly';
        this.el('cron-monthday').disabled = this.frequency !== 'monthly';
        this.el('cron-monthday').required = this.frequency === 'monthly';
        this.el('cron-advanced-schedule').open = this.frequency === 'custom';
    },
    restoreSchedule(expression) {
        this.el('cron-interval').value = '5'; this.el('cron-hour-minute').value = '0';
        this.el('cron-time').value = '00:00'; this.el('cron-weekday').value = '1'; this.el('cron-monthday').value = '1';
        this.frequency = 'custom';
        const [minute,hour,day,month,weekday] = expression.trim().split(/\s+/);
        const isMinute = /^\d+$/.test(minute) && Number(minute) < 60;
        const isHour = /^\d+$/.test(hour) && Number(hour) < 24;
        if (day === '*' && month === '*' && weekday === '*' && hour === '*' && /^\*\/(1|5|10|15|20|30)$/.test(minute)) { this.frequency = 'minutes'; this.el('cron-interval').value = minute.slice(2); }
        else if (minute === '*' && hour === '*' && day === '*' && month === '*' && weekday === '*') { this.frequency = 'minutes'; this.el('cron-interval').value = '1'; }
        else if (isMinute && hour === '*' && day === '*' && month === '*' && weekday === '*') { this.frequency = 'hourly'; this.el('cron-hour-minute').value = minute; }
        else if (isMinute && isHour && month === '*') {
            this.el('cron-time').value = `${hour.padStart(2,'0')}:${minute.padStart(2,'0')}`;
            if (day === '*' && weekday === '*') this.frequency = 'daily';
            else if (day === '*' && /^[0-6]$/.test(weekday)) { this.frequency = 'weekly'; this.el('cron-weekday').value = weekday; }
            else if (/^\d+$/.test(day) && Number(day) >= 1 && Number(day) <= 31 && weekday === '*') { this.frequency = 'monthly'; this.el('cron-monthday').value = day; }
        }
        // Preserve the exact stored expression on edit, including advanced rules.
        this.el('cron-schedule').value = expression;
        this.syncFrequency(); this.updateSummary();
    },
    updateSchedule() {
        const [hour,minute] = this.el('cron-time').value.split(':').map(Number);
        let expression;
        if (this.frequency === 'minutes') { const interval = Number(this.el('cron-interval').value); expression = interval === 1 ? '* * * * *' : `*/${interval} * * * *`; }
        if (this.frequency === 'hourly') expression = `${Number(this.el('cron-hour-minute').value)} * * * *`;
        if (this.frequency === 'daily') expression = `${minute} ${hour} * * *`;
        if (this.frequency === 'weekly') expression = `${minute} ${hour} * * ${this.el('cron-weekday').value}`;
        if (this.frequency === 'monthly') expression = `${minute} ${hour} ${Number(this.el('cron-monthday').value)} * *`;
        if (expression) this.el('cron-schedule').value = expression;
        this.updateSummary(); this.queuePreview();
    },
    updateSummary() {
        const time = this.el('cron-time').value || 'a time you choose';
        const weekdays = ['Sunday','Monday','Tuesday','Wednesday','Thursday','Friday','Saturday'];
        const descriptions = {minutes:`Every ${this.el('cron-interval').value} minute${this.el('cron-interval').value === '1' ? '' : 's'}`,hourly:`Every hour at minute ${this.el('cron-hour-minute').value}`,daily:`Every day at ${time}`,weekly:`Every ${weekdays[Number(this.el('cron-weekday').value)]} at ${time}`,monthly:`On day ${this.el('cron-monthday').value} of each month at ${time}`,custom:`Custom schedule: ${this.el('cron-schedule').value}`};
        this.el('cron-schedule-summary').textContent = `${descriptions[this.frequency]} · ${this.el('cron-timezone').value.trim() || 'choose a timezone'}`;
    },
    queuePreview() { ++this.previewGeneration; clearTimeout(this.previewTimer); this.el('cron-preview').textContent = 'Updating upcoming runs…'; this.previewTimer = setTimeout(() => this.preview(), 450); },
    async preview() {
        clearTimeout(this.previewTimer);
        const generation = ++this.previewGeneration;
        const timezone = this.el('cron-timezone').value.trim();
        const params = new URLSearchParams({schedule:this.el('cron-schedule').value.trim(),timezone});
        this.el('cron-preview').textContent = 'Checking schedule…';
        try {
            // Invalid expressions are inline form feedback, not global API errors.
            const response = await fetch(App.config.API_BASE + App.API.url(this.endpoint('', '/preview')) + '?' + params);
            const value = await response.json();
            if (generation !== this.previewGeneration || !this.el('cron-editor').open) return;
            if (!response.ok || !value.success) throw new Error(value.error || 'Unable to validate this schedule.');
            this.el('cron-preview').innerHTML = value.data.next_runs.map(date => `<li>${this.escape(this.date(date,timezone))}</li>`).join('');
        } catch (error) { if (generation === this.previewGeneration && this.el('cron-editor').open) this.el('cron-preview').textContent = error.message; }
    },
    payload() {
        return {name:this.el('cron-name').value.trim(),schedule:this.el('cron-schedule').value.trim(),timezone:this.el('cron-timezone').value.trim(),command:this.el('cron-command').value.trim(),working_dir:this.el('cron-directory').value.trim(),timeout_seconds:Number(this.el('cron-timeout').value),enabled:this.el('cron-enabled').checked};
    },
    async save() {
        if (this.saving || !this.canWrite() || !this.el('cron-form').reportValidity()) return;
        this.saving = true; this.el('cron-save').disabled = true;
        const editing = this.editID;
        try {
            const payload = this.payload();
            if (editing) await App.API.put(this.endpoint(editing),payload);
            else await App.API.post(this.endpoint(),payload);
            this.el('cron-editor').close();
            App.UI.toast('success',editing ? 'Job updated. Active runs keep their original command.' : 'Cron job created.');
            await this.load();
        } catch (error) { this.el('cron-form-error').textContent = error.message; }
        finally { this.saving = false; this.el('cron-save').disabled = false; }
    },
    openHistory(job) {
        this.historyID = job.id; this.runID = null; this.offset = 0;
        this.historySignature = null;
        this.el('cron-history-subtitle').textContent = `${job.name} · ${job.timezone}`;
        this.el('cron-run-detail').classList.add('hidden');
        this.el('cron-history').showModal();
        this.loadHistory();
    },
    closeHistory() { this.el('cron-history').close(); },
    async loadHistory() {
        if (!this.historyID) return;
        const id = this.historyID, generation = ++this.historyGeneration;
        this.el('cron-history-refresh').disabled = true;
        try {
            const response = await App.API.get(this.endpoint(id, `/runs?limit=25&offset=${this.offset}`));
            if (generation !== this.historyGeneration || id !== this.historyID) return;
            const runs = response.data || [];
            this.el('cron-history-error').textContent = '';
            const signature = JSON.stringify({id,offset:this.offset,runs});
            if (signature !== this.historySignature) {
                const focusedRun = document.activeElement?.dataset.runId;
                this.historySignature = signature;
                this.el('cron-runs').innerHTML = runs.length ? runs.map(run => `<tr><td>${this.escape(this.date(run.started_at))}</td><td>${this.badge(run)}</td><td>${this.escape(run.trigger)}<small>${this.escape(run.actor)}</small></td><td>${run.status === 'running' ? 'In progress' : `${(run.duration_ms / 1000).toFixed(1)}s`}</td><td><button class="cron-row-button" data-run-id="${this.escape(run.id)}">Inspect<i class="fas fa-arrow-right" aria-hidden="true"></i></button></td></tr>`).join('') : '<tr><td colspan="5" class="cron-empty">No executions on this page.</td></tr>';
                if (focusedRun) Array.from(this.el('cron-runs').querySelectorAll('button[data-run-id]')).find(button => button.dataset.runId === focusedRun)?.focus();
            }
            this.el('cron-runs-prev').disabled = this.offset === 0;
            this.el('cron-runs-next').disabled = runs.length < 25 || this.offset >= 75;
            this.el('cron-runs-page').textContent = `Page ${this.offset / 25 + 1}`;
            if (this.runID) await this.inspectRun(this.runID);
        } catch (error) { if (generation === this.historyGeneration) this.el('cron-history-error').textContent = error.message; }
        finally { if (generation === this.historyGeneration) this.el('cron-history-refresh').disabled = false; }
    },
    async inspectRun(runID) {
        const jobID = this.historyID;
        this.runID = runID;
        try {
            const response = await App.API.get(this.endpoint(jobID, '/runs/' + encodeURIComponent(runID)));
            if (jobID !== this.historyID || runID !== this.runID || !this.el('cron-history').open) return;
            const run = response.data;
            this.el('cron-run-detail').classList.remove('hidden');
            this.el('cron-run-command').textContent = run.command;
            this.el('cron-run-meta').textContent = `${this.status(run)} · ${this.date(run.started_at)} · Exit code ${run.exit_code ?? '—'}`;
            this.el('cron-run-error').textContent = run.error || '';
            this.el('cron-run-output').textContent = run.output || (run.status === 'running' ? 'Command is running. Output is available when it completes.' : 'This execution produced no output.');
            this.output = run.output || '';
            this.el('cron-output-note').textContent = run.output_truncated ? 'Output truncated at 64 KiB.' : 'Combined stdout and stderr. Times in this history use your browser timezone.';
        } catch (error) { if (jobID === this.historyID) this.el('cron-history-error').textContent = error.message; }
    },
    async copyOutput() {
        try {
            if (navigator.clipboard && window.isSecureContext) await navigator.clipboard.writeText(this.output || '');
            else {
                const field = document.createElement('textarea'); field.value = this.output || ''; field.style.position = 'fixed'; field.style.opacity = '0';
                this.el('cron-history').append(field); field.select();
                try { if (!document.execCommand('copy')) throw new Error('Clipboard unavailable. Select and copy the output.'); }
                finally { field.remove(); this.el('cron-copy-output').focus(); }
            }
            App.UI.toast('success','Output copied.');
        } catch (error) { App.UI.toast('error',error.message); }
    },
    deactivate() {
        this.el('cron-editor')?.close(); this.el('cron-history')?.close();
    }
};
