// Master host settings are independent of the selected managed server.
App.DashboardSettings = {
    data:null,busy:false,operationRunning:false,preview:'',
    el(id){return document.getElementById('master-domain-'+id);},
    api(path='',options={}){return App.Infrastructure.api('/settings/dashboard/domain'+path,options);},
    init(){
        const panel=this.el('panel');if(!panel)return;
        if(panel.dataset.admin!=='true'){this.el('status').textContent='Administrator access';return;}
        this.el('form').addEventListener('submit',event=>{event.preventDefault();this.save();});
        this.el('form').addEventListener('input',()=>{this.syncTLS();this.syncActions();});
        this.el('apply').onclick=()=>this.apply();this.el('check').onclick=()=>this.check();
        this.el('preview').addEventListener('toggle',()=>{if(this.el('preview').open)this.loadPreview();});
        this.el('copy').onclick=()=>App.Agents.copy(this.preview);
        this.load();
    },
    payload(){return {domain:this.el('hostname').value.trim().toLowerCase().replace(/\.$/,''),https:this.el('https').checked,email:this.el('email').value.trim(),agree_terms:this.el('terms').checked,expected_ip:this.el('ip').value.trim()};},
    dirty(){if(!this.data)return false;const value=this.payload(),saved=this.data.settings;return Object.keys(value).some(key=>value[key]!==saved[key]);},
    syncTLS(){const enabled=this.el('https').checked;this.el('certificate').hidden=!enabled;this.el('email').required=enabled;this.el('terms').required=enabled;},
    syncActions(){
        const saved=Boolean(this.data?.settings.domain),dirty=this.dirty(),blocked=this.busy||this.operationRunning;
        this.el('save').disabled=blocked||!this.data;
        this.el('apply').disabled=blocked||!saved||dirty||!this.data?.managed_supported;
        this.el('check').disabled=blocked||!saved||dirty;
        this.el('copy').disabled=!this.preview||dirty;
        this.el('save-note').textContent=dirty?'You have unsaved changes. Save before checking or applying.':'Save your settings, then apply setup on the master host.';
        this.el('form').querySelectorAll('input').forEach(input=>input.disabled=blocked||(!this.el('https').checked&&[this.el('email'),this.el('terms')].includes(input)));
    },
    error(message=''){this.el('error').textContent=message;this.el('error').hidden=!message;},
    async load(){
        try{this.data=await this.api();this.fill();if(this.el('preview').open)await this.loadPreview();if(this.data.settings.operation_id&&!this.operationRunning)this.inspectExisting(this.data.settings.operation_id);}
        catch(error){this.error(error.message);this.el('status').textContent='Unavailable';}
    },
    fill(){
        const {settings,upstream,platform,managed_supported,public_url}=this.data;
        for(const [id,key] of [['hostname','domain'],['ip','expected_ip'],['email','email']])this.el(id).value=settings[key]||'';
        this.el('https').checked=settings.https;this.el('terms').checked=settings.agree_terms;
        const applied=settings.applied_revision&&settings.applied_revision===settings.revision;
        this.el('status').textContent=settings.last_error?'Setup failed':!settings.domain?'Not configured':applied?'Applied':'Saved · not applied';
        this.el('status').dataset.state=settings.last_error?'error':applied?'active':'pending';
        const origin=public_url||window.location.origin;this.el('current-url').href=origin;this.el('current-url').textContent=origin;
        this.el('address-note').textContent=public_url?'Active master dashboard address. Your direct address remains available.':'Your current address stays available during setup.';
        this.el('runtime').textContent=`Nginx forwards traffic to ${upstream}. WebSockets, logs, uploads, and MCP are included.`;
        this.el('platform').textContent=managed_supported?'Requires Linux with systemd, curl, and root or passwordless sudo. Missing Nginx/Certbot packages are installed through apt.':`This master host runs ${platform}. Automatic setup is available on Linux; the preview can be used with a separately managed proxy.`;
        this.el('apply-note').textContent=managed_supported?'Setup changes only the master dashboard virtual host. It validates and reloads Nginx, provisions HTTPS, and enables certificate renewal.':'Save and preview settings here. Deploy GoPulse on a Linux master host to use automatic setup.';
        this.renderCheck(settings.check);this.syncTLS();this.syncActions();
        if(settings.last_error)this.error(settings.last_error);
    },
    renderCheck(check){
        const root=this.el('dns-records');root.replaceChildren();
        for(const address of check?.addresses||[]){const record=document.createElement('code');record.textContent=(address.includes(':')?'AAAA':'A')+' · '+address;root.append(record);}
        this.el('check-message').textContent=check?.message||'Check DNS and confirm that the address reaches this GoPulse instance.';
        this.el('check-time').textContent=check?.checked_at&&!check.checked_at.startsWith('0001-')?'Checked '+new Date(check.checked_at).toLocaleString():'';
    },
    async save(){
        if(this.busy||this.operationRunning||!this.el('form').reportValidity())return;
        this.busy=true;this.error();this.syncActions();
        try{await this.api('',{method:'PUT',body:JSON.stringify(this.payload())});this.preview='';await this.load();App.UI.toast('success','Domain settings saved. Apply setup when DNS is ready.');}
        catch(error){this.error(error.message);}
        finally{this.busy=false;this.syncActions();}
    },
    async check(){
        if(this.busy||this.operationRunning||this.dirty())return;this.busy=true;this.error();this.syncActions();
        try{const check=await this.api('/check',{method:'POST'});this.data.settings.check=check;this.renderCheck(check);}
        catch(error){this.error(error.message);}
        finally{this.busy=false;this.syncActions();}
    },
    async loadPreview(){
        if(!this.data?.settings.domain||this.dirty())return;
        try{const result=await this.api('/nginx');this.preview=result.configuration;this.el('nginx').textContent=this.preview;this.syncActions();}
        catch(error){this.error(error.message);}
    },
    async apply(){
        if(this.busy||this.operationRunning||this.dirty()||!this.data?.managed_supported||!this.data.settings.domain)return;
        const settings=this.data.settings;
        if(!await App.UI.confirm(`Configure ${settings.domain} on this master host? Setup installs missing Nginx${settings.https?' and Certbot':''} packages where apt is available, updates the dashboard virtual host, and reloads Nginx${settings.https?' to enable HTTPS and automatic renewal':''}.`,{title:'Set up dashboard domain',confirmLabel:'Set up domain',danger:false}))return;
        this.busy=true;this.error();this.syncActions();
        try{const operation=await this.api('/apply',{method:'POST',headers:{'X-GoPulse-Human-Approval':'true'}});this.data.settings.operation_id=operation.operation_id;this.track(operation.operation_id);}
        catch(error){this.error(error.message);}
        finally{this.busy=false;this.syncActions();}
    },
    async inspectExisting(id){try{const op=await App.Infrastructure.api('/operations/'+encodeURIComponent(id));if(op.status==='running')this.track(id);else this.renderOperation(op);}catch(error){this.error(error.message);}},
    renderOperation(op){this.el('operation').hidden=false;this.el('operation-status').textContent='Domain setup · '+op.status;const steps=this.el('operation-steps');steps.replaceChildren();for(const text of op.steps||[]){const item=document.createElement('li');item.textContent=text;steps.append(item);}if(op.error)this.error(op.error);},
    async track(id){
        if(this.operationRunning)return;this.operationRunning=true;this.syncActions();
        try{while(this.el('panel').isConnected){const op=await App.Infrastructure.api('/operations/'+encodeURIComponent(id));this.renderOperation(op);if(op.status!=='running'){if(op.status==='completed')App.UI.toast('success','Dashboard domain setup completed.');break;}await new Promise(resolve=>setTimeout(resolve,1500));}}
        catch(error){this.error(error.message);}
        finally{this.operationRunning=false;await this.load();this.syncActions();}
    }
};
