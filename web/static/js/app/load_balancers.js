/**
 * GoPulse App - Load Balancers Module
 * Load Balancer management: list, add, remove load balancers, and manage backends
 */

App.LoadBalancers = {
    loadBalancers: [],
    currentLbId: null,

    async load() {
        try {
            const res = await App.API.get(`/servers/${App.config.SERVER_ID}/load-balancers`);
            if (res.success) {
                this.loadBalancers = res.data || [];
                this.render();
                this.updateStats();
                if (this.currentLbId) {
                    this.showDetailsPanel(this.currentLbId);
                }
            }
        } catch (e) {
            console.error('Failed to load load balancers:', e);
        }
    },

    render() {
        const tbody = App.UI.el('lb-tbody');
        if (!tbody) return;

        if (!this.loadBalancers || this.loadBalancers.length === 0) {
            tbody.innerHTML = `
                <tr>
                    <td colspan="7" class="px-4 py-12 text-center text-gray-500">
                        <i class="fas fa-network-wired text-4xl mb-3 block opacity-30"></i>
                        <p>No load balancers configured yet</p>
                        <p class="text-xs mt-1 text-gray-600">Click "Add Load Balancer" to orchestrate multi-node routing</p>
                    </td>
                </tr>`;
            return;
        }

        tbody.innerHTML = this.loadBalancers.map(lb => {
            const created = lb.created_at ? new Date(lb.created_at).toLocaleDateString('en-US', {
                month: 'short', day: 'numeric', year: 'numeric'
            }) : '—';

            const sslBadge = lb.ssl
                ? '<span class="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-green-900/40 text-green-400 border border-green-800"><i class="fas fa-lock mr-1"></i>HTTPS</span>'
                : '<span class="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-gray-700 text-gray-400"><i class="fas fa-lock-open mr-1"></i>HTTP</span>';

            let statusBadge = '';
            if (lb.status === 'active') {
                statusBadge = '<span class="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-green-900/30 text-green-400 border border-green-800/50"><span class="h-1.5 w-1.5 rounded-full bg-green-400 mr-1.5 animate-pulse"></span>Active</span>';
            } else if (lb.status === 'error') {
                statusBadge = `<span onclick="App.LoadBalancers.showLogs('${lb.id}')" class="cursor-pointer inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-red-900/30 text-red-400 border border-red-800/50 hover:bg-red-900/50"><span class="h-1.5 w-1.5 rounded-full bg-red-400 mr-1.5"></span>Error <i class="fas fa-terminal ml-1 text-[10px]"></i></span>`;
            } else {
                statusBadge = '<span class="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-yellow-900/30 text-yellow-400 border border-yellow-800/50"><span class="h-1.5 w-1.5 rounded-full bg-yellow-400 mr-1.5 animate-spin"></span>Configuring...</span>';
            }

            const totalBackends = lb.backends ? lb.backends.length : 0;
            const healthyBackends = lb.backends ? lb.backends.filter(b => b.status === 'healthy').length : 0;
            const backendBadge = totalBackends > 0
                ? `<span class="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium ${healthyBackends === totalBackends ? 'bg-emerald-950 text-emerald-400 border border-emerald-900' : 'bg-amber-950 text-amber-400 border border-amber-900'}">${healthyBackends}/${totalBackends} Healthy</span>`
                : '<span class="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-gray-700 text-gray-400">No Nodes</span>';

            const algoLabel = lb.algorithm === 'least_conn' ? 'Least Connections' : (lb.algorithm === 'ip_hash' ? 'IP Hash' : 'Round Robin');

            return `
                <tr class="hover:bg-gray-700/30 transition-colors">
                    <td class="px-4 py-3">
                        <div class="flex items-center space-x-3">
                            <div class="p-2 bg-blue-900/20 rounded-lg">
                                <i class="fas fa-network-wired text-blue-400"></i>
                            </div>
                            <div>
                                <div class="text-white font-medium">${this.escapeHtml(lb.name)}</div>
                                <div class="text-xs text-gray-500 font-mono">${this.escapeHtml(lb.domain)}</div>
                            </div>
                        </div>
                    </td>
                    <td class="px-4 py-3 font-mono text-sm text-gray-300">${lb.port}</td>
                    <td class="px-4 py-3 text-gray-300">${algoLabel}</td>
                    <td class="px-4 py-3">${sslBadge}</td>
                    <td class="px-4 py-3">${statusBadge}</td>
                    <td class="px-4 py-3">${backendBadge}</td>
                    <td class="px-4 py-3 text-right space-x-2">
                        <button onclick="App.LoadBalancers.showLogs('${lb.id}')"
                            class="text-amber-400 hover:text-amber-300 hover:bg-amber-900/30 px-3 py-1.5 rounded-lg transition-all text-sm font-semibold">
                            <i class="fas fa-terminal mr-1"></i>Logs
                        </button>
                        <button onclick="App.LoadBalancers.showDetailsPanel('${lb.id}')"
                            class="text-blue-400 hover:text-blue-300 hover:bg-blue-900/30 px-3 py-1.5 rounded-lg transition-all text-sm font-semibold">
                            <i class="fas fa-tasks mr-1"></i>Manage
                        </button>
                        <button onclick="App.LoadBalancers.removeLoadBalancer('${lb.id}', '${this.escapeHtml(lb.name)}')"
                            class="text-red-400 hover:text-red-300 hover:bg-red-900/30 px-3 py-1.5 rounded-lg transition-all text-sm font-semibold">
                            <i class="fas fa-trash-alt mr-1"></i>Remove
                        </button>
                    </td>
                </tr>`;
        }).join('');
    },

    updateStats() {
        let total = this.loadBalancers.length;
        let healthy = 0;
        let unhealthy = 0;

        this.loadBalancers.forEach(lb => {
            if (lb.backends) {
                lb.backends.forEach(b => {
                    if (b.status === 'healthy') healthy++;
                    else if (b.status === 'unhealthy') unhealthy++;
                });
            }
        });

        App.UI.el('lb-stats-total').textContent = total;
        App.UI.el('lb-stats-healthy').textContent = healthy;
        App.UI.el('lb-stats-unhealthy').textContent = unhealthy;
    },

    // --- Detail Panel (Manage Backends) ---
    showDetailsPanel(id) {
        this.currentLbId = id;
        const lb = this.loadBalancers.find(item => item.id === id);
        if (!lb) return;

        App.UI.el('lb-details-title').textContent = `Manage Backends - ${lb.name}`;
        let actions = document.getElementById('lb-deploy-actions');
        if (!actions) {actions=document.createElement('div');actions.id='lb-deploy-actions';App.UI.el('lb-details-domain').after(actions);}
        actions.innerHTML = `<button class="text-blue-300 mr-4" onclick="App.LoadBalancerWizard.edit('${lb.id}')">Edit configuration</button><button class="text-blue-300" onclick="App.LoadBalancerWizard.retry('${lb.id}')">Deploy / Retry verification</button>`;
        App.UI.el('lb-details-domain').textContent = `${lb.domain} (Algorithm: ${lb.algorithm})`;
        App.UI.el('lb-details-panel').classList.remove('hidden');

        // Render Error message banner if status is error
        const errBanner = App.UI.el('lb-error-banner');
        const errMsg = App.UI.el('lb-error-message');
        if (errBanner && errMsg) {
            if (lb.error_message) {
                errMsg.textContent = lb.error_message;
                errBanner.classList.remove('hidden');
            } else {
                errBanner.classList.add('hidden');
            }
        }

        const tbody = App.UI.el('lb-backends-tbody');
        if (!tbody) return;

        if (!lb.backends || lb.backends.length === 0) {
            tbody.innerHTML = `
                <tr>
                    <td colspan="6" class="px-4 py-8 text-center text-gray-500">
                        <i class="fas fa-server text-2xl mb-2 block opacity-30"></i>
                        No backend target nodes defined. Click "Add Backend Target" to route traffic.
                    </td>
                </tr>`;
            return;
        }

        tbody.innerHTML = lb.backends.map(b => {
            const role = b.backup
                ? '<span class="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-purple-900/40 text-purple-400 border border-purple-800">Backup Failover</span>'
                : '<span class="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-blue-900/40 text-blue-400 border border-blue-800">Primary Node</span>';

            let statusBadge = '';
            if (b.status === 'healthy') {
                statusBadge = '<span class="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-green-950 text-green-400 border border-green-900"><i class="fas fa-check-circle mr-1"></i>Healthy</span>';
            } else if (b.status === 'unhealthy') {
                statusBadge = '<span class="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-red-950 text-red-400 border border-red-900"><i class="fas fa-exclamation-circle mr-1"></i>Unhealthy</span>';
            } else {
                statusBadge = '<span class="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-gray-700 text-gray-400"><i class="fas fa-question-circle mr-1"></i>Unknown</span>';
            }

            const targetName = b.target_project_id
                ? `<div class="font-medium text-white">${this.escapeHtml(b.host)}</div><div class="text-xs text-gray-500 font-mono">Project Target: ${b.target_project_id.substring(0,8)}...</div>`
                : `<div class="font-medium text-white">${this.escapeHtml(b.host)}</div><div class="text-xs text-gray-500 font-mono">Custom target</div>`;

            return `
                <tr class="hover:bg-gray-700/20 transition-colors">
                    <td class="px-4 py-3">${targetName}</td>
                    <td class="px-4 py-3 font-mono text-sm text-gray-300">${b.port}</td>
                    <td class="px-4 py-3 font-mono text-sm text-gray-300">${b.weight}</td>
                    <td class="px-4 py-3">${role}</td>
                    <td class="px-4 py-3">${statusBadge}</td>
                    <td class="px-4 py-3 text-right">
                        <button onclick="App.LoadBalancers.removeBackend('${b.id}')"
                            class="text-red-400 hover:text-red-300 hover:bg-red-900/20 px-2.5 py-1 rounded transition-colors text-xs font-semibold">
                            <i class="fas fa-trash mr-1"></i>Remove
                        </button>
                    </td>
                </tr>`;
        }).join('');
    },

    hideDetailsPanel() {
        this.currentLbId = null;
        App.UI.el('lb-details-panel').classList.add('hidden');
    },

    // --- Modal Add Load Balancer ---
    showAddModal() {
        const modal = App.UI.el('add-lb-modal');
        if (!modal) return;
        App.UI.el('add-lb-name').value = '';
        App.UI.el('add-lb-domain').value = '';
        App.UI.el('add-lb-port').value = '80';
        App.UI.el('add-lb-algorithm').value = 'round-robin';
        App.UI.el('add-lb-health-path').value = '/health';
        App.UI.el('add-lb-health-interval').value = '10';
        App.UI.el('add-lb-ssl').checked = false;

        modal.classList.remove('hidden');
        requestAnimationFrame(() => {
            modal.classList.remove('opacity-0');
            modal.querySelector('div').classList.remove('scale-95');
            modal.querySelector('div').classList.add('scale-100');
        });
    },

    closeAddModal() {
        const modal = App.UI.el('add-lb-modal');
        if (!modal) return;
        modal.classList.add('opacity-0');
        modal.querySelector('div').classList.remove('scale-100');
        modal.querySelector('div').classList.add('scale-95');
        setTimeout(() => modal.classList.add('hidden'), 300);
    },

    async addLoadBalancer() {
        const name = App.UI.el('add-lb-name').value.trim();
        const domain = App.UI.el('add-lb-domain').value.trim();
        const port = parseInt(App.UI.el('add-lb-port').value, 10);
        const algorithm = App.UI.el('add-lb-algorithm').value;
        const healthCheckPath = App.UI.el('add-lb-health-path').value.trim();
        const healthCheckInterval = parseInt(App.UI.el('add-lb-health-interval').value, 10);
        const ssl = App.UI.el('add-lb-ssl').checked;

        if (!name || !domain) {
            App.UI.toast('error', 'Name and domain are required');
            return;
        }
        if (!port || port <= 0 || port > 65535) {
            App.UI.toast('error', 'Please enter a valid port (1-65535)');
            return;
        }

        const btn = App.UI.el('add-lb-submit-btn');
        const originalContent = btn ? btn.innerHTML : '';
        if (btn) {
            btn.innerHTML = `<i class="fas fa-circle-notch fa-spin mr-2"></i>Deploying...`;
            btn.disabled = true;
            btn.classList.add('opacity-75', 'cursor-not-allowed');
        }

        try {
            const res = await App.API.post(`/servers/${App.config.SERVER_ID}/load-balancers`, {
                name, domain, port, algorithm, ssl, health_check_path: healthCheckPath, health_check_interval: healthCheckInterval
            });
            if (res.success) {
                App.UI.toast('success', 'Load balancer deployment queued successfully');
                this.closeAddModal();
                this.load();
            } else {
                App.UI.toast('error', res.error || 'Failed to add load balancer');
            }
        } catch (e) {
            // Handled
        } finally {
            if (btn) {
                btn.innerHTML = originalContent;
                btn.disabled = false;
                btn.classList.remove('opacity-75', 'cursor-not-allowed');
            }
        }
    },

    async removeLoadBalancer(id, name) {
        if (!await App.UI.confirm(`Are you sure you want to remove "${name}"?\n\nThis will clean up the Nginx configuration on the entry-point VPS.`)) {
            return;
        }

        try {
            const res = await App.API.delete(`/servers/${App.config.SERVER_ID}/load-balancers/${id}?confirm=${encodeURIComponent(id)}`);
            if (res.success) {
                App.UI.toast('success', 'Load balancer removed successfully');
                if (this.currentLbId === id) this.hideDetailsPanel();
                this.load();
            } else {
                App.UI.toast('error', res.error || 'Failed to remove load balancer');
            }
        } catch (e) {
            // Handled
        }
    },

    // --- Modal Add Backend Target ---
    async showAddBackendModal() {
        const modal = App.UI.el('add-lb-backend-modal');
        if (!modal) return;

        App.UI.el('add-backend-type').value = 'project';
        App.UI.el('add-backend-host').value = '';
        App.UI.el('add-backend-port').value = '';
        App.UI.el('add-backend-weight').value = '1';
        App.UI.el('add-backend-backup').checked = false;

        this.toggleBackendTypeForm();

        // Populate Servers Dropdown
        const srvSelect = App.UI.el('add-backend-server');
        srvSelect.innerHTML = '<option value="">Select Target Server...</option>';
        try {
            const res = await App.API.get('/servers');
            if (res.success && res.data) {
                res.data.forEach(s => {
                    srvSelect.innerHTML += `<option value="${s.id}">${this.escapeHtml(s.name)} (${s.host})</option>`;
                });
            }
        } catch (e) {
            console.error('Failed to load server list:', e);
        }

        modal.classList.remove('hidden');
        requestAnimationFrame(() => {
            modal.classList.remove('opacity-0');
            modal.querySelector('div').classList.remove('scale-95');
            modal.querySelector('div').classList.add('scale-100');
        });
    },

    closeAddBackendModal() {
        const modal = App.UI.el('add-lb-backend-modal');
        if (!modal) return;
        modal.classList.add('opacity-0');
        modal.querySelector('div').classList.remove('scale-100');
        modal.querySelector('div').classList.add('scale-95');
        setTimeout(() => modal.classList.add('hidden'), 300);
    },

    toggleBackendTypeForm() {
        const type = App.UI.el('add-backend-type').value;
        if (type === 'project') {
            App.UI.el('backend-project-section').classList.remove('hidden');
            App.UI.el('backend-custom-section').classList.add('hidden');
        } else {
            App.UI.el('backend-project-section').classList.add('hidden');
            App.UI.el('backend-custom-section').classList.remove('hidden');
        }
    },

    async onServerSelect() {
        const serverID = App.UI.el('add-backend-server').value;
        const projSelect = App.UI.el('add-backend-project');
        projSelect.innerHTML = '<option value="">Select Project...</option>';

        if (!serverID) return;

        try {
            const res = await App.API.get(`/servers/${serverID}/projects`);
            if (res.success && res.data) {
                res.data.forEach(p => {
                    projSelect.innerHTML += `<option value="${p.id}">${this.escapeHtml(p.name)} (${p.port})</option>`;
                });
            }
        } catch (e) {
            console.error('Failed to load server projects:', e);
        }
    },

    async addBackend() {
        if (!this.currentLbId) return;

        const type = App.UI.el('add-backend-type').value;
        const weight = parseInt(App.UI.el('add-backend-weight').value, 10);
        const backup = App.UI.el('add-backend-backup').checked;

        let targetServerID = '';
        let targetProjectID = '';
        let host = '';
        let port = 0;

        if (type === 'project') {
            targetServerID = App.UI.el('add-backend-server').value;
            targetProjectID = App.UI.el('add-backend-project').value;

            if (!targetServerID || !targetProjectID) {
                App.UI.toast('error', 'Please select server and project');
                return;
            }

            // Obtain server and project details to store fallback host & port
            const srvSelect = App.UI.el('add-backend-server');
            const optSrv = srvSelect.options[srvSelect.selectedIndex].text;
            // Extrapolate host from dropdown label "Name (Host)"
            const matches = optSrv.match(/\(([^)]+)\)/);
            host = matches ? matches[1] : '127.0.0.1';

            const projSelect = App.UI.el('add-backend-project');
            const optProj = projSelect.options[projSelect.selectedIndex].text;
            const portMatch = optProj.match(/\(([^)]+)\)/);
            port = portMatch ? parseInt(portMatch[1], 10) : 80;
        } else {
            host = App.UI.el('add-backend-host').value.trim();
            port = parseInt(App.UI.el('add-backend-port').value, 10);

            if (!host || !port) {
                App.UI.toast('error', 'Please fill host and port');
                return;
            }
        }

        const btn = App.UI.el('add-backend-submit-btn');
        if (btn) {
            btn.disabled = true;
            btn.innerHTML = `<i class="fas fa-circle-notch fa-spin mr-2"></i>Adding...`;
        }

        try {
            const res = await App.API.post(`/servers/${App.config.SERVER_ID}/load-balancers/${this.currentLbId}/backends`, {
                target_server_id: targetServerID,
                target_project_id: targetProjectID,
                host,
                port,
                weight,
                backup
            });

            if (res.success) {
                this.closeAddBackendModal();
                const root = App.Infrastructure.dialog('Deploy backend change', '<p>Deployment queued…</p>');
                await App.Infrastructure.track(res.data.operation_id, root, () => this.load());
            } else {
                App.UI.toast('error', res.error || 'Failed to add backend');
            }
        } catch (e) {
            // Handled
        } finally {
            if (btn) {
                btn.disabled = false;
                btn.innerHTML = 'Add Backend';
            }
        }
    },

    async removeBackend(backendID) {
        if (!this.currentLbId) return;

        if (!await App.UI.confirm('Are you sure you want to remove this backend node? This will trigger an Nginx reload.')) {
            return;
        }

        try {
            const res = await App.API.delete(`/servers/${App.config.SERVER_ID}/load-balancers/${this.currentLbId}/backends/${backendID}`);
            if (res.success) {
                const root = App.Infrastructure.dialog('Deploy backend removal', '<p>Deployment queued…</p>');
                await App.Infrastructure.track(res.data.operation_id, root, () => this.load());
            } else {
                App.UI.toast('error', res.error || 'Failed to remove backend');
            }
        } catch (e) {
            // Handled
        }
    },

    // --- Logs Viewer ---
    currentLogLbId: null,
    currentLogTab: 'deploy',
    logData: null,

    async showLogs(id) {
        this.currentLogLbId = id;
        const lb = this.loadBalancers.find(item => item.id === id);
        if (!lb) return;

        const modal = App.UI.el('lb-logs-modal');
        const title = App.UI.el('lb-logs-title');
        const output = App.UI.el('lb-logs-output');

        if (!modal || !title || !output) return;

        title.textContent = `${lb.name} (${lb.domain})`;
        output.innerHTML = '<div class="text-gray-500 mb-1 opacity-50"># Fetching logs via SSH...</div>';

        // Reset to default tab
        this.switchLogsTab('deploy');

        // Show modal with animation
        modal.classList.remove('hidden');
        setTimeout(() => {
            modal.classList.remove('opacity-0');
            modal.querySelector('div').classList.remove('scale-95');
        }, 50);

        // Fetch logs
        await this.refreshLogs();
    },

    async refreshLogs() {
        if (!this.currentLogLbId) return;
        const output = App.UI.el('lb-logs-output');
        if (!output) return;

        output.innerHTML = '<div class="text-gray-500 mb-1 opacity-50"># Fetching logs via SSH...</div>';

        try {
            const res = await App.API.get(`/servers/${App.config.SERVER_ID}/load-balancers/${this.currentLogLbId}/logs`);
            if (res.success && res.data) {
                this.logData = res.data;
                this.renderLogs();
            } else {
                output.innerHTML = `<div class="text-red-500"># Error: ${res.error || 'Failed to fetch logs'}</div>`;
            }
        } catch (e) {
            output.innerHTML = `<div class="text-red-500"># Connection lost or error fetching logs: ${e.message || e}</div>`;
        }
    },

    closeLogsModal() {
        const modal = App.UI.el('lb-logs-modal');
        if (!modal) return;

        modal.classList.add('opacity-0');
        modal.querySelector('div').classList.add('scale-95');
        setTimeout(() => {
            modal.classList.add('hidden');
            this.currentLogLbId = null;
            this.logData = null;
        }, 300);
    },

    switchLogsTab(tab) {
        this.currentLogTab = tab;
        
        // Update tab buttons style
        const tabs = document.querySelectorAll('.lb-log-tab');
        tabs.forEach(btn => {
            if (btn.id === `lb-tab-${tab}`) {
                btn.classList.remove('text-gray-400', 'hover:text-white');
                btn.classList.add('text-white', 'bg-blue-600');
            } else {
                btn.classList.remove('text-white', 'bg-blue-600');
                btn.classList.add('text-gray-400', 'hover:text-white');
            }
        });

        this.renderLogs();
    },

    renderLogs() {
        const output = App.UI.el('lb-logs-output');
        if (!output || !this.logData) return;

        let content = '';
        if (this.currentLogTab === 'deploy') {
            content = this.logData.deployment_error 
                ? `# Captured configuration/SSL deployment trace:\n\n${this.logData.deployment_error}`
                : '# No deployment errors found. Load balancer deployed successfully!';
        } else if (this.currentLogTab === 'access') {
            content = this.logData.access_logs && this.logData.access_logs.trim()
                ? this.logData.access_logs
                : '# No Nginx access logs recorded for this domain yet.';
        } else if (this.currentLogTab === 'error') {
            content = this.logData.error_logs && this.logData.error_logs.trim()
                ? this.logData.error_logs
                : '# No Nginx error logs recorded for this domain yet.';
        }

        output.textContent = content;
    },

    // --- Helpers ---
    escapeHtml(str) {
        if (!str) return '';
        const div = document.createElement('div');
        div.textContent = str;
        return div.innerHTML;
    }
};
