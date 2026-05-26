/**
 * GoPulse App - Containers Module
 */

App.Docker = App.Docker || {};

App.Docker.Containers = {
    async load() {
        if (!App.config.SERVER_ID) return;
        try {
            const response = await App.API.get('/containers');
            this.render(response.data || []);
        } catch (e) { /* Handled */ }
    },

    render(containers) {
        const grid = App.UI.el('containers-grid');
        if (!grid) return;

        if (containers.length === 0 && App.state.lastContainers && App.state.lastContainers.length > 0) {
             App.UI.toast('error', 'Using cached container data');
             return;
        }
        App.state.lastContainers = containers;

        if (containers.length === 0) {
            grid.innerHTML = `<div class="col-span-full text-center py-8 text-gray-400"><i class="fas fa-box-open text-4xl mb-2"></i><p>No containers found</p></div>`;
            return;
        }

        grid.innerHTML = containers.map(c => `
            <div class="bg-gray-700 rounded-lg p-4 ${c.state === 'running' ? 'container-running' : 'container-stopped'} transition-all duration-300 hover:shadow-xl">
                <div class="flex items-start justify-between mb-3">
                    <div class="flex-1 min-w-0">
                        <h3 class="font-bold text-lg text-white truncate" title="${c.name}">${c.name}</h3>
                        <p class="text-sm text-gray-400 truncate" title="${c.image}">${c.image}</p>
                    </div>
                    <span class="px-2 py-1 text-xs rounded-full ${c.state === 'running' ? 'bg-green-900 text-green-300' : 'bg-red-900 text-red-300'}">${c.state}</span>
                </div>
                <p class="text-xs text-gray-500 mb-4">${c.status}</p>
                <div class="flex space-x-2">
                    ${c.state === 'running' ? 
                        `<button id="btn-stop-${c.id}" onclick="App.Docker.Containers.action('${c.id}', 'stop')" class="flex-1 bg-red-600 hover:bg-red-700 px-3 py-2 rounded text-sm text-white transition-colors"><i class="fas fa-stop mr-1"></i>Stop</button>` :
                        `<button id="btn-start-${c.id}" onclick="App.Docker.Containers.action('${c.id}', 'start')" class="flex-1 bg-green-600 hover:bg-green-700 px-3 py-2 rounded text-sm text-white transition-colors"><i class="fas fa-play mr-1"></i>Start</button>`
                    }
                    <button id="btn-restart-${c.id}" onclick="App.Docker.Containers.action('${c.id}', 'restart')" class="flex-1 bg-blue-600 hover:bg-blue-700 px-3 py-2 rounded text-sm text-white transition-colors"><i class="fas fa-sync mr-1"></i>Restart</button>
                    <button id="btn-delete-${c.id}" onclick="App.Docker.Containers.action('${c.id}', 'delete')" class="px-3 py-2 text-red-400 hover:text-red-300 rounded text-sm"><i class="fas fa-trash"></i></button>
                </div>
                <div class="mt-2 text-right">
                     <button onclick="App.Docker.Containers.logs('${c.id}')" class="text-xs text-gray-400 hover:text-white"><i class="fas fa-terminal mr-1"></i>Logs</button>
                </div>
            </div>
        `).join('');
    },

    async action(id, actionType) {
        if (actionType === 'delete' && !confirm('Are you sure you want to remove this container?')) return;
        
        const btnId = `btn-${actionType}-${id}`;
        const btn = document.getElementById(btnId);
        let originalContent = '';
        
        if (btn) {
            originalContent = btn.innerHTML;
            btn.innerHTML = `<i class="fas fa-circle-notch fa-spin"></i>`;
            btn.disabled = true;
            btn.classList.add('opacity-75', 'cursor-not-allowed');
        }

        // Map delete to remove for API
        const apiAction = actionType === 'delete' ? '' : `/${actionType}`; // Delete is DELETE /containers/id, others are POST /containers/id/action
        const method = actionType === 'delete' ? 'DELETE' : 'POST';
        const url = actionType === 'delete' ? `/containers/${id}` : `/containers/${id}${apiAction}`;

        try {
            if (method === 'DELETE') {
                 await App.API.delete(url);
            } else {
                 await App.API.post(url, {});
            }
            App.UI.toast('success', `Container action ${actionType} successful`);
            this.load();
        } catch (e) { 
            // Only restore if error, otherwise load() will replace the buttons
            if (btn) {
                btn.innerHTML = originalContent;
                btn.disabled = false;
                btn.classList.remove('opacity-75', 'cursor-not-allowed');
            }
            App.UI.toast('error', `Action failed: ${e.message}`);
        }
    },

    currentLogsContainerId: null,

    logs(id) {
        this.currentLogsContainerId = id;
        const modal = document.getElementById('docker-logs-modal');
        const title = document.getElementById('docker-logs-title');
        const output = document.getElementById('docker-logs-output');
        
        // Find container name for title
        const container = App.state.lastContainers ? App.state.lastContainers.find(c => c.id === id) : null;
        title.textContent = container ? container.name : id.substring(0, 12);
        
        output.innerHTML = '<div class="text-gray-500 mb-1 opacity-50"># Fetching logs...</div>';
        
        modal.classList.remove('hidden');
        setTimeout(() => {
            modal.classList.remove('opacity-0');
            modal.querySelector('div').classList.remove('scale-95');
        }, 10);
        
        this.fetchLogs(id);
    },

    async fetchLogs(id) {
        const output = document.getElementById('docker-logs-output');
        try {
            const response = await App.API.get(`/containers/${id}/logs`);
            if (response.success && response.data) {
                const logsList = response.data.logs || [];
                if (logsList.length === 0) {
                     output.innerHTML = '<div class="text-gray-500 opacity-50"># No logs found.</div>';
                     return;
                }
                
                output.innerHTML = logsList.map(entry => {
                    const time = new Date(entry.timestamp).toLocaleTimeString();
                    // Simple color coding based on message content
                    let colorClass = 'text-gray-300';
                    const msg = entry.message.toLowerCase();
                    if (msg.includes('error') || msg.includes('fail') || msg.includes('exception')) colorClass = 'text-red-400';
                    else if (msg.includes('warn')) colorClass = 'text-yellow-400';
                    else if (msg.includes('info')) colorClass = 'text-blue-300';
                    
                    // Escape HTML in message
                    const safeMessage = entry.message.replace(/&/g, "&amp;")
                        .replace(/</g, "&lt;")
                        .replace(/>/g, "&gt;")
                        .replace(/"/g, "&quot;")
                        .replace(/'/g, "&#039;");

                    return `<div class="font-mono text-xs hover:bg-gray-900 px-1 rounded select-text">
                        <span class="text-gray-600 mr-2 select-none">[${time}]</span>
                        <span class="${colorClass}">${safeMessage}</span>
                    </div>`;
                }).join('');
                
                // Scroll to bottom
                output.scrollTop = output.scrollHeight;
            } else {
                 output.innerHTML = '<div class="text-red-500">Failed to load logs.</div>';
            }
        } catch (e) {
            output.innerHTML = `<div class="text-red-500">Error: ${e.message}</div>`;
        }
    },

    refreshLogs() {
        if (this.currentLogsContainerId) {
            this.fetchLogs(this.currentLogsContainerId);
        }
    },

    closeLogs() {
        const modal = document.getElementById('docker-logs-modal');
        modal.classList.add('opacity-0');
        modal.querySelector('div').classList.add('scale-95');
        setTimeout(() => {
            modal.classList.add('hidden');
            this.currentLogsContainerId = null;
        }, 300);
    }
};

// Aliases for backward compatibility if needed, or just update main.js
window.loadContainers = () => App.Docker.Containers.load();
