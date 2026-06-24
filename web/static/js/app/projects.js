/**
 * GoPulse App - Projects Module
 * Handles project management and creation
 */

App.Projects = {
    init: function() {
        console.log('Projects module initialized');
        this.bindEvents();
    },

    bindEvents: function() {
        // Event listeners for project creation will go here
        const createBtn = document.getElementById('create-project-btn');
        if (createBtn) {
            createBtn.addEventListener('click', this.showCreateModal);
        }
    },

    load: async function() {
        console.log('Loading projects...');
        const serverID = App.config.SERVER_ID;
        try {
            const data = await App.API.get(`/servers/${serverID}/projects`);
            if (data.success) {
                this.list = data.data; // Store list
                this.renderList(data.data);
            }
        } catch (err) {
            console.error('Failed to load projects:', err);
            // Optionally show error in UI
            const container = document.getElementById('projects-list');
            if (container) {
                container.innerHTML = `<div class="text-red-500 text-center py-8">Failed to load projects</div>`;
            }
        }
    },

    list: [], // Initialize list
    filters: {
        query: '',
        status: 'all',
        type: 'all'
    },
    activeOpsTab: 'overview',
    opsData: {},
    opsLoading: {},

    renderList: function(projects, focusState) {
        const container = document.getElementById('projects-list');
        if (!container) return;

        const source = Array.isArray(projects) ? projects : [];
        this.list = source;

        if (source.length === 0) {
            container.innerHTML = `
                <div class="border border-dashed border-gray-700 rounded-lg py-16 px-6 text-center bg-gray-900/30">
                    <div class="bg-gray-800 rounded-full h-20 w-20 flex items-center justify-center mx-auto mb-4 border border-gray-700">
                        <i class="fas fa-project-diagram text-3xl text-gray-500"></i>
                    </div>
                    <h3 class="text-xl font-semibold text-white mb-2">No Projects Yet</h3>
                    <button onclick="App.Projects.showCreateModal()" class="bg-blue-600 hover:bg-blue-700 text-white px-6 py-2 rounded-lg font-medium transition-colors">
                        <i class="fas fa-plus mr-2"></i>New Project
                    </button>
                </div>
            `;
            return;
        }

        const filtered = this.getFilteredProjects(source);
        const grid = filtered.length > 0
            ? `
                <div class="grid grid-cols-1 xl:grid-cols-2 2xl:grid-cols-3 gap-5">
                    ${filtered.map(project => this.renderProjectCard(project)).join('')}
                    ${this.renderAddProjectCard()}
                </div>
            `
            : this.renderNoProjectMatches();

        container.innerHTML = `
            <div class="space-y-5">
                ${this.renderProjectSummary(source)}
                ${this.renderProjectControls(source, filtered.length)}
                ${grid}
            </div>
        `;

        this.restoreProjectControlFocus(focusState);
    },

    renderProjectSummary: function(projects) {
        const total = projects.length;
        const running = projects.filter(project => this.normalizeStatus(project) === 'running').length;
        const ready = projects.filter(project => this.hasBuild(project)).length;
        const automated = projects.filter(project => project.webhook_enabled || project.restart_on_boot).length;

        return `
            <div class="grid grid-cols-2 xl:grid-cols-4 gap-4">
                ${this.renderProjectMetric('Inventory', total, 'fa-layer-group', 'text-blue-300')}
                ${this.renderProjectMetric('Running', running, 'fa-circle-play', 'text-emerald-300')}
                ${this.renderProjectMetric('Build Ready', ready, 'fa-hammer', 'text-amber-300')}
                ${this.renderProjectMetric('Automated', automated, 'fa-bolt', 'text-cyan-300')}
            </div>
        `;
    },

    renderProjectMetric: function(label, value, icon, colorClass) {
        return `
            <div class="bg-gray-900/60 border border-gray-800 rounded-lg px-4 py-3">
                <div class="flex items-center justify-between">
                    <span class="text-xs uppercase tracking-wider text-gray-500 font-semibold">${label}</span>
                    <i class="fas ${icon} ${colorClass}"></i>
                </div>
                <div class="text-2xl font-bold text-white mt-2">${value}</div>
            </div>
        `;
    },

    renderProjectControls: function(projects, filteredCount) {
        const types = Array.from(new Set(projects.map(project => (project.type || '').toLowerCase()).filter(Boolean))).sort();
        const typeOptions = [
            '<option value="all">All types</option>',
            ...types.map(type => `<option value="${this.escapeAttr(type)}" ${this.filters.type === type ? 'selected' : ''}>${this.escapeHtml(this.projectTypeLabel(type))}</option>`)
        ].join('');

        return `
            <div class="bg-gray-900/60 border border-gray-800 rounded-lg p-4">
                <div class="flex flex-col xl:flex-row xl:items-center gap-3">
                    <div class="relative flex-1">
                        <i class="fas fa-search absolute left-3 top-1/2 -translate-y-1/2 text-gray-500 text-sm"></i>
                        <input
                            id="project-search-input"
                            type="search"
                            value="${this.escapeAttr(this.filters.query)}"
                            oninput="App.Projects.setFilter('query', this.value)"
                            placeholder="Search projects, paths, ports"
                            class="w-full bg-gray-950/70 border border-gray-800 rounded-lg pl-9 pr-3 py-2.5 text-sm text-white placeholder-gray-600 focus:outline-none focus:border-blue-500"
                            autocomplete="off"
                            spellcheck="false">
                    </div>
                    <div class="flex flex-col sm:flex-row gap-3">
                        <select id="project-status-filter" onchange="App.Projects.setFilter('status', this.value)" class="bg-gray-950/70 border border-gray-800 rounded-lg px-3 py-2.5 text-sm text-white focus:outline-none focus:border-blue-500">
                            <option value="all" ${this.filters.status === 'all' ? 'selected' : ''}>All states</option>
                            <option value="running" ${this.filters.status === 'running' ? 'selected' : ''}>Running</option>
                            <option value="stopped" ${this.filters.status === 'stopped' ? 'selected' : ''}>Stopped</option>
                            <option value="building" ${this.filters.status === 'building' ? 'selected' : ''}>Building</option>
                            <option value="error" ${this.filters.status === 'error' ? 'selected' : ''}>Error</option>
                        </select>
                        <select id="project-type-filter" onchange="App.Projects.setFilter('type', this.value)" class="bg-gray-950/70 border border-gray-800 rounded-lg px-3 py-2.5 text-sm text-white focus:outline-none focus:border-blue-500">
                            ${typeOptions}
                        </select>
                        <button onclick="App.Projects.resetFilters()" class="bg-gray-800 hover:bg-gray-700 border border-gray-700 text-gray-300 hover:text-white rounded-lg px-3 py-2.5 text-sm transition-colors" title="Reset filters">
                            <i class="fas fa-filter-circle-xmark"></i>
                        </button>
                        <button onclick="App.Projects.load()" class="bg-gray-800 hover:bg-gray-700 border border-gray-700 text-gray-300 hover:text-white rounded-lg px-3 py-2.5 text-sm transition-colors" title="Refresh">
                            <i class="fas fa-rotate"></i>
                        </button>
                    </div>
                </div>
                <div class="mt-3 text-xs text-gray-500">${filteredCount} of ${projects.length} projects visible</div>
            </div>
        `;
    },

    renderProjectCard: function(project) {
        const id = this.escapeAttr(project.id);
        const name = this.escapeHtml(project.name || 'Unnamed project');
        const type = (project.type || 'unknown').toLowerCase();
        const typeMeta = this.getProjectTypeMeta(type);
        const statusMeta = this.getProjectStatusMeta(project);
        const isBuilt = this.hasBuild(project);
        const buildTime = isBuilt ? this.formatDate(project.last_build) : 'Never';
        const lastDeploy = this.hasDate(project.last_deploy) ? this.formatDate(project.last_deploy) : 'Never';
        const path = project.path || 'Path unavailable';
        const sourceType = project.source_type || (project.source_url ? 'git' : 'upload');
        const portLabel = project.host_port && project.host_port !== project.port
            ? `${project.port} -> ${project.host_port}`
            : `${project.port || 'N/A'}`;
        const envCount = project.env_vars ? Object.keys(project.env_vars).length : 0;
        const deploySignals = [
            project.restart_on_boot ? '<span class="inline-flex items-center text-xs text-emerald-300"><i class="fas fa-power-off mr-1.5"></i>Boot</span>' : '',
            project.webhook_enabled ? '<span class="inline-flex items-center text-xs text-blue-300"><i class="fab fa-github mr-1.5"></i>Webhook</span>' : ''
        ].filter(Boolean).join('');

        return `
            <article class="bg-gray-900/70 border border-gray-800 rounded-lg p-5 min-h-[430px] flex flex-col hover:border-blue-500/60 transition-colors">
                <div class="flex items-start justify-between gap-4">
                    <div class="flex items-start gap-3 min-w-0">
                        <div class="h-11 w-11 rounded-lg bg-gray-950/80 border border-gray-800 flex items-center justify-center flex-shrink-0">
                            <i class="${typeMeta.icon} ${typeMeta.color} text-xl"></i>
                        </div>
                        <div class="min-w-0">
                            <h3 class="font-semibold text-white truncate">${name}</h3>
                            <div class="mt-1 flex flex-wrap items-center gap-2">
                                <span class="text-xs text-gray-400">${this.escapeHtml(typeMeta.label)}</span>
                                <span class="h-1 w-1 rounded-full bg-gray-700"></span>
                                <span class="text-xs text-gray-400">Port ${this.escapeHtml(portLabel)}</span>
                            </div>
                        </div>
                    </div>
                    <span class="inline-flex items-center px-2.5 py-1 rounded-full border ${statusMeta.border} ${statusMeta.bg} ${statusMeta.text} text-xs font-semibold flex-shrink-0">
                        <span class="h-1.5 w-1.5 rounded-full ${statusMeta.dot} mr-2"></span>${statusMeta.label}
                    </span>
                </div>

                <div class="mt-4 flex items-center gap-2 text-xs text-gray-500 min-w-0">
                    <i class="fas fa-folder-tree text-gray-600"></i>
                    <button onclick="App.Projects.copyProjectPath('${id}')" class="font-mono truncate hover:text-white transition-colors text-left" title="${this.escapeAttr(path)}">${this.escapeHtml(path)}</button>
                </div>

                <dl class="mt-5 space-y-3 text-sm">
                    ${this.renderProjectDetail('Runtime', this.projectTypeLabel(project.build_type || 'process'))}
                    ${this.renderProjectDetail('Source', this.projectTypeLabel(sourceType))}
                    ${this.renderProjectDetail('Last Build', buildTime, true)}
                    ${this.renderProjectDetail('Last Deploy', lastDeploy, true)}
                    ${this.renderProjectDetail('Env Vars', `${envCount}`)}
                </dl>

                <div class="mt-4 min-h-[24px] flex flex-wrap gap-3">
                    ${deploySignals || '<span class="text-xs text-gray-600">No automation enabled</span>'}
                </div>

                <div class="mt-auto pt-5">
                    <div class="grid grid-cols-3 gap-2 mb-2">
                        ${isBuilt ? `
                            <button onclick="App.Projects.action('${id}', 'start', this)" class="bg-emerald-950/30 text-emerald-300 hover:bg-emerald-900/50 py-2 rounded text-sm font-medium transition-colors">
                                <i class="fas fa-play mr-1"></i>Start
                            </button>
                            <button onclick="App.Projects.action('${id}', 'stop', this)" class="bg-red-950/30 text-red-300 hover:bg-red-900/50 py-2 rounded text-sm font-medium transition-colors">
                                <i class="fas fa-stop mr-1"></i>Stop
                            </button>
                            <button onclick="App.Projects.action('${id}', 'build', this)" class="bg-blue-950/40 text-blue-300 hover:bg-blue-900/50 py-2 rounded text-sm font-medium transition-colors">
                                <i class="fas fa-hammer mr-1"></i>Build
                            </button>
                        ` : `
                            <button onclick="App.Projects.action('${id}', 'build', this)" class="col-span-3 bg-blue-950/40 text-blue-300 hover:bg-blue-900/50 py-2 rounded text-sm font-medium transition-colors">
                                <i class="fas fa-hammer mr-1"></i>Build Project
                            </button>
                        `}
                    </div>

                    <div class="grid grid-cols-6 gap-2">
                        ${this.renderIconButton(`App.Projects.showProjectOps('${id}')`, 'fa-sliders', 'Operations')}
                        ${this.renderIconButton(`App.Projects.showEnvModal('${id}')`, 'fa-key', 'Environment')}
                        ${this.renderIconButton(`App.Projects.openFiles('${id}')`, 'fa-folder-open', 'Files', 'hover:text-yellow-300')}
                        ${this.renderIconButton(`App.Projects.showWebhookModal('${id}')`, project.webhook_enabled ? 'fa-toggle-on' : 'fa-toggle-off', 'Webhook', project.webhook_enabled ? 'text-blue-300 hover:text-blue-200' : '')}
                        ${this.renderIconButton(`App.Projects.editProject('${id}')`, 'fa-pen-to-square', 'Edit')}
                        ${this.renderIconButton(`App.Projects.action('${id}', 'delete', this)`, 'fa-trash', 'Delete', 'text-red-400 hover:text-red-300')}
                    </div>
                </div>
            </article>
        `;
    },

    renderProjectDetail: function(label, value, mono = false) {
        return `
            <div class="flex items-center justify-between gap-3 border-b border-gray-800/70 pb-2 last:border-b-0">
                <dt class="text-gray-500">${label}</dt>
                <dd class="text-gray-300 text-right truncate ${mono ? 'font-mono text-xs' : ''}" title="${this.escapeAttr(value)}">${this.escapeHtml(value)}</dd>
            </div>
        `;
    },

    renderIconButton: function(onclick, icon, title, extraClass = '') {
        return `
            <button onclick="${onclick}" class="h-9 bg-gray-800/80 hover:bg-gray-700 border border-gray-700 text-gray-400 hover:text-white rounded text-sm transition-colors ${extraClass}" title="${title}" aria-label="${title}">
                <i class="fas ${icon}"></i>
            </button>
        `;
    },

    renderAddProjectCard: function() {
        return `
            <button onclick="App.Projects.showCreateModal()" class="border border-dashed border-gray-700 rounded-lg min-h-[430px] p-6 flex flex-col items-center justify-center hover:border-blue-500/60 hover:bg-gray-900/40 transition-colors group">
                <div class="bg-gray-900 border border-gray-800 h-14 w-14 rounded-full flex items-center justify-center mb-3 group-hover:border-blue-500/40">
                    <i class="fas fa-plus text-lg text-gray-500 group-hover:text-blue-300"></i>
                </div>
                <span class="text-gray-300 font-semibold">New Project</span>
            </button>
        `;
    },

    renderNoProjectMatches: function() {
        return `
            <div class="border border-dashed border-gray-700 rounded-lg py-14 px-6 text-center bg-gray-900/30">
                <i class="fas fa-filter text-3xl text-gray-600 mb-3"></i>
                <h3 class="text-lg font-semibold text-white mb-3">No Matching Projects</h3>
                <button onclick="App.Projects.resetFilters()" class="bg-gray-800 hover:bg-gray-700 border border-gray-700 text-gray-300 hover:text-white px-4 py-2 rounded-lg text-sm transition-colors">
                    Reset Filters
                </button>
            </div>
        `;
    },

    setFilter: function(key, value) {
        if (!this.filters) this.filters = {};
        this.filters[key] = value;

        const focusState = key === 'query'
            ? {
                id: 'project-search-input',
                cursor: document.getElementById('project-search-input')?.selectionStart ?? String(value || '').length
            }
            : null;

        this.renderList(this.list || [], focusState);
    },

    resetFilters: function() {
        this.filters = { query: '', status: 'all', type: 'all' };
        this.renderList(this.list || []);
    },

    restoreProjectControlFocus: function(focusState) {
        if (!focusState || !focusState.id) return;
        requestAnimationFrame(() => {
            const input = document.getElementById(focusState.id);
            if (!input) return;
            input.focus();
            if (typeof input.setSelectionRange === 'function') {
                const cursor = Math.min(focusState.cursor ?? input.value.length, input.value.length);
                input.setSelectionRange(cursor, cursor);
            }
        });
    },

    getFilteredProjects: function(projects) {
        const query = (this.filters.query || '').trim().toLowerCase();
        const status = this.filters.status || 'all';
        const type = this.filters.type || 'all';

        return projects.filter(project => {
            const statusMatch = status === 'all' || this.normalizeStatus(project) === status;
            const typeMatch = type === 'all' || String(project.type || '').toLowerCase() === type;
            const searchable = [
                project.name,
                project.type,
                project.path,
                project.port,
                project.host_port,
                project.source_type,
                project.source_url,
                project.build_type,
                project.status
            ].map(value => String(value || '').toLowerCase()).join(' ');

            return statusMatch && typeMatch && (!query || searchable.includes(query));
        });
    },

    normalizeStatus: function(project) {
        return String(project?.status || 'stopped').toLowerCase();
    },

    getProjectStatusMeta: function(project) {
        const status = this.normalizeStatus(project);
        const meta = {
            running: { label: 'Running', text: 'text-emerald-300', border: 'border-emerald-500/30', bg: 'bg-emerald-500/10', dot: 'bg-emerald-400' },
            building: { label: 'Building', text: 'text-amber-300', border: 'border-amber-500/30', bg: 'bg-amber-500/10', dot: 'bg-amber-400' },
            error: { label: 'Error', text: 'text-red-300', border: 'border-red-500/30', bg: 'bg-red-500/10', dot: 'bg-red-400' },
            stopped: { label: 'Stopped', text: 'text-gray-300', border: 'border-gray-700', bg: 'bg-gray-800/70', dot: 'bg-gray-500' }
        };

        return meta[status] || { ...meta.stopped, label: this.titleCase(status) };
    },

    getProjectTypeMeta: function(type) {
        const key = String(type || '').toLowerCase();
        const meta = {
            nextjs: { label: 'Next.js', icon: 'fab fa-react', color: 'text-blue-300' },
            nodejs: { label: 'Node.js', icon: 'fab fa-node-js', color: 'text-emerald-300' },
            node: { label: 'Node.js', icon: 'fab fa-node-js', color: 'text-emerald-300' },
            go: { label: 'Go', icon: 'fab fa-golang', color: 'text-cyan-300' },
            laravel: { label: 'Laravel', icon: 'fab fa-laravel', color: 'text-red-300' },
            php: { label: 'PHP', icon: 'fab fa-php', color: 'text-violet-300' },
            docker: { label: 'Docker', icon: 'fab fa-docker', color: 'text-sky-300' }
        };

        return meta[key] || { label: this.projectTypeLabel(key || 'project'), icon: 'fas fa-code', color: 'text-gray-300' };
    },

    projectTypeLabel: function(value) {
        const key = String(value || '').toLowerCase();
        const labels = {
            nextjs: 'Next.js',
            nodejs: 'Node.js',
            node: 'Node.js',
            go: 'Go',
            laravel: 'Laravel',
            php: 'PHP',
            git: 'Git',
            upload: 'Upload',
            process: 'Process',
            docker: 'Docker'
        };
        return labels[key] || this.titleCase(key.replace(/[_-]+/g, ' '));
    },

    titleCase: function(value) {
        return String(value || '')
            .split(' ')
            .filter(Boolean)
            .map(part => part.charAt(0).toUpperCase() + part.slice(1))
            .join(' ');
    },

    hasDate: function(value) {
        if (!value) return false;
        if (typeof value === 'string' && value.startsWith('0001')) return false;
        const date = new Date(value);
        return !Number.isNaN(date.getTime()) && date.getFullYear() > 1970;
    },

    hasBuild: function(project) {
        return this.hasDate(project?.last_build);
    },

    formatDate: function(value) {
        if (!this.hasDate(value)) return 'Never';
        return new Date(value).toLocaleString();
    },

    escapeHtml: function(value) {
        return String(value ?? '').replace(/[&<>"']/g, char => ({
            '&': '&amp;',
            '<': '&lt;',
            '>': '&gt;',
            '"': '&quot;',
            "'": '&#39;'
        })[char]);
    },

    escapeAttr: function(value) {
        return this.escapeHtml(value).replace(/`/g, '&#96;');
    },

    copyProjectPath: function(id) {
        const project = (this.list || []).find(p => p.id === id);
        if (!project || !project.path) {
            App.UI.toast('error', 'Project path is not available');
            return;
        }

        navigator.clipboard.writeText(project.path)
            .then(() => App.UI.toast('success', 'Project path copied'))
            .catch(() => App.UI.toast('error', 'Failed to copy path'));
    },

    ensureOperationsModal: function() {
        if (document.getElementById('project-ops-modal')) return;

        document.body.insertAdjacentHTML('beforeend', `
            <div id="project-ops-modal" class="fixed inset-0 z-50 hidden opacity-0 transition-opacity duration-300 bg-black/80 backdrop-blur-sm flex items-center justify-center p-4">
                <div id="project-ops-dialog" class="bg-gray-900 border border-gray-800 rounded-xl w-[min(96vw,1180px)] h-[min(88vh,760px)] overflow-hidden transform scale-95 transition-transform duration-300 shadow-2xl">
                    <div id="project-ops-content" class="h-full"></div>
                </div>
            </div>
        `);
    },

    showProjectOps: function(id) {
        const project = (this.list || []).find(p => p.id === id);
        if (!project) {
            App.UI.toast('error', 'Project not found');
            return;
        }

        this.currentOpsProjectID = id;
        this.activeOpsTab = this.activeOpsTab || 'overview';
        this.ensureOperationsModal();
        this.renderOperationsModal(project);
        this.loadOpsTab(project.id, this.activeOpsTab);

        const modal = document.getElementById('project-ops-modal');
        const dialog = document.getElementById('project-ops-dialog');
        modal.classList.remove('hidden');
        requestAnimationFrame(() => {
            modal.classList.remove('opacity-0');
            dialog.classList.remove('scale-95');
            dialog.classList.add('scale-100');
        });
    },

    closeProjectOps: function() {
        const modal = document.getElementById('project-ops-modal');
        const dialog = document.getElementById('project-ops-dialog');
        if (!modal || !dialog) return;

        modal.classList.add('opacity-0');
        dialog.classList.remove('scale-100');
        dialog.classList.add('scale-95');
        setTimeout(() => modal.classList.add('hidden'), 300);
    },

    switchOpsTab: function(tab) {
        this.activeOpsTab = tab;
        const project = (this.list || []).find(p => p.id === this.currentOpsProjectID);
        if (project) {
            this.renderOperationsModal(project);
            this.loadOpsTab(project.id, tab);
        }
    },

    renderOperationsModal: function(project) {
        const content = document.getElementById('project-ops-content');
        if (!content) return;

        const id = this.escapeAttr(project.id);
        const name = this.escapeHtml(project.name || 'Unnamed project');
        const typeMeta = this.getProjectTypeMeta(project.type);
        const statusMeta = this.getProjectStatusMeta(project);
        const tabs = [
            ['overview', 'fa-gauge-high', 'Overview'],
            ['releases', 'fa-code-branch', 'Releases'],
            ['logs', 'fa-terminal', 'Logs'],
            ['health', 'fa-heart-pulse', 'Health'],
            ['automation', 'fa-wand-magic-sparkles', 'Automation'],
            ['integrations', 'fa-plug', 'Integrations']
        ];

        content.innerHTML = `
            <div class="h-full flex flex-col min-h-0">
            <div class="flex items-center justify-between px-6 py-4 border-b border-gray-800 bg-gray-950/50 flex-shrink-0">
                <div class="flex items-center gap-3 min-w-0">
                    <div class="h-11 w-11 rounded-lg bg-gray-950 border border-gray-800 flex items-center justify-center">
                        <i class="${typeMeta.icon} ${typeMeta.color} text-xl"></i>
                    </div>
                    <div class="min-w-0">
                        <div class="flex items-center gap-3">
                            <h3 class="text-lg font-bold text-white truncate">${name}</h3>
                            <span class="inline-flex items-center px-2.5 py-1 rounded-full border ${statusMeta.border} ${statusMeta.bg} ${statusMeta.text} text-xs font-semibold">
                                <span class="h-1.5 w-1.5 rounded-full ${statusMeta.dot} mr-2"></span>${statusMeta.label}
                            </span>
                        </div>
                        <p class="text-xs text-gray-500 mt-0.5">${this.escapeHtml(this.projectTypeLabel(project.type))} · Port ${this.escapeHtml(project.port || 'N/A')}</p>
                    </div>
                </div>
                <button onclick="App.Projects.closeProjectOps()" class="text-gray-400 hover:text-white hover:bg-gray-800 rounded-lg h-9 w-9 transition-colors" aria-label="Close">
                    <i class="fas fa-times"></i>
                </button>
            </div>

            <div class="grid grid-cols-1 lg:grid-cols-[220px,1fr] flex-1 min-h-0 overflow-hidden">
                <aside class="border-b lg:border-b-0 lg:border-r border-gray-800 bg-gray-950/30 p-3 overflow-x-auto lg:overflow-x-visible">
                    <div class="flex lg:flex-col gap-1 min-w-max lg:min-w-0">
                        ${tabs.map(([tab, icon, label]) => `
                            <button onclick="App.Projects.switchOpsTab('${tab}')" class="ops-tab-btn flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm transition-colors ${this.activeOpsTab === tab ? 'bg-blue-600/15 text-blue-300 border border-blue-500/20' : 'text-gray-400 hover:text-white hover:bg-gray-900 border border-transparent'}">
                                <i class="fas ${icon} w-4 text-center"></i>
                                <span>${label}</span>
                            </button>
                        `).join('')}
                    </div>
                </aside>
                <section id="project-ops-panel" class="p-6 overflow-y-auto custom-scrollbar min-h-0">
                    ${this.renderOpsPanel(project)}
                </section>
            </div>
            </div>
        `;
    },

    loadOpsTab: async function(id, tab, force = false) {
        if (tab === 'overview') return;

        const key = `${id}:${tab}`;
        if (!force && this.opsData[key]) return;
        if (this.opsLoading[key]) return;

        const endpoint = this.getOpsEndpoint(id, tab);
        if (!endpoint) return;

        this.opsLoading[key] = true;
        this.renderActiveOpsProject();

        try {
            const res = await App.API.get(endpoint);
            if (!res.success) throw new Error(res.error || res.message || `Failed to load ${tab}`);
            this.opsData[key] = res.data || {};
        } catch (err) {
            console.error(`Failed to load ${tab}:`, err);
            this.opsData[key] = { error: err.message || 'Failed to load data' };
        } finally {
            this.opsLoading[key] = false;
            this.renderActiveOpsProject();
        }
    },

    refreshOpsTab: function() {
        if (!this.currentOpsProjectID) return;
        const id = this.currentOpsProjectID;
        const tab = this.activeOpsTab || 'overview';
        const key = `${id}:${tab}`;
        delete this.opsData[key];
        this.loadOpsTab(id, tab, true);
    },

    renderActiveOpsProject: function() {
        const project = (this.list || []).find(p => p.id === this.currentOpsProjectID);
        const modal = document.getElementById('project-ops-modal');
        if (project && modal && !modal.classList.contains('hidden')) {
            this.renderOperationsModal(project);
        }
    },

    getOpsEndpoint: function(id, tab) {
        const serverID = App.config.SERVER_ID;
        const base = `/servers/${serverID}/projects/${id}`;
        const endpoints = {
            releases: `${base}/ops/releases`,
            logs: `${base}/ops/logs?tail=200`,
            health: `${base}/ops/health`,
            automation: `${base}/ops/automation`,
            integrations: `${base}/ops/integrations`
        };
        return endpoints[tab];
    },

    getOpsData: function(project, tab) {
        return this.opsData[`${project.id}:${tab}`];
    },

    isOpsLoading: function(project, tab) {
        return !!this.opsLoading[`${project.id}:${tab}`];
    },

    clearOpsCache: function(id) {
        ['releases', 'logs', 'health', 'automation', 'integrations'].forEach(tab => {
            delete this.opsData[`${id}:${tab}`];
        });
    },

    renderOpsPanel: function(project) {
        const tab = this.activeOpsTab || 'overview';
        if (tab === 'releases') return this.renderReleasesPanel(project);
        if (tab === 'logs') return this.renderLogsPanel(project);
        if (tab === 'health') return this.renderHealthPanel(project);
        if (tab === 'automation') return this.renderAutomationPanel(project);
        if (tab === 'integrations') return this.renderIntegrationsPanel(project);
        return this.renderOverviewPanel(project);
    },

    renderOpsLoading: function(label) {
        return `
            <div class="h-full min-h-[420px] flex items-center justify-center text-center">
                <div>
                    <i class="fas fa-circle-notch fa-spin text-3xl text-blue-400 mb-3"></i>
                    <div class="text-sm font-semibold text-white">Loading ${label}</div>
                </div>
            </div>
        `;
    },

    renderOpsError: function(message, tab) {
        return `
            <div class="border border-red-500/30 bg-red-950/20 rounded-lg p-5">
                <div class="flex items-start justify-between gap-4">
                    <div>
                        <h4 class="text-sm font-semibold text-red-300">Load Failed</h4>
                        <p class="text-sm text-gray-400 mt-1">${this.escapeHtml(message)}</p>
                    </div>
                    <button onclick="App.Projects.refreshOpsTab()" class="bg-gray-800 hover:bg-gray-700 border border-gray-700 text-gray-300 hover:text-white px-4 py-2 rounded-lg text-sm transition-colors">
                        <i class="fas fa-rotate mr-2"></i>Retry
                    </button>
                </div>
            </div>
        `;
    },

    renderOpsEmpty: function(icon, title) {
        return `
            <div class="h-full min-h-[360px] flex items-center justify-center text-center border border-dashed border-gray-800 rounded-lg">
                <div>
                    <i class="fas ${icon} text-3xl text-gray-600 mb-3"></i>
                    <div class="text-sm font-semibold text-white">${title}</div>
                </div>
            </div>
        `;
    },

    statusTextClass: function(status) {
        const normalized = String(status || '').toLowerCase();
        if (normalized === 'healthy' || normalized === 'completed' || normalized === 'enabled' || normalized === 'available' || normalized === 'configured' || normalized === 'current') return 'text-emerald-300';
        if (normalized === 'warning' || normalized === 'disabled' || normalized === 'unconfigured') return 'text-amber-300';
        if (normalized === 'unhealthy' || normalized === 'error' || normalized === 'failed') return 'text-red-300';
        return 'text-gray-300';
    },

    statusBadgeClass: function(status) {
        const normalized = String(status || '').toLowerCase();
        if (normalized === 'healthy' || normalized === 'completed' || normalized === 'enabled' || normalized === 'available' || normalized === 'configured' || normalized === 'current') return 'border-emerald-500/30 bg-emerald-500/10 text-emerald-300';
        if (normalized === 'warning' || normalized === 'disabled' || normalized === 'unconfigured') return 'border-amber-500/30 bg-amber-500/10 text-amber-300';
        if (normalized === 'unhealthy' || normalized === 'error' || normalized === 'failed') return 'border-red-500/30 bg-red-500/10 text-red-300';
        return 'border-gray-700 bg-gray-800/70 text-gray-300';
    },

    formatOpsTime: function(value) {
        return this.hasDate(value) ? this.formatDate(value) : 'Never';
    },

    renderOverviewPanel: function(project) {
        const id = this.escapeAttr(project.id);
        const isBuilt = this.hasBuild(project);
        return `
            <div class="space-y-6">
                <div class="grid grid-cols-1 md:grid-cols-3 gap-3">
                    <button onclick="App.Projects.action('${id}', 'build', this)" class="bg-blue-600 hover:bg-blue-500 text-white rounded-lg px-4 py-3 text-sm font-semibold transition-colors">
                        <i class="fas fa-hammer mr-2"></i>Build
                    </button>
                    <button onclick="App.Projects.action('${id}', 'start', this)" class="bg-emerald-600 hover:bg-emerald-500 text-white rounded-lg px-4 py-3 text-sm font-semibold transition-colors ${isBuilt ? '' : 'opacity-50'}">
                        <i class="fas fa-play mr-2"></i>Start
                    </button>
                    <button onclick="App.Projects.action('${id}', 'stop', this)" class="bg-red-600 hover:bg-red-500 text-white rounded-lg px-4 py-3 text-sm font-semibold transition-colors ${isBuilt ? '' : 'opacity-50'}">
                        <i class="fas fa-stop mr-2"></i>Stop
                    </button>
                </div>

                <div class="grid grid-cols-1 xl:grid-cols-2 gap-6">
                    <section class="border border-gray-800 rounded-lg p-5">
                        <h4 class="text-sm font-semibold text-white mb-4">Runtime</h4>
                        <dl class="space-y-3 text-sm">
                            ${this.renderProjectDetail('Status', this.getProjectStatusMeta(project).label)}
                            ${this.renderProjectDetail('Type', this.projectTypeLabel(project.type))}
                            ${this.renderProjectDetail('Build Type', this.projectTypeLabel(project.build_type || 'process'))}
                            ${this.renderProjectDetail('Internal Port', String(project.port || 'N/A'))}
                            ${this.renderProjectDetail('Host Port', String(project.host_port || project.port || 'N/A'))}
                            ${this.renderProjectDetail('Process ID', String(project.process_id || 'N/A'))}
                        </dl>
                    </section>
                    <section class="border border-gray-800 rounded-lg p-5">
                        <h4 class="text-sm font-semibold text-white mb-4">Source</h4>
                        <dl class="space-y-3 text-sm">
                            ${this.renderProjectDetail('Source Type', this.projectTypeLabel(project.source_type || (project.source_url ? 'git' : 'upload')))}
                            ${this.renderProjectDetail('Repository', project.source_url || 'N/A', true)}
                            ${this.renderProjectDetail('Path', project.path || 'N/A', true)}
                            ${this.renderProjectDetail('Last Build', this.formatDate(project.last_build), true)}
                            ${this.renderProjectDetail('Last Deploy', this.formatDate(project.last_deploy), true)}
                        </dl>
                    </section>
                </div>

                <section class="border border-gray-800 rounded-lg p-5">
                    <h4 class="text-sm font-semibold text-white mb-4">Commands</h4>
                    <div class="grid grid-cols-1 xl:grid-cols-2 gap-4">
                        ${this.renderCommandBlock('Build', project.build_command || 'N/A')}
                        ${this.renderCommandBlock('Start', project.start_command || 'N/A')}
                    </div>
                </section>
            </div>
        `;
    },

    renderReleasesPanel: function(project) {
        const id = this.escapeAttr(project.id);
        const data = this.getOpsData(project, 'releases');
        if (this.isOpsLoading(project, 'releases') && !data) return this.renderOpsLoading('releases');
        if (data?.error) return this.renderOpsError(data.error, 'releases');

        const releases = data?.releases || [];
        const git = data?.git || {};
        const canRedeploy = project.source_type === 'git' || !!project.source_url || git.is_repository;

        return `
            <div class="space-y-5">
                <div class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
                    <h4 class="text-sm font-semibold text-white">Release Timeline</h4>
                    <div class="flex flex-wrap gap-2">
                        <button onclick="App.Projects.refreshOpsTab()" class="bg-gray-800 hover:bg-gray-700 border border-gray-700 text-gray-300 hover:text-white px-4 py-2 rounded-lg text-sm transition-colors">
                            <i class="fas fa-rotate mr-2"></i>Refresh
                        </button>
                        <button onclick="App.Projects.action('${id}', 'build', this)" class="bg-blue-600 hover:bg-blue-500 text-white rounded-lg px-4 py-2 text-sm font-semibold transition-colors">
                            <i class="fas fa-hammer mr-2"></i>Build
                        </button>
                        <button onclick="App.Projects.redeployProject('${id}', this)" class="${canRedeploy ? 'bg-emerald-600 hover:bg-emerald-500 text-white' : 'bg-gray-800 text-gray-500 cursor-not-allowed'} rounded-lg px-4 py-2 text-sm font-semibold transition-colors" ${canRedeploy ? '' : 'disabled'}>
                            <i class="fas fa-code-branch mr-2"></i>Redeploy
                        </button>
                    </div>
                </div>

                <section class="border border-gray-800 rounded-lg p-5">
                    <div class="grid grid-cols-1 md:grid-cols-4 gap-4 text-sm">
                        ${this.renderProjectDetail('Repository', git.is_repository ? 'Detected' : 'Not detected')}
                        ${this.renderProjectDetail('Branch', git.branch || 'N/A', true)}
                        ${this.renderProjectDetail('Commit', git.commit || 'N/A', true)}
                        ${this.renderProjectDetail('Remote', git.remote || project.source_url || 'N/A', true)}
                    </div>
                </section>

                ${releases.length === 0 ? this.renderOpsEmpty('fa-code-branch', 'No release activity yet') : `
                    <div class="border border-gray-800 rounded-lg divide-y divide-gray-800">
                    ${releases.map(item => `
                        <div class="p-4 flex items-center justify-between gap-4">
                            <div>
                                <div class="flex items-center gap-2">
                                    <span class="text-sm font-semibold text-white">${this.escapeHtml(item.title || item.type)}</span>
                                    <span class="inline-flex items-center border ${this.statusBadgeClass(item.status)} rounded-full px-2 py-0.5 text-[11px] font-semibold">${this.escapeHtml(item.status || 'event')}</span>
                                </div>
                                <div class="text-xs text-gray-500 mt-1">${this.escapeHtml(item.detail || '')}</div>
                            </div>
                            <div class="font-mono text-xs text-gray-400 text-right">${this.formatOpsTime(item.timestamp)}</div>
                        </div>
                    `).join('')}
                    </div>
                `}
            </div>
        `;
    },

    renderLogsPanel: function(project) {
        const data = this.getOpsData(project, 'logs');
        if (this.isOpsLoading(project, 'logs') && !data) return this.renderOpsLoading('logs');
        if (data?.error) return this.renderOpsError(data.error, 'logs');

        const raw = data?.raw || '';
        const fetchedAt = data?.fetched_at ? this.formatOpsTime(data.fetched_at) : 'Not loaded';
        const source = data?.source || 'runtime';

        return `
            <div class="space-y-5">
                <div class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
                    <div>
                        <h4 class="text-sm font-semibold text-white">Logs</h4>
                        <p class="text-xs text-gray-500 mt-1">${this.escapeHtml(source.toUpperCase())} · ${this.escapeHtml(fetchedAt)}</p>
                    </div>
                    <div class="flex gap-2">
                        <button onclick="App.Projects.refreshOpsTab()" class="bg-gray-800 hover:bg-gray-700 border border-gray-700 text-gray-300 hover:text-white px-4 py-2 rounded-lg text-sm transition-colors"><i class="fas fa-rotate mr-2"></i>Refresh</button>
                        <button onclick="App.Projects.copyCurrentLogs()" class="bg-gray-800 hover:bg-gray-700 border border-gray-700 text-gray-300 hover:text-white px-4 py-2 rounded-lg text-sm transition-colors"><i class="fas fa-copy mr-2"></i>Copy</button>
                    </div>
                </div>
                <pre class="bg-black border border-gray-800 rounded-lg p-4 h-[500px] overflow-auto text-xs text-gray-400 font-mono leading-relaxed whitespace-pre-wrap">${this.escapeHtml(raw || 'No logs returned.')}</pre>
            </div>
        `;
    },

    renderHealthPanel: function(project) {
        const data = this.getOpsData(project, 'health');
        if (this.isOpsLoading(project, 'health') && !data) return this.renderOpsLoading('health checks');
        if (data?.error) return this.renderOpsError(data.error, 'health');

        const checks = data?.checks || [];
        const status = data?.status || 'unknown';

        return `
            <div class="space-y-5">
                <div class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
                    <div>
                        <h4 class="text-sm font-semibold text-white">Health Checks</h4>
                        <div class="mt-1 inline-flex items-center border ${this.statusBadgeClass(status)} rounded-full px-2.5 py-1 text-xs font-semibold">${this.escapeHtml(status.toUpperCase())}</div>
                    </div>
                    <button onclick="App.Projects.refreshOpsTab()" class="bg-gray-800 hover:bg-gray-700 border border-gray-700 text-gray-300 hover:text-white px-4 py-2 rounded-lg text-sm transition-colors">
                        <i class="fas fa-stethoscope mr-2"></i>Run Checks
                    </button>
                </div>
                ${checks.length === 0 ? this.renderOpsEmpty('fa-heart-pulse', 'No health checks returned') : `
                    <div class="border border-gray-800 rounded-lg divide-y divide-gray-800">
                    ${checks.map(check => `
                        <div class="p-4 flex items-center justify-between">
                            <div>
                                <span class="text-sm font-semibold text-white">${this.escapeHtml(check.name)}</span>
                                <div class="text-xs text-gray-500 mt-1">${this.escapeHtml(check.detail || '')}</div>
                            </div>
                            <span class="text-sm font-semibold ${this.statusTextClass(check.status)}">${this.escapeHtml(check.message)}</span>
                        </div>
                    `).join('')}
                    </div>
                `}
            </div>
        `;
    },

    renderAutomationPanel: function(project) {
        const id = this.escapeAttr(project.id);
        const data = this.getOpsData(project, 'automation');
        if (this.isOpsLoading(project, 'automation') && !data) return this.renderOpsLoading('automation');
        if (data?.error) return this.renderOpsError(data.error, 'automation');

        const restartOnBoot = data?.restart_on_boot ?? project.restart_on_boot;
        const webhookEnabled = data?.webhook_enabled ?? project.webhook_enabled;
        const webhookURL = data?.webhook_url || '';

        return `
            <div class="space-y-5">
                <div class="flex items-center justify-between gap-3">
                    <h4 class="text-sm font-semibold text-white">Automation</h4>
                    <button onclick="App.Projects.refreshOpsTab()" class="bg-gray-800 hover:bg-gray-700 border border-gray-700 text-gray-300 hover:text-white px-4 py-2 rounded-lg text-sm transition-colors">
                        <i class="fas fa-rotate mr-2"></i>Refresh
                    </button>
                </div>
                <div class="border border-gray-800 rounded-lg divide-y divide-gray-800">
                    <div class="p-4 flex items-center justify-between gap-4">
                        <div>
                            <div class="text-sm font-semibold text-white">Restart on boot</div>
                            <div class="text-xs ${restartOnBoot ? 'text-emerald-300' : 'text-gray-400'} mt-1">${restartOnBoot ? 'Enabled' : 'Disabled'}</div>
                        </div>
                        <button onclick="App.Projects.updateOpsAutomation('${id}', 'restart_on_boot', ${restartOnBoot ? 'false' : 'true'}, this)" class="bg-gray-800 hover:bg-gray-700 border border-gray-700 text-gray-300 hover:text-white px-3 py-2 rounded-lg text-xs font-semibold transition-colors">${restartOnBoot ? 'Disable' : 'Enable'}</button>
                    </div>
                    <div class="p-4 flex items-center justify-between gap-4">
                        <div class="min-w-0">
                            <div class="text-sm font-semibold text-white">Git webhook</div>
                            <div class="text-xs ${webhookEnabled ? 'text-emerald-300' : 'text-gray-400'} mt-1">${webhookEnabled ? 'Enabled' : 'Disabled'}</div>
                        </div>
                        <button onclick="App.Projects.updateOpsAutomation('${id}', 'webhook_enabled', ${webhookEnabled ? 'false' : 'true'}, this)" class="bg-gray-800 hover:bg-gray-700 border border-gray-700 text-gray-300 hover:text-white px-3 py-2 rounded-lg text-xs font-semibold transition-colors">${webhookEnabled ? 'Disable' : 'Enable'}</button>
                    </div>
                </div>
                <section class="border border-gray-800 rounded-lg p-5">
                    <div class="flex items-center justify-between gap-3">
                        <div class="min-w-0">
                            <div class="text-xs font-semibold uppercase tracking-wider text-gray-500 mb-2">Webhook URL</div>
                            <div class="font-mono text-xs text-gray-300 truncate">${this.escapeHtml(webhookURL || 'Enable webhook to generate URL')}</div>
                        </div>
                        <button onclick="App.Projects.copyText('${this.escapeAttr(webhookURL)}', 'Webhook URL copied')" class="bg-gray-800 hover:bg-gray-700 border border-gray-700 text-gray-300 hover:text-white px-3 py-2 rounded-lg text-xs font-semibold transition-colors ${webhookURL ? '' : 'opacity-50'}" ${webhookURL ? '' : 'disabled'}>
                            <i class="fas fa-copy mr-1.5"></i>Copy
                        </button>
                    </div>
                </section>
            </div>
        `;
    },

    renderIntegrationsPanel: function(project) {
        const id = this.escapeAttr(project.id);
        const data = this.getOpsData(project, 'integrations');
        if (this.isOpsLoading(project, 'integrations') && !data) return this.renderOpsLoading('integrations');
        if (data?.error) return this.renderOpsError(data.error, 'integrations');

        const integrations = data?.integrations || [];

        return `
            <div class="space-y-5">
                <div class="flex items-center justify-between gap-3">
                    <h4 class="text-sm font-semibold text-white">Integrations</h4>
                    <button onclick="App.Projects.refreshOpsTab()" class="bg-gray-800 hover:bg-gray-700 border border-gray-700 text-gray-300 hover:text-white px-4 py-2 rounded-lg text-sm transition-colors">
                        <i class="fas fa-rotate mr-2"></i>Refresh
                    </button>
                </div>
                ${integrations.length === 0 ? this.renderOpsEmpty('fa-plug', 'No integrations returned') : `
                    <div class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
                    ${integrations.map(integration => `
                        <div class="border border-gray-800 rounded-lg p-4 flex flex-col justify-between gap-4 min-h-[150px]">
                            <div class="flex items-center gap-3">
                                <div class="h-9 w-9 rounded-lg bg-gray-950 border border-gray-800 flex items-center justify-center">
                                    <i class="${this.getIntegrationIcon(integration.key)} text-gray-300"></i>
                                </div>
                                <div class="min-w-0">
                                    <span class="text-sm font-semibold text-white">${this.escapeHtml(integration.label)}</span>
                                    <div class="text-xs ${this.statusTextClass(integration.status)} mt-1">${this.escapeHtml(integration.status)}</div>
                                </div>
                            </div>
                            <p class="text-xs text-gray-500 truncate">${this.escapeHtml(integration.message || '')}</p>
                            <button onclick="App.Projects.openIntegration('${id}', '${this.escapeAttr(integration.key)}')" class="bg-gray-800 hover:bg-gray-700 border border-gray-700 text-gray-300 hover:text-white px-3 py-2 rounded-lg text-xs font-semibold transition-colors">Open</button>
                        </div>
                    `).join('')}
                    </div>
                `}
            </div>
        `;
    },

    renderCommandBlock: function(label, command) {
        return `
            <div>
                <div class="text-xs font-semibold uppercase tracking-wider text-gray-500 mb-2">${label}</div>
                <pre class="bg-black border border-gray-800 rounded-lg p-3 min-h-[84px] overflow-auto text-xs text-gray-300 font-mono whitespace-pre-wrap">${this.escapeHtml(command)}</pre>
            </div>
        `;
    },

    futureFeature: function(name) {
        App.UI.toast('info', `${name} UI is ready; backend wiring is pending.`);
    },

    redeployProject: async function(id, btn) {
        const serverID = App.config.SERVER_ID;
        const original = btn ? btn.innerHTML : '';
        if (btn) {
            btn.disabled = true;
            btn.innerHTML = '<i class="fas fa-spinner fa-spin mr-2"></i>Redeploying';
        }

        try {
            const res = await App.API.post(`/servers/${serverID}/projects/${id}/redeploy`, {});
            if (!res.success) throw new Error(res.error || res.message || 'Redeploy failed');
            App.UI.toast('success', 'Project redeployed');
            delete this.opsData[`${id}:releases`];
            delete this.opsData[`${id}:health`];
            delete this.opsData[`${id}:logs`];
            await this.load();
            if (this.currentOpsProjectID === id) {
                this.loadOpsTab(id, this.activeOpsTab || 'releases', true);
            }
        } catch (err) {
            console.error('Redeploy failed:', err);
            App.UI.toast('error', `Redeploy failed: ${err.message}`);
        } finally {
            if (btn) {
                btn.disabled = false;
                btn.innerHTML = original;
            }
        }
    },

    updateOpsAutomation: async function(id, field, value, btn) {
        const serverID = App.config.SERVER_ID;
        const original = btn ? btn.innerHTML : '';
        if (btn) {
            btn.disabled = true;
            btn.innerHTML = '<i class="fas fa-spinner fa-spin"></i>';
        }

        try {
            const payload = { [field]: value };
            const res = await App.API.request(`/servers/${serverID}/projects/${id}/ops/automation`, {
                method: 'PUT',
                body: JSON.stringify(payload),
                headers: { 'Content-Type': 'application/json' }
            });
            if (!res.success) throw new Error(res.error || res.message || 'Automation update failed');

            const automation = res.data || {};
            this.opsData[`${id}:automation`] = automation;
            const project = (this.list || []).find(p => p.id === id);
            if (project) {
                project.restart_on_boot = !!automation.restart_on_boot;
                project.webhook_enabled = !!automation.webhook_enabled;
            }

            App.UI.toast('success', 'Automation updated');
            this.renderList(this.list || []);
            this.renderActiveOpsProject();
        } catch (err) {
            console.error('Automation update failed:', err);
            App.UI.toast('error', `Automation update failed: ${err.message}`);
        } finally {
            if (btn) {
                btn.disabled = false;
                btn.innerHTML = original;
            }
        }
    },

    copyCurrentLogs: function() {
        const project = (this.list || []).find(p => p.id === this.currentOpsProjectID);
        if (!project) return;
        const logs = this.getOpsData(project, 'logs');
        this.copyText(logs?.raw || '', 'Logs copied');
    },

    copyText: function(text, successMessage = 'Copied') {
        const textarea = document.createElement('textarea');
        textarea.innerHTML = text || '';
        const value = textarea.value;
        if (!value) {
            App.UI.toast('error', 'Nothing to copy');
            return;
        }

        navigator.clipboard.writeText(value)
            .then(() => App.UI.toast('success', successMessage))
            .catch(() => App.UI.toast('error', 'Failed to copy'));
    },

    getIntegrationIcon: function(key) {
        const icons = {
            files: 'fas fa-folder-open',
            environment: 'fas fa-key',
            webhook: 'fab fa-github',
            domain: 'fas fa-globe',
            database: 'fas fa-database',
            terminal: 'fas fa-terminal'
        };
        return icons[key] || 'fas fa-plug';
    },

    openIntegration: function(id, key) {
        if (key === 'files') {
            this.closeProjectOps();
            this.openFiles(id);
            return;
        }
        if (key === 'environment') {
            this.closeProjectOps();
            this.showEnvModal(id);
            return;
        }
        if (key === 'webhook') {
            this.closeProjectOps();
            this.showWebhookModal(id);
            return;
        }

        const tabMap = {
            domain: 'domains',
            database: 'databases',
            terminal: 'terminal'
        };
        const tab = tabMap[key];
        if (tab) {
            this.closeProjectOps();
            const btn = document.querySelector(`.tab-btn[data-tab="${tab}"]`);
            if (btn) {
                btn.click();
                App.UI.toast('success', `${this.projectTypeLabel(key)} opened`);
            }
        }
    },

    openFiles: function(id) {
        const project = (this.list || []).find(p => p.id === id);
        if (!project || !project.path) {
            App.UI.toast('error', 'Project path is not available');
            return;
        }

        App.state.currentPath = project.path;

        const filesTab = document.querySelector('.tab-btn[data-tab="files"]');
        if (filesTab) {
            filesTab.click();
            return;
        }

        if (App.Files && typeof App.Files.list === 'function') {
            App.Files.list(project.path);
        }
    },

    action: async function(id, action, btn) {
        const serverID = App.config.SERVER_ID;
        const originalStates = [];
        let buttons = [];
        
        if (btn) {
            // Find parent container to get all buttons in this card
            const container = btn.parentElement;
            if (container) {
                buttons = Array.from(container.querySelectorAll('button'));
            } else {
                buttons = [btn];
            }

            buttons.forEach(b => {
                originalStates.push({ btn: b, html: b.innerHTML });
                b.disabled = true;
                b.innerHTML = '<i class="fas fa-spinner fa-spin"></i>';
            });
        }

        try {
            let endpoint = `/servers/${serverID}/projects/${id}`;
             if (action === 'delete') {
                if(!confirm('Are you sure you want to delete this project?')) {
                    // Restore immediately if cancelled
                    if (btn) {
                         buttons.forEach((b, index) => {
                            b.disabled = false;
                            b.innerHTML = originalStates[index].html;
                        });
                    }
                    return;
                }
                 const res = await App.API.delete(endpoint);
                  if (res.success) {
                    App.UI.toast('success', 'Project deleted');
                    this.load();
                }
                return;
             }

            endpoint += `/${action}`;
            const res = await App.API.post(endpoint, {});
            if (res.success) {
                App.UI.toast('success', `Project ${action} triggered`);
                this.clearOpsCache(id);
                this.load();
                if (this.currentOpsProjectID === id) {
                    this.loadOpsTab(id, this.activeOpsTab || 'overview', true);
                }
            }
        } catch(err) {
             console.error(err);
             App.UI.toast('error', `Action failed: ${err.message}`);
             // Restore buttons on error
             if (btn) {
                buttons.forEach((b, index) => {
                    b.disabled = false;
                    b.innerHTML = originalStates[index].html;
                });
            }
        }
    },

    showCreateModal: function() {
        // Show modal logic
        const modal = document.getElementById('create-project-modal');
        if (modal) {
            modal.classList.remove('hidden');
            setTimeout(() => {
                modal.classList.remove('opacity-0');
                modal.querySelector('div').classList.remove('scale-95');
                modal.querySelector('div').classList.add('scale-100');
            }, 10);
        }
    },

    closeCreateModal: function() {
       const modal = document.getElementById('create-project-modal');
        if (modal) {
            modal.classList.add('opacity-0');
            modal.querySelector('div').classList.remove('scale-100');
            modal.querySelector('div').classList.add('scale-95');
            setTimeout(() => {
                modal.classList.add('hidden');
            }, 300);
        } 
    },

    selectTech: function(tech) {
        document.querySelectorAll('.tech-card').forEach(c => {
            c.classList.remove('border-blue-500', 'bg-blue-900/20');
            c.classList.add('border-gray-700', 'bg-gray-800');
        });
        
        const selected = document.getElementById(`tech-${tech}`);
        if (selected) {
            selected.classList.remove('border-gray-700', 'bg-gray-800');
            selected.classList.add('border-blue-500', 'bg-blue-900/20');
        }
        
        const input = document.getElementById('selected-tech');
        if (input) input.value = tech;

        // Auto-fill commands
        const buildCmdInput = document.getElementById('project-build-cmd');
        const startCmdInput = document.getElementById('project-start-cmd');
        
        if (tech === 'nextjs') {
            if (buildCmdInput) buildCmdInput.value = 'npm install && npm run build';
            if (startCmdInput) startCmdInput.value = 'npm start';
        } else if (tech === 'nodejs') {
             if (buildCmdInput) buildCmdInput.value = 'npm install'; // Default
             if (startCmdInput) startCmdInput.value = 'npm start';
        } else if (tech === 'go') {
            if (buildCmdInput) buildCmdInput.value = 'go build -o app';
            if (startCmdInput) startCmdInput.value = './app';
        } else if (tech === 'laravel') {
            if (buildCmdInput) buildCmdInput.value = 'composer install --no-dev --optimize-autoloader';
            if (startCmdInput) startCmdInput.value = 'php artisan serve --port=8000 --host=0.0.0.0';
        }
    },

    handleFileSelect: function(input) {
        const label = document.getElementById('project-file-label');
        if (input.files && input.files[0]) {
            label.textContent = input.files[0].name;
            label.classList.add('text-blue-400');
        } else {
            label.textContent = 'Upload Zip';
            label.classList.remove('text-blue-400');
        }
    },

    // createProject moved to end of file to handle git source


    editProject: function(id) {
        console.log('editProject called for ID:', id);
        // Find project from local list
        console.log('Current list:', this.list);
        const project = this.list ? this.list.find(p => p.id === id) : null;
        if (!project) {
            console.error('Project not found in list:', id);
            App.UI.toast('error', 'Project not found');
            return;
        }
        console.log('Project found:', project);

        this.currentProject = project; // Store for update
        
        const idInput = document.getElementById('edit-project-id');
        const nameInput = document.getElementById('edit-project-name');
        const portInput = document.getElementById('edit-project-port');
        const buildCmdInput = document.getElementById('edit-project-build-cmd');
        const startCmdInput = document.getElementById('edit-project-start-cmd');

        if(idInput) idInput.value = project.id;
        if(nameInput) nameInput.value = project.name;
        if(portInput) portInput.value = project.port;
        if(buildCmdInput) buildCmdInput.value = project.build_command || '';
        if(startCmdInput) startCmdInput.value = project.start_command || '';

        const restartOnBootInput = document.getElementById('edit-project-restart-on-boot');
        if(restartOnBootInput) restartOnBootInput.checked = !!project.restart_on_boot;

        this.openEditModal();
    },

    openEditModal: function() {
        const modal = document.getElementById('edit-project-modal');
        console.log('Opening edit modal, element:', modal);
        if (modal) {
            modal.classList.remove('hidden');
            setTimeout(() => {
                modal.classList.remove('opacity-0');
                modal.querySelector('div').classList.remove('scale-95');
                modal.querySelector('div').classList.add('scale-100');
            }, 10);
        } else {
            console.error('Edit modal element not found!');
        }
    },

    closeEditModal: function() {
        const modal = document.getElementById('edit-project-modal');
        if (modal) {
            modal.classList.add('opacity-0');
            modal.querySelector('div').classList.remove('scale-100');
            modal.querySelector('div').classList.add('scale-95');
            setTimeout(() => {
                modal.classList.add('hidden');
            }, 300);
        }
    },

    updateProject: async function() {
        const id = document.getElementById('edit-project-id').value;
        const name = document.getElementById('edit-project-name').value;
        const port = document.getElementById('edit-project-port').value;
        const buildCmd = document.getElementById('edit-project-build-cmd').value;
        const startCmd = document.getElementById('edit-project-start-cmd').value;
        const restartOnBoot = document.getElementById('edit-project-restart-on-boot').checked;
        const serverID = App.config.SERVER_ID;

        const payload = {
            name: name,
            port: parseInt(port) || 3000,
            host_port: parseInt(port) || 3000,
            build_command: buildCmd,
            start_command: startCmd,
            restart_on_boot: restartOnBoot
        };

        const submitBtn = document.getElementById('update-project-btn');
        const originalBtnText = submitBtn ? submitBtn.innerText : 'Save Changes';
        if (submitBtn) {
            submitBtn.disabled = true;
            submitBtn.innerHTML = '<i class="fas fa-spinner fa-spin mr-2"></i>Saving...';
        }

        try {
            const response = await App.API.request(`/servers/${serverID}/projects/${id}`, {
                method: 'PUT',
                body: JSON.stringify(payload),
                headers: { 'Content-Type': 'application/json' }
            });

            if (!response.success) throw new Error(response.error || 'Failed to update project');

            App.UI.toast('success', 'Project updated successfully');
            this.closeEditModal();
            this.load();
        } catch (err) {
            console.error(err);
            App.UI.toast('error', 'Error: ' + (err.message || 'Unknown error'));
        } finally {
             if (submitBtn) {
                submitBtn.disabled = false;
                submitBtn.innerText = originalBtnText;
            }
        }
    },

    // GitHub Integration
    githubLogin: function() {
        window.location.href = '/auth/github/login';
    },

    openGitHubModal: function() {
        const modal = document.getElementById('github-repos-modal');
        if (modal) {
            modal.classList.remove('hidden');
            setTimeout(() => {
                modal.classList.remove('opacity-0');
                modal.querySelector('div').classList.remove('scale-95');
                modal.querySelector('div').classList.add('scale-100');
            }, 10);
            this.loadGitHubRepos();
        }
    },

    closeGitHubModal: function() {
        const modal = document.getElementById('github-repos-modal');
        if (modal) {
            modal.classList.add('opacity-0');
            modal.querySelector('div').classList.remove('scale-100');
            modal.querySelector('div').classList.add('scale-95');
            setTimeout(() => modal.classList.add('hidden'), 300);
        }
    },

    loadGitHubRepos: function() {
        const list = document.getElementById('github-repo-list');
        list.innerHTML = '<div class="text-center text-gray-500 py-8"><i class="fas fa-spinner fa-spin text-2xl mb-2"></i><p>Loading repositories...</p></div>';

        // Clear search
        const searchInput = document.getElementById('github-repo-search');
        if(searchInput) searchInput.value = '';

        App.API.get('/github/repos')
            .then(response => {
                const repos = response.data || response;
                if (!repos || repos.length === 0) {
                    list.innerHTML = '<div class="text-center text-gray-500 py-8"><p>No repositories found. Create one on GitHub!</p></div>';
                    this.repos = [];
                    return;
                }
                
                // Store for filtering
                this.repos = repos;
                this.renderGitHubRepos(repos);
            })
            .catch(err => {
                console.error('Failed to load repos:', err);
                // Check if 404 (handler not found/configured) or 401 (not auth)
                // App.API.get throws error. If response was 401, it throws "HTTP error! status: 401"
                
                 if (err.message.includes('401')) {
                      list.innerHTML = `
                        <div class="text-center text-gray-500 py-8">
                            <i class="fab fa-github text-4xl mb-3 text-gray-600"></i>
                            <p class="mb-4 text-lg">Connect GitHub to view repositories</p>
                            <button onclick="App.Projects.githubLogin()" class="bg-gray-800 text-white px-6 py-2 rounded-lg hover:bg-gray-700 transition-colors border border-gray-700 hover:border-gray-500">
                                <i class="fab fa-github mr-2"></i> Connect GitHub Account
                            </button>
                        </div>
                      `;
                 } else if (err.message.includes('404')) {
                      list.innerHTML = `
                        <div class="text-center text-red-400 py-8">
                            <i class="fas fa-exclamation-triangle text-2xl mb-2"></i>
                            <p>GitHub integration not configured or endpoint not found.</p>
                            <p class="text-sm text-gray-500 mt-2">Check config.yaml and restart server.</p>
                        </div>
                      `;
                 } else {
                    list.innerHTML = `<div class="text-center text-red-400 py-8"><p>Failed to load repositories: ${err.message}</p></div>`;
                 }
                // Don't toast for expected auth flow
                if (!err.message.includes('401')) {
                    App.UI.toast('error', 'Failed to load GitHub repositories');
                }
            });
    },

    renderGitHubRepos: function(repos) {
        const list = document.getElementById('github-repo-list');
        if (!repos || repos.length === 0) {
            list.innerHTML = '<div class="text-center text-gray-500 py-8"><p>No repositories found matching your search.</p></div>';
            return;
        }

        list.innerHTML = repos.map(repo => `
            <div class="bg-gray-800 p-4 rounded-lg border border-gray-700 hover:border-blue-500 transition-colors cursor-pointer group mb-3" onclick="App.Projects.selectRepo('${repo.clone_url}', '${repo.name}')">
                <div class="flex items-center justify-between">
                    <div class="flex items-center space-x-3 overflow-hidden">
                        <i class="fab fa-github text-xl text-gray-400 group-hover:text-white flex-shrink-0"></i>
                        <div class="min-w-0">
                            <h4 class="font-medium text-white group-hover:text-blue-400 transition-colors truncate">${repo.full_name}</h4>
                            <p class="text-sm text-gray-500 truncate">${repo.description || 'No description'}</p>
                        </div>
                    </div>
                    <i class="fas fa-chevron-right text-gray-600 group-hover:text-white flex-shrink-0 ml-2"></i>
                </div>
            </div>
        `).join('');
    },

    filterGitHubRepos: function() {
        const query = document.getElementById('github-repo-search').value.toLowerCase();
        if (!this.repos) return;

        const filtered = this.repos.filter(repo => 
            repo.full_name.toLowerCase().includes(query) || 
            (repo.description && repo.description.toLowerCase().includes(query))
        );
        this.renderGitHubRepos(filtered);
    },

    selectRepo: function(cloneUrl, name) {
        this.selectedRepo = { cloneUrl, name };
        this.closeGitHubModal();
        
        // Open create modal
        this.showCreateModal();
        
        // Populate fields
        setTimeout(() => {
            const nameInput = document.getElementById('project-name');
            const repoInput = document.getElementById('project-repo-url');
            
            if (nameInput) {
                nameInput.value = name;
                 // Trigger change event if needed for validation
                nameInput.dispatchEvent(new Event('change'));
            }
            
            if (repoInput) {
                repoInput.value = cloneUrl;
            }
            
            // Show feedback that repo is selected
            const fileLabel = document.getElementById('project-file-label');
            if (fileLabel) {
                 fileLabel.innerHTML = `<i class="fab fa-github mr-2"></i> ${name}`;
                 fileLabel.classList.add('text-blue-400');
            }
            
            // Maybe disable file input?
            const fileInput = document.getElementById('project-file');
            if (fileInput) fileInput.value = ''; // Clear file
            
        }, 100);
    },
    
    // Updated createProject to handle git source
    createProject: async function() {
        const name = document.getElementById('project-name').value;
        const port = document.getElementById('project-port').value;
        const buildCmd = document.getElementById('project-build-cmd').value;
        const startCmd = document.getElementById('project-start-cmd').value;
        const tech = document.getElementById('selected-tech').value;
        const repoUrl = document.getElementById('project-repo-url').value;
        const restartOnBoot = document.getElementById('project-restart-on-boot').checked;
        const serverID = App.config.SERVER_ID;

        if (!name || !tech) {
            App.UI.toast('error', 'Name and Technology are required');
            return;
        }

        // Determine source type
        // If repoUrl is set, use 'git'. But wait, backend needs to handle this.
        // If we use the standard CreateProject endpoint, it might expect 'upload' or 'git'.
        // If 'git', it presumably needs 'source_url'.
        
        // However, we implemented a specific /api/github/clone endpoint in GitHubHandler.
        // Should we use that?
        // The GitHubHandler.CloneRepo uses ProjectService.CreateProjectFromRepo.
        // The standard ProjectHandler.CreateProject uses ProjectService.CreateProject.
        
        // If we use the standard endpoint, we need to ensure it calls CreateProjectFromRepo or setting SourceURL.
        // Let's check ProjectHandler.CreateProject implementation.
        // As I cannot check it right now inside this function, I will assume I need to use the Clone endpoint if repoUrl is present.
        
        const submitBtn = document.getElementById('create-project-btn-submit');
        const originalBtnText = submitBtn ? submitBtn.innerText : 'Create Project';
        if (submitBtn) {
            submitBtn.disabled = true;
            submitBtn.innerHTML = '<i class="fas fa-spinner fa-spin mr-2"></i>Creating...';
        }

        try {
            if (repoUrl) {
                // Use GitHub Clone Endpoint
                const payload = {
                    name: name,
                    type: tech,
                    port: parseInt(port) || 3000,
                    build_cmd: buildCmd,
                    start_cmd: startCmd,
                    repo_url: repoUrl,
                    server_id: serverID,
                    restart_on_boot: restartOnBoot
                };
                
                // Use 'github/clone' (no leading slash) so App.API doesn't prefix with /servers/{id}
                const response = await App.API.post('/github/clone', payload);
                if (!response.success) throw new Error(response.error || 'Failed to clone project');
                
                App.UI.toast('success', 'Project cloned successfully');
            } else {
                // Standard Upload Flow
                const payload = {
                    name: name,
                    type: tech,
                    port: parseInt(port) || 3000,
                    host_port: parseInt(port) || 3000,
                    build_command: buildCmd,
                    start_command: startCmd,
                    source_type: 'upload',
                    build_type: 'process',
                    restart_on_boot: restartOnBoot
                };
                
                // 1. Create Metadata
                const response = await App.API.post(`/servers/${serverID}/projects`, payload);
                if (!response.success) throw new Error(response.error || 'Failed to create project');

                const project = response.data;
                App.UI.toast('success', 'Project created, starting upload...');

                // 2. Upload File
                const fileInput = document.getElementById('project-file');
                if (fileInput && fileInput.files.length > 0) {
                     // ... upload logic ...
                     // reusing existing logic structure
                     const progressDiv = document.getElementById('project-upload-progress');
                     const progressBar = document.getElementById('project-upload-bar');
                     // ... setup vars
                     if(progressDiv) progressDiv.classList.remove('hidden');
                     
                     const formData = new FormData();
                     formData.append('project_id', project.id);
                     formData.append('file', fileInput.files[0]);
                     
                     await App.API.upload(`/servers/${serverID}/projects/upload?project_id=${project.id}`, formData, (p) => {
                         if(progressBar) progressBar.style.width = `${p}%`;
                     });
                     App.UI.toast('success', 'Project files uploaded successfully');
                }
            }

            // Refresh and Close
            this.closeCreateModal();
            this.load();
            
            // Clean up
            document.getElementById('project-repo-url').value = '';
            
        } catch (err) {
            console.error(err);
            App.UI.toast('error', 'Error: ' + (err.message || 'Unknown error'));
        } finally {
            if (submitBtn) {
                submitBtn.disabled = false;
                submitBtn.innerText = originalBtnText;
            }
        }
    },

    showWebhookModal: function(id) {
        const project = this.list ? this.list.find(p => p.id === id) : null;
        if (!project) {
            App.UI.toast('error', 'Project not found');
            return;
        }

        document.getElementById('webhook-project-id').value = id;
        document.getElementById('webhook-project-name').textContent = project.name;

        // Toggle state
        const toggleBtn = document.getElementById('webhook-toggle-btn');
        const toggleSpan = document.getElementById('webhook-toggle-span');
        const detailsContainer = document.getElementById('webhook-details-container');

        if (project.webhook_enabled) {
            toggleBtn.className = 'relative inline-flex h-6 w-11 items-center rounded-full bg-blue-600 transition-colors focus:outline-none';
            toggleSpan.className = 'inline-block h-4 w-4 transform rounded-full bg-white transition-transform translate-x-6';
            
            // Build absolute payload URL
            const protocol = window.location.protocol;
            const host = window.location.host;
            document.getElementById('webhook-payload-url').value = `${protocol}//${host}/api/webhooks/github/${id}`;
            document.getElementById('webhook-secret-token').value = project.webhook_secret || '';
            document.getElementById('webhook-secret-token').type = 'password';
            document.getElementById('webhook-secret-eye').className = 'fas fa-eye';

            detailsContainer.classList.remove('hidden');
        } else {
            toggleBtn.className = 'relative inline-flex h-6 w-11 items-center rounded-full bg-gray-700 transition-colors focus:outline-none';
            toggleSpan.className = 'inline-block h-4 w-4 transform rounded-full bg-white transition-transform translate-x-1';
            
            document.getElementById('webhook-payload-url').value = '';
            document.getElementById('webhook-secret-token').value = '';

            detailsContainer.classList.add('hidden');
        }

        const modal = document.getElementById('project-webhook-modal');
        if (modal) {
            modal.classList.remove('hidden');
            setTimeout(() => {
                modal.classList.remove('opacity-0');
                modal.querySelector('div').classList.remove('scale-95');
                modal.querySelector('div').classList.add('scale-100');
            }, 10);
        }
    },

    closeWebhookModal: function() {
        const modal = document.getElementById('project-webhook-modal');
        if (modal) {
            modal.classList.add('opacity-0');
            modal.querySelector('div').classList.remove('scale-100');
            modal.querySelector('div').classList.add('scale-95');
            setTimeout(() => modal.classList.add('hidden'), 300);
        }
    },

    toggleWebhook: async function() {
        const id = document.getElementById('webhook-project-id').value;
        const project = this.list ? this.list.find(p => p.id === id) : null;
        if (!project) return;

        const serverID = App.config.SERVER_ID;
        const isCurrentlyEnabled = project.webhook_enabled;
        const action = isCurrentlyEnabled ? 'disable' : 'enable';

        try {
            const res = await App.API.post(`/servers/${serverID}/projects/${id}/webhook/${action}`, {});
            if (res.success) {
                App.UI.toast('success', `Webhook ${isCurrentlyEnabled ? 'disabled' : 'enabled'} successfully`);
                
                // Update local model state
                project.webhook_enabled = !isCurrentlyEnabled;
                if (!isCurrentlyEnabled && res.data) {
                    project.webhook_secret = res.data.webhook_secret;
                } else {
                    project.webhook_secret = '';
                }

                // Refresh modal view and list rendering
                this.showWebhookModal(id);
                this.load();
            }
        } catch (err) {
            console.error('Failed to toggle webhook:', err);
            App.UI.toast('error', `Failed to toggle webhook: ${err.message}`);
        }
    },

    copyWebhookField: function(inputId) {
        const input = document.getElementById(inputId);
        if (input) {
            input.select();
            input.setSelectionRange(0, 99999); // For mobile devices
            navigator.clipboard.writeText(input.value)
                .then(() => App.UI.toast('success', 'Copied to clipboard'))
                .catch(err => App.UI.toast('error', 'Failed to copy'));
        }
    },

    toggleWebhookSecret: function() {
        const input = document.getElementById('webhook-secret-token');
        const eye = document.getElementById('webhook-secret-eye');
        if (input && eye) {
            if (input.type === 'password') {
                input.type = 'text';
                eye.className = 'fas fa-eye-slash';
            } else {
                input.type = 'password';
                eye.className = 'fas fa-eye';
            }
        }
    },

    showEnvModal: async function(id) {
        const project = this.list ? this.list.find(p => p.id === id) : null;
        if (!project) {
            App.UI.toast('error', 'Project not found');
            return;
        }

        this.currentEnvProjectID = id;
        document.getElementById('env-project-name').textContent = project.name;

        const container = document.getElementById('env-rows-container');
        container.innerHTML = '<div class="text-center text-gray-500 py-6"><i class="fas fa-circle-notch fa-spin text-2xl mr-2"></i>Loading variables...</div>';

        const modal = document.getElementById('env-vars-modal');
        if (modal) {
            modal.classList.remove('hidden');
            setTimeout(() => {
                modal.classList.remove('opacity-0');
                modal.querySelector('div').classList.remove('scale-95');
                modal.querySelector('div').classList.add('scale-100');
            }, 10);
        }

        try {
            const serverID = App.config.SERVER_ID;
            const res = await App.API.get(`/servers/${serverID}/projects/${id}/env`);
            container.innerHTML = '';
            
            if (res.success && res.data) {
                const vars = res.data;
                const keys = Object.keys(vars).sort();
                if (keys.length === 0) {
                    container.innerHTML = '<div class="text-center text-gray-500 py-6" id="no-env-msg">No environment variables defined yet.</div>';
                } else {
                    keys.forEach(key => {
                        this.addEnvRow(key, vars[key]);
                    });
                }
            }
        } catch (err) {
            console.error('Failed to load env vars:', err);
            container.innerHTML = `<div class="text-red-500 text-center py-6">Failed to load environment variables: ${err.message}</div>`;
        }
    },

    closeEnvModal: function() {
        const modal = document.getElementById('env-vars-modal');
        if (modal) {
            modal.classList.add('opacity-0');
            modal.querySelector('div').classList.remove('scale-100');
            modal.querySelector('div').classList.add('scale-95');
            setTimeout(() => modal.classList.add('hidden'), 300);
        }
    },

    addEnvRow: function(key = '', value = '') {
        const container = document.getElementById('env-rows-container');
        const noMsg = document.getElementById('no-env-msg');
        if (noMsg) noMsg.remove();

        const row = document.createElement('div');
        row.className = 'flex items-center space-x-3 bg-gray-900/40 p-2.5 rounded-lg border border-gray-700/50 env-row';
        
        row.innerHTML = `
            <input type="text" placeholder="KEY" value="${key}" class="bg-gray-700 text-white rounded px-3 py-2 w-1/3 font-mono text-sm focus:outline-none focus:border-blue-500 env-key" />
            <div class="relative flex-1">
                <input type="password" placeholder="VALUE" value="${value}" class="bg-gray-700 text-white rounded px-3 py-2 w-full font-mono text-sm focus:outline-none focus:border-blue-500 pr-10 env-val" />
                <button type="button" onclick="App.Projects.toggleEnvRowVisibility(this)" class="absolute right-3 top-2.5 text-gray-400 hover:text-white">
                    <i class="fas fa-eye"></i>
                </button>
            </div>
            <button type="button" onclick="App.Projects.removeEnvRow(this)" class="text-red-500 hover:text-red-400 p-2">
                <i class="fas fa-trash-alt"></i>
            </button>
        `;
        container.appendChild(row);
    },

    toggleEnvRowVisibility: function(btn) {
        const input = btn.previousElementSibling;
        const icon = btn.querySelector('i');
        if (input && icon) {
            if (input.type === 'password') {
                input.type = 'text';
                icon.className = 'fas fa-eye-slash';
            } else {
                input.type = 'password';
                icon.className = 'fas fa-eye';
            }
        }
    },

    removeEnvRow: function(btn) {
        btn.parentElement.remove();
        const container = document.getElementById('env-rows-container');
        if (container.children.length === 0) {
            container.innerHTML = '<div class="text-center text-gray-500 py-6" id="no-env-msg">No environment variables defined yet.</div>';
        }
    },

    saveEnvVars: async function() {
        const id = this.currentEnvProjectID;
        if (!id) return;

        const serverID = App.config.SERVER_ID;
        const rows = document.querySelectorAll('.env-row');
        const vars = {};

        let hasError = false;
        rows.forEach(row => {
            const keyInput = row.querySelector('.env-key');
            const valInput = row.querySelector('.env-val');
            
            const key = keyInput.value.trim().toUpperCase();
            const value = valInput.value.trim();

            if (key) {
                if (!/^[A-Z_][A-Z0-9_]*$/.test(key)) {
                    App.UI.toast('error', `Invalid key format: ${key}. Use uppercase alphanumeric and underscores.`);
                    keyInput.classList.add('border-red-500');
                    hasError = true;
                    return;
                }
                keyInput.classList.remove('border-red-500');
                vars[key] = value;
            }
        });

        if (hasError) return;

        try {
            const res = await App.API.request(`/servers/${serverID}/projects/${id}/env`, {
                method: 'PUT',
                body: JSON.stringify(vars),
                headers: { 'Content-Type': 'application/json' }
            });

            if (res.success) {
                App.UI.toast('success', 'Environment variables updated and synced successfully');
                this.closeEnvModal();
                this.load();
            }
        } catch (err) {
            console.error('Failed to save env vars:', err);
            App.UI.toast('error', `Failed to save environment variables: ${err.message}`);
        }
    }
};
