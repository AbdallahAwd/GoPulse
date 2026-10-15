/**
 * GoPulse App - Main Entry Point
 * Initialization, Event Listeners, and Polling
 */

function init() {
    if (!App.config.SERVER_ID) return;

    App.System.init();

    // Search Debounce
    App.UI.el('process-search')?.addEventListener('input', function() {
        clearTimeout(App.state.searchTimeout);
        App.state.searchTimeout = setTimeout(() => App.Processes.load(), 500);
    });

    // Polling System
    let statsLoading = false;
    const poll = async () => {
        if (statsLoading || document.hidden) return;
        statsLoading = true;
        try {
            if (App.Navigation.current !== 'overview') return;
            const res = await App.API.get(`/stats?server_id=${App.config.SERVER_ID}`);
            if (res.success && res.data.stats) App.UI.updateStats(res.data.stats);
        } catch (e) {} finally { statsLoading = false; }
    };

    poll();
    setInterval(poll, 5000);
    setInterval(() => {
        if (App.Navigation.current === 'processes') {
            App.Processes.load();
        }
    }, 10000);
}

// Ensure App is available globally (already done in core.js, but good to be sure)
window.App = App;

// Run
document.addEventListener('DOMContentLoaded', () => {
    App.Navigation.init();
    init();
    App.Workspace.init();
    App.Agents.init();
    App.DashboardSettings.init();
    App.Fleet.init();
});
