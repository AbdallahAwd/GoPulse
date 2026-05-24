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
             tbody.innerHTML = '<tr><td colspan="5" class="text-center py-4"><i class="fas fa-spinner fa-spin mr-2"></i>Loading networks...</td></tr>';
        }

        try {
            const response = await App.API.get('/networks');
            this.render(response.data || []);
        } catch (e) {
             tbody.innerHTML = '<tr><td colspan="5" class="text-center py-4 text-red-400">Failed to load networks</td></tr>';
        }
    },

    render(networks) {
        const tbody = App.UI.el('networks-tbody');
        if (!tbody) return;

        if (networks.length === 0) {
            tbody.innerHTML = '<tr><td colspan="5" class="text-center py-4 text-gray-400">No networks found</td></tr>';
            return;
        }

        tbody.innerHTML = networks.map(n => `
            <tr class="hover:bg-gray-700 transition-colors">
                <td class="px-4 py-3 font-mono text-xs text-gray-400" title="${n.id}">${n.id.substring(0, 12)}</td>
                <td class="px-4 py-3 text-white font-medium">${n.name}</td>
                <td class="px-4 py-3 text-gray-400">${n.driver}</td>
                <td class="px-4 py-3 text-gray-400">${n.scope}</td>
                <td class="px-4 py-3 text-right">
                    <button onclick="App.Docker.Networks.remove('${n.id}')" class="text-red-400 hover:text-red-300 hover:bg-red-900/30 p-2 rounded transition-colors" title="Remove Network">
                        <i class="fas fa-trash-alt"></i>
                    </button>
                </td>
            </tr>
        `).join('');
    },

    async remove(id) {
        if (!confirm('Are you sure you want to remove this network?')) return;
        try {
            await App.API.delete(`/networks/${id}`);
            App.UI.toast('success', 'Network removed successfully');
            this.load();
        } catch (e) { /* Handled */ }
    }
};
