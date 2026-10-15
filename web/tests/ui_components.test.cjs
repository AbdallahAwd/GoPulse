const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');

function harness() {
    function element() {
        const classes = new Set(['hidden']);
        return {
            classList: {add: (...names) => names.forEach(name => classes.add(name)), remove: (...names) => names.forEach(name => classes.delete(name)), contains: name => classes.has(name), toggle() {}},
            dataset: {}, textContent: '', value: '', focus() {},
        };
    }
    const ids = ['toast', 'toast-title', 'toast-icon', 'toast-message', 'ui-confirm-dialog', 'ui-confirm-title', 'ui-confirm-message', 'ui-confirm-input-label', 'ui-confirm-input', 'ui-confirm-submit'];
    const elements = Object.fromEntries(ids.map(id => [id, element()]));
    const dialog = elements['ui-confirm-dialog'];
    const listeners = new Map();
    dialog.showModal = () => { assert.equal(dialog.open, false); dialog.open = true; };
    dialog.open = false;
    dialog.addEventListener = (name, callback) => listeners.set(name, callback);
    dialog.removeEventListener = name => listeners.delete(name);
    dialog.close = value => { dialog.returnValue = value; dialog.open = false; listeners.get('close')(); };
    let sequence = 0;
    const timers = new Map();
    const context = {
        App: {UI: {el: id => elements[id]}},
        document: {body: {...element(), append() {}}, querySelector: () => null, querySelectorAll: () => [], addEventListener() {}},
        MutationObserver: class { observe() {} },
        setTimeout(fn) { const id = ++sequence; timers.set(id, fn); return id; },
        clearTimeout(id) { timers.delete(id); },
    };
    vm.createContext(context);
    vm.runInContext(fs.readFileSync(path.join(__dirname, '../static/js/app/ui.js'), 'utf8'), context);
    return {UI: context.App.UI, elements, dialog, timers};
}
const flush = async () => { await Promise.resolve(); await Promise.resolve(); await Promise.resolve(); };

test('destructive action waits for confirmation; Cancel and Escape cannot approve it', async () => {
    const {UI, dialog} = harness();
    let deletions = 0;
    const remove = async () => { if (await UI.confirm('Remove server?')) deletions++; };
    const action = remove();
    await flush();
    assert.equal(dialog.open, true);
    assert.equal(deletions, 0);
    dialog.close('cancel');
    await action;
    assert.equal(deletions, 0);
    const approved = remove();
    await flush();
    dialog.close('confirm');
    await approved;
    assert.equal(deletions, 1);
});

test('concurrent confirmations are queued and each owns its result', async () => {
    const {UI, dialog, elements} = harness();
    const first = UI.confirm('First');
    const second = UI.confirm('Second');
    await flush();
    assert.equal(elements['ui-confirm-message'].textContent, 'First');
    dialog.close('cancel');
    assert.equal(await first, false);
    await flush();
    assert.equal(elements['ui-confirm-message'].textContent, 'Second');
    dialog.close('confirm');
    assert.equal(await second, true);
});

test('typed confirmation returns exact text, cancellation returns null, and remote text is not HTML', async () => {
    const {UI, dialog, elements} = harness();
    const typed = UI.prompt('<img src=x onerror=alert(1)>');
    await flush();
    assert.equal(elements['ui-confirm-message'].textContent, '<img src=x onerror=alert(1)>');
    assert.equal(elements['ui-confirm-input-label'].hidden, false);
    elements['ui-confirm-input'].value = 'collection-1';
    dialog.close('confirm');
    assert.equal(await typed, 'collection-1');
    const cancelled = UI.prompt('Again');
    await flush();
    assert.equal(elements['ui-confirm-input'].value, '');
    dialog.close('cancel');
    assert.equal(await cancelled, null);
});

test('a new toast owns its dismissal timer and safely renders its message', () => {
    const {UI, elements, timers} = harness();
    UI.toast('success', 'Old message');
    UI.toast('error', '<script>unsafe()</script>');
    assert.equal(timers.size, 1);
    assert.equal(elements.toast.dataset.severity, 'error');
    assert.equal(elements['toast-message'].textContent, '<script>unsafe()</script>');
    assert.equal(elements.toast.classList.contains('hidden'), false);
    UI.dismissToast();
    assert.equal(elements.toast.classList.contains('hidden'), true);
    assert.equal(timers.size, 0);
});
