/**
 * GoPulse App - Firewall Module
 * VPS firewall management: UFW, firewalld, iptables, nftables
 */

App.Firewall = {
	state: {
		enabled: false,
		backend: 'none',
		defaultIncomingPolicy: 'deny',
		defaultOutgoingPolicy: 'allow',
		rules: [],
		backups: [],
		schedules: [],
		logs: [],
		selectedRuleIds: new Set(),
		activeSubTab: 'rules'
	},

	async load() {
		this.showLoading(true);
		try {
			await this.fetchData(false);
			await this.loadBackups();
			await this.loadSchedules();
			await this.loadLogs();
			this.render();
		} catch (e) {
			console.error('Failed to load firewall:', e);
		} finally {
			this.showLoading(false);
		}
	},

	async refresh() {
		this.showLoading(true);
		try {
			await this.fetchData(true);
			await this.loadBackups();
			await this.loadSchedules();
			await this.loadLogs();
			this.render();
			App.UI.toast('success', 'Firewall status synced from VPS successfully');
		} catch (e) {
			console.error('Failed to sync firewall from VPS:', e);
		} finally {
			this.showLoading(false);
		}
	},

	async fetchData(syncFromVPS = false) {
		const url = `/firewall` + (syncFromVPS ? '?refresh=true' : '');
		const res = await App.API.get(url);
		if (res.success && res.data) {
			this.state.enabled = res.data.state.enabled;
			this.state.backend = res.data.state.backend;
			this.state.defaultIncomingPolicy = res.data.state.default_incoming_policy;
			this.state.defaultOutgoingPolicy = res.data.state.default_outgoing_policy;
			this.state.rules = res.data.rules || [];
			this.state.selectedRuleIds.clear();
		}
	},

	async loadBackups() {
		try {
			const res = await App.API.get('/firewall/backups');
			if (res.success) {
				this.state.backups = res.data || [];
			}
		} catch (e) {
			console.error('Failed to load backups:', e);
		}
	},

	async loadSchedules() {
		try {
			const res = await App.API.get('/firewall/schedules');
			if (res.success) {
				this.state.schedules = res.data || [];
			}
		} catch (e) {
			console.error('Failed to load schedules:', e);
		}
	},

	async loadLogs() {
		try {
			const res = await App.API.get('/firewall/logs');
			if (res.success) {
				this.state.logs = res.data || [];
			}
		} catch (e) {
			console.error('Failed to load logs:', e);
		}
	},

	showLoading(isLoading) {
		const spinner = document.getElementById('firewall-loading');
		const content = document.getElementById('firewall-content');
		if (spinner && content) {
			if (isLoading) {
				spinner.classList.remove('hidden');
				content.classList.add('opacity-50');
			} else {
				spinner.classList.add('hidden');
				content.classList.remove('opacity-50');
			}
		}
	},

	render() {
		this.renderStatusCard();
		this.renderSubTabs();

		const noFw = document.getElementById('fw-not-installed-overlay');
		const fwContent = document.getElementById('fw-installed-content');
		if (noFw && fwContent) {
			if (this.state.backend === 'none') {
				noFw.classList.remove('hidden');
				fwContent.classList.add('hidden');
			} else {
				noFw.classList.add('hidden');
				fwContent.classList.remove('hidden');
				this.renderRulesTable();
				this.renderBackups();
				this.renderSchedules();
				this.renderLogs();
			}
		}
	},

	renderStatusCard() {
		// Enabled Badge / Status Card
		const statusBadge = document.getElementById('fw-status-badge');
		const statusText = document.getElementById('fw-status-text');
		const toggleBtn = document.getElementById('fw-toggle-btn');
		const toggleBtnIcon = document.getElementById('fw-toggle-btn-icon');
		const toggleBtnText = document.getElementById('fw-toggle-btn-text');

		if (statusBadge && statusText) {
			if (this.state.enabled) {
				statusBadge.className = 'inline-flex items-center px-3 py-1 rounded-full text-sm font-semibold bg-green-900/40 text-green-400 border border-green-800';
				statusBadge.innerHTML = '<span class="h-2 w-2 rounded-full bg-green-400 mr-2 pulse"></span>Active';
				statusText.textContent = 'Firewall is currently shielding your VPS.';
				if (toggleBtn) {
					toggleBtn.className = 'px-4 py-2 bg-red-600 hover:bg-red-500 text-white rounded-lg font-semibold shadow transition-all flex items-center space-x-2';
					toggleBtnText.textContent = 'Disable Firewall';
					toggleBtnIcon.className = 'fas fa-shield-alt';
				}
			} else {
				statusBadge.className = 'inline-flex items-center px-3 py-1 rounded-full text-sm font-semibold bg-red-900/40 text-red-400 border border-red-800';
				statusBadge.innerHTML = '<span class="h-2 w-2 rounded-full bg-red-400 mr-2"></span>Inactive';
				statusText.textContent = 'Firewall is disabled. Your server ports are exposed.';
				if (toggleBtn) {
					toggleBtn.className = 'px-4 py-2 bg-green-600 hover:bg-green-500 text-white rounded-lg font-semibold shadow transition-all flex items-center space-x-2';
					toggleBtnText.textContent = 'Enable Firewall';
					toggleBtnIcon.className = 'fas fa-shield-alt';
				}
			}
		}

		// Policies
		const incPolicy = document.getElementById('fw-inc-policy');
		const outPolicy = document.getElementById('fw-out-policy');
		if (incPolicy) {
			incPolicy.textContent = this.state.defaultIncomingPolicy.toUpperCase();
			if (this.state.defaultIncomingPolicy === 'deny' || this.state.defaultIncomingPolicy === 'reject') {
				incPolicy.className = 'text-lg font-black text-red-400';
			} else {
				incPolicy.className = 'text-lg font-black text-green-400';
			}
		}
		if (outPolicy) {
			outPolicy.textContent = this.state.defaultOutgoingPolicy.toUpperCase();
			if (this.state.defaultOutgoingPolicy === 'allow') {
				outPolicy.className = 'text-lg font-black text-green-400';
			} else {
				outPolicy.className = 'text-lg font-black text-red-400';
			}
		}

		// Engine / Rules Count
		const engineVal = document.getElementById('fw-engine-val');
		const rulesCountVal = document.getElementById('fw-rules-count-val');
		if (engineVal) engineVal.textContent = this.state.backend.toUpperCase();
		if (rulesCountVal) rulesCountVal.textContent = this.state.rules.length;
	},

	renderSubTabs() {
		// Manage subtabs visibility
		const tabs = ['rules', 'schedules', 'backups', 'logs'];
		tabs.forEach(t => {
			const btn = document.getElementById(`fw-tab-btn-${t}`);
			const content = document.getElementById(`fw-tab-content-${t}`);
			if (btn && content) {
				if (t === this.state.activeSubTab) {
					btn.classList.add('border-blue-500', 'text-blue-400');
					btn.classList.remove('border-transparent', 'text-gray-400');
					content.classList.remove('hidden');
				} else {
					btn.classList.remove('border-blue-500', 'text-blue-400');
					btn.classList.add('border-transparent', 'text-gray-400');
					content.classList.add('hidden');
				}
			}
		});
	},

	switchSubTab(tab) {
		this.state.activeSubTab = tab;
		this.render();
	},

	renderRulesTable() {
		const tbody = document.getElementById('fw-rules-tbody');
		if (!tbody) return;

		// Get filters & search values
		const searchVal = document.getElementById('fw-rules-search')?.value.toLowerCase() || '';
		const dirFilter = document.getElementById('fw-filter-direction')?.value || 'all';
		const protoFilter = document.getElementById('fw-filter-protocol')?.value || 'all';
		const actionFilter = document.getElementById('fw-filter-action')?.value || 'all';

		const filteredRules = this.state.rules.filter(r => {
			// Search match
			const matchSearch = r.port.toLowerCase().includes(searchVal) ||
				r.source.toLowerCase().includes(searchVal) ||
				r.interface.toLowerCase().includes(searchVal) ||
				r.comment.toLowerCase().includes(searchVal);

			// Direction match
			const matchDir = dirFilter === 'all' || r.direction === dirFilter;

			// Protocol match
			const matchProto = protoFilter === 'all' || r.protocol === protoFilter;

			// Action match
			const matchAction = actionFilter === 'all' || r.action === actionFilter;

			return matchSearch && matchDir && matchProto && matchAction;
		});

		// Bulk action toolbar visibility
		const bulkBar = document.getElementById('fw-bulk-bar');
		if (bulkBar) {
			if (this.state.selectedRuleIds.size > 0) {
				bulkBar.classList.remove('hidden');
				document.getElementById('fw-bulk-count').textContent = this.state.selectedRuleIds.size;
			} else {
				bulkBar.classList.add('hidden');
			}
		}

		if (filteredRules.length === 0) {
			tbody.innerHTML = `
                <tr>
                    <td colspan="8" class="px-4 py-12 text-center text-gray-500">
                        <i class="fas fa-shield-alt text-4xl mb-3 block opacity-30"></i>
                        <p>No firewall rules match your filters.</p>
                        <p class="text-xs mt-1 text-gray-600">Click "Add Rule" or select a preset to configure a port access rule</p>
                    </td>
                </tr>`;
			return;
		}

		tbody.innerHTML = filteredRules.map(r => {
			const isChecked = this.state.selectedRuleIds.has(r.id) ? 'checked' : '';

			const dirBadge = r.direction === 'ingress'
				? '<span class="inline-flex items-center text-xs text-indigo-400 bg-indigo-950/40 px-2 py-0.5 rounded border border-indigo-900/60"><i class="fas fa-sign-in-alt mr-1"></i>Ingress</span>'
				: '<span class="inline-flex items-center text-xs text-pink-400 bg-pink-950/40 px-2 py-0.5 rounded border border-pink-900/60"><i class="fas fa-sign-out-alt mr-1"></i>Egress</span>';

			const actionBadge = {
				allow: '<span class="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-green-900/30 text-green-400 border border-green-800/40">ALLOW</span>',
				deny: '<span class="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-red-900/30 text-red-400 border border-red-800/40">DENY</span>',
				reject: '<span class="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-red-900/30 text-red-400 border border-red-800/40">REJECT</span>',
				limit: '<span class="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-yellow-900/30 text-yellow-400 border border-yellow-800/40">LIMIT</span>'
			}[r.action] || r.action;

			const protoBadge = `<span class="px-1.5 py-0.5 bg-gray-700/50 rounded font-mono text-xs uppercase text-gray-300 border border-gray-600/40">${r.protocol}</span>`;

			const portText = r.port === 'any' || r.port === ''
				? '<span class="text-gray-500 italic">Any Port</span>'
				: `<span class="font-mono font-bold text-white">${r.port}</span>`;

			const srcBadge = r.source === 'any' || r.source === 'Anywhere' || r.source === ''
				? '<span class="inline-flex items-center px-1.5 py-0.5 bg-gray-800/60 text-gray-400 rounded text-xs border border-gray-700/50">Anywhere</span>'
				: `<span class="inline-flex items-center px-1.5 py-0.5 bg-blue-950/40 text-blue-400 rounded text-xs font-mono border border-blue-900/50">${r.source}</span>`;

			const interfaceBadge = r.interface === 'any' || r.interface === ''
				? '<span class="text-xs text-gray-500">all</span>'
				: `<span class="font-mono text-xs bg-gray-700 text-gray-300 px-1 py-0.5 rounded">${r.interface}</span>`;

			const statusToggle = `
                <label class="relative inline-flex items-center cursor-pointer select-none">
                    <input type="checkbox" class="sr-only peer" ${r.enabled ? 'checked' : ''} 
                        onchange="App.Firewall.toggleRuleEnabled('${r.id}', this.checked)">
                    <div class="w-9 h-5 bg-gray-700 peer-focus:outline-none rounded-full peer peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-gray-300 after:border-gray-300 after:border after:rounded-full after:h-4 after:w-4 after:transition-all peer-checked:bg-blue-600"></div>
                </label>`;

			return `
                <tr class="hover:bg-gray-800/40 border-b border-gray-700/30 transition-colors">
                    <td class="px-4 py-3 text-center">
                        <input type="checkbox" class="rounded text-blue-600 bg-gray-700 border-gray-600 focus:ring-blue-500/20 w-4 h-4" 
                            ${isChecked} onchange="App.Firewall.toggleRuleSelection('${r.id}', this.checked)">
                    </td>
                    <td class="px-4 py-3">${dirBadge}</td>
                    <td class="px-4 py-3">${actionBadge}</td>
                    <td class="px-4 py-3">${protoBadge}</td>
                    <td class="px-4 py-3">${portText}</td>
                    <td class="px-4 py-3">${srcBadge}</td>
                    <td class="px-4 py-3">${interfaceBadge}</td>
                    <td class="px-4 py-3 text-sm text-gray-400 max-w-xs truncate" title="${this.escapeHtml(r.comment)}">${this.escapeHtml(r.comment || '—')}</td>
                    <td class="px-4 py-3">${statusToggle}</td>
                    <td class="px-4 py-3 text-right">
                        <div class="flex items-center justify-end space-x-2">
                            <button onclick="App.Firewall.openEditRuleDrawer('${r.id}')" 
                                class="text-blue-400 hover:text-blue-300 p-1.5 hover:bg-blue-500/10 rounded-lg transition-all" title="Edit Rule">
                                <i class="fas fa-edit"></i>
                            </button>
                            <button onclick="App.Firewall.confirmDeleteRule('${r.id}', '${r.port}')" 
                                class="text-red-400 hover:text-red-300 p-1.5 hover:bg-red-500/10 rounded-lg transition-all" title="Delete Rule">
                                <i class="fas fa-trash-alt"></i>
                            </button>
                        </div>
                    </td>
                </tr>`;
		}).join('');
	},

	toggleAllRulesSelection(checked) {
		this.state.rules.forEach(r => {
			if (checked) {
				this.state.selectedRuleIds.add(r.id);
			} else {
				this.state.selectedRuleIds.delete(r.id);
			}
		});
		this.renderRulesTable();
	},

	toggleRuleSelection(id, checked) {
		if (checked) {
			this.state.selectedRuleIds.add(id);
		} else {
			this.state.selectedRuleIds.delete(id);
		}
		this.renderRulesTable();
	},

	async toggleRuleEnabled(id, enabled) {
		const rule = this.state.rules.find(r => r.id === id);
		if (!rule) return;

		this.showLoading(true);
		try {
			const res = await App.API.put(`/firewall/rules/${id}`, {
				...rule,
				enabled: enabled
			});
			if (res.success) {
				rule.enabled = enabled;
				App.UI.toast('success', `Rule successfully ${enabled ? 'enabled' : 'disabled'}`);
				await this.loadLogs();
				this.render();
			} else {
				// Revert checkbox
				this.load();
			}
		} catch (e) {
			this.load();
		}
	},

	// --- Drawers / Modals ---
	openAddRuleDrawer(preset = null) {
		const form = document.getElementById('fw-rule-form');
		if (form) form.reset();

		document.getElementById('fw-rule-id').value = '';
		document.getElementById('fw-rule-drawer-title').textContent = 'Add Firewall Rule';

		// Apply preset values if clicked
		if (preset) {
			document.getElementById('fw-rule-action').value = 'allow';
			document.getElementById('fw-rule-direction').value = 'ingress';
			document.getElementById('fw-rule-port').value = preset.port;
			document.getElementById('fw-rule-protocol').value = preset.protocol || 'tcp';
			document.getElementById('fw-rule-source').value = 'any';
			document.getElementById('fw-rule-interface').value = 'any';
			document.getElementById('fw-rule-comment').value = preset.comment || '';
		}

		const drawer = document.getElementById('fw-rule-drawer');
		if (drawer) {
			drawer.classList.remove('translate-x-full');
			drawer.classList.add('translate-x-0');
		}
	},

	openEditRuleDrawer(id) {
		const rule = this.state.rules.find(r => r.id === id);
		if (!rule) return;

		document.getElementById('fw-rule-id').value = rule.id;
		document.getElementById('fw-rule-action').value = rule.action;
		document.getElementById('fw-rule-direction').value = rule.direction;
		document.getElementById('fw-rule-port').value = rule.port === 'any' ? '' : rule.port;
		document.getElementById('fw-rule-protocol').value = rule.protocol;
		document.getElementById('fw-rule-source').value = rule.source === 'any' ? '' : rule.source;
		document.getElementById('fw-rule-interface').value = rule.interface === 'any' ? '' : rule.interface;
		document.getElementById('fw-rule-comment').value = rule.comment;

		document.getElementById('fw-rule-drawer-title').textContent = 'Edit Firewall Rule';

		const drawer = document.getElementById('fw-rule-drawer');
		if (drawer) {
			drawer.classList.remove('translate-x-full');
			drawer.classList.add('translate-x-0');
		}
	},

	closeRuleDrawer() {
		const drawer = document.getElementById('fw-rule-drawer');
		if (drawer) {
			drawer.classList.remove('translate-x-0');
			drawer.classList.add('translate-x-full');
		}
	},

	// --- Save Rule Action ---
	async saveRule() {
		const id = document.getElementById('fw-rule-id').value;
		const action = document.getElementById('fw-rule-action').value;
		const direction = document.getElementById('fw-rule-direction').value;
		const port = document.getElementById('fw-rule-port').value.trim() || 'any';
		const protocol = document.getElementById('fw-rule-protocol').value;
		const source = document.getElementById('fw-rule-source').value.trim() || 'any';
		const intf = document.getElementById('fw-rule-interface').value.trim() || 'any';
		const comment = document.getElementById('fw-rule-comment').value.trim();
		const scheduleTimeVal = document.getElementById('fw-rule-schedule-time').value;

		const payload = {
			action,
			direction,
			port,
			protocol,
			source,
			interface: intf,
			comment,
			enabled: true
		};

		// Validation on client-side
		if (port !== 'any') {
			if (port.includes(':')) {
				const parts = port.split(':');
				if (parts.length !== 2 || isNaN(parts[0]) || isNaN(parts[1])) {
					App.UI.toast('error', 'Port range must be in format START:END (e.g. 10000:20000)');
					return;
				}
			} else if (isNaN(port) || parseInt(port) < 1 || parseInt(port) > 65535) {
				App.UI.toast('error', 'Single Port must be a valid number between 1 and 65535');
				return;
			}
		}

		if (source !== 'any' && source !== 'Anywhere') {
			if (source.includes('/')) {
				const parts = source.split('/');
				if (parts.length !== 2 || isNaN(parts[1])) {
					App.UI.toast('error', 'CIDR format must be valid (e.g. 192.168.1.0/24)');
					return;
				}
			}
		}

		this.closeRuleDrawer();
		this.showLoading(true);

		try {
			// Schedule instead of instant save if schedule time is specified
			if (scheduleTimeVal) {
				const schedTime = new Date(scheduleTimeVal).toISOString();
				const res = await App.API.post(`/firewall/schedules`, {
					rule: payload,
					action_type: id ? 'enable' : 'add',
					run_time: schedTime
				});
				if (res.success) {
					App.UI.toast('success', 'Firewall rule change scheduled successfully!');
					await this.loadSchedules();
					this.switchSubTab('schedules');
				}
			} else {
				// Instant apply
				let res;
				if (id) {
					res = await App.API.put(`/firewall/rules/${id}`, payload);
				} else {
					res = await App.API.post(`/firewall/rules`, payload);
				}

				if (res.success) {
					App.UI.toast('success', id ? 'Firewall rule updated' : 'Firewall rule added');
					await this.fetchData(false);
					await this.loadLogs();
					this.render();
				}
			}
		} catch (e) {
			console.error(e);
		} finally {
			this.showLoading(false);
			document.getElementById('fw-rule-schedule-time').value = '';
		}
	},

	// --- Delete Rule Action ---
	async confirmDeleteRule(id, port) {
		if (await App.UI.confirm(`Are you sure you want to delete the rule for port "${port}"?`)) {
			this.deleteRule(id);
		}
	},

	async deleteRule(id) {
		this.showLoading(true);
		try {
			const res = await App.API.delete(`/firewall/rules/${id}`);
			if (res.success) {
				App.UI.toast('success', 'Firewall rule deleted successfully');
				await this.fetchData(false);
				await this.loadLogs();
				this.render();
			}
		} catch (e) {
			console.error(e);
		} finally {
			this.showLoading(false);
		}
	},

	// --- Bulk Actions ---
	async executeBulk(action) {
		if (this.state.selectedRuleIds.size === 0) return;

		let confirmMsg = `Are you sure you want to ${action} ${this.state.selectedRuleIds.size} selected rules?`;
		if (action === 'delete') {
			confirmMsg = `WARNING: Are you sure you want to PERMANENTLY delete ${this.state.selectedRuleIds.size} selected rules? This could disrupt SSH connectivity.`;
		}

		if (!await App.UI.confirm(confirmMsg)) return;

		this.showLoading(true);
		try {
			const res = await App.API.post(`/firewall/rules/bulk`, {
				action: action,
				rule_ids: Array.from(this.state.selectedRuleIds)
			});

			if (res.success) {
				App.UI.toast('success', `Bulk ${action} execution succeeded`);
				await this.fetchData(false);
				await this.loadLogs();
				this.render();
			}
		} catch (e) {
			console.error(e);
		} finally {
			this.showLoading(false);
		}
	},

	// --- Firewall Enable / Disable ---
	async toggleFirewall() {
		const action = this.state.enabled ? 'disable' : 'enable';

		if (action === 'disable') {
			if (!await App.UI.confirm('WARNING: Disabling the firewall will expose all ports on your VPS. Are you sure you want to proceed?')) {
				return;
			}
		} else {
			if (!await App.UI.confirm('Enable firewall? Lockout prevention will check and allow your active SSH connection port.')) {
				return;
			}
		}

		this.showLoading(true);
		try {
			const res = await App.API.post(`/firewall/${action}`);
			if (res.success) {
				App.UI.toast('success', `Firewall successfully ${action}d`);
				await this.fetchData(false);
				await this.loadLogs();
				this.render();
			}
		} catch (e) {
			console.error(e);
		} finally {
			this.showLoading(false);
		}
	},

	// --- Reset Firewall ---
	async confirmReset() {
		if (await App.UI.confirm('CAUTION: This will delete ALL database rule cache and run a firewall reset command on the VPS. Connection safety checks will run to preserve SSH access. Do you want to proceed?')) {
			this.resetFirewall();
		}
	},

	async resetFirewall() {
		this.showLoading(true);
		try {
			const res = await App.API.post(`/firewall/reset`);
			if (res.success) {
				App.UI.toast('success', 'Firewall rules successfully reset');
				await this.fetchData(false);
				await this.loadLogs();
				this.render();
			}
		} catch (e) {
			console.error(e);
		} finally {
			this.showLoading(false);
		}
	},

	// --- Policies Modal ---
	openPolicyModal() {
		document.getElementById('fw-policy-incoming').value = this.state.defaultIncomingPolicy;
		document.getElementById('fw-policy-outgoing').value = this.state.defaultOutgoingPolicy;
		document.getElementById('fw-policy-modal').classList.remove('hidden');
	},

	closePolicyModal() {
		document.getElementById('fw-policy-modal').classList.add('hidden');
	},

	async savePolicies() {
		const incoming = document.getElementById('fw-policy-incoming').value;
		const outgoing = document.getElementById('fw-policy-outgoing').value;

		this.closePolicyModal();
		this.showLoading(true);

		try {
			const res = await App.API.post(`/firewall/policies`, { incoming, outgoing });
			if (res.success) {
				App.UI.toast('success', 'Default policies updated successfully');
				await this.fetchData(false);
				await this.loadLogs();
				this.render();
			}
		} catch (e) {
			console.error(e);
		} finally {
			this.showLoading(false);
		}
	},

	// --- Install UFW ---
	async installUFW() {
		const btn = document.getElementById('install-ufw-btn');
		const terminal = document.getElementById('ufw-install-terminal');
		const output = document.getElementById('ufw-install-output');

		if (!btn || !terminal || !output) return;

		btn.innerHTML = '<i class="fas fa-circle-notch fa-spin mr-2"></i>Installing UFW...';
		btn.disabled = true;
		btn.classList.add('opacity-75', 'cursor-not-allowed');
		terminal.classList.remove('hidden');
		output.textContent = '[System] Starting UFW installation and initial rules setup...\n';

		try {
			const res = await App.API.post('/firewall/install');
			if (res.success) {
				output.textContent += '\n[System] UFW successfully installed, default policies applied, and SSH port allowed!\n';
				App.UI.toast('success', 'UFW installed and activated!');
				setTimeout(() => {
					this.load();
				}, 2000);
			} else {
				throw new Error('Install returned failure status');
			}
		} catch (e) {
			output.textContent += `\n[Error] UFW Installation failed: ${e.message}\n`;
			btn.innerHTML = '<i class="fas fa-download mr-2"></i>Retry Install';
			btn.disabled = false;
			btn.classList.remove('opacity-75', 'cursor-not-allowed');
			App.UI.toast('error', 'Firewall installation failed');
		}
	},

	// --- Backups Tab Operations ---
	renderBackups() {
		const tbody = document.getElementById('fw-backups-tbody');
		if (!tbody) return;

		if (this.state.backups.length === 0) {
			tbody.innerHTML = `
                <tr>
                    <td colspan="4" class="px-4 py-8 text-center text-gray-500">
                        No firewall backups found.
                    </td>
                </tr>`;
			return;
		}

		tbody.innerHTML = this.state.backups.map(b => {
			const created = new Date(b.created_at).toLocaleString();
			const ruleCount = JSON.parse(b.rules_json || '[]').length;

			return `
                <tr class="hover:bg-gray-800/40 border-b border-gray-700/30 transition-colors">
                    <td class="px-4 py-3 font-semibold text-white">${this.escapeHtml(b.name)}</td>
                    <td class="px-4 py-3 text-gray-400 font-mono text-sm">${ruleCount} rules</td>
                    <td class="px-4 py-3 text-sm text-gray-400">${created}</td>
                    <td class="px-4 py-3 text-right">
                        <div class="flex items-center justify-end space-x-3">
                            <button onclick="App.Firewall.restoreBackup('${b.id}')"
                                class="text-green-400 hover:text-green-300 hover:bg-green-500/10 px-2 py-1 rounded transition-all text-sm">
                                <i class="fas fa-undo mr-1"></i>Restore
                            </button>
                            <button onclick="App.Firewall.deleteBackup('${b.id}')"
                                class="text-red-400 hover:text-red-300 hover:bg-red-500/10 px-2 py-1 rounded transition-all text-sm">
                                <i class="fas fa-trash-alt mr-1"></i>Delete
                            </button>
                        </div>
                    </td>
                </tr>`;
		}).join('');
	},

	openCreateBackupModal() {
		document.getElementById('fw-backup-name').value = `Backup-${new Date().toISOString().slice(0, 10)}`;
		document.getElementById('fw-backup-modal').classList.remove('hidden');
	},

	closeCreateBackupModal() {
		document.getElementById('fw-backup-modal').classList.add('hidden');
	},

	async saveBackup() {
		const name = document.getElementById('fw-backup-name').value.trim();
		if (!name) return;

		this.closeCreateBackupModal();
		this.showLoading(true);

		try {
			const res = await App.API.post('/firewall/backups', { name });
			if (res.success) {
				App.UI.toast('success', 'Backup created successfully');
				await this.loadBackups();
				await this.loadLogs();
				this.render();
			}
		} catch (e) {
			console.error(e);
		} finally {
			this.showLoading(false);
		}
	},

	async restoreBackup(id) {
		if (!await App.UI.confirm('WARNING: Restoring this backup will replace all current rules both in local cache and on the VPS firewall! Do you want to proceed?')) {
			return;
		}

		this.showLoading(true);
		try {
			const res = await App.API.post(`/firewall/backups/${id}/restore`);
			if (res.success) {
				App.UI.toast('success', 'Rules backup restored successfully');
				await this.fetchData(false);
				await this.loadLogs();
				this.render();
			}
		} catch (e) {
			console.error(e);
		} finally {
			this.showLoading(false);
		}
	},

	async deleteBackup(id) {
		if (!await App.UI.confirm('Are you sure you want to delete this backup permanently?')) return;

		this.showLoading(true);
		try {
			const res = await App.API.delete(`/firewall/backups/${id}`);
			if (res.success) {
				App.UI.toast('success', 'Backup successfully deleted');
				await this.loadBackups();
				this.render();
			}
		} catch (e) {
			console.error(e);
		} finally {
			this.showLoading(false);
		}
	},

	// --- Schedules Tab Operations ---
	renderSchedules() {
		const tbody = document.getElementById('fw-schedules-tbody');
		if (!tbody) return;

		if (this.state.schedules.length === 0) {
			tbody.innerHTML = `
                <tr>
                    <td colspan="5" class="px-4 py-8 text-center text-gray-500">
                        No scheduled firewall changes configured.
                    </td>
                </tr>`;
			return;
		}

		tbody.innerHTML = this.state.schedules.map(s => {
			const rule = JSON.parse(s.rule_data || '{}');
			const runTime = new Date(s.scheduled_at).toLocaleString();

			const actBadge = s.action_type === 'add' || s.action_type === 'enable'
				? '<span class="text-xs bg-green-950 text-green-400 px-2 py-0.5 rounded border border-green-900">ADD/ENABLE</span>'
				: '<span class="text-xs bg-red-950 text-red-400 px-2 py-0.5 rounded border border-red-900">DELETE/DISABLE</span>';

			const statusBadge = {
				pending: '<span class="text-xs text-yellow-400 bg-yellow-950/40 px-2 py-0.5 rounded border border-yellow-900/60"><i class="fas fa-clock mr-1"></i>Pending</span>',
				success: '<span class="text-xs text-green-400 bg-green-950/40 px-2 py-0.5 rounded border border-green-900/60"><i class="fas fa-check mr-1"></i>Success</span>',
				failed: `<span class="text-xs text-red-400 bg-red-950/40 px-2 py-0.5 rounded border border-red-900/60" title="${this.escapeHtml(s.error_message)}"><i class="fas fa-exclamation-triangle mr-1"></i>Failed</span>`
			}[s.status] || s.status;

			return `
                <tr class="hover:bg-gray-800/40 border-b border-gray-700/30 transition-colors">
                    <td class="px-4 py-3 font-mono text-sm">${rule.port} (${rule.protocol})</td>
                    <td class="px-4 py-3">${actBadge}</td>
                    <td class="px-4 py-3 text-sm text-gray-400">${runTime}</td>
                    <td class="px-4 py-3">${statusBadge}</td>
                    <td class="px-4 py-3 text-right">
                        ${!s.executed ? `
                            <button onclick="App.Firewall.cancelSchedule('${s.id}')"
                                class="text-red-400 hover:text-red-300 hover:bg-red-500/10 px-2 py-1 rounded transition-all text-sm">
                                <i class="fas fa-times-circle mr-1"></i>Cancel
                            </button>
                        ` : '—'}
                    </td>
                </tr>`;
		}).join('');
	},

	async cancelSchedule(id) {
		if (!await App.UI.confirm('Are you sure you want to cancel this scheduled task?')) return;

		this.showLoading(true);
		try {
			const res = await App.API.delete(`/firewall/schedules/${id}`);
			if (res.success) {
				App.UI.toast('success', 'Scheduled change cancelled successfully');
				await this.loadSchedules();
				this.render();
			}
		} catch (e) {
			console.error(e);
		} finally {
			this.showLoading(false);
		}
	},

	// --- Logs Tab Operations ---
	renderLogs() {
		const tbody = document.getElementById('fw-logs-tbody');
		if (!tbody) return;

		if (this.state.logs.length === 0) {
			tbody.innerHTML = `
                <tr>
                    <td colspan="3" class="px-4 py-8 text-center text-gray-500">
                        No activity logged yet.
                    </td>
                </tr>`;
			return;
		}

		tbody.innerHTML = this.state.logs.map(l => {
			const created = new Date(l.timestamp).toLocaleString();
			const details = this.escapeHtml(l.details);

			return `
                <tr class="hover:bg-gray-800/40 border-b border-gray-700/30 transition-colors">
                    <td class="px-4 py-3 text-sm text-gray-400 font-mono">${created}</td>
                    <td class="px-4 py-3"><span class="font-bold text-white uppercase text-xs bg-gray-700 px-1.5 py-0.5 rounded border border-gray-600">${l.action}</span></td>
                    <td class="px-4 py-3 text-gray-300 text-sm">${details}</td>
                </tr>`;
		}).join('');
	},

	// Helper to escape HTML characters
	escapeHtml(text) {
		if (!text) return '';
		return text
			.replace(/&/g, "&amp;")
			.replace(/</g, "&lt;")
			.replace(/>/g, "&gt;")
			.replace(/"/g, "&quot;")
			.replace(/'/g, "&#039;");
	}
};
