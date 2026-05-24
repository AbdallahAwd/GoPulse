/**
 * GoPulse App - Domains Module
 * Domain management: list, add, remove domains and check Nginx status
 */

App.Domains = {
    domains: [],

    async load() {
        // Check if Nginx is installed first
        const installed = await this.checkNginxInstalled();
        if (!installed) return;

        try {
            const res = await App.API.get('/domains');
            if (res.success) {
                this.domains = res.data || [];
                this.render();
                this.checkNginx();
            }
        } catch (e) {
            console.error('Failed to load domains:', e);
        }
    },

    async checkNginxInstalled() {
        const overlay = document.getElementById('nginx-not-installed');
        const content = document.getElementById('domains-content');
        if (!overlay || !content) return true;

        try {
            const res = await App.API.get('/system/check/nginx');
            if (res.success && res.data && res.data.installed) {
                overlay.classList.add('hidden');
                content.classList.remove('hidden');
                return true;
            } else {
                overlay.classList.remove('hidden');
                content.classList.add('hidden');
                return false;
            }
        } catch (e) {
            overlay.classList.add('hidden');
            content.classList.remove('hidden');
            return true;
        }
    },

    async installNginx() {
        const btn = document.getElementById('install-nginx-btn');
        const terminal = document.getElementById('nginx-install-terminal');
        const output = document.getElementById('nginx-install-output');

        if (!btn || !terminal || !output) return;

        // Loading state
        btn.innerHTML = '<i class="fas fa-circle-notch fa-spin mr-2"></i>Installing Nginx...';
        btn.disabled = true;
        btn.classList.add('opacity-75', 'cursor-not-allowed');
        terminal.classList.remove('hidden');
        output.textContent = '[System] Starting Nginx installation...\n';

        try {
            const response = await fetch(`${App.config.API_BASE}/servers/${App.config.SERVER_ID}/system/install/nginx`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' }
            });

            const reader = response.body.getReader();
            const decoder = new TextDecoder();

            while (true) {
                const { done, value } = await reader.read();
                if (done) break;
                output.textContent += decoder.decode(value, { stream: true });
                output.scrollTop = output.scrollHeight;
            }

            App.UI.toast('success', 'Nginx installed successfully!');

            setTimeout(() => {
                btn.innerHTML = '<i class="fas fa-check mr-2"></i>Installed!';
                btn.classList.remove('opacity-75', 'cursor-not-allowed');
                btn.classList.replace('bg-green-600', 'bg-emerald-600');
                this.checkNginxInstalled();
                this.load();
            }, 1500);
        } catch (e) {
            output.textContent += `\n[Error] Installation failed: ${e.message}\n`;
            btn.innerHTML = '<i class="fas fa-download mr-2"></i>Retry Install';
            btn.disabled = false;
            btn.classList.remove('opacity-75', 'cursor-not-allowed');
            App.UI.toast('error', 'Nginx installation failed');
        }
    },

    render() {
        const tbody = App.UI.el('domains-tbody');
        if (!tbody) return;

        if (!this.domains || this.domains.length === 0) {
            tbody.innerHTML = `
                <tr>
                    <td colspan="5" class="px-4 py-12 text-center text-gray-500">
                        <i class="fas fa-globe text-4xl mb-3 block opacity-30"></i>
                        <p>No domains configured yet</p>
                        <p class="text-xs mt-1 text-gray-600">Click "Add Domain" to map a domain to a port</p>
                    </td>
                </tr>`;
            return;
        }

        tbody.innerHTML = this.domains.map(d => {
            const created = d.created_at ? new Date(d.created_at).toLocaleDateString('en-US', {
                month: 'short', day: 'numeric', year: 'numeric'
            }) : '—';

            const sslBadge = d.ssl
                ? '<span class="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-green-900/40 text-green-400 border border-green-800"><i class="fas fa-lock mr-1"></i>Enabled</span>'
                : '<span class="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-gray-700 text-gray-400"><i class="fas fa-lock-open mr-1"></i>Off</span>';

            return `
                <tr class="hover:bg-gray-700/50 transition-colors">
                    <td class="px-4 py-3">
                        <div class="flex items-center space-x-3">
                            <div class="p-2 bg-blue-900/20 rounded-lg">
                                <i class="fas fa-globe text-blue-400"></i>
                            </div>
                            <div>
                                <div class="text-white font-medium">${this.escapeHtml(d.domain)}</div>
                                <div class="text-xs text-gray-500 font-mono">→ 127.0.0.1:${d.port}</div>
                            </div>
                        </div>
                    </td>
                    <td class="px-4 py-3">
                        <span class="font-mono text-blue-400 bg-blue-900/20 px-2 py-1 rounded text-sm">${d.port}</span>
                    </td>
                    <td class="px-4 py-3">${sslBadge}</td>
                    <td class="px-4 py-3 text-gray-400 text-sm">${created}</td>
                    <td class="px-4 py-3 text-right">
                        <button onclick="App.Domains.removeDomain('${d.id}', '${this.escapeHtml(d.domain)}')"
                            class="text-red-400 hover:text-red-300 hover:bg-red-900/30 px-3 py-1.5 rounded-lg transition-all text-sm">
                            <i class="fas fa-trash-alt mr-1"></i>Remove
                        </button>
                    </td>
                </tr>`;
        }).join('');
    },

    // --- Modal ---
    showAddModal() {
        const modal = App.UI.el('add-domain-modal');
        if (!modal) return;
        // Reset fields
        App.UI.el('add-domain-name').value = '';
        App.UI.el('add-domain-port').value = '';
        App.UI.el('add-domain-ssl').checked = false;

        modal.classList.remove('hidden');
        requestAnimationFrame(() => {
            modal.classList.remove('opacity-0');
            modal.querySelector('div').classList.remove('scale-95');
            modal.querySelector('div').classList.add('scale-100');
        });
    },

    closeAddModal() {
        const modal = App.UI.el('add-domain-modal');
        if (!modal) return;
        modal.classList.add('opacity-0');
        modal.querySelector('div').classList.remove('scale-100');
        modal.querySelector('div').classList.add('scale-95');
        setTimeout(() => modal.classList.add('hidden'), 300);
    },

    // --- Actions ---
    async addDomain() {
        const domain = App.UI.el('add-domain-name').value.trim();
        const port = parseInt(App.UI.el('add-domain-port').value, 10);
        const ssl = App.UI.el('add-domain-ssl').checked;

        if (!domain) {
            App.UI.toast('error', 'Domain name is required');
            return;
        }
        if (!port || port <= 0 || port > 65535) {
            App.UI.toast('error', 'Please enter a valid port (1-65535)');
            return;
        }

        // Show loading state on submit button
        const btn = App.UI.el('add-domain-submit-btn');
        const originalContent = btn ? btn.innerHTML : '';
        if (btn) {
            btn.innerHTML = `<i class="fas fa-circle-notch fa-spin mr-2"></i>${ssl ? 'Configuring SSL...' : 'Adding Domain...'}`;
            btn.disabled = true;
            btn.classList.add('opacity-75', 'cursor-not-allowed');
        }

        try {
            const res = await App.API.post('/domains', { domain, port, ssl });
            if (res.success) {
                App.UI.toast('success', res.message || 'Domain added successfully');
                this.closeAddModal();
                this.load();
            } else {
                App.UI.toast('error', res.error || 'Failed to add domain');
            }
        } catch (e) {
            // Error already handled by App.API
        } finally {
            if (btn) {
                btn.innerHTML = originalContent;
                btn.disabled = false;
                btn.classList.remove('opacity-75', 'cursor-not-allowed');
            }
        }
    },

    async removeDomain(id, name) {
        if (!confirm(`Are you sure you want to remove "${name}"?\n\nThis will also remove the Nginx configuration.`)) {
            return;
        }

        try {
            const res = await App.API.delete(`/domains/${id}`);
            if (res.success) {
                App.UI.toast('success', 'Domain removed successfully');
                this.load();
            } else {
                App.UI.toast('error', res.error || 'Failed to remove domain');
            }
        } catch (e) {
            // Error already handled by App.API
        }
    },

    // --- Nginx Status ---
    async checkNginx() {
        const dot = App.UI.el('nginx-status-dot');
        const text = App.UI.el('nginx-status-text');
        const badge = App.UI.el('nginx-status-badge');
        if (!dot || !text || !badge) return;

        try {
            const res = await App.API.get('/domains/nginx/status');
            if (res.success && res.data) {
                const status = res.data.status;
                if (status === 'active') {
                    dot.className = 'h-2 w-2 rounded-full bg-green-500 animate-pulse';
                    text.textContent = 'Nginx Active';
                    badge.className = 'flex items-center space-x-2 px-3 py-1 rounded-full text-xs font-medium bg-green-900/30 text-green-400 border border-green-800';
                } else if (status === 'not-installed') {
                    dot.className = 'h-2 w-2 rounded-full bg-yellow-500';
                    text.textContent = 'Nginx Not Installed';
                    badge.className = 'flex items-center space-x-2 px-3 py-1 rounded-full text-xs font-medium bg-yellow-900/30 text-yellow-400 border border-yellow-800';
                } else {
                    dot.className = 'h-2 w-2 rounded-full bg-red-500';
                    text.textContent = `Nginx: ${status}`;
                    badge.className = 'flex items-center space-x-2 px-3 py-1 rounded-full text-xs font-medium bg-red-900/30 text-red-400 border border-red-800';
                }
            }
        } catch (e) {
            dot.className = 'h-2 w-2 rounded-full bg-gray-500';
            text.textContent = 'Status Unknown';
            badge.className = 'flex items-center space-x-2 px-3 py-1 rounded-full text-xs font-medium bg-gray-700 text-gray-400';
        }
    },

    // --- Helpers ---
    escapeHtml(str) {
        const div = document.createElement('div');
        div.textContent = str;
        return div.innerHTML;
    }
};
