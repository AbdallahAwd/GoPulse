const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

function fixture(mode, currentPath) {
    const container = {innerHTML: ''};
    const breadcrumbs = {innerHTML: ''};
    const elements = {'file-list-container': container, 'file-breadcrumbs-display': breadcrumbs};
    const App = {state: {currentPath}, UI: {el: id => elements[id], formatBytes: () => '1 B'}};
    const context = vm.createContext({App, localStorage: {getItem: () => mode}, document: {addEventListener() {}}, window: {}, console});
    vm.runInContext(fs.readFileSync(path.join(__dirname, '../static/js/app/files.js'), 'utf8'), context);
    App.Files.attachContextMenu = () => {};
    return {App, context, container, breadcrumbs};
}
function decode(value) {
    return value.replace(/&(amp|lt|gt|quot|#39);/g, (_, name) => ({amp: '&', lt: '<', gt: '>', quot: '"', '#39': "'"}[name]));
}

for (const mode of ['list', 'grid']) {
    test(`file ${mode} renders malicious names as text and passes exact paths to handlers`, () => {
        const name = `evil');globalThis.injected=true;//<img src=x onerror=alert(1)>"&.txt`;
        const root = `/folder'"&`;
        const {App, context, container} = fixture(mode, root);
        App.Files.render([{name, size: 1, is_dir: false, mod_time: '2026-01-01'}]);
        const html = container.innerHTML;
        assert.ok(!html.includes('<img src=x'));
        assert.ok(html.includes('&lt;img'));
        const handlers = [...html.matchAll(/onclick="([^"]*)"/g)].map(match => decode(match[1]));
        const calls = [];
        App.Editor = {open: value => calls.push(value)};
        for (const action of ['list', 'download', 'zip', 'unzip', 'delete']) App.Files[action] = value => calls.push(value);
        context.event = {preventDefault() {}, stopPropagation() {}};
        for (const handler of handlers) vm.runInContext(handler, context);
        assert.equal(context.injected, undefined);
        assert.ok(calls.includes(`${root}/${name}`));
        assert.ok(calls.every(value => value === '/' || value === `${root}/${name}`));
    });
}

test('breadcrumbs preserve quoted paths without executing their contents', () => {
    const currentPath = `/safe/evil');globalThis.injected=true;///"<img>`;
    const {App, context, breadcrumbs} = fixture('list', currentPath);
    App.Files.renderBreadcrumbs(currentPath);
    assert.ok(!breadcrumbs.innerHTML.includes('<img>'));
    const calls = [];
    App.Files.navigateToSegment = value => calls.push(value);
    context.event = {stopPropagation() {}};
    for (const match of breadcrumbs.innerHTML.matchAll(/onclick="([^"]*)"/g)) vm.runInContext(decode(match[1]), context);
    assert.equal(context.injected, undefined);
    assert.ok(calls.some(value => value.includes("evil');globalThis.injected=true;")));
});
