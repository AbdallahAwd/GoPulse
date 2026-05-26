/**
 * GoPulse App - Main Entry Point
 * Initialization, Event Listeners, and Polling
 */

function init() {
    if (!App.config.SERVER_ID) return;

    // Initialize Charts
    App.Charts.init();

    // Initialize System Settings and configurations
    App.System.init();

    // Tabs
    document.querySelectorAll('.tab-btn').forEach(btn => {
        btn.addEventListener('click', function() {
            const tab = this.dataset.tab;
            
            // UI Update
            document.querySelectorAll('.tab-btn').forEach(b => {
                b.classList.remove('border-blue-500', 'bg-gradient-to-r', 'from-blue-600/10', 'to-purple-600/10', 'text-white');
                b.classList.add('border-transparent', 'text-gray-400', 'hover:text-white', 'hover:bg-gray-700/30');
                
                // Reset icon color
                const icon = b.querySelector('i');
                if (icon) {
                    icon.classList.remove('text-blue-400');
                    icon.classList.add('text-gray-400');
                }
            });
            this.classList.remove('border-transparent', 'text-gray-400', 'hover:text-white', 'hover:bg-gray-700/30');
            this.classList.add('border-blue-500', 'bg-gradient-to-r', 'from-blue-600/10', 'to-purple-600/10', 'text-white');
            
            // Set active icon color
            const activeIcon = this.querySelector('i');
            if (activeIcon) {
                activeIcon.classList.remove('text-gray-400');
                activeIcon.classList.add('text-blue-400');
            }

            document.querySelectorAll('.tab-content').forEach(c => c.classList.add('hidden'));
            document.getElementById(`${tab}-tab`).classList.remove('hidden');

            // Logic
            if (tab === 'terminal') {
                App.Terminal.open();
            } else {
                App.Terminal.close();
            }
            if (tab === 'processes') App.Processes.load();
            if (tab === 'docker') App.Docker.refresh();
            if (tab === 'files') App.Files.list(App.state.currentPath);
            if (tab === 'projects') App.Projects.load();
            if (tab === 'domains') App.Domains.load();
            if (tab === 'databases') App.Databases.load();
            if (tab === 'system-users') App.SystemUsers.load();
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
