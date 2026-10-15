const test=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const vm=require('node:vm');
function fleet() {
    const context=vm.createContext({App:{}});
    vm.runInContext(fs.readFileSync(path.join(__dirname,'../static/js/app/fleet.js'),'utf8'),context);
    return context.App.Fleet;
}
test('workload groups come from saved resources and can span several roles',()=>{
    const ui=fleet();
    assert.deepEqual(Array.from(ui.roles({counts:{apps:3,data:1,edge:2}})),['edge','apps','data']);
    assert.deepEqual(Array.from(ui.roles({counts:{apps:0,data:0,edge:0}})),['general']);
    assert.deepEqual(Array.from(ui.roles({is_db_only:true,counts:{}})),['data']);
});
test('failed or incomplete inventories never imply an empty general-purpose server',()=>{
    const ui=fleet();
    assert.deepEqual(Array.from(ui.roles({counts:{apps:0,data:null,edge:0}})),[]);
    assert.deepEqual(Array.from(ui.roles({counts:{apps:2,data:null,edge:0}})),['apps']);
});
test('fleet search combines workload, name, host, and tags',()=>{
    const ui=fleet();
    const server={name:'Apps Europe',host:'10.0.0.12',tags:['Production'],counts:{apps:2,data:0,edge:0}};
    assert.equal(ui.matches(server,' EUROPE ','apps'),true);
    assert.equal(ui.matches(server,'10.0.0.12',''),true);
    assert.equal(ui.matches(server,'production','apps'),true);
    assert.equal(ui.matches(server,'production','data'),false);
});
