/**
 * GoPulse App - Processes Module
 */

App.Processes = {
    async load() {
        if (!App.config.SERVER_ID) return;
        const search = App.UI.el('process-search')?.value || '';
        const endpoint = search ? `/processes?q=${encodeURIComponent(search)}` : '/processes';

        try {
            const response = await App.API.get(endpoint);
            this.render(response.data || []);
        } catch (e) {
            // Error handled in API
        }
    },

    render(processes) {
        const tbody = App.UI.el('processes-tbody');
        if (!tbody) return;

        // Cache fallback
        if (processes.length === 0 && App.state.lastProcesses.length > 0) {
            App.UI.toast('error', 'Received empty process list, using cached data');
            return;
        }
        App.state.lastProcesses = processes;

        if (processes.length === 0) {
            tbody.innerHTML = `<tr><td colspan="7" class="px-4 py-8 text-center text-gray-400">No processes found</td></tr>`;
            return;
        }

        processes.sort((a, b) => b.cpu_percent - a.cpu_percent);
        const top50 = processes.slice(0, 50);

        tbody.innerHTML = top50.map(proc => `
            <tr class="hover:bg-gray-750">
                <td class="px-4 py-3">${proc.pid}</td>
                <td class="px-4 py-3 font-medium">${proc.name}</td>
                <td class="px-4 py-3"><span class="px-2 py-1 text-xs rounded-full ${proc.status === 'running' ? 'bg-green-900 text-green-300' : 'bg-gray-700 text-gray-300'}">${proc.status}</span></td>
                <td class="px-4 py-3">${proc.cpu_percent.toFixed(1)}%</td>
                <td class="px-4 py-3">${proc.memory_mb.toFixed(1)}</td>
                <td class="px-4 py-3 text-sm text-gray-400">${proc.username}</td>
                <td class="px-4 py-3">
                    <button onclick="App.Processes.kill(${proc.pid})" class="text-red-400 hover:text-red-300"><i class="fas fa-times-circle"></i></button>
                </td>
            </tr>
        `).join('');
    },

    async kill(pid) {
        if (!await App.UI.confirm(`Are you sure you want to kill process ${pid}?`)) return;
        try {
            await App.API.delete(`/processes/${pid}`);
            App.UI.toast('success', `Process ${pid} terminated`);
            this.load();
        } catch (e) { /* Handled */ }
    }
};

window.loadProcesses = () => App.Processes.load();
