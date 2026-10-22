const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const candidate = {host:'2001:db8::1',port:2222,algorithm:'ssh-ed25519',fingerprint:'SHA256:reviewed-key',public_key:'ssh-ed25519 exact-reviewed-key'};

function harness({approve=true,key=candidate,probeError=false}={}) {
    const calls=[], confirmations=[], toasts=[];
    const App = {
        API: {
            post: async (url,body) => {calls.push({method:'POST',url,body});if(probeError)throw new Error('SSH handshake failed');return {success:true,data:key};},
            put: async (url,body) => {calls.push({method:'PUT',url,body});return {success:true};}
        },
        UI: {confirm:async(message,options)=>{confirmations.push({message,options});return approve;},toast:(...args)=>toasts.push(args)}
    };
    vm.runInContext(fs.readFileSync(path.join(__dirname,'../static/js/app/ssh-trust.js'),'utf8'),vm.createContext({App}));
    return {App,calls,confirmations,toasts};
}

test('cancelled fingerprint review does not save trust',async()=>{
    const {App,calls,confirmations}=harness({approve:false});
    const button={disabled:false};
    await App.SSHTrust.verifyServer({id:'a/b'},button);
    assert.equal(calls.length,1);
    assert.equal(calls[0].url,'/servers/a%2Fb/ssh-host-key');
    assert.deepEqual(JSON.parse(JSON.stringify(calls[0].body)),{});
    assert.equal(confirmations.length,1);
    assert.equal(button.disabled,false);
});

test('approval saves exactly the reviewed key and endpoint after showing its fingerprint',async()=>{
    const {App,calls,confirmations}=harness();
    await App.SSHTrust.verifyServer({id:'server'},{});
    assert.equal(calls.length,2);
    assert.equal(calls[1].method,'PUT');
    assert.equal(calls[1].body,candidate);
    assert.match(confirmations[0].message,/\[2001:db8::1\]:2222/);
    assert.match(confirmations[0].message,/SHA256:reviewed-key/);
    assert.match(confirmations[0].message,/hosting provider console/);
    assert.equal(confirmations[0].options.confirmLabel,'Trust this key');
});

test('changed saved key shows old and new fingerprints and asks to trust the replacement',async()=>{
    const {App,confirmations}=harness({key:{...candidate,trusted_fingerprint:'SHA256:old-key'}});
    await App.SSHTrust.verifyServer({id:'server'},{});
    assert.match(confirmations[0].message,/WARNING/);
    assert.match(confirmations[0].message,/SHA256:old-key/);
    assert.match(confirmations[0].message,/SHA256:reviewed-key/);
    assert.equal(confirmations[0].options.confirmLabel,'Trust replacement key');
    assert.equal(confirmations[0].options.danger,true);
});

test('probe failure never asks for trust or writes it, and releases the button',async()=>{
    const {App,calls,confirmations,toasts}=harness({probeError:true});
    const button={disabled:false};
    await App.SSHTrust.verifyServer({id:'server'},button);
    assert.equal(calls.length,1);
    assert.equal(confirmations.length,0);
    assert.equal(toasts[0][0],'error');
    assert.equal(button.disabled,false);
});

test('closing setup during discovery prevents a late trust dialog',async()=>{
    const {App,confirmations}=harness();
    const result=await App.SSHTrust.review('/servers/ssh-host-key',{host:'host',port:22},()=>false);
    assert.equal(result,null);
    assert.equal(confirmations.length,0);
});

function onboarding({approve=true,closeDuringProbe=false,keyAuth=false}={}) {
    const {App,calls,confirmations}=harness({approve});
    const fields={
        name:{value:'Production'},host:{value:'2001:db8::1'},'ssh-port':{value:'2222'},user:{value:'root'},password:{value:'private-password'},
        'key-file':{files:keyAuth?[{name:'private-key'}]:[]},'password-field':{},'key-field':{},'key-file-name':{}
    };
    const button={innerHTML:'Review SSH key',disabled:false};
    const form={querySelector:()=>button,querySelectorAll:()=>[...Object.values(fields),button],reset:()=>{}};
    const listeners={};
    const dialog={open:true,addEventListener:(name,callback)=>listeners[name]=callback,removeEventListener:name=>delete listeners[name]};
    fields['add-server-form']=form;fields['add-server-modal']=dialog;
    const document={getElementById:id=>fields[id],querySelector:()=>({value:keyAuth?'key':'password'})};
    const context=vm.createContext({App,document,Array,Number,Error,loadServers:()=>{},closeAddServerModal:()=>{dialog.open=false;},toggleAuthField:()=>{},updateKeyFileName:()=>{},showLocalToast:()=>{},fetch:()=>{throw new Error('private key must not be uploaded before trust');}});
    const template=fs.readFileSync(path.join(__dirname,'../templates/server_list.html'),'utf8');
    const start=template.indexOf('    async function handleAddServer(e) {');
    const end=template.indexOf('    async function deleteServer(id) {',start);
    vm.runInContext(template.slice(start,end),context);
    if(closeDuringProbe){const post=App.API.post;App.API.post=async(...args)=>{const result=await post(...args);dialog.open=false;listeners.close();return result;};}
    return {App,calls,confirmations,button,run:()=>context.handleAddServer({preventDefault(){},submitter:button})};
}

test('onboarding sends only the address before trust and saves the approved pin with credentials',async()=>{
    const {calls,button,run}=onboarding();
    await run();
    assert.equal(calls.length,2);
    assert.equal(calls[0].url,'/servers/ssh-host-key');
    assert.deepEqual(JSON.parse(JSON.stringify(calls[0].body)),{host:'2001:db8::1',port:2222});
    assert.equal(calls[1].url,'/servers');
    assert.equal(calls[1].body.trusted_host_key,candidate.public_key);
    assert.equal(calls[1].body.host,candidate.host);
    assert.equal(calls[1].body.port,candidate.port);
    assert.equal(calls[1].body.password,'private-password');
    assert.equal(button.disabled,false);
});

test('cancelled onboarding saves no credentials and uploads no private key',async()=>{
    for(const keyAuth of [false,true]){
        const {calls,button,run}=onboarding({approve:false,keyAuth});
        await run();
        assert.equal(calls.length,1);
        assert.equal(JSON.stringify(calls).includes('private-password'),false);
        assert.equal(button.disabled,false);
    }
});

test('closing onboarding during discovery discards the response and releases the form',async()=>{
    const {calls,confirmations,button,run}=onboarding({closeDuringProbe:true});
    await run();
    assert.equal(calls.length,1);
    assert.equal(confirmations.length,0);
    assert.equal(button.disabled,false);
});
