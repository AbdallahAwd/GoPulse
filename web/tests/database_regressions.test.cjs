const test=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const vm=require('node:vm');
function load(context,file){vm.runInContext(fs.readFileSync(path.join(__dirname,'../static/js/app',file+'.js'),'utf8'),context);}
function databaseUI(read){
 const elements=new Map(),toasts=[],copied=[];
 function element(id){if(!elements.has(id))elements.set(id,{value:'',innerText:'',textContent:'',innerHTML:'',style:{},dataset:{},classList:{add(){},remove(){},contains(){return true;}},appendChild(){}});return elements.get(id);}
 const context=vm.createContext({App:{config:{SERVER_ID:'server',SERVER_HOST:'db.example.test'},System:{currentServer:{host:'wrong-control-host'}},API:{get:read},UI:{el:element,toast:(...values)=>toasts.push(values)}},document:{getElementById:element,querySelectorAll:()=>[],querySelector:()=>null,addEventListener(){}},navigator:{clipboard:{writeText:async text=>copied.push(text)}},console,encodeURIComponent,setInterval:()=>1,clearInterval(){},setTimeout});
 load(context,'database_connection');load(context,'databases');
 return {App:context.App,ui:context.App.Databases,elements,element,toasts,copied};
}
const db={id:'db-test',type:'postgres',name:'Main',username:'app',database_name:'main',host_port:5432,status:'Running',password:''};
test('opening database details fetches metadata once and uses selected server host',async()=>{
 const requests=[];const {ui,element}=databaseUI(async path=>{requests.push(path);return {success:true,data:db};});
 await ui.showManageView(db.id);
 assert.deepEqual(requests,['/servers/server/databases/db-test']);
 assert.match(element('db-conn-uri').value,/db\.example\.test:5432/);
 assert.match(element('db-conn-uri').value,/<password>/);
 assert.equal(element('db-settings-pass').innerText,'••••••••');
});
test('failed metadata requests show the actual error without retries or unhandled rejection',async()=>{
 let calls=0;const {ui,toasts}=databaseUI(async()=>{calls++;throw new Error('Too many API requests. Try again in 3 seconds.');});
 await ui.showManageView(db.id);
 assert.equal(calls,1);assert.match(toasts[0][1],/Too many API requests/);
});
test('connection copy reads protected credentials only on demand and encodes them',async()=>{
 const requests=[];const {ui,copied,element}=databaseUI(async path=>{requests.push(path);return {success:true,data:path.endsWith('/credentials')?{password:'p@ss/# ?'}:{...db}};});
 await ui.showManageView(db.id);assert.equal(requests.length,1);
 await ui.copyConnURI();assert.equal(requests.length,2);
 assert.match(copied[0],/app:p%40ss%2F%23%20%3F@db\.example\.test/);
 assert.match(element('db-conn-uri').value,/<password>/);
});
test('out-of-order database requests cannot replace the selected database',async()=>{
 let resolveFirst;
 const {ui,element}=databaseUI(path=>path.endsWith('/first')?new Promise(resolve=>resolveFirst=resolve):Promise.resolve({success:true,data:{...db,id:'second',name:'Second'}}));
 const first=ui.showManageView('first');await ui.showManageView('second');resolveFirst({success:true,data:{...db,id:'first',name:'First'}});await first;
 assert.equal(element('db-manage-name').innerText,'Second');
});
test('URI helpers support IPv6 and non-password engines',()=>{
 const context=vm.createContext({App:{},encodeURIComponent});load(context,'database_connection');
 const uri=context.App.DatabaseConnection.uri({...db,password:'secret'},'2001:db8::1',true);
 assert.match(uri,/@\[2001:db8::1\]:5432/);
 assert.equal(context.App.DatabaseConnection.uri({type:'sqlite',id:'db'},'ignored'),'/var/lib/gopulse/databases/db/sqlite.db');
 assert.equal(context.App.DatabaseConnection.uri({type:'pocketbase',host_port:8090},'db.example.test'),'http://db.example.test:8090');
});
test('terminal close after remote disconnect disposes timers and ignores retired socket callbacks',()=>{
 const context=vm.createContext({App:{},window:{removeEventListener(){}},clearInterval(){},setTimeout});load(context,'terminal');
 const term=context.App.Terminal;let disposed=0,closed=0;
 term.isOpen=false;term.term={dispose(){disposed++;}};term.socket={close(){closed++;}};
 term.close();assert.equal(disposed,1);assert.equal(closed,1);assert.equal(term.term,null);assert.equal(term.socket,null);
});

test('a second click cannot submit a database query while the first is pending',async()=>{
 const {App,ui,element}=databaseUI(async()=>({success:true,data:{...db}}));
 await ui.showManageView(db.id);element('db-console-query').value='SELECT 1';
 let resolveQuery,calls=0;
 App.API.post=()=>{calls++;return new Promise(resolve=>resolveQuery=resolve);};
 const pending=ui.runQuery();await ui.runQuery();assert.equal(calls,1);
 resolveQuery({success:true,data:{type:'raw',data:'1'}});await pending;
 App.API.post=async()=>{calls++;return {success:true,data:{type:'raw',data:'1'}};};
 await ui.runQuery();assert.equal(calls,2);
});
