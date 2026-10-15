const test=require('node:test');const assert=require('node:assert/strict');const fs=require('node:fs');const path=require('node:path');const vm=require('node:vm');
function harness(){
    const nodes=new Map(),calls=[];let confirm=true;
    function el(id){if(!nodes.has(id))nodes.set(id,{value:'',checked:false,disabled:false,hidden:false,required:false,textContent:'',dataset:{},open:false,isConnected:true,children:[],addEventListener(){},querySelectorAll(){return ['hostname','ip','email','https','terms'].map(name=>el('master-domain-'+name));},reportValidity(){return true;},replaceChildren(){this.children=[];},append(node){this.children.push(node);}});return nodes.get(id);}
    const settings={domain:'dashboard.example.com',https:true,email:'admin@example.com',agree_terms:true,expected_ip:'',revision:'saved',applied_revision:'saved',applied_domain:'dashboard.example.com',applied_https:true,operation_id:'',check:{addresses:[],message:''}};
    const data={settings,upstream:'127.0.0.1:2886',platform:'linux',managed_supported:true,public_url:'https://dashboard.example.com'};
    const App={Infrastructure:{api:async(url,options={})=>{calls.push({url,options});if(url.endsWith('/nginx'))return {configuration:'server { }'};if(url.endsWith('/check'))return {addresses:['8.8.8.8','2001:4860::8888'],message:'Verified'};if(url.endsWith('/apply'))return {operation_id:'operation'};return data;}},UI:{confirm:async()=>confirm,toast(){}},Agents:{copy(){}}};
    const document={getElementById:el,createElement:()=>({textContent:''})};const context=vm.createContext({App,document,window:{location:{origin:'http://127.0.0.1:2886'}},Date,setTimeout});vm.runInContext(fs.readFileSync(path.join(__dirname,'../static/js/app/dashboard_settings.js'),'utf8'),context);
    const ui=App.DashboardSettings;ui.data=data;ui.fill();return {ui,el,calls,data,confirm:value=>confirm=value};
}
test('unsaved domain changes cannot check or deploy an older saved configuration',async()=>{
    const {ui,el,calls}=harness();el('master-domain-hostname').value='new.example.com';ui.syncActions();assert.equal(el('master-domain-apply').disabled,true);assert.equal(el('master-domain-check').disabled,true);await ui.apply();await ui.check();assert.equal(calls.length,0);assert.match(el('master-domain-save-note').textContent,/unsaved/);
});
test('saving domain settings makes a settings request without applying routing',async()=>{
    const {ui,el,calls}=harness();el('master-domain-hostname').value=' New.Example.com. ';await ui.save();const writes=calls.filter(x=>x.options.method);assert.equal(writes.length,1);assert.equal(writes[0].options.method,'PUT');assert.equal(JSON.parse(writes[0].options.body).domain,'new.example.com');assert.equal(calls.some(x=>x.url.endsWith('/apply')),false);assert.equal(ui.busy,false);
});
test('host setup requires confirmation and sends the browser approval header',async()=>{
    const {ui,calls,confirm}=harness();confirm(false);await ui.apply();assert.equal(calls.length,0);confirm(true);let tracked='';ui.track=id=>{tracked=id;};await ui.apply();assert.equal(calls[0].url,'/settings/dashboard/domain/apply');assert.equal(calls[0].options.headers['X-GoPulse-Human-Approval'],'true');assert.equal(tracked,'operation');assert.equal(ui.busy,false);
});
test('non-Linux master hosts can save and preview but cannot apply',async()=>{
    const {ui,el,data,calls}=harness();data.platform='darwin';data.managed_supported=false;ui.fill();assert.equal(el('master-domain-apply').disabled,true);assert.equal(el('master-domain-save').disabled,false);assert.match(el('master-domain-platform').textContent,/darwin/);await ui.apply();assert.equal(calls.length,0);await ui.loadPreview();assert.equal(ui.preview,'server { }');
});
test('turning HTTPS off removes hidden certificate inputs from form validation',()=>{
    const {ui,el}=harness();el('master-domain-https').checked=false;ui.syncTLS();ui.syncActions();assert.equal(el('master-domain-certificate').hidden,true);assert.equal(el('master-domain-email').required,false);assert.equal(el('master-domain-email').disabled,true);assert.equal(el('master-domain-terms').disabled,true);
});
test('connection checks display DNS safely and release controls after a failed request',async()=>{
    const {ui,el,calls}=harness();await ui.check();assert.equal(calls[0].options.method,'POST');assert.equal(el('master-domain-dns-records').children.length,2);assert.match(el('master-domain-dns-records').children[0].textContent,/A ·/);assert.match(el('master-domain-dns-records').children[1].textContent,/AAAA ·/);ui.api=async()=>{throw new Error('Unavailable');};await ui.check();assert.equal(ui.busy,false);assert.equal(el('master-domain-error').textContent,'Unavailable');
});
