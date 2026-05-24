/**
 * GoPulse App - Main Entry Point
 * Initialization, Event Listeners, and Polling
 */

function init() {
    if (!App.config.SERVER_ID) return;

    // Tabs
    document.querySelectorAll('.tab-btn').forEach(btn => {
        btn.addEventListener('click', function() {
            const tab = this.dataset.tab;
            
            // UI Update
            document.querySelectorAll('.tab-btn').forEach(b => {
                b.classList.remove('border-blue-500', 'text-blue-500');
                b.classList.add('border-transparent', 'text-gray-400');
            });
            this.classList.remove('border-transparent', 'text-gray-400');
            this.classList.add('border-blue-500', 'text-blue-500');

            document.querySelectorAll('.tab-content').forEach(c => c.classList.add('hidden'));
            document.getElementById(`${tab}-tab`).classList.remove('hidden');

            // Logic
            if (tab === 'processes') App.Processes.load();
            if (tab === 'docker') App.Docker.refresh();
            if (tab === 'files') App.Files.list(App.state.currentPath);
            if (tab === 'projects') App.Projects.load();
            if (tab === 'domains') App.Domains.load();
        });
    });

    // Search Debounce
    App.UI.el('process-search')?.addEventListener('input', function() {
        clearTimeout(App.state.searchTimeout);
        App.state.searchTimeout = setTimeout(() => App.Processes.load(), 500);
    });

    // Polling System
    const poll = async () => {
        try {
            const res = await App.API.get(`/stats?server_id=${App.config.SERVER_ID}`);
            if (res.success && res.data.stats) App.UI.updateStats(res.data.stats);
        } catch (e) {}
    };

    App.Processes.load();
    poll();
    setInterval(poll, 5000);
    setInterval(() => {
        if (document.querySelector('.tab-btn.border-blue-500')?.dataset.tab === 'processes') {
            App.Processes.load();
        }
    }, 10000);
}

// Ensure App is available globally (already done in core.js, but good to be sure)
window.App = App;

// Run
document.addEventListener('DOMContentLoaded', init);
