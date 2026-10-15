/**
 * GoPulse App - Volumes Module
 */

App.Docker = App.Docker || {};

App.Docker.Volumes = {
    async load() {
        if (!App.config.SERVER_ID) return;
        const tbody = App.UI.el('volumes-tbody');
        if (!tbody) return;

        try {
            const response = await App.API.get('/volumes');
            const volumes = response.data || [];

            // Cache fallback logic
            if (volumes.length === 0 && App.state.lastVolumes.length > 0) {
                 App.UI.toast('error', 'Received empty volume list, using cached data');
                 return;
            }
            App.state.lastVolumes = volumes;

            this.render(volumes);
        } catch (e) {
             if (App.state.lastVolumes.length === 0) {
                tbody.innerHTML = '<tr><td colspan="4" class="text-center py-4 text-red-400">Failed to load volumes</td></tr>';
             } else {
                App.UI.toast('error', 'Failed to refresh volumes, keeping cached data');
             }
        }
    },

    render(volumes) {
        const tbody = App.UI.el('volumes-tbody');
        if (!tbody) return;

        const count = App.UI.el('docker-volumes-count');
        if (count) count.textContent = `${volumes.length} ${volumes.length === 1 ? 'volume' : 'volumes'}`;
        if (volumes.length === 0) { tbody.innerHTML = '<tr><td colspan="4" class="docker-table-state">No volumes found</td></tr>'; return; }
        const text = value => App.Docker.escapeHTML(value);
        tbody.innerHTML = volumes.map(v => `<tr><td class="docker-resource-name font-mono">${text(v.name)}</td><td class="docker-table-muted">${text(v.driver)}</td><td class="docker-table-path" title="${text(v.mountpoint)}">${text(v.mountpoint)}</td><td><div class="docker-row-actions"><button onclick="App.Docker.Volumes.remove(${App.Docker.handlerArgument(v.name)})" class="docker-icon-button docker-danger-button" title="Remove volume" aria-label="Remove volume ${text(v.name)}"><i class="far fa-trash-alt" aria-hidden="true"></i></button></div></td></tr>`).join('');
    },

    async remove(name) {
        if (!await App.UI.confirm('Are you sure you want to remove this volume?')) return;
        try {
            await App.API.delete(`/volumes/${name}`);
            App.UI.toast('success', 'Volume removed successfully');
            this.load();
        } catch (e) { /* Handled */ }
    }
};
