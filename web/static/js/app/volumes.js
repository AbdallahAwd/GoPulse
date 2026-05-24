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

        if (volumes.length === 0) {
            tbody.innerHTML = '<tr><td colspan="4" class="text-center py-4 text-gray-400">No volumes found</td></tr>';
            return;
        }

        tbody.innerHTML = volumes.map(v => `
            <tr class="hover:bg-gray-700 transition-colors">
                <td class="px-4 py-3 font-medium text-white">${v.name}</td>
                <td class="px-4 py-3 text-gray-400">${v.driver}</td>
                <td class="px-4 py-3 text-gray-400 truncate max-w-xs" title="${v.mountpoint}">${v.mountpoint}</td>
                <td class="px-4 py-3 text-right">
                    <button onclick="App.Docker.Volumes.remove('${v.name}')" class="text-red-400 hover:text-red-300 hover:bg-red-900/30 p-2 rounded transition-colors" title="Remove Volume">
                        <i class="fas fa-trash-alt"></i>
                    </button>
                </td>
            </tr>
        `).join('');
    },

    async remove(name) {
        if (!confirm('Are you sure you want to remove this volume?')) return;
        try {
            await App.API.delete(`/volumes/${name}`);
            App.UI.toast('success', 'Volume removed successfully');
            this.load();
        } catch (e) { /* Handled */ }
    }
};
