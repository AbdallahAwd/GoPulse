/* Shared console feedback and dialogs. No backend actions are performed here. */
(() => {
    const UI = App.UI;
    let toastTimer;
    UI.dismissToast = () => {
        clearTimeout(toastTimer);
        UI.el('toast')?.classList.add('hidden');
    };
    UI.toast = (type, message) => {
        const root = UI.el('toast');
        if (!root) return;
        clearTimeout(toastTimer);
        // A native modal makes body siblings inert; keep feedback inside its top layer.
        const container = document.querySelector('dialog[open]') || document.body;
        if (root.parentElement !== container) container.append(root);
        const severity = ['success', 'error', 'warning', 'info'].includes(type) ? type : 'info';
        const titles = {success: 'Completed', error: 'Something went wrong', warning: 'Attention needed', info: 'Notice'};
        const icons = {success: 'fa-check', error: 'fa-circle-exclamation', warning: 'fa-triangle-exclamation', info: 'fa-circle-info'};
        root.dataset.severity = severity;
        UI.el('toast-icon').className = `fas ${icons[severity]}`;
        UI.el('toast-title').textContent = titles[severity];
        UI.el('toast-message').textContent = String(message);
        root.classList.remove('hidden', 'translate-y-32');
        // Keep errors available long enough to read; subsequent messages own their timer.
        toastTimer = setTimeout(UI.dismissToast, severity === 'error' ? 10000 : 5000);
    };

    let pendingDialog = Promise.resolve();
    function ask(message, options = {}, input = false) {
        const task = pendingDialog.then(() => new Promise(resolve => {
            const dialog = UI.el('ui-confirm-dialog');
            if (!dialog) { resolve(input ? null : false); return; }
            const field = UI.el('ui-confirm-input');
            UI.el('ui-confirm-title').textContent = options.title || (input ? 'Confirm by typing' : 'Confirm action');
            UI.el('ui-confirm-message').textContent = String(message);
            UI.el('ui-confirm-input-label').hidden = !input;
            field.value = '';
            field.required = input;
            const submit = UI.el('ui-confirm-submit');
            submit.textContent = options.confirmLabel || 'Confirm action';
            submit.className = `ui-button ${options.danger === false ? 'ui-button-primary' : 'ui-button-danger'}`;
            dialog.returnValue = 'cancel';
            const onClose = () => {
                dialog.removeEventListener('close', onClose);
                resolve(dialog.returnValue === 'confirm' ? (input ? field.value : true) : (input ? null : false));
            };
            dialog.addEventListener('close', onClose);
            dialog.showModal();
            if (input) field.focus();
        }));
        pendingDialog = task.catch(() => {});
        return task;
    }
    UI.confirm = (message, options) => ask(message, options);
    UI.prompt = (message, options) => ask(message, options, true);

    // Existing resource modules own opening/closing and cleanup. Add keyboard behavior
    // around them so Escape uses their close button (including terminal teardown).
    const opened = new Map();
    const selector = '.ui-dialog-overlay, #fw-rule-drawer, dialog:not(#ui-confirm-dialog)';
    const visible = root => root.isConnected && !root.classList.contains('hidden') &&
        !root.classList.contains('translate-x-full') && (root.tagName !== 'DIALOG' || root.open) && root.getClientRects().length > 0;
    const focusable = root => Array.from(root.querySelectorAll('button, a[href], input:not([type="hidden"]), select, textarea, [tabindex]'))
        .filter(el => !el.disabled && el.tabIndex >= 0 && el.getClientRects().length > 0);
    const top = () => Array.from(opened.keys()).filter(visible)
        .sort((a, b) => Number(getComputedStyle(a).zIndex) - Number(getComputedStyle(b).zIndex)).pop();
    let labelSequence = 0;
    function syncDialogs() {
        for (const root of document.querySelectorAll(selector)) {
            const panel = root.tagName === 'DIALOG' || root.id === 'fw-rule-drawer' ? root : Array.from(root.children).find(el => el.tagName === 'DIV');
            if (!panel) continue;
            if (root.id === 'fw-rule-drawer') {
                root.inert = !visible(root);
                root.setAttribute('aria-hidden', String(!visible(root)));
            }
            if (!panel.classList.contains('ui-dialog-panel')) panel.classList.add('ui-dialog-panel');
            panel.setAttribute('role', 'dialog');
            panel.setAttribute('aria-modal', 'true');
            if (panel.tabIndex < 0) panel.tabIndex = -1;
            for (const label of panel.querySelectorAll('label:not([for])')) {
                if (label.querySelector('input, select, textarea')) continue;
                const sibling = label.nextElementSibling;
                if (!sibling) continue;
                const fields = sibling.matches('input, select, textarea') ? [sibling] : sibling.querySelectorAll('input, select, textarea');
                if (fields.length === 1 && fields[0].id) label.htmlFor = fields[0].id;
            }
            const title = panel.querySelector('h2, h3');
            if (title && !panel.hasAttribute('aria-label') && !panel.hasAttribute('aria-labelledby')) {
                if (!title.id) title.id = `ui-dialog-heading-${++labelSequence}`;
                panel.setAttribute('aria-labelledby', title.id);
            }
            const close = panel.querySelector('[data-close], button[onclick*="close"], button[onclick*="Close"]');
            if (close && !close.textContent.trim()) close.setAttribute('aria-label', 'Close dialog');
            if (visible(root) && !opened.has(root)) {
                opened.set(root, document.activeElement);
                if (root.tagName !== 'DIALOG') (focusable(root)[0] || panel).focus();
            }
        }
        for (const [root, opener] of opened) {
            if (!visible(root)) {
                opened.delete(root);
                if (opener?.isConnected && (!top() || top().contains(opener))) opener.focus();
            }
        }
        document.body.classList.toggle('ui-dialog-open', opened.size > 0);
    }
    document.addEventListener('close', () => {
        const toast = UI.el('toast');
        const container = document.querySelector('dialog[open]') || document.body;
        if (toast && toast.parentElement !== container) container.append(toast);
    }, true);
    document.addEventListener('keydown', event => {
        // Native dialog handles Escape, Tab, and focus restoration on its own.
        if (UI.el('ui-confirm-dialog')?.open) {
            if (event.key === 'Escape') event.stopImmediatePropagation();
            return;
        }
        const root = top();
        if (!root || root.tagName === 'DIALOG') return;
        if (event.key === 'Escape') {
            const close = root.querySelector('[data-close], button[onclick*="close"], button[onclick*="Close"]');
            if (close) { event.preventDefault(); event.stopImmediatePropagation(); close.click(); }
        }
        if (event.key === 'Tab') {
            const items = focusable(root);
            if (!items.length) { event.preventDefault(); return; }
            const first = items[0], last = items[items.length - 1];
            if (event.shiftKey && (document.activeElement === first || !root.contains(document.activeElement))) {
                event.preventDefault(); last.focus();
            } else if (!event.shiftKey && (document.activeElement === last || !root.contains(document.activeElement))) {
                event.preventDefault(); first.focus();
            }
        }
    }, true);
    new MutationObserver(syncDialogs).observe(document.body, {subtree: true, childList: true, attributes: true, attributeFilter: ['class', 'open']});
    syncDialogs();
})();
