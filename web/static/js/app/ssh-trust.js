// Discovery is read-only. Only the explicit trust button approves the displayed
// public key; neither a cancelled review nor a failed probe changes saved trust.
App.SSHTrust = {
    async review(endpoint, address, isCurrent = () => true) {
        const result = await App.API.post(endpoint, address);
        if (!result.success || !result.data) throw new Error(result.error || 'Unable to fetch SSH host key.');
        if (!isCurrent()) return null;
        const key = result.data;
        const changed = key.trusted_fingerprint && key.trusted_fingerprint !== key.fingerprint;
        const host = key.host.includes(':') ? `[${key.host}]` : key.host;
        const message = [
            changed ? 'WARNING: This server is presenting a different SSH host key.' : 'Review the SSH host key before trusting this server.',
            `${host}:${key.port}`,
            `Algorithm: ${key.algorithm}`,
            `Fingerprint: ${key.fingerprint}`,
            ...(changed ? [`Previously trusted: ${key.trusted_fingerprint}`] : []),
            'Compare this fingerprint with your hosting provider console or another trusted source. Fetching a key does not verify the server’s identity.',
            changed ? 'Trust the replacement only after confirming why the key changed.' : 'Trusting saves this exact key for future SSH connections.'
        ].join('\n\n');
        const approved = await App.UI.confirm(message, {
            title: changed ? 'SSH host key changed' : 'Verify SSH host key',
            confirmLabel: changed ? 'Trust replacement key' : 'Trust this key',
            danger: Boolean(changed)
        });
        return approved && isCurrent() ? key : null;
    },
    async verifyServer(server, button) {
        button.disabled = true;
        try {
            const endpoint = `/servers/${encodeURIComponent(server.id)}/ssh-host-key`;
            const key = await this.review(endpoint, {});
            if (!key) return;
            const result = await App.API.put(endpoint, key);
            if (!result.success) throw new Error(result.error || 'Unable to save SSH host key.');
            App.UI.toast('success', 'SSH host key trusted. You can now reopen the terminal.');
        } catch (error) {
            App.UI.toast('error', error.message);
        } finally {
            button.disabled = false;
        }
    }
};
