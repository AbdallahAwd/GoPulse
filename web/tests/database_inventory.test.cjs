const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
function harness() {
    const nodes = new Map(), requests = [], tracked = [];
    function make() {
        const classes = new Set();
        const children = [];
        return {
            value: '', style: {}, children, innerHTML: '', textContent: '', disabled: false,
            classList: {add: (...values) => values.forEach(v => classes.add(v)), remove: (...values) => values.forEach(v => classes.delete(v)), contains: v => classes.has(v)},
            appendChild(node) { children.push(node); }, replaceChildren(...values) { children.splice(0, children.length, ...values); },
            querySelector(selector) { return this.parts?.[selector] || make(); }, setAttribute() {},
        };
    }
    function el(id) {
        if (!nodes.has(id)) nodes.set(id, make());
        return nodes.get(id);
    }
    el('provision-db-modal').children.push(make());
    el('provision-db-modal').querySelector = selector => selector === 'h3' ? el('provision-title') : el('provision-panel');
    const App = {
        config: {SERVER_ID: 'selected-server', SERVER_HOST: 'db.example.test'},
        UI: {el, toast() {}},
        API: {get: async () => ({success: true, data: []}), post: async (url, payload) => {requests.push({url, payload});return {success: true};}},
        Infrastructure: {
            api: async (url, options) => {requests.push({url, payload: JSON.parse(options.body)});return {operation_id: 'op-vector'};},
            dialog: () => make(), track: async id => tracked.push(id),
        },
    };
    const context = vm.createContext({App, document: {getElementById: el, querySelectorAll: () => [], querySelector: () => el('db-provision-submit'), addEventListener() {}, createElement: make}, console, setTimeout: () => {}, setInterval: () => {}, clearInterval() {}, encodeURIComponent});
    vm.runInContext(fs.readFileSync(path.join(__dirname, '../static/js/app/databases.js'), 'utf8'), context);
    return {App, ui: App.Databases, el, requests, tracked};
}
test('one inventory includes vector and backend services, with real counts and installed engines only', async () => {
    const {App, ui, el} = harness();
    App.API.get = async () => ({success: true, data: [
        {id:'one', name:'Search', type:'qdrant', status:'Running', host_port:6333},
        {id:'two', name:'Backend', type:'pocketbase', status:'Stopped', host_port:8090},
    ]});
    await ui.load();
    assert.equal(el('databases-grid').children.length, 2);
    assert.equal(el('db-stat-total').textContent, 2);
    assert.equal(el('db-stat-running').textContent, 1);
    assert.equal(el('db-stat-engines').textContent, 2);
    assert.deepEqual(el('db-engine-filter').children.map(o => o.value), ['all','pocketbase','qdrant']);
    el('db-engine-filter').value = 'qdrant';
    ui.filterInventory();
    assert.equal(el('databases-grid').children.length, 1);
    assert.equal(el('db-inventory-count').textContent, '1 of 2 instances');
    el('db-inventory-search').value = 'unmatched';
    ui.filterInventory();
    assert.equal(el('db-empty-title').textContent, 'No matching databases');
    ui.clearInventoryFilters();
    assert.equal(el('databases-grid').children.length, 2);
});
test('search combines engine selection and case-insensitive names without remote requests', () => {
    const {ui} = harness();
    const items = [{name:'Semantic search',type:'qdrant'}, {name:'Search backend',type:'pocketbase'}, {name:'Main',type:'postgres',database_name:'SearchData'}];
    assert.equal(ui.filterItems(items, ' SEARCH ', 'all').length, 3);
    assert.equal(ui.filterItems(items, ' SEARCH ', 'qdrant').length, 1);
    assert.equal(ui.filterItems(items, 'PostgreSQL', 'all')[0].type, 'postgres');
});
test('Qdrant uses the selected server and specialized operation endpoint without SQL credentials', async () => {
    const {ui, el, requests, tracked} = harness();
    ui.openProvisionModal('qdrant');
    assert.equal(el('db-prov-type').value, 'qdrant');
    assert.equal(el('db-prov-port').value, 6333);
    assert.equal(el('db-prov-credentials').classList.contains('hidden'), true);
    el('db-prov-name').value = 'Semantic search';
    el('db-prov-vector-key').value = 'private-test-key';
    await ui.submitProvision();
    assert.equal(requests.length, 1);
    assert.equal(requests[0].url, '/servers/selected-server/databases/qdrant/install');
    assert.equal(requests[0].payload.name, 'Semantic search');
    assert.equal(requests[0].payload.api_key, 'private-test-key');
    assert.equal(requests[0].payload.config.bind_address, '127.0.0.1');
    assert.equal(requests[0].payload.username, undefined);
    assert.deepEqual(tracked, ['op-vector']);
    assert.equal(el('db-prov-vector-key').value, '');
});
test('changing away from a vector engine restores database fields and engine defaults', () => {
    const {ui, el} = harness();
    ui.openProvisionModal('qdrant');
    el('db-prov-type').value = 'postgres';
    ui.onProvTypeChange();
    assert.equal(el('db-prov-user').value, 'postgres');
    assert.equal(el('db-prov-port').value, 5432);
    assert.equal(el('db-prov-credentials').classList.contains('hidden'), false);
    assert.equal(el('db-prov-dbname-wrapper').classList.contains('hidden'), false);
    assert.equal(el('db-prov-vector-config').classList.contains('hidden'), true);
    el('db-prov-type').value = 'sqlite';ui.onProvTypeChange();
    assert.equal(el('db-prov-port').disabled, true);
});
test('invalid vector configuration and repeated submission do not issue extra installs', async () => {
    const {App, ui, el, requests} = harness();
    ui.openProvisionModal('qdrant');el('db-prov-name').value = 'Search';
    el('db-prov-port').value = '6333.5';
    await ui.submitProvision();assert.equal(requests.length, 0);
    assert.match(el('db-prov-error').textContent, /1024/);
    el('db-prov-port').value = '6333';
    let resolve;
    App.Infrastructure.api = (url, options) => {requests.push({url});return new Promise(r => resolve = r);};
    const pending = ui.submitProvision();await ui.submitProvision();
    assert.equal(requests.length, 1);
    assert.equal(el('db-provision-submit').disabled, true);
    resolve({operation_id:'one'});await pending;
    assert.equal(el('db-provision-submit').disabled, false);
});
test('a rejected vector install stays in the form and allows retry', async () => {
    const {App, ui, el} = harness();
    ui.openProvisionModal('qdrant');el('db-prov-name').value = 'Search';
    App.Infrastructure.api = async () => {throw new Error('Private bind address required');};
    await ui.submitProvision();
    assert.equal(el('db-prov-error').textContent, 'Private bind address required');
    assert.equal(el('db-provision-submit').disabled, false);
});
test('relational provisioning keeps its existing API payload after changing from Qdrant', async () => {
    const {ui, el, requests} = harness();
    ui.openProvisionModal('qdrant');
    el('db-prov-type').value = 'postgres';ui.onProvTypeChange();
    el('db-prov-name').value = 'Production';
    const password = el('db-prov-pass').value;
    await ui.submitProvision();
    assert.equal(requests.length, 1);
    assert.equal(requests[0].url, '/servers/selected-server/databases');
    assert.equal(requests[0].payload.type, 'postgres');
    assert.equal(requests[0].payload.username, 'postgres');
    assert.equal(requests[0].payload.database_name, 'gopulse');
    assert.equal(requests[0].payload.password, password);
    assert.equal(requests[0].payload.host_port, 5432);
    assert.equal(el('db-prov-pass').value, '');
});
