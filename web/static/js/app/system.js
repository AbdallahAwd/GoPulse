/**
 * GoPulse App - System Module
 * Handles system operations like shutdown and restart
 */

App.System = {
    init: function() {
        console.log('System module initialized');
    },

    showPowerModal: function() {
        const modal = document.getElementById('power-modal');
        if (modal) {
            modal.classList.remove('hidden');
            // Reset state
            const warning = document.getElementById('shutdown-warning');
            if(warning) warning.classList.add('hidden');
            
            setTimeout(() => {
                modal.classList.remove('opacity-0');
                modal.querySelector('div').classList.remove('scale-95');
                modal.querySelector('div').classList.add('scale-100');
            }, 10);
        }
    },

    closePowerModal: function() {
        const modal = document.getElementById('power-modal');
        if (modal) {
            modal.classList.add('opacity-0');
            modal.querySelector('div').classList.remove('scale-100');
            modal.querySelector('div').classList.add('scale-95');
            setTimeout(() => {
                modal.classList.add('hidden');
            }, 300);
        }
    },

    restart: async function() {
        if (!confirm('Are you sure you want to restart the remote server?')) return;
        
        try {
            const endpoint = `/servers/${App.config.SERVER_ID}/system/restart`;
            App.UI.toast('info', 'Restarting remote server...');
            const res = await App.API.post(endpoint, {});
            if (res.success) {
                // Show overlay
                document.body.innerHTML = `
                    <div class="fixed inset-0 bg-black flex flex-col items-center justify-center text-white z-50">
                        <i class="fas fa-sync-alt fa-spin text-5xl text-blue-500 mb-6"></i>
                        <h1 class="text-2xl font-bold mb-2">Restarting Remote Server...</h1>
                        <p class="text-gray-400">Please wait while the server restarts.</p>
                    </div>
                `;
                
                // Attempt to reload after 30 seconds (remote reboot takes longer)
                setTimeout(() => {
                    window.location.reload();
                }, 30000);
            }
        } catch (err) {
            console.error(err);
            App.UI.toast('error', 'Failed to restart: ' + err.message);
        }
    },

    shutdown: function() {
        const warning = document.getElementById('shutdown-warning');
        if (warning) {
            warning.classList.remove('hidden');
        }
    },

    confirmShutdown: async function() {
        try {
            const endpoint = `/servers/${App.config.SERVER_ID}/system/shutdown`;
            const res = await App.API.post(endpoint, {});
            if (res.success) {
                this.closePowerModal();
                document.body.innerHTML = `
                    <div class="fixed inset-0 bg-black flex flex-col items-center justify-center text-white z-50">
                        <i class="fas fa-power-off text-5xl text-red-500 mb-6"></i>
                        <h1 class="text-2xl font-bold mb-2">Remote Server Shutdown</h1>
                        <p class="text-gray-400">The remote server has been shut down.</p>
                        <p class="text-gray-500 text-sm mt-4">You must manually start it again.</p>
                    </div>
                `;
            }
        } catch (err) {
            console.error(err);
            App.UI.toast('error', 'Failed to shutdown: ' + err.message);
        }
    }
};
