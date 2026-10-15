/**
 * GoPulse App - Networks Module
 */

App.Docker = App.Docker || {};

App.Docker.Networks = {
    async load() {
        if (!App.config.SERVER_ID) return;
        const tbody = App.UI.el('networks-tbody');
        if (!tbody) return;

        // Show loading
        if (!tbody.hasChildNodes() || tbody.children[0].innerText.includes('Loading')) {
             tbody.innerHTML = '<tr><td colspan="4" class="text-center py-4"><i class="fas fa-spinner fa-spin mr-2"></i>Loading networks...</td></tr>';
        }

        try {
            const response = await App.API.get('/networks');
            this.render(response.data || []);
        } catch (e) {
             tbody.innerHTML = '<tr><td colspan="4" class="text-center py-4 text-red-400">Failed to load networks</td></tr>';
        }
    },

    render(networks) {
        const tbody = App.UI.el('networks-tbody');
        if (!tbody) return;

        const count = App.UI.el('docker-networks-count');
        if (count) count.textContent = `${networks.length} ${networks.length === 1 ? 'network' : 'networks'}`;
        if (networks.length === 0) { tbody.innerHTML = '<tr><td colspan="4" class="docker-table-state">No networks found</td></tr>'; return; }
        const text = value => App.Docker.escapeHTML(value);
        tbody.innerHTML = networks.map(n => `<tr><td><div class="docker-resource-name">${text(n.name)}</div><div class="docker-resource-id" title="${text(n.id)}">${text(String(n.id).substring(0,12))}</div></td><td class="docker-table-muted">${text(n.driver)}</td><td class="docker-table-muted">${text(n.scope)}</td><td><div class="docker-row-actions"><button onclick="App.Docker.Networks.remove(${App.Docker.handlerArgument(n.id)})" class="docker-icon-button docker-danger-button" title="Remove network" aria-label="Remove network ${text(n.name)}"><i class="far fa-trash-alt" aria-hidden="true"></i></button></div></td></tr>`).join('');
    },

    async remove(id) {
        if (!await App.UI.confirm('Are you sure you want to remove this network?')) return;
        try {
            await App.API.delete(`/networks/${id}`);
            App.UI.toast('success', 'Network removed successfully');
            this.load();
        } catch (e) { /* Handled */ }
    }
};
