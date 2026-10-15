const test=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const vm=require('node:vm');
function api(fetch){
 const context={document:{getElementById(){return null;}},console:{error(){}},fetch};context.window=context;vm.createContext(context);
 vm.runInContext(fs.readFileSync(path.join(__dirname,'../static/js/app/core.js'),'utf8'),context);
 context.App.UI.toast=()=>{};return context.App.API;
}
test('rate limits carry recovery information and never automatically retry a write',async()=>{
 let calls=0;
 const client=api(async()=>{calls++;return {ok:false,status:429,headers:{get:()=> '3'},text:async()=>JSON.stringify({error:'Too many requests'})};});
 await assert.rejects(client.post('/query',{query:'SELECT 1'}),error=>error.status===429 && error.retryAfter===3 && /Try again in 3 seconds/.test(error.message));
 assert.equal(calls,1);
});
test('SSH and database errors remain actionable instead of becoming JSON parse failures',async()=>{
 const client=api(async()=>({ok:false,status:502,headers:{get:()=>null},text:async()=>JSON.stringify({message:'Unable to verify health',error:'SSH host key is not trusted'})}));
 await assert.rejects(client.get('/servers/server/databases/db?refresh=true'),/SSH host key is not trusted/);
});
