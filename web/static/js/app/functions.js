App.Functions = {
 functions: [], runs: [], versions: [], selected: null, selectedRun: null, editing: null,
 initialized: false, timer: null, generation: 0, epoch: 0, loading: false, busy: false,
 el(id) { return document.getElementById(id); },
 escape(value) {return String(value ?? '').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));},
 canWrite() {return this.el('functions-tab')?.dataset.canWrite==='true';},
 base(id) {return `/functions/${encodeURIComponent(id)}`;},
 date(value) {return !value || String(value).startsWith('0001-')?'—':new Date(value).toLocaleString();},
 status(value) {return ({running:'Running',succeeded:'Succeeded',failed:'Failed',cancelled:'Cancelled',interrupted:'Interrupted',timed_out:'Timed out / killed',skipped:'Skipped'})[value] || 'Unknown';},
 example(runtime) {return runtime==='javascript'?`module.exports.handler = async (event, context) => {\n  console.log('Invocation:', context.invocation_id);\n  return { message: 'Hello, ' + (event.name || 'World') + '!' };\n};\n`:`def handler(event, context):\n    print('Invocation:', context['invocation_id'])\n    return {'message': 'Hello, ' + event.get('name', 'World') + '!'}\n`;},
 init() {
  if(this.initialized || !this.el('functions-tab'))return;this.initialized=true;
  this.el('fn-refresh').addEventListener('click',()=>this.refresh());this.el('fn-create')?.addEventListener('click',()=>this.openEditor());
  this.el('fn-form').addEventListener('submit',e=>{e.preventDefault();this.save();});
  for(const id of ['fn-editor-close','fn-editor-cancel'])this.el(id).addEventListener('click',()=>this.closeEditor());
  this.el('fn-runtime').addEventListener('change',()=>{if(window.confirm('Replace the editor contents with the example for this runtime?'))this.el('fn-code').value=this.example(this.el('fn-runtime').value);this.codeHint();});
  this.el('fn-detail-close').addEventListener('click',()=>{this.selected=null;this.clearToken();this.el('fn-detail').classList.add('hidden');});
  this.el('fn-list').addEventListener('click',e=>{const b=e.target.closest('button[data-action]');if(b)this.action(b.dataset.action,b.dataset.id);});
  this.el('fn-runs').addEventListener('click',e=>{const b=e.target.closest('button[data-run]');if(!b)return;if(b.dataset.action==='cancel')this.action('cancel',this.selected,b.dataset.run);else{this.selectedRun=b.dataset.run;this.renderLogs();}});
  this.el('fn-run')?.addEventListener('click',()=>this.action('run',this.selected));this.el('fn-token')?.addEventListener('click',()=>this.action('token',this.selected));
 },
 async load() {this.init();if(!this.timer)this.timer=setInterval(()=>{if(!document.hidden && App.Navigation.current==='functions')this.refresh();},3000);await this.refresh();},
 deactivate() {if(this.timer)clearInterval(this.timer);this.timer=null;this.generation++;this.epoch++;this.loading=false;this.clearToken();this.closeEditor();},
 clearToken() {if(!this.el('fn-token-value'))return;this.el('fn-token-value').value='';this.el('fn-trigger-url').value='';this.el('fn-token-result').classList.add('hidden');},
 async refresh() {
  if(this.loading)return;this.loading=true;const generation=++this.generation;
  try{const result=await App.API.get('/functions');if(generation!==this.generation)return;this.functions=result.data || [];this.el('fn-error').textContent='';this.render();if(this.selected)await this.details(generation);}
  catch(e){if(generation===this.generation)this.el('fn-error').textContent=e.message;}finally{if(generation===this.generation)this.loading=false;}
 },
 render() {
  this.el('fn-summary').textContent=`${this.functions.length} functions · ${this.functions.filter(f=>f.deployed).length} deployed`;
  this.el('fn-list').innerHTML=this.functions.map(f=>this.card(f)).join('') || '<div class="monitor-empty"><i class="fas fa-bolt" aria-hidden="true"></i><h3>Your first function starts here</h3><p>Create a Python or JavaScript function, deploy it, and try a test event.</p></div>';
 },
 card(f) {
  const e=v=>this.escape(v),spec=f.deployed?.spec || f.draft;
  const button=(action,label)=>`<button class="console-button secondary" data-action="${action}" data-id="${e(f.id)}" ${this.busy?'disabled':''}>${label}</button>`;
  return `<article class="monitor-card"><header><h3><i class="fas fa-bolt" aria-hidden="true"></i> ${e(f.draft.name)}</h3><span class="monitor-state ${f.deployed?'healthy':'disabled'}">${f.deployed?'Deployed':'Draft'}</span></header><p>${e(spec.runtime==='python'?'Python':'JavaScript')} · ${Number(spec.memory_mb)} MiB · ${Number(spec.cpu_millis)}m CPU · ${Number(spec.timeout_seconds)}s</p><p class="console-subtle">Draft revision ${Number(f.revision)}${f.deployed?` · Deployed revision ${Number(f.deployed.revision)}`:''}<br>${f.paused?'Automatic triggers paused':spec.schedule_enabled?'Next scheduled run: '+e(this.date(f.next_run_at)):'Manual'+(spec.http_enabled?' & HTTP':'')+' triggers'}</p><div class="monitor-actions">${button('details','Open function')}${this.canWrite()?button('edit','Edit draft')+button('deploy','Deploy draft')+(f.deployed?button('pause',f.paused?'Resume triggers':'Pause triggers'):'')+button('delete','Delete'):''}</div></article>`;
 },
 codeHint() {this.el('fn-code-hint').textContent=this.el('fn-runtime').value==='python'?'Define handler(event, context). Async handlers are supported.':'Export module.exports.handler = async (event, context) => … (CommonJS).';},
 openEditor(f=null) {
  if(!this.canWrite() || this.busy)return;this.editing=f?.id || null;const spec=f?.draft || {runtime:'python',timeout_seconds:30,memory_mb:256,cpu_millis:500,concurrency:1,timezone:'UTC'};
  this.el('fn-editor-title').textContent=f?'Edit function draft':'Create function';this.el('fn-name').value=spec.name || '';this.el('fn-runtime').value=spec.runtime;this.el('fn-code').value=spec.code || this.example(spec.runtime);
  for(const [id,key] of [['fn-timeout','timeout_seconds'],['fn-memory','memory_mb'],['fn-cpu','cpu_millis'],['fn-concurrency','concurrency'],['fn-schedule','schedule'],['fn-timezone','timezone']])this.el(id).value=spec[key] ?? '';
  this.el('fn-schedule-enabled').checked=!!spec.schedule_enabled;this.el('fn-http-enabled').checked=!!spec.http_enabled;this.el('fn-form-error').textContent='';this.codeHint();this.el('fn-editor').showModal();this.el('fn-name').focus();
 },
 closeEditor(){this.el('fn-editor')?.close();},
 async save() {
  if(!this.canWrite() || this.busy || !this.el('fn-form').reportValidity())return;this.busy=true;this.el('fn-save').disabled=true;
  const spec={name:this.el('fn-name').value,runtime:this.el('fn-runtime').value,code:this.el('fn-code').value,timeout_seconds:Number(this.el('fn-timeout').value),memory_mb:Number(this.el('fn-memory').value),cpu_millis:Number(this.el('fn-cpu').value),concurrency:Number(this.el('fn-concurrency').value),schedule:this.el('fn-schedule').value,timezone:this.el('fn-timezone').value,schedule_enabled:this.el('fn-schedule-enabled').checked,http_enabled:this.el('fn-http-enabled').checked};
  try{const result=this.editing?await App.API.put(this.base(this.editing),spec):await App.API.post('/functions',spec);this.selected=result.data.id;this.selectedRun=null;this.clearToken();this.closeEditor();App.UI.toast('success','Draft saved. Deploy it to make these changes available.');await this.refresh();}catch(e){this.el('fn-form-error').textContent=e.message;}finally{this.busy=false;this.el('fn-save').disabled=false;this.render();}
 },
 async details(generation=this.generation) {
  const id=this.selected,f=this.functions.find(f=>f.id===id);if(!f){this.selected=null;this.clearToken();this.el('fn-detail').classList.add('hidden');return;}
  try{const [runs,versions]=await Promise.all([App.API.get(this.base(id)+'/runs'),App.API.get(this.base(id)+'/versions')]);if(generation!==this.generation || id!==this.selected)return;this.runs=runs.data || [];this.versions=versions.data || [];this.el('fn-detail').classList.remove('hidden');this.el('fn-detail-title').textContent=f.draft.name;this.el('fn-detail-info').textContent=f.deployed?`Running revision ${f.deployed.revision} · ${f.deployed.id}`:'Draft only. Deploy before running.';
   this.el('fn-triggers').textContent=f.paused?'Automatic triggers are paused. Manual runs remain available.':f.deployed?`Manual${f.deployed.spec.http_enabled?' · Authenticated HTTP':''}${f.deployed.spec.schedule_enabled?' · '+f.deployed.spec.schedule+' ('+f.deployed.spec.timezone+')':''}`:'No deployed triggers.';
   if(this.el('fn-run'))this.el('fn-run').disabled=this.busy || !f.deployed;this.renderRuns();
  }catch(e){if(generation===this.generation)this.el('fn-error').textContent=e.message;}
 },
 renderRuns() {
  const e=v=>this.escape(v);this.el('fn-versions').innerHTML=this.versions.map(v=>`<li>Revision ${Number(v.revision)} · ${e(this.date(v.created_at))}<small>${e(v.code_sha256.slice(0,16))}…</small></li>`).join('') || '<li>No successful deployments yet.</li>';
  this.el('fn-runs').innerHTML=this.runs.map(r=>`<tr><td>${e(this.date(r.started_at))}</td><td>${r.kind==='deploy'?'Deployment':'Invocation'} · r${Number(r.revision)}<small>${e(r.trigger)}</small></td><td>${e(this.status(r.status))}${r.needs_cleanup && r.status!=='running'?'<small>Cleanup pending</small>':''}</td><td>${r.status==='running'?'—':(Number(r.duration_ms)/1000).toFixed(2)+'s'}</td><td><div class="monitor-actions"><button class="console-button secondary" data-run="${e(r.id)}">Logs</button>${this.canWrite() && r.status==='running'?`<button class="console-button secondary" data-action="cancel" data-run="${e(r.id)}" ${this.busy?'disabled':''}>Cancel</button>`:''}</div></td></tr>`).join('') || '<tr><td colspan="5">No executions yet. Deploy a draft to get started.</td></tr>';
  if(!this.runs.some(r=>r.id===this.selectedRun))this.selectedRun=this.runs[0]?.id || null;this.renderLogs();
 },
 renderLogs() {const r=this.runs.find(r=>r.id===this.selectedRun);this.el('fn-log-title').textContent=r?`${r.kind==='deploy'?'Deployment':'Invocation'} · ${r.id}`:'Select an execution to view logs';this.el('fn-log-status').textContent=r?this.status(r.status):'';this.el('fn-log-error').textContent=r?.error || '';this.el('fn-logs').textContent=r?(r.logs || (r.status==='running'?'Waiting for output…':'No console output.'))+(r.logs_truncated?'\n[Log limit reached; additional output discarded]':''):'';},
 async action(action,id,runID='') {
  if(this.busy)return;const f=this.functions.find(f=>f.id===id);if(!f)return;
  if(action==='details'){if(this.selected!==id){this.clearToken();this.selectedRun=null;}this.selected=id;await this.details();return;}
  if(!this.canWrite())return;if(action==='edit'){this.openEditor(f);return;}
  if(action==='delete'&&!window.confirm(`Delete “${f.draft.name}”, all versions, and its invocation history? Active runs must finish first.`))return;
  if(action==='token'&&!window.confirm('Create a new HTTP bearer token? Any previous token stops working immediately.'))return;
  let event;
  if(action==='run'){try{event=JSON.parse(this.el('fn-event').value);}catch(e){this.el('fn-event-error').textContent='Enter valid JSON before running.';return;}this.el('fn-event-error').textContent='';}
  const epoch=this.epoch;this.busy=true;this.render();
  try{
   let result;
   if(action==='deploy')result=await App.API.post(this.base(id)+'/deploy',{});
   else if(action==='run')result=await App.API.post(this.base(id)+'/run',event);
   else if(action==='cancel')await App.API.post(this.base(id)+'/runs/'+encodeURIComponent(runID)+'/cancel',{});
   else if(action==='pause')await App.API.post(this.base(id)+'/pause',{paused:!f.paused});
   else if(action==='delete'){await App.API.delete(this.base(id));if(this.selected===id){this.selected=null;this.clearToken();this.el('fn-detail').classList.add('hidden');}}
   else if(action==='token'){
    const token=await App.API.post(this.base(id)+'/http-token',{});
    if(epoch===this.epoch && this.selected===id){this.el('fn-token-value').value=token.data.token;this.el('fn-trigger-url').value=window.location.origin+token.data.path;this.el('fn-token-result').classList.remove('hidden');}return;
   }
   if(epoch!==this.epoch)return;
   if(result){this.selected=id;this.selectedRun=result.data.id;}await this.refresh();
  }catch(e){if(epoch===this.epoch)this.el('fn-error').textContent=e.message;}finally{this.busy=false;this.render();if(this.selected)this.renderRuns();}
 }
};
