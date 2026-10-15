const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

function modules() {
    const context = vm.createContext({App:{}, URL, URLSearchParams, window:{location:{href:'https://panel.example/servers/a/dashboard'}}});
    for (const name of ['navigation','workspace']) vm.runInContext(fs.readFileSync(path.join(__dirname, '../static/js/app', name+'.js'),'utf8'),context);
    return context.App;
}

test('direct links, legacy hashes, and invalid sections resolve safely', () => {
    const {Navigation} = modules();
    assert.equal(Navigation.resolve('?view=projects'), 'projects');
    assert.equal(Navigation.resolve('', '#databases'), 'databases');
    assert.equal(Navigation.resolve('?view=terminal', '#projects'), 'terminal');
    assert.equal(Navigation.resolve('?view=constructor'), 'overview');
    assert.equal(Navigation.resolve('?view=not-a-section'), 'overview');
});

test('changing server section clears old resource and creation context', () => {
    const {Navigation} = modules();
    const result = Navigation.sectionURL('files','https://panel.example/servers/a/dashboard?view=projects&resource=private&action=create&panel=releases#projects');
    assert.equal(result.pathname, '/servers/a/dashboard');
    assert.equal(result.search, '?view=files');
    assert.equal(result.hash, '');
});

test('resource links encode identities without changing destination scope', () => {
    const {Workspace} = modules();
    const result = new URL(Workspace.serverURL('a/b','projects','item&action=create','releases'),'https://panel.example');
    assert.equal(result.pathname,'/servers/a%2Fb/dashboard');
    assert.equal(result.searchParams.get('resource'),'item&action=create');
    assert.equal(result.searchParams.has('action'),false);
    assert.equal(result.searchParams.get('panel'),'releases');
});

test('global filtering combines server scope with case-insensitive search', () => {
    const {Workspace} = modules();
    const row = {name:'Payments API', serverID:'a', server:'Production',kind:'application',detail:'go',status:'Running'};
    assert.equal(Workspace.matches(row,' PAYMENTS ','a'),true);
    assert.equal(Workspace.matches(row,'payments','b'),false);
    assert.equal(Workspace.matches(row,'stopped','a'),false);
});

test('inventory links do not carry stored credentials or environment values', () => {
    const {Workspace} = modules();
    const row = Workspace.row({id:'db',name:'Main database',type:'postgres',password:'sensitive',env_vars:{TOKEN:'sensitive'}},{id:'server',name:'Production'},'database');
    assert.equal(JSON.stringify(row).includes('sensitive'),false);
    assert.equal(row.status,'Unknown');
});
