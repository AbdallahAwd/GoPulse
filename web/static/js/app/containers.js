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

        const count = App.UI.el('docker-containers-count');
        if (count) count.textContent = `${containers.length} ${containers.length === 1 ? 'container' : 'containers'}`;
        if (containers.length === 0) {
            grid.innerHTML = `<div class="docker-empty-state"><i class="fas fa-box-open" aria-hidden="true"></i><h3>No containers yet</h3><p>Run a local image or build a Docker Compose project to create containers.</p></div>`;
            return;
        }
        const text = value => App.Docker.escapeHTML(value);
        const arg = value => App.Docker.handlerArgument(value);
        grid.innerHTML = containers.map(c => `<article class="docker-container-card">
            <div class="docker-container-heading"><div><h3 title="${text(c.name)}">${text(c.name)}</h3><p class="docker-container-id">${text(String(c.id).substring(0,12))}</p></div><span class="app-status" data-status="${text(c.state)}"><span aria-hidden="true"></span>${text(c.state)}</span></div>
            <dl class="docker-container-details"><div><dt>Image</dt><dd title="${text(c.image)}">${text(c.image)}</dd></div><div><dt>Status</dt><dd>${text(c.status)}</dd></div></dl>
            <div class="docker-container-actions">
                <button onclick="App.Docker.Containers.logs(${arg(c.id)})" class="console-button secondary"><i class="fas fa-terminal" aria-hidden="true"></i>Logs</button>
                ${c.state === 'running' ? `<button id="btn-stop-${text(c.id)}" onclick="App.Docker.Containers.action(${arg(c.id)}, 'stop')" class="console-button secondary"><i class="fas fa-stop" aria-hidden="true"></i>Stop</button>` : `<button id="btn-start-${text(c.id)}" onclick="App.Docker.Containers.action(${arg(c.id)}, 'start')" class="console-button secondary"><i class="fas fa-play" aria-hidden="true"></i>Start</button>`}
                <details class="docker-container-menu"><summary aria-label="More actions for ${text(c.name)}"><i class="fas fa-ellipsis" aria-hidden="true"></i></summary><div>
                    <button id="btn-restart-${text(c.id)}" onclick="App.Docker.Containers.action(${arg(c.id)}, 'restart')" class="docker-inline-button"><i class="fas fa-rotate" aria-hidden="true"></i>Restart</button>
                    <button id="btn-delete-${text(c.id)}" onclick="App.Docker.Containers.action(${arg(c.id)}, 'delete')" class="docker-inline-button docker-danger-button"><i class="far fa-trash-alt" aria-hidden="true"></i>Remove container</button>
                </div></details>
            </div>
        </article>`).join('');
    },

    async action(id, actionType) {
        if (actionType === 'delete' && !await App.UI.confirm('Are you sure you want to remove this container?')) return;
        
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

                    return `<div class="docker-log-line"><span class="docker-log-time">[${time}]</span><span class="${colorClass}">${safeMessage}</span></div>`;
                }).join('');
                
                // Scroll to bottom
                output.scrollTop = output.scrollHeight;
            } else {
                 output.innerHTML = '<div class="text-red-500">Failed to load logs.</div>';
            }
        } catch (e) {
            output.innerHTML = `<div class="text-red-500">Error: ${App.Docker.escapeHTML(e.message)}</div>`;
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
