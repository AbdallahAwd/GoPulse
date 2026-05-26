/**
 * GoPulse App - System Module
 * Handles system operations like shutdown, restart, and Server configurations
 */

App.System = {
    currentServer: null,

    init: async function() {
        console.log('System module initialized');
        
        // Fetch server details on-demand to initialize settings
        if (App.config.SERVER_ID) {
            try {
                const res = await App.API.get(`/servers/${App.config.SERVER_ID}`);
                if (res.success && res.data) {
                    this.currentServer = res.data;
                    
                    // Populate Settings Inputs
                    const hostInput = document.getElementById('settings-server-host');
                    if (hostInput) hostInput.value = res.data.host;
                    
                    const nameInput = document.getElementById('settings-server-name');
                    if (nameInput) nameInput.value = res.data.name;

                    // Synchronize toggle switch state
                    this.syncFocusToggleState(res.data.is_db_only);
                    
                    // If dedicated database host focus is active and user lands on the default "processes" tab,
                    // automatically redirect the tab display directly to "databases"!
                    const hashTab = window.location.hash.substring(1);
                    if (res.data.is_db_only && (!hashTab || hashTab === 'processes')) {
                        setTimeout(() => {
                            const dbBtn = document.getElementById('nav-btn-databases');
                            if (dbBtn) dbBtn.click();
                        }, 50);
                    }
                }
            } catch (e) {
                console.error('Failed to load server configuration', e);
            }
        }
    },

    syncFocusToggleState: function(isDbOnly) {
        const toggle = document.getElementById('server-db-focus-toggle');
        const knob = document.getElementById('server-db-focus-knob');
        if (!toggle || !knob) return;

        if (isDbOnly) {
            toggle.classList.remove('bg-gray-700');
            toggle.classList.add('bg-blue-600');
            knob.classList.remove('translate-x-1');
            knob.classList.add('translate-x-6');
        } else {
            toggle.classList.remove('bg-blue-600');
            toggle.classList.add('bg-gray-700');
            knob.classList.remove('translate-x-6');
            knob.classList.add('translate-x-1');
        }
    },

    toggleDbFocus: function() {
        if (!this.currentServer) return;
        this.currentServer.is_db_only = !this.currentServer.is_db_only;
        this.syncFocusToggleState(this.currentServer.is_db_only);
    },

    saveServerSettings: async function() {
        if (!this.currentServer) return;

        const nameInput = document.getElementById('settings-server-name');
        if (nameInput) {
            this.currentServer.name = nameInput.value.trim();
            if (!this.currentServer.name) {
                App.UI.toast('error', 'Server name is required.');
                return;
            }
        }

        try {
            App.UI.toast('info', 'Saving server configurations...');
            const res = await App.API.put(`/servers/${App.config.SERVER_ID}`, this.currentServer);
            if (res.success) {
                App.UI.toast('success', 'Server configurations saved successfully!');
                
                // Short reload delay to apply layout and tab transitions cleanly
                setTimeout(() => {
                    window.location.reload();
                }, 800);
            } else {
                App.UI.toast('error', `Save failed: ${res.message}`);
            }
        } catch (e) {
            App.UI.toast('error', 'Failed to save server settings.');
        }
    },

    showPowerModal: function() {
        const modal = document.getElementById('power-modal');
        if (modal) {
            modal.classList.remove('hidden');
            const warning = document.getElementById('shutdown-warning');
            if (warning) warning.classList.add('hidden');
            
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
                document.body.innerHTML = `
                    <div class="fixed inset-0 bg-black flex flex-col items-center justify-center text-white z-50">
                        <i class="fas fa-sync-alt fa-spin text-5xl text-blue-500 mb-6"></i>
                        <h1 class="text-2xl font-bold mb-2">Restarting Remote Server...</h1>
                        <p class="text-gray-400">Please wait while the server restarts.</p>
                    </div>
                `;
                
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
