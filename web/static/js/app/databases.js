/**
 * GoPulse App - Databases Module
 * Provisioning, Lifecycle, Backup & Query consoles
 */

App.Databases = (function() {
    let currentDb = null;
    let activeManageTab = 'overview';

    // Engine Icons & Color Mapping
    const engines = {
        mysql: {
            name: "MySQL 8.0",
            icon: "fas fa-database",
            logoClass: "bg-blue-900/30 text-blue-400 border border-blue-500/30",
            badgeClass: "bg-blue-900/50 text-blue-400 border border-blue-500/20",
            defaultPort: 3306,
            defaultUser: "root"
        },
        postgres: {
            name: "PostgreSQL 15",
            icon: "fas fa-server",
            logoClass: "bg-indigo-900/30 text-indigo-400 border border-indigo-500/30",
            badgeClass: "bg-indigo-900/50 text-indigo-400 border border-indigo-500/20",
            defaultPort: 5432,
            defaultUser: "postgres"
        },
        mongodb: {
            name: "MongoDB 6.0",
            icon: "fas fa-cubes",
            logoClass: "bg-emerald-900/30 text-emerald-400 border border-emerald-500/30",
            badgeClass: "bg-emerald-900/50 text-emerald-400 border border-emerald-500/20",
            defaultPort: 27017,
            defaultUser: "root"
        },
        redis: {
            name: "Redis 7.0",
            icon: "fas fa-database",
            logoClass: "bg-red-900/30 text-red-500 border border-red-500/30",
            badgeClass: "bg-red-900/50 text-red-400 border border-red-500/20",
            defaultPort: 6379,
            defaultUser: "default"
        },
        sqlite: {
            name: "SQLite 3",
            icon: "fas fa-file-alt",
            logoClass: "bg-sky-900/30 text-sky-400 border border-sky-500/30",
            badgeClass: "bg-sky-900/50 text-sky-400 border border-sky-500/20",
            defaultPort: 0,
            defaultUser: ""
        },
        pocketbase: {
            name: "PocketBase",
            icon: "fas fa-server",
            logoClass: "bg-orange-950/40 text-orange-500 border border-orange-500/30",
            badgeClass: "bg-orange-950/60 text-orange-400 border border-orange-500/20",
            defaultPort: 8090,
            defaultUser: "admin"
        }
    };

    // Load and render all databases for the active server
    async function load() {
        const grid = document.getElementById('databases-grid');
        const emptyState = document.getElementById('db-empty-state');
        if (!grid) return;

        grid.innerHTML = '<div class="col-span-2 text-center py-12"><i class="fas fa-spinner fa-spin text-3xl text-gray-500"></i></div>';
        emptyState.classList.add('hidden');

        try {
            const res = await App.API.get(`/servers/${App.config.SERVER_ID}/databases`);
            if (!res.success || !res.data || res.data.length === 0) {
                grid.innerHTML = '';
                emptyState.classList.remove('hidden');
                updateStatsRibbon([]);
                return;
            }

            const dbs = res.data;
            updateStatsRibbon(dbs);
            grid.innerHTML = '';

            const focusedDiags = document.getElementById('db-focused-diagnostics');
            if (focusedDiags && !focusedDiags.classList.contains('hidden')) {
                fetchFocusedDiagnostics(dbs);
            }

            dbs.forEach(db => {
                const config = engines[db.type] || engines.mysql;
                const statusColor = getStatusColor(db.status);
                
                const card = document.createElement('div');
                card.className = "bg-gray-800/60 backdrop-blur-md rounded-xl p-6 border border-gray-700/60 shadow-lg hover:border-gray-600 transition-all flex flex-col justify-between space-y-4";
                card.innerHTML = `
                    <div class="flex justify-between items-start">
                        <div class="flex items-center space-x-3.5">
                            <div class="p-3.5 rounded-xl ${config.logoClass} text-xl">
                                <i class="${config.icon}"></i>
                            </div>
                            <div>
                                <h3 class="text-lg font-bold text-white tracking-tight">${escapeHTML(db.name)}</h3>
                                <div class="flex items-center space-x-2 mt-1">
                                    <span class="px-2 py-0.5 text-[10px] font-bold tracking-wider rounded-md uppercase ${config.badgeClass}">${db.type}</span>
                                    <span class="px-2 py-0.5 text-[10px] font-bold tracking-wider rounded-md uppercase ${statusColor} flex items-center">
                                        <span class="w-1.5 h-1.5 rounded-full mr-1.5 bg-current ${db.status === 'Running' ? 'animate-pulse' : ''}"></span>
                                        <span>${db.status}</span>
                                    </span>
                                </div>
                            </div>
                        </div>

                        <!-- Quick Actions -->
                        <div class="flex items-center space-x-1.5">
                            <button onclick="App.Databases.quickCheckHealth('${db.id}', this)" class="p-2 bg-gray-700/50 hover:bg-gray-700 text-gray-400 hover:text-white rounded-lg transition-all text-xs" title="Verify Connection Ping">
                                <i class="fas fa-plug"></i>
                            </button>
                        </div>
                    </div>

                    <div class="border-t border-gray-700/50 pt-4 flex flex-col space-y-2 text-sm text-gray-300 font-mono">
                        <div class="flex justify-between">
                            <span class="text-gray-400">Host Port:</span>
                            <span class="text-white font-semibold">${db.host_port}</span>
                        </div>
                        <div class="flex justify-between">
                            <span class="text-gray-400">Database:</span>
                            <span class="text-white font-semibold">${escapeHTML(db.database_name)}</span>
                        </div>
                        <div class="flex justify-between">
                            <span class="text-gray-400">Username:</span>
                            <span class="text-white font-semibold">${escapeHTML(db.username)}</span>
                        </div>
                    </div>

                    <div class="border-t border-gray-700/50 pt-4 flex justify-between items-center gap-3">
                        <div class="flex space-x-2">
                            ${db.status === 'Running' 
                                ? `<button onclick="App.Databases.togglePower('${db.id}', 'stop')" class="px-3.5 py-2 bg-red-950/30 hover:bg-red-950/60 border border-red-500/20 text-red-400 text-xs font-bold rounded-lg transition-all"><i class="fas fa-stop mr-1.5"></i>Stop</button>`
                                : `<button onclick="App.Databases.togglePower('${db.id}', 'start')" class="px-3.5 py-2 bg-green-950/30 hover:bg-green-950/60 border border-green-500/20 text-green-400 text-xs font-bold rounded-lg transition-all"><i class="fas fa-play mr-1.5"></i>Start</button>`
                            }
                            <button onclick="App.Databases.togglePower('${db.id}', 'restart')" class="px-3 py-2 bg-gray-700/50 hover:bg-gray-700 text-gray-300 text-xs font-bold rounded-lg transition-all" title="Reboot Container"><i class="fas fa-sync-alt"></i></button>
                        </div>

                        <button onclick="App.Databases.showManageView('${db.id}')" class="px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white text-xs font-bold rounded-lg transition-all shadow-md flex items-center space-x-1.5">
                            <span>Manage Cluster</span>
                            <i class="fas fa-chevron-right text-[10px]"></i>
                        </button>
                    </div>
                `;
                grid.appendChild(card);
            });

        } catch (e) {
            grid.innerHTML = '<div class="col-span-2 text-center text-red-400 py-12">Failed to load databases catalog.</div>';
        }
    }

    function updateStatsRibbon(dbs) {
        let mysql = 0, postgres = 0, mongodb = 0, redis = 0, sqlite = 0, pocketbase = 0;
        dbs.forEach(db => {
            if (db.type === 'mysql') mysql++;
            if (db.type === 'postgres') postgres++;
            if (db.type === 'mongodb') mongodb++;
            if (db.type === 'redis') redis++;
            if (db.type === 'sqlite') sqlite++;
            if (db.type === 'pocketbase') pocketbase++;
        });
        const elMysql = App.UI.el('db-stat-mysql');
        const elPostgres = App.UI.el('db-stat-postgres');
        const elMongodb = App.UI.el('db-stat-mongodb');
        const elRedis = App.UI.el('db-stat-redis');
        const elSqlite = App.UI.el('db-stat-sqlite');
        const elPocketbase = App.UI.el('db-stat-pocketbase');
        if (elMysql) elMysql.innerText = mysql;
        if (elPostgres) elPostgres.innerText = postgres;
        if (elMongodb) elMongodb.innerText = mongodb;
        if (elRedis) elRedis.innerText = redis;
        if (elSqlite) elSqlite.innerText = sqlite;
        if (elPocketbase) elPocketbase.innerText = pocketbase;
    }

    async function fetchFocusedDiagnostics(dbs) {
        const diagnosticsContainer = document.getElementById('db-engine-diagnostics-container');
        const connVal = document.getElementById('db-conn-gauge-val');
        const connCircle = document.getElementById('db-conn-gauge-circle');
        const footprintVal = document.getElementById('db-footprint-val');
        const footprintBar = document.getElementById('db-footprint-bar');

        if (!diagnosticsContainer) return;

        let totalConnections = 0;
        let totalSize = 0; // In bytes
        let diagnosticsHTML = '';

        const runningDbs = dbs.filter(db => db.status === 'Running');

        if (runningDbs.length === 0) {
            diagnosticsContainer.innerHTML = `
                <div class="flex items-center justify-between p-2 rounded-lg bg-gray-900/30 border border-gray-700/20">
                    <div class="flex items-center space-x-2">
                        <i class="fas fa-exclamation-triangle text-yellow-500 text-xs"></i>
                        <span class="text-xs font-semibold text-gray-400">No active database engines running</span>
                    </div>
                </div>
            `;
            if (connVal) connVal.innerText = '0';
            if (connCircle) connCircle.style.strokeDashoffset = '276.46';
            if (footprintVal) footprintVal.innerHTML = '0.00 <span class="text-xs font-semibold text-gray-400">MB</span>';
            if (footprintBar) footprintBar.style.width = '0%';
            return;
        }

        const promises = runningDbs.map(async db => {
            const config = engines[db.type] || engines.mysql;
            try {
                const res = await App.API.get(`/servers/${App.config.SERVER_ID}/databases/${db.id}/telemetry`);
                if (res.success && res.data) {
                    const conn = res.data.active_connections || 0;
                    const size = res.data.data_size || 0;
                    totalConnections += conn;
                    totalSize += size;

                    diagnosticsHTML += `
                        <div class="flex items-center justify-between p-2 rounded-lg bg-gray-900/40 border border-gray-700/30">
                            <div class="flex items-center space-x-2.5">
                                <div class="w-1.5 h-1.5 rounded-full bg-green-500 animate-pulse"></div>
                                <i class="${config.icon} text-xs text-gray-400"></i>
                                <span class="text-xs font-semibold text-gray-200 truncate max-w-[100px]">${escapeHTML(db.name)}</span>
                            </div>
                            <div class="flex items-center space-x-3 text-xs text-gray-400 font-mono">
                                <span><i class="fas fa-plug text-[10px] text-indigo-400 mr-1"></i>${conn}</span>
                                <span><i class="fas fa-hdd text-[10px] text-blue-400 mr-1"></i>${(size / (1024 * 1024)).toFixed(1)} MB</span>
                            </div>
                        </div>
                    `;
                }
            } catch (e) {
                console.error(`Failed to fetch telemetry for DB ${db.id}`, e);
                diagnosticsHTML += `
                    <div class="flex items-center justify-between p-2 rounded-lg bg-red-950/10 border border-red-500/20">
                        <div class="flex items-center space-x-2.5">
                            <div class="w-1.5 h-1.5 rounded-full bg-red-500"></div>
                            <i class="${config.icon} text-xs text-gray-500"></i>
                            <span class="text-xs font-semibold text-gray-400 truncate max-w-[100px]">${escapeHTML(db.name)}</span>
                        </div>
                        <span class="text-[10px] text-red-400 uppercase font-bold">Error</span>
                    </div>
                `;
            }
        });

        await Promise.all(promises);

        diagnosticsContainer.innerHTML = diagnosticsHTML;

        if (connVal) connVal.innerText = totalConnections;
        if (connCircle) {
            const maxConns = 50;
            const percentage = Math.min(totalConnections / maxConns, 1);
            const circumference = 276.46;
            const offset = circumference - (percentage * circumference);
            connCircle.style.strokeDashoffset = offset;
        }

        if (footprintVal) {
            const sizeInMB = totalSize / (1024 * 1024);
            if (sizeInMB > 1024) {
                footprintVal.innerHTML = `${(sizeInMB / 1024).toFixed(2)} <span class="text-xs font-semibold text-gray-400 uppercase">GB</span>`;
            } else {
                footprintVal.innerHTML = `${sizeInMB.toFixed(2)} <span class="text-xs font-semibold text-gray-400 uppercase">MB</span>`;
            }
        }
        if (footprintBar) {
            const maxVolumeSpace = 5 * 1024 * 1024 * 1024;
            const footprintPercentage = Math.min(totalSize / maxVolumeSpace, 1) * 100;
            footprintBar.style.width = `${footprintPercentage}%`;
        }
    }

    function getStatusColor(status) {
        switch (status) {
            case 'Running': return 'bg-green-950/50 text-green-400 border border-green-500/20';
            case 'Starting': return 'bg-yellow-950/50 text-yellow-400 border border-yellow-500/20';
            case 'Stopped': return 'bg-gray-900 text-gray-400 border border-gray-700/20';
            default: return 'bg-red-950/50 text-red-400 border border-red-500/20';
        }
    }

    // ---- Provision Modal ----

    function openProvisionModal() {
        const modal = document.getElementById('provision-db-modal');
        if (!modal) return;
        
        // Reset form fields
        const nameEl = App.UI.el('db-prov-name');
        if (nameEl) nameEl.value = '';

        modal.classList.remove('hidden');
        setTimeout(() => {
            modal.classList.add('opacity-100');
            const inner = modal.querySelector('[class*="scale-95"]') || modal.children[0];
            if (inner) inner.classList.remove('scale-95');
        }, 10);

        onProvTypeChange();
        generatePass();
    }

    function closeProvisionModal() {
        const modal = document.getElementById('provision-db-modal');
        if (!modal) return;

        modal.classList.remove('opacity-100');
        const inner = modal.children[0];
        if (inner) inner.classList.add('scale-95');
        setTimeout(() => modal.classList.add('hidden'), 300);
    }

    function onProvTypeChange() {
        const typeEl = App.UI.el('db-prov-type');
        const portEl = App.UI.el('db-prov-port');
        const userEl = App.UI.el('db-prov-user');
        const dbNameEl = App.UI.el('db-prov-dbname');
        const passEl = App.UI.el('db-prov-pass');
        if (!typeEl) return;

        const type = typeEl.value;
        const config = engines[type] || engines.mysql;
        const isSQLite = (type === 'sqlite');
        const isPocketBase = (type === 'pocketbase');
        const hasIgnoredCredentials = isSQLite || isPocketBase;

        // Toggle field availability for SQLite & PocketBase
        if (portEl) {
            portEl.value = isSQLite ? '' : config.defaultPort;
            portEl.disabled = isSQLite;
            portEl.style.opacity = isSQLite ? '0.4' : '1';
            portEl.placeholder = isSQLite ? 'N/A (File-based)' : '';
        }
        if (userEl) {
            userEl.value = hasIgnoredCredentials ? '' : config.defaultUser;
            userEl.disabled = hasIgnoredCredentials;
            userEl.style.opacity = hasIgnoredCredentials ? '0.4' : '1';
            userEl.placeholder = hasIgnoredCredentials ? 'N/A (GUI managed)' : '';
        }
        if (passEl) {
            passEl.disabled = hasIgnoredCredentials;
            passEl.style.opacity = hasIgnoredCredentials ? '0.4' : '1';
            if (hasIgnoredCredentials) passEl.value = 'none';
        }
        
        if (dbNameEl) {
            if (isSQLite) {
                dbNameEl.value = 'sqlite';
            } else if (isPocketBase) {
                dbNameEl.value = 'pocketbase';
            } else {
                dbNameEl.value = (type === 'mongodb') ? 'admin' : ((type === 'redis') ? 'db0' : 'gopulse');
            }
        }
    }

    function generatePass() {
        // Shell-safe charset only — no quotes, backslashes, pipes, or ampersands
        const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-+=.~";
        let pass = "";
        for (let i = 0; i < 20; i++) {
            pass += chars.charAt(Math.floor(Math.random() * chars.length));
        }
        const passEl = App.UI.el('db-prov-pass');
        if (passEl) passEl.value = pass;
    }

    async function submitProvision() {
        const name     = (App.UI.el('db-prov-name')   || {}).value || '';
        const type     = (App.UI.el('db-prov-type')   || {}).value || '';
        const dbName   = (App.UI.el('db-prov-dbname') || {}).value || '';
        const username = (App.UI.el('db-prov-user')   || {}).value || '';
        const password = (App.UI.el('db-prov-pass')   || {}).value || '';
        const port     = parseInt((App.UI.el('db-prov-port') || {}).value, 10) || 0;

        if (!name || !type) {
            App.UI.toast('error', 'Please fill in all required fields.');
            return;
        }

        // SQLite and PocketBase have different credentials requirements
        const isSQLite = (type === 'sqlite');
        const isPocketBase = (type === 'pocketbase');
        const skipCreds = isSQLite || isPocketBase;

        if (skipCreds) {
            if (!name) {
                App.UI.toast('error', 'Please enter a cluster name.');
                return;
            }
            if (isPocketBase && !port) {
                App.UI.toast('error', 'Please specify a host port.');
                return;
            }
        } else {
            if (!dbName || !username || !password || !port) {
                App.UI.toast('error', 'Please fill in all required fields.');
                return;
            }
        }

        const button = document.querySelector('#provision-db-modal button[onclick*="submitProvision"]');
        if (!button) return;
        const originalText = button.innerHTML;
        button.innerHTML = '<i class="fas fa-spinner fa-spin mr-2"></i>Provisioning...';
        button.disabled = true;

        try {
            const res = await App.API.post(`/servers/${App.config.SERVER_ID}/databases`, {
                name, type, database_name: dbName, username, password, host_port: port
            });

            if (res.success) {
                closeProvisionModal();
                App.UI.toast('success', 'Database cluster deployed successfully!');
                load();
            } else {
                App.UI.toast('error', `Provisioning failed: ${res.error || res.message}`);
            }
        } catch (e) {
            // Specific API/network error is already toasted by App.API.request
        } finally {
            button.innerHTML = originalText;
            button.disabled = false;
        }
    }

    // ---- Lifecycle Controls ----

    async function togglePower(dbID, action) {
        try {
            const res = await App.API.post(`/servers/${App.config.SERVER_ID}/databases/${dbID}/${action}`);
            if (res.success) {
                App.UI.toast('success', `Database ${action} executed.`);
                load();
                if (currentDb && currentDb.id === dbID) {
                    showManageView(dbID);
                }
            } else {
                App.UI.toast('error', `Failed: ${res.message}`);
            }
        } catch (e) {
            App.UI.toast('error', 'Lifecycle connection failure.');
        }
    }

    // Health diagnostics pinger
    async function quickCheckHealth(dbID, btnElement) {
        const icon = btnElement.querySelector('i');
        icon.className = "fas fa-spinner fa-spin";
        btnElement.disabled = true;

        try {
            const res = await App.API.get(`/servers/${App.config.SERVER_ID}/databases/${dbID}`);
            if (res.success && res.data.status === 'Running') {
                App.UI.toast('success', 'Database is HEALTHY and accepting connections.');
            } else {
                App.UI.toast('error', `Database status: ${res.data ? res.data.status : 'Unknown'}`);
            }
        } catch (e) {
            App.UI.toast('error', 'Diagnostic connection timed out.');
        } finally {
            icon.className = "fas fa-plug";
            btnElement.disabled = false;
            load();
        }
    }

    // ---- Panel View Switching ----

    function showListView() {
        document.getElementById('db-manage-view').classList.add('hidden');
        document.getElementById('db-list-view').classList.remove('hidden');
        currentDb = null;
        load();
    }

    async function showManageView(dbID) {
        document.getElementById('db-list-view').classList.add('hidden');
        document.getElementById('db-manage-view').classList.remove('hidden');

        try {
            const res = await App.API.get(`/servers/${App.config.SERVER_ID}/databases/${dbID}`);
            if (!res.success || !res.data) {
                App.UI.toast('error', 'Failed to load database details.');
                showListView();
                return;
            }

            currentDb = res.data;
            const config = engines[currentDb.type] || engines.mysql;
            
            // Populate Header
            const logoEl = document.getElementById('db-manage-logo');
            logoEl.className = `p-4 rounded-xl text-3xl ${config.logoClass}`;
            logoEl.innerHTML = `<i class="${config.icon}"></i>`;
            document.getElementById('db-manage-name').innerText = currentDb.name;
            
            const typeBadge = document.getElementById('db-manage-type-badge');
            typeBadge.className = `px-2.5 py-0.5 text-xs font-bold rounded-full uppercase ${config.badgeClass}`;
            typeBadge.innerText = currentDb.type;

            const statusBadge = document.getElementById('db-manage-status-badge');
            const statusColor = getStatusColor(currentDb.status);
            statusBadge.className = `px-2.5 py-0.5 text-xs font-bold rounded-full uppercase flex items-center ${statusColor}`;
            statusBadge.innerHTML = `
                <span class="w-1.5 h-1.5 rounded-full mr-1.5 bg-current ${currentDb.status === 'Running' ? 'animate-pulse' : ''}"></span>
                <span>${currentDb.status}</span>
            `;

            // Lifecycle Actions in Header
            const actionsWrapper = document.getElementById('db-manage-actions');
            actionsWrapper.innerHTML = `
                ${currentDb.status === 'Running'
                    ? `<button onclick="App.Databases.togglePower('${currentDb.id}', 'stop')" class="px-4 py-2 bg-red-950/30 hover:bg-red-950/60 border border-red-500/20 text-red-400 text-sm font-bold rounded-lg transition-colors flex items-center space-x-1.5"><i class="fas fa-stop"></i><span>Stop</span></button>`
                    : `<button onclick="App.Databases.togglePower('${currentDb.id}', 'start')" class="px-4 py-2 bg-green-950/30 hover:bg-green-950/60 border border-green-500/20 text-green-400 text-sm font-bold rounded-lg transition-colors flex items-center space-x-1.5"><i class="fas fa-play"></i><span>Start</span></button>`
                }
                <button onclick="App.Databases.togglePower('${currentDb.id}', 'restart')" class="px-3.5 py-2 bg-gray-700/50 hover:bg-gray-700 border border-gray-600/30 text-gray-300 text-sm font-bold rounded-lg transition-colors" title="Reboot Cluster"><i class="fas fa-sync-alt"></i></button>
            `;

            // Overview Tab - Connection Parameters
            document.getElementById('db-conn-port').innerText = currentDb.host_port;
            document.getElementById('db-conn-dbname').innerText = currentDb.database_name;
            document.getElementById('db-conn-user').innerText = currentDb.username;

            // Connection URI string
            const hostIP = (App.System && App.System.currentServer && App.System.currentServer.host) 
                || document.getElementById('settings-server-host')?.value 
                || window.location.hostname 
                || "127.0.0.1";
            let uri = "";
            if (currentDb.type === 'mysql') {
                uri = `mysql://${currentDb.username}:${currentDb.password}@${hostIP}:${currentDb.host_port}/${currentDb.database_name}`;
            } else if (currentDb.type === 'postgres') {
                uri = `postgresql://${currentDb.username}:${currentDb.password}@${hostIP}:${currentDb.host_port}/${currentDb.database_name}?sslmode=disable`;
            } else if (currentDb.type === 'mongodb') {
                uri = `mongodb://${currentDb.username}:${currentDb.password}@${hostIP}:${currentDb.host_port}/${currentDb.database_name}?authSource=admin`;
            } else if (currentDb.type === 'redis') {
                uri = `redis://:${currentDb.password}@${hostIP}:${currentDb.host_port}`;
            } else if (currentDb.type === 'sqlite') {
                const volumePath = `/var/lib/gopulse/databases/${currentDb.id}/sqlite.db`;
                uri = volumePath;
            } else if (currentDb.type === 'pocketbase') {
                uri = `http://${hostIP}:${currentDb.host_port}`;
            }
            const uriEl = document.getElementById('db-conn-uri');
            if (uriEl) uriEl.value = uri;

            // Render PocketBase admin panel shortcut banner if pocketbase type
            const pbBanner = document.getElementById('db-conn-pocketbase-banner');
            if (pbBanner) {
                if (currentDb.type === 'pocketbase') {
                    pbBanner.innerHTML = `
                        <div class="p-4 bg-orange-500/10 border border-orange-500/30 rounded-xl text-orange-400 text-xs flex flex-col sm:flex-row items-center justify-between gap-3 shadow-lg shadow-orange-500/5 backdrop-blur-md">
                            <div class="flex items-start space-x-3 text-left">
                                <div class="p-1.5 bg-orange-500/20 rounded-lg text-orange-300 mt-0.5">
                                    <i class="fas fa-server text-base"></i>
                                </div>
                                <div>
                                    <h4 class="font-bold text-sm text-white mb-0.5">PocketBase Admin Panel Ready</h4>
                                    <p class="text-orange-300/80 leading-relaxed">Launch the PocketBase Admin control panel in a new tab to create your superuser account and visually manage your schemas, users, and settings.</p>
                                </div>
                            </div>
                            <a href="http://${hostIP}:${currentDb.host_port}/_/" target="_blank" class="px-4 py-2 bg-orange-600 hover:bg-orange-500 text-white rounded-lg transition-all text-xs font-bold shadow-lg shadow-orange-600/30 flex items-center space-x-1.5 flex-shrink-0">
                                <span>Open Admin GUI</span>
                                <i class="fas fa-external-link-alt text-[10px]"></i>
                            </a>
                        </div>
                    `;
                    pbBanner.classList.remove('hidden');
                } else {
                    pbBanner.classList.add('hidden');
                    pbBanner.innerHTML = '';
                }
            }

            // Settings Tab Fields
            document.getElementById('db-settings-port').innerText = currentDb.host_port;
            document.getElementById('db-settings-dbname').innerText = currentDb.database_name;
            document.getElementById('db-settings-user').innerText = currentDb.username;
            document.getElementById('db-settings-pass').innerText = currentDb.password;

            // Console Tab
            document.getElementById('db-console-engine-label').innerText = currentDb.type.toUpperCase();
            const queryArea = document.getElementById('db-console-query');
            if (queryArea) {
                queryArea.value = '';
                if (currentDb.type === 'mongodb') {
                    queryArea.placeholder = "db.collection('users').find().toArray()";
                } else if (currentDb.type === 'redis') {
                    queryArea.placeholder = "KEYS *";
                } else if (currentDb.type === 'sqlite') {
                    queryArea.placeholder = "SELECT * FROM my_table LIMIT 10;";
                } else {
                    queryArea.placeholder = "SELECT * FROM users LIMIT 10;";
                }
            }
            // Hide previous results
            const resultsCard = document.getElementById('db-console-results-card');
            if (resultsCard) resultsCard.classList.add('hidden');

            switchManageTab(activeManageTab);

        } catch (e) {
            App.UI.toast('error', 'Failed to connect to cluster dashboard.');
            showListView();
        }
    }

    function switchManageTab(tab) {
        activeManageTab = tab;
        document.querySelectorAll('.db-tab-btn').forEach(btn => {
            if (btn.dataset.dbTab === tab) {
                btn.classList.remove('border-transparent', 'text-gray-400', 'hover:text-white');
                btn.classList.add('border-blue-500', 'text-blue-400');
            } else {
                btn.classList.remove('border-blue-500', 'text-blue-400');
                btn.classList.add('border-transparent', 'text-gray-400', 'hover:text-white');
            }
        });

        document.querySelectorAll('.db-panel-content').forEach(p => p.classList.add('hidden'));
        const panel = document.getElementById(`db-panel-${tab}`);
        if (panel) panel.classList.remove('hidden');

        if (tab === 'backups') {
            loadBackups();
        }
        if (tab === 'explorer') {
            loadSchemas();
        }
        if (tab === 'console') {
            if (!explorerState.schemas || explorerState.schemas.length === 0) {
                loadSchemas();
            } else {
                updateConsoleSchemaSelect();
            }
        }
        if (tab === 'users') {
            loadUsers();
        }
    }

    function copyConnURI() {
        const input = document.getElementById('db-conn-uri');
        if (!input) return;
        const text = input.value;
        if (navigator.clipboard && navigator.clipboard.writeText) {
            navigator.clipboard.writeText(text).then(() => {
                App.UI.toast('success', 'Connection URI copied to clipboard.');
            });
        } else {
            // Fallback for older browsers
            input.select();
            input.setSelectionRange(0, 99999);
            document.execCommand('copy');
            App.UI.toast('success', 'Connection URI copied to clipboard.');
        }
    }

    // ---- Interactive Console ----

    async function runQuery() {
        if (!currentDb) return;
        const queryEl = document.getElementById('db-console-query');
        if (!queryEl) return;
        const query = queryEl.value;
        if (!query.trim()) {
            App.UI.toast('error', 'Please enter a query to execute.');
            return;
        }

        const selectEl = document.getElementById('db-console-schema-select');
        const schema = selectEl ? selectEl.value : '';

        const resultsWrapper = document.getElementById('db-console-results-card');
        const resultsContainer = document.getElementById('db-console-results-container');
        
        resultsWrapper.classList.remove('hidden');
        resultsContainer.innerHTML = '<div class="p-4 text-gray-400"><i class="fas fa-spinner fa-spin mr-2"></i>Executing query...</div>';

        try {
            const res = await App.API.post(`/servers/${App.config.SERVER_ID}/databases/${currentDb.id}/query`, {
                query,
                schema
            });

            if (res.success) {
                const payload = res.data;
                if (!payload) {
                    resultsContainer.innerHTML = '<div class="p-4 text-green-400 font-mono">Query completed successfully. Empty result set.</div>';
                    return;
                }

                if (payload.type === 'table') {
                    let html = `<table class="w-full text-left font-mono text-xs border-collapse">`;
                    html += `<thead><tr class="bg-gray-800 border-b border-gray-700 text-gray-300">`;
                    payload.columns.forEach(col => {
                        html += `<th class="px-4 py-2 border-r border-gray-700/50">${escapeHTML(col)}</th>`;
                    });
                    html += `</tr></thead><tbody class="divide-y divide-gray-800">`;
                    
                    if (payload.rows && payload.rows.length > 0) {
                        payload.rows.forEach(row => {
                            html += `<tr class="hover:bg-gray-900/50 text-gray-400">`;
                            row.forEach(val => {
                                html += `<td class="px-4 py-2 border-r border-gray-700/50 font-mono whitespace-nowrap">${escapeHTML(val)}</td>`;
                            });
                            html += `</tr>`;
                        });
                    } else {
                        html += `<tr><td colspan="${payload.columns.length}" class="p-4 text-gray-500 text-center">Empty result set.</td></tr>`;
                    }
                    
                    html += `</tbody></table>`;
                    resultsContainer.innerHTML = html;

                } else if (payload.type === 'raw') {
                    resultsContainer.innerHTML = `<pre class="p-4 text-green-400 font-mono text-sm whitespace-pre-wrap">${escapeHTML(payload.data)}</pre>`;
                } else {
                    resultsContainer.innerHTML = `<pre class="p-4 text-green-400 font-mono text-sm whitespace-pre-wrap">${escapeHTML(JSON.stringify(payload, null, 2))}</pre>`;
                }
            } else {
                resultsContainer.innerHTML = `<div class="p-4 text-red-400 border border-red-500/20 bg-red-950/10 font-mono text-sm"><i class="fas fa-exclamation-triangle mr-2"></i>Execution Failed:<br><pre class="mt-2 whitespace-pre-wrap">${escapeHTML(res.error || res.message || 'Unknown error')}</pre></div>`;
            }
        } catch (e) {
            resultsContainer.innerHTML = '<div class="p-4 text-red-400 font-mono">Query execution failed. Check your connection.</div>';
        }
    }

    function escapeHTML(str) {
        if (str == null) return '';
        return String(str).replace(/[&<>'"]/g, 
            tag => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', "'": '&#39;', '"': '&quot;' }[tag] || tag)
        );
    }

    // ---- Backups ----

    async function loadAutoBackupConfig() {
        if (!currentDb) return;
        const toggle = document.getElementById('db-auto-backup-toggle');
        const retention = document.getElementById('db-auto-backup-retention');
        const statusText = document.getElementById('db-auto-backup-status-text');
        const settingsGrid = document.getElementById('db-auto-backup-settings');
        
        if (!toggle || !retention) return;

        try {
            const res = await App.API.get(`/servers/${App.config.SERVER_ID}/databases/${currentDb.id}/auto-backup`);
            if (res.success && res.data) {
                toggle.checked = res.data.auto_backup;
                retention.value = res.data.retention_days;
                
                if (res.data.auto_backup) {
                    statusText.textContent = 'Enabled';
                    statusText.className = 'ml-3 text-sm font-semibold text-indigo-400';
                    settingsGrid.classList.remove('hidden');
                } else {
                    statusText.textContent = 'Disabled';
                    statusText.className = 'ml-3 text-sm font-medium text-gray-400';
                    settingsGrid.classList.add('hidden');
                }
            }
        } catch (e) {
            console.error('Failed to load auto backup configuration', e);
        }
    }

    async function saveAutoBackupConfig() {
        if (!currentDb) return;
        const toggle = document.getElementById('db-auto-backup-toggle');
        const retention = document.getElementById('db-auto-backup-retention');
        if (!toggle || !retention) return;

        const enabled = toggle.checked;
        const retentionDays = parseInt(retention.value, 10);

        try {
            const res = await App.API.put(`/servers/${App.config.SERVER_ID}/databases/${currentDb.id}/auto-backup`, {
                auto_backup: enabled,
                retention_days: retentionDays
            });

            if (res.success) {
                App.UI.toast('success', 'Backup configuration updated.');
                loadAutoBackupConfig();
            } else {
                App.UI.toast('error', `Failed to save configuration: ${res.message}`);
            }
        } catch (e) {
            App.UI.toast('error', 'Connection error while saving configuration.');
        }
    }

    async function loadBackups() {
        if (!currentDb) return;
        
        // Load automatic daily backup settings
        loadAutoBackupConfig();
        const tbody = document.getElementById('db-backups-table-body');
        const empty = document.getElementById('db-backups-empty');
        if (!tbody) return;
        tbody.innerHTML = '';
        empty.classList.add('hidden');

        try {
            const res = await App.API.get(`/servers/${App.config.SERVER_ID}/databases/${currentDb.id}/backups`);
            if (!res.success || !res.data || res.data.length === 0) {
                empty.classList.remove('hidden');
                return;
            }

            res.data.forEach(backup => {
                const tr = document.createElement('tr');
                tr.className = "hover:bg-gray-800/20 border-b border-gray-700/50 text-sm text-gray-300";
                
                const sizeMB = (backup.size / (1024 * 1024)).toFixed(2);
                const createdTime = new Date(backup.created_at).toLocaleString();

                tr.innerHTML = `
                    <td class="px-6 py-4 font-mono text-xs text-white flex items-center space-x-2">
                        <i class="far fa-file-archive text-green-400"></i>
                        <span>${escapeHTML(backup.filename)}</span>
                    </td>
                    <td class="px-6 py-4 font-semibold">${sizeMB} MB</td>
                    <td class="px-6 py-4 text-gray-400">${createdTime}</td>
                    <td class="px-6 py-4 text-right">
                        <div class="flex justify-end space-x-2">
                            <button onclick="App.Databases.restoreBackup('${escapeHTML(backup.filename)}', this)" class="px-3 py-1.5 bg-indigo-950/40 hover:bg-indigo-950/60 border border-indigo-500/20 text-indigo-400 text-xs font-bold rounded-lg transition-colors flex items-center space-x-1">
                                <i class="fas fa-history"></i>
                                <span>Restore</span>
                            </button>
                            <a href="/api/servers/${App.config.SERVER_ID}/databases/${currentDb.id}/backups/download?filename=${encodeURIComponent(backup.filename)}" download class="px-3 py-1.5 bg-gray-700/50 hover:bg-gray-700 border border-gray-600/30 text-gray-300 text-xs font-bold rounded-lg transition-colors flex items-center space-x-1">
                                <i class="fas fa-download"></i>
                                <span>Download</span>
                            </a>
                        </div>
                    </td>
                `;
                tbody.appendChild(tr);
            });
        } catch (e) {
            empty.classList.remove('hidden');
        }
    }

    async function createBackup() {
        if (!currentDb) return;
        const btn = document.querySelector('#db-panel-backups button[onclick*="createBackup"]');
        if (!btn) return;
        const orig = btn.innerHTML;
        btn.innerHTML = '<i class="fas fa-spinner fa-spin mr-2"></i>Generating dump...';
        btn.disabled = true;

        try {
            const res = await App.API.post(`/servers/${App.config.SERVER_ID}/databases/${currentDb.id}/backup`);
            if (res.success) {
                App.UI.toast('success', 'Backup dump generated and archived.');
                loadBackups();
            } else {
                App.UI.toast('error', `Backup failed: ${res.message}`);
            }
        } catch (e) {
            App.UI.toast('error', 'Backup operation timed out.');
        } finally {
            btn.innerHTML = orig;
            btn.disabled = false;
        }
    }

    async function restoreBackup(filename, btnElement) {
        if (!confirm(`Restore "${filename}"?\n\nThis will OVERWRITE current data in the live container.`)) {
            return;
        }

        const orig = btnElement.innerHTML;
        btnElement.innerHTML = '<i class="fas fa-spinner fa-spin"></i>';
        btnElement.disabled = true;

        try {
            const res = await App.API.post(`/servers/${App.config.SERVER_ID}/databases/${currentDb.id}/backups/restore`, {
                filename
            });

            if (res.success) {
                App.UI.toast('success', 'Database restored successfully from snapshot.');
            } else {
                App.UI.toast('error', `Restore failed: ${res.message}`);
            }
        } catch (e) {
            App.UI.toast('error', 'Restore connection failed.');
        } finally {
            btnElement.innerHTML = orig;
            btnElement.disabled = false;
        }
    }

    async function importBackup(event) {
        if (!currentDb) return;
        const file = event.target.files[0];
        if (!file) return;

        // Reset the file input so the same file can be uploaded again if needed
        const inputElement = event.target;
        
        const btn = document.querySelector('#db-panel-backups button[onclick*="db-import-backup-file"]');
        let orig = '';
        if (btn) {
            orig = btn.innerHTML;
            btn.innerHTML = '<i class="fas fa-spinner fa-spin mr-2"></i>Uploading (0%)...';
            btn.disabled = true;
        }

        const formData = new FormData();
        formData.append('file', file);

        try {
            App.UI.toast('info', 'Uploading database backup...');
            const res = await App.API.upload(
                `/servers/${App.config.SERVER_ID}/databases/${currentDb.id}/backups/import`,
                formData,
                (percent) => {
                    if (btn) {
                        if (percent < 100) {
                            btn.innerHTML = `<i class="fas fa-spinner fa-spin mr-2"></i>Uploading (${Math.round(percent)}%)...`;
                        } else {
                            btn.innerHTML = `<i class="fas fa-spinner fa-spin mr-2"></i>Importing into database...`;
                        }
                    }
                }
            );
            if (res.success) {
                App.UI.toast('success', 'Database backup successfully imported and restored.');
                loadBackups();
            } else {
                App.UI.toast('error', `Import failed: ${res.message}`);
            }
        } catch (e) {
            App.UI.toast('error', 'Import failed due to server error or network issue.');
        } finally {
            inputElement.value = ''; // Reset input
            if (btn) {
                btn.innerHTML = orig;
                btn.disabled = false;
            }
        }
    }

    // ---- Delete ----

    async function deleteDatabase() {
        if (!currentDb) return;
        const deleteVolume = document.getElementById('db-delete-volume-check').checked;
        
        let confirmMsg = `Permanently delete "${currentDb.name}"?`;
        if (deleteVolume) {
            confirmMsg += `\n\n⚠️ This will also PURGE all data volumes on the remote host!`;
        }

        if (!confirm(confirmMsg)) return;

        try {
            const res = await App.API.delete(`/servers/${App.config.SERVER_ID}/databases/${currentDb.id}?delete_volume=${deleteVolume}`);
            if (res.success) {
                App.UI.toast('success', 'Database cluster deleted.');
                showListView();
            } else {
                App.UI.toast('error', `Delete failed: ${res.message}`);
            }
        } catch (e) {
            App.UI.toast('error', 'Failed to execute database deletion.');
        }
    }

    // =========================================================================
    // Explorer — Schema / Table / Row Browser
    // =========================================================================

    let explorerState = {
        schemas: [],
        expandedSchemas: {},  // schema -> [tables]
        activeSchema: null,
        activeTable: null,
        page: 1,
        limit: 50,
        total: 0
    };

    async function loadSchemas() {
        if (!currentDb) return;
        const tree = document.getElementById('db-explorer-tree');
        if (!tree) return;

        const createSchemaBtn = document.querySelector('button[onclick="App.Databases.openCreateSchemaModal()"]');
        if (createSchemaBtn) {
            if (currentDb.type === 'redis') {
                createSchemaBtn.classList.add('hidden');
            } else {
                createSchemaBtn.classList.remove('hidden');
            }
        }

        tree.innerHTML = '<div class="text-center text-gray-500 py-8"><i class="fas fa-spinner fa-spin text-lg mb-2"></i><p class="text-xs">Loading schemas...</p></div>';

        try {
            const res = await App.API.get(`/servers/${App.config.SERVER_ID}/databases/${currentDb.id}/schemas`);
            if (!res.success || !res.data || res.data.length === 0) {
                tree.innerHTML = '<div class="text-center text-gray-500 py-8"><i class="fas fa-folder-open text-lg mb-2 opacity-40"></i><p class="text-xs">No schemas found.</p></div>';
                return;
            }

            explorerState.schemas = res.data;
            renderTree();
            updateConsoleSchemaSelect();
        } catch (e) {
            tree.innerHTML = '<div class="text-center text-red-400 py-8"><p class="text-xs">Failed to load schemas.</p></div>';
        }
    }

    function updateConsoleSchemaSelect() {
        const select = document.getElementById('db-console-schema-select');
        if (!select) return;

        const currentVal = select.value;
        select.innerHTML = '';

        if (!explorerState.schemas || explorerState.schemas.length === 0) {
            select.innerHTML = '<option value="">No databases</option>';
            return;
        }

        explorerState.schemas.forEach(schema => {
            const opt = document.createElement('option');
            opt.value = schema;
            opt.textContent = schema;
            select.appendChild(opt);
        });

        if (currentVal && explorerState.schemas.includes(currentVal)) {
            select.value = currentVal;
        } else if (currentDb && currentDb.database_name && explorerState.schemas.includes(currentDb.database_name)) {
            select.value = currentDb.database_name;
        }
    }

    function renderTree() {
        const tree = document.getElementById('db-explorer-tree');
        if (!tree) return;

        const isMongoType = currentDb && currentDb.type === 'mongodb';
        const isRedisType = currentDb && currentDb.type === 'redis';
        const isNoSQL = isMongoType || isRedisType;
        const schemaLabel = isNoSQL ? 'Database' : 'Schema';
        const tableLabel = isRedisType ? 'Keyspace' : (isMongoType ? 'Collection' : 'Table');
        const schemaIcon = isNoSQL ? 'fas fa-cubes' : 'fas fa-database';
        const tableIcon = isRedisType ? 'fas fa-key' : (isMongoType ? 'fas fa-layer-group' : 'fas fa-table');

        let html = '';
        explorerState.schemas.forEach(schema => {
            const isExpanded = !!explorerState.expandedSchemas[schema];
            const tables = explorerState.expandedSchemas[schema] || [];
            const chevron = isExpanded ? 'fa-chevron-down' : 'fa-chevron-right';

            let actionHtml = '';
            if (currentDb.type !== 'redis') {
                actionHtml = `
                    <div class="hidden group-hover:flex items-center space-x-1 ml-2 flex-shrink-0">
                        <button onclick="event.stopPropagation(); App.Databases.openCreateTableModal('${escapeHTML(schema)}')" class="p-1 text-green-400 hover:text-white rounded hover:bg-gray-700 transition-colors" title="Create ${tableLabel}">
                            <i class="fas fa-plus text-[10px]"></i>
                        </button>
                        <button onclick="event.stopPropagation(); App.Databases.deleteSchema('${escapeHTML(schema)}')" class="p-1 text-red-400 hover:text-white rounded hover:bg-gray-700 transition-colors" title="Drop ${schemaLabel}">
                            <i class="fas fa-trash text-[10px]"></i>
                        </button>
                    </div>
                `;
            }

            html += `
                <div class="select-none">
                    <div class="group flex items-center justify-between px-2.5 py-2 rounded-lg cursor-pointer transition-colors ${isExpanded ? 'bg-gray-800/80 text-white' : 'text-gray-400 hover:bg-gray-800/40 hover:text-gray-200'}">
                        <div onclick="App.Databases.toggleSchema('${escapeHTML(schema)}')" class="flex items-center min-w-0 flex-1">
                            <i class="fas ${chevron} text-[8px] mr-2.5 transition-transform text-gray-500"></i>
                            <i class="${schemaIcon} text-xs mr-2 ${isExpanded ? 'text-blue-400' : 'text-gray-500'}"></i>
                            <span class="text-xs font-semibold truncate">${escapeHTML(schema)}</span>
                        </div>
                        ${actionHtml}
                    </div>`;

            if (isExpanded) {
                if (tables.length === 0) {
                    html += '<div class="pl-8 py-2 text-xs text-gray-600 italic">No items found</div>';
                } else {
                    tables.forEach(tbl => {
                        const isActive = explorerState.activeSchema === schema && explorerState.activeTable === tbl;
                        html += `
                            <div class="group flex items-center justify-between pl-8 pr-2.5 py-1.5 rounded-lg cursor-pointer transition-colors ${isActive ? 'bg-blue-600/20 text-blue-300' : 'text-gray-400 hover:bg-gray-800/40 hover:text-gray-200'}">
                                <div onclick="App.Databases.selectTable('${escapeHTML(schema)}', '${escapeHTML(tbl)}')" class="flex items-center min-w-0 flex-1">
                                    <i class="${tableIcon} text-[10px] mr-2 ${isActive ? 'text-blue-400' : 'text-gray-600'}"></i>
                                    <span class="text-xs truncate">${escapeHTML(tbl)}</span>
                                </div>
                                <div class="hidden group-hover:flex items-center ml-2 flex-shrink-0">
                                    <button onclick="event.stopPropagation(); App.Databases.deleteTable('${escapeHTML(schema)}', '${escapeHTML(tbl)}')" class="p-1 text-red-400 hover:text-white rounded hover:bg-gray-700 transition-colors" title="Drop ${tableLabel}">
                                        <i class="fas fa-trash text-[10px]"></i>
                                    </button>
                                </div>
                            </div>`;
                    });
                }
            }

            html += '</div>';
        });

        tree.innerHTML = html;
    }

    async function toggleSchema(schema) {
        if (explorerState.expandedSchemas[schema]) {
            // Collapse
            delete explorerState.expandedSchemas[schema];
            renderTree();
            return;
        }

        // Expand — fetch tables
        explorerState.expandedSchemas[schema] = []; // Set loading state
        renderTree();

        try {
            const res = await App.API.get(`/servers/${App.config.SERVER_ID}/databases/${currentDb.id}/schemas/${encodeURIComponent(schema)}/tables`);
            if (res.success && res.data) {
                explorerState.expandedSchemas[schema] = res.data;
            }
        } catch (e) {
            // Keep empty
        }

        renderTree();
    }

    async function selectTable(schema, table) {
        explorerState.activeSchema = schema;
        explorerState.activeTable = table;
        explorerState.page = 1;
        renderTree();
        await loadTableData(schema, table, 1);
    }

    async function loadTableData(schema, table, page) {
        if (!currentDb) return;

        const dataContainer = document.getElementById('db-explorer-data');
        const titleEl = document.getElementById('db-explorer-title');
        const metaEl = document.getElementById('db-explorer-meta');
        const pagination = document.getElementById('db-explorer-pagination');

        const isMongoType = currentDb.type === 'mongodb';
        const isRedisType = currentDb.type === 'redis';
        const label = isRedisType ? 'Keyspace' : (isMongoType ? 'Collection' : 'Table');
        const recordLabel = isRedisType ? 'keys' : (isMongoType ? 'documents' : 'rows');

        titleEl.textContent = `${schema} → ${table}`;
        metaEl.innerHTML = '<i class="fas fa-spinner fa-spin"></i>';
        dataContainer.innerHTML = '<div class="flex items-center justify-center h-full text-gray-500"><i class="fas fa-spinner fa-spin text-2xl"></i></div>';
        pagination.classList.add('hidden');

        try {
            const res = await App.API.get(`/servers/${App.config.SERVER_ID}/databases/${currentDb.id}/schemas/${encodeURIComponent(schema)}/tables/${encodeURIComponent(table)}/rows?page=${page}&limit=${explorerState.limit}`);

            if (!res.success || !res.data) {
                dataContainer.innerHTML = '<div class="flex items-center justify-center h-full text-red-400"><p class="text-sm">Failed to load data.</p></div>';
                metaEl.innerHTML = '';
                return;
            }

            const data = res.data;
            explorerState.total = data.total || 0;
            explorerState.page = data.page || 1;

            // Meta info
            metaEl.innerHTML = `
                <span>${label}: <strong class="text-gray-300">${escapeHTML(table)}</strong></span>
                <span class="text-gray-600">|</span>
                <span>${data.total.toLocaleString()} ${recordLabel}</span>
            `;

            // Show add row button when table data is active
            const addRowBtn = document.getElementById('db-explorer-add-row-btn');
            if (addRowBtn) addRowBtn.classList.remove('hidden');

            // Render data grid
            if (!data.columns || data.columns.length === 0) {
                dataContainer.innerHTML = '<div class="flex items-center justify-center h-full text-gray-500"><div class="text-center"><i class="fas fa-inbox text-3xl mb-3 opacity-30"></i><p class="text-sm">Table is empty.</p></div></div>';
                return;
            }

            // Find Primary Key column for deletes
            let pkIdx = data.columns.indexOf('id');
            if (pkIdx === -1) pkIdx = data.columns.indexOf('_id');
            if (pkIdx === -1) pkIdx = data.columns.indexOf('key');
            if (pkIdx === -1) pkIdx = data.columns.findIndex(c => c.toLowerCase() === 'id');
            if (pkIdx === -1) pkIdx = 0; // fallback to first column
            const pkCol = data.columns[pkIdx];

            let html = '<table class="w-full text-left font-mono text-xs border-collapse">';
            // Header
            html += '<thead class="sticky top-0 z-10"><tr class="bg-gray-800 border-b border-gray-700">';
            data.columns.forEach((col, idx) => {
                html += `<th class="px-4 py-2.5 text-gray-400 font-bold uppercase text-[10px] tracking-wider border-r border-gray-700/40 whitespace-nowrap">${escapeHTML(col)}</th>`;
            });
            html += '<th class="px-4 py-2.5 text-center text-gray-400 font-bold uppercase text-[10px] tracking-wider border-r border-gray-700/40 w-12">Actions</th>';
            html += '</tr></thead>';

            // Body
            html += '<tbody class="divide-y divide-gray-800/60">';
            if (data.rows && data.rows.length > 0) {
                data.rows.forEach((row, rowIdx) => {
                    html += `<tr class="hover:bg-blue-900/10 transition-colors">`;
                    row.forEach(val => {
                        const display = (val === null || val === 'NULL' || val === '') ? '<span class="text-gray-600 italic">NULL</span>' : escapeHTML(val);
                        html += `<td class="px-4 py-2 text-gray-300 border-r border-gray-800/40 whitespace-nowrap max-w-xs truncate" title="${escapeHTML(val)}">${display}</td>`;
                    });
                    html += `
                        <td class="px-4 py-2 text-center border-r border-gray-800/40 w-12">
                            <button onclick="App.Databases.deleteRow('${escapeHTML(schema)}', '${escapeHTML(table)}', '${escapeHTML(pkCol)}', '${escapeHTML(row[pkIdx])}')" class="p-1 text-red-400 hover:text-red-300 rounded hover:bg-gray-800 transition-colors" title="Delete Record">
                                <i class="fas fa-trash text-[10px]"></i>
                            </button>
                        </td>
                    `;
                    html += '</tr>';
                });
            } else {
                html += `<tr><td colspan="${data.columns.length + 1}" class="p-6 text-gray-500 text-center">No rows found.</td></tr>`;
            }
            html += '</tbody></table>';

            dataContainer.innerHTML = html;

            // Update pagination
            const totalPages = Math.max(1, Math.ceil(data.total / explorerState.limit));
            pagination.classList.remove('hidden');

            document.getElementById('db-explorer-row-info').textContent = 
                `Showing ${((page - 1) * explorerState.limit) + 1}–${Math.min(page * explorerState.limit, data.total)} of ${data.total.toLocaleString()} rows`;
            document.getElementById('db-explorer-page-info').textContent = `${page} / ${totalPages}`;

            const prevBtn = document.getElementById('db-explorer-prev-btn');
            const nextBtn = document.getElementById('db-explorer-next-btn');
            prevBtn.disabled = page <= 1;
            nextBtn.disabled = page >= totalPages;

        } catch (e) {
            dataContainer.innerHTML = '<div class="flex items-center justify-center h-full text-red-400"><p class="text-sm">Connection error.</p></div>';
            metaEl.innerHTML = '';
        }
    }

    function explorerPrevPage() {
        if (explorerState.page <= 1) return;
        explorerState.page--;
        loadTableData(explorerState.activeSchema, explorerState.activeTable, explorerState.page);
    }

    function explorerNextPage() {
        const totalPages = Math.max(1, Math.ceil(explorerState.total / explorerState.limit));
        if (explorerState.page >= totalPages) return;
        explorerState.page++;
        loadTableData(explorerState.activeSchema, explorerState.activeTable, explorerState.page);
    }

    function refreshExplorer() {
        explorerState.expandedSchemas = {};
        explorerState.activeSchema = null;
        explorerState.activeTable = null;
        explorerState.page = 1;

        // Hide add row button
        const addRowBtn = document.getElementById('db-explorer-add-row-btn');
        if (addRowBtn) addRowBtn.classList.add('hidden');

        // Reset data panel
        const dataContainer = document.getElementById('db-explorer-data');
        if (dataContainer) {
            dataContainer.innerHTML = '<div class="flex items-center justify-center h-full text-gray-500"><div class="text-center"><i class="fas fa-mouse-pointer text-3xl mb-3 opacity-30"></i><p class="text-sm">Click on a table or collection from the sidebar.</p></div></div>';
        }
        document.getElementById('db-explorer-title').textContent = 'Select a table to view data';
        document.getElementById('db-explorer-meta').innerHTML = '';
        document.getElementById('db-explorer-pagination').classList.add('hidden');

        loadSchemas();
    }

    // ---- Modal Animations ----
    function openModal(modalId) {
        const modal = document.getElementById(modalId);
        if (!modal) return;
        modal.classList.remove('hidden');
        setTimeout(() => {
            modal.classList.remove('opacity-0');
            modal.querySelector('.transform')?.classList.remove('scale-95');
            modal.querySelector('.transform')?.classList.add('scale-100');
        }, 10);
    }

    function closeModal(modalId) {
        const modal = document.getElementById(modalId);
        if (!modal) return;
        modal.classList.add('opacity-0');
        modal.querySelector('.transform')?.classList.remove('scale-100');
        modal.querySelector('.transform')?.classList.add('scale-95');
        setTimeout(() => {
            modal.classList.add('hidden');
        }, 300);
    }

    // ---- Create/Delete Schema (Database) ----
    let activeTargetSchema = null; // Used as context when creating table

    function openCreateSchemaModal() {
        if (!currentDb) return;
        const isMongo = currentDb.type === 'mongodb';
        document.getElementById('db-schema-modal-title').textContent = isMongo ? 'Create MongoDB Database' : 'Create Schema/Database';
        document.getElementById('db-schema-modal-label').textContent = isMongo ? 'Database Name' : 'Database/Schema Name';
        document.getElementById('db-create-schema-name').value = '';
        openModal('db-create-schema-modal');
    }

    function closeSchemaModal() {
        closeModal('db-create-schema-modal');
    }

    async function submitCreateSchema() {
        if (!currentDb) return;
        const nameInput = document.getElementById('db-create-schema-name');
        const name = nameInput.value.trim();
        if (!name) {
            App.UI.toast('error', 'Name is required.');
            return;
        }

        try {
            const res = await App.API.post(`/servers/${App.config.SERVER_ID}/databases/${currentDb.id}/schemas`, { name });
            if (res.success) {
                App.UI.toast('success', 'Database/Schema created successfully.');
                closeSchemaModal();
                loadSchemas();
            } else {
                App.UI.toast('error', `Creation failed: ${res.message}`);
            }
        } catch (e) {
            App.UI.toast('error', 'Failed to execute creation.');
        }
    }

    async function deleteSchema(schemaName) {
        if (!currentDb) return;
        const isMongo = currentDb.type === 'mongodb';
        const label = isMongo ? 'Database' : 'Schema';
        if (!confirm(`Are you absolutely sure you want to drop the ${label} "${schemaName}"?\n\n⚠️ This will delete ALL tables, collections, and data within it!`)) {
            return;
        }

        try {
            const res = await App.API.delete(`/servers/${App.config.SERVER_ID}/databases/${currentDb.id}/schemas/${encodeURIComponent(schemaName)}`);
            if (res.success) {
                App.UI.toast('success', `${label} successfully dropped.`);
                loadSchemas();
            } else {
                App.UI.toast('error', `Drop failed: ${res.message}`);
            }
        } catch (e) {
            App.UI.toast('error', 'Failed to drop database.');
        }
    }

    // ---- Create/Delete Table (Collection) ----
    function openCreateTableModal(schema) {
        if (!currentDb) return;
        activeTargetSchema = schema;
        const isMongo = currentDb.type === 'mongodb';
        document.getElementById('db-table-modal-title').textContent = isMongo ? 'Create Collection' : 'Create Table';
        document.getElementById('db-table-modal-label').textContent = isMongo ? 'Collection Name' : 'Table Name';
        document.getElementById('db-create-table-name').value = '';
        
        const colsSection = document.getElementById('db-table-columns-section');
        const colsContainer = document.getElementById('db-table-columns-container');
        
        if (colsSection && colsContainer) {
            colsContainer.innerHTML = '';
            if (isMongo) {
                colsSection.classList.add('hidden');
            } else {
                colsSection.classList.remove('hidden');
                // Add a default column as starter
                addSchemaBuilderColumn();
            }
        }
        
        openModal('db-create-table-modal');
    }

    function closeCreateTableModal() {
        closeModal('db-create-table-modal');
    }

    function addSchemaBuilderColumn() {
        const container = document.getElementById('db-table-columns-container');
        if (!container) return;

        const optionsHtml = `
            <option value="VARCHAR(255)">VARCHAR(255)</option>
            <option value="VARCHAR(100)">VARCHAR(100)</option>
            <option value="TEXT">TEXT</option>
            <option value="INT">INT</option>
            <option value="BIGINT">BIGINT</option>
            <option value="DECIMAL(10,2)">DECIMAL(10,2)</option>
            <option value="BOOLEAN">BOOLEAN</option>
            <option value="TIMESTAMP">TIMESTAMP</option>
            <option value="DATE">DATE</option>
        `;

        const row = document.createElement('div');
        row.className = "db-schema-col-row flex items-center space-x-2 bg-gray-900/40 p-2 border border-gray-800 rounded-lg";
        row.innerHTML = `
            <input type="text" placeholder="column_name" class="db-schema-col-name flex-1 bg-gray-800 text-white border border-gray-700 rounded px-2.5 py-1.5 text-xs font-mono focus:outline-none focus:border-indigo-500">
            
            <select class="db-schema-col-type bg-gray-800 text-white border border-gray-700 rounded px-2 py-1.5 text-xs focus:outline-none focus:border-indigo-500 font-mono">
                ${optionsHtml}
            </select>

            <label class="flex items-center space-x-1 select-none cursor-pointer flex-shrink-0">
                <input type="checkbox" class="db-schema-col-null rounded bg-gray-800 border-gray-700 text-indigo-600 focus:ring-0 focus:ring-offset-0 w-3.5 h-3.5" checked>
                <span class="text-[10px] text-gray-400">Null</span>
            </label>

            <label class="flex items-center space-x-1 select-none cursor-pointer flex-shrink-0">
                <input type="checkbox" class="db-schema-col-unique rounded bg-gray-800 border-gray-700 text-indigo-600 focus:ring-0 focus:ring-offset-0 w-3.5 h-3.5">
                <span class="text-[10px] text-gray-400">Unique</span>
            </label>

            <button type="button" onclick="this.closest('.db-schema-col-row').remove()" class="p-1 text-red-400 hover:text-red-300 rounded hover:bg-gray-800 transition-colors flex-shrink-0">
                <i class="fas fa-times text-xs"></i>
            </button>
        `;
        container.appendChild(row);
        container.scrollTop = container.scrollHeight;
    }

    async function submitCreateTable() {
        if (!currentDb || !activeTargetSchema) return;
        const nameInput = document.getElementById('db-create-table-name');
        const name = nameInput.value.trim();
        if (!name) {
            App.UI.toast('error', 'Name is required.');
            return;
        }

        const isMongo = currentDb.type === 'mongodb';
        const label = isMongo ? 'Collection' : 'Table';
        let columns = [];

        if (!isMongo) {
            const rows = document.querySelectorAll('.db-schema-col-row');
            let hasError = false;
            rows.forEach(row => {
                const colName = row.querySelector('.db-schema-col-name').value.trim();
                const colType = row.querySelector('.db-schema-col-type').value;
                const isNullable = row.querySelector('.db-schema-col-null').checked;
                const isUnique = row.querySelector('.db-schema-col-unique').checked;

                if (!colName) {
                    hasError = true;
                    return;
                }

                columns.push({
                    name: colName,
                    type: colType,
                    nullable: isNullable,
                    unique: isUnique
                });
            });

            if (hasError) {
                App.UI.toast('error', 'All columns must have a valid name.');
                return;
            }

            if (columns.length === 0) {
                App.UI.toast('error', 'At least one column definition is required.');
                return;
            }
        }

        try {
            const res = await App.API.post(`/servers/${App.config.SERVER_ID}/databases/${currentDb.id}/schemas/${encodeURIComponent(activeTargetSchema)}/tables`, { 
                name,
                columns
            });
            if (res.success) {
                App.UI.toast('success', `${label} created successfully.`);
                closeCreateTableModal();
                explorerState.expandedSchemas[activeTargetSchema] = [];
                await toggleSchema(activeTargetSchema);
            } else {
                App.UI.toast('error', `Creation failed: ${res.message}`);
            }
        } catch (e) {
            App.UI.toast('error', 'Failed to execute creation.');
        }
    }

    async function deleteTable(schema, table) {
        if (!currentDb) return;
        const isMongo = currentDb.type === 'mongodb';
        const label = isMongo ? 'Collection' : 'Table';
        if (!confirm(`Are you absolutely sure you want to drop the ${label} "${table}"?`)) {
            return;
        }

        try {
            const res = await App.API.delete(`/servers/${App.config.SERVER_ID}/databases/${currentDb.id}/schemas/${encodeURIComponent(schema)}/tables/${encodeURIComponent(table)}`);
            if (res.success) {
                App.UI.toast('success', `${label} dropped.`);
                
                // Clear selection if we dropped the active table
                if (explorerState.activeSchema === schema && explorerState.activeTable === table) {
                    refreshExplorer();
                } else {
                    explorerState.expandedSchemas[schema] = [];
                    await toggleSchema(schema);
                }
            } else {
                App.UI.toast('error', `Drop failed: ${res.message}`);
            }
        } catch (e) {
            App.UI.toast('error', 'Failed to drop table.');
        }
    }

    // ---- Row CRUD Operations ----
    let activeTableColumns = []; // Store columns info for the active table insert

    async function openInsertRowModal() {
        const schema = explorerState.activeSchema;
        const table = explorerState.activeTable;
        if (!currentDb || !schema || !table) return;

        const isNoSQL = currentDb.type === 'mongodb' || currentDb.type === 'redis';
        
        const container = document.getElementById('db-row-fields-container');
        container.innerHTML = '<div class="text-center py-6 text-gray-500"><i class="fas fa-spinner fa-spin mr-2"></i>Loading schema details...</div>';
        
        document.getElementById('db-row-modal-title').textContent = (currentDb.type === 'redis') ? 'Insert Key-Value' : (isNoSQL ? 'Insert Document' : 'Insert Row');
        openModal('db-insert-row-modal');
 
        if (isNoSQL) {
            const placeholder = (currentDb.type === 'redis')
                ? '{\n  "key": "user:100",\n  "type": "string",\n  "value": "Abdallah Awd",\n  "ttl": 3600\n}'
                : '{\n  "id": "usr_100",\n  "name": "Abdallah"\n}';
            container.innerHTML = `
                <div class="space-y-2">
                    <label class="block text-gray-400 text-sm font-semibold">Document JSON</label>
                    <textarea id="db-insert-row-json" rows="8" class="w-full bg-gray-950/80 text-green-400 font-mono p-4 rounded-lg border border-gray-700 focus:outline-none focus:border-indigo-500 text-sm" placeholder='${placeholder}'></textarea>
                </div>
            `;
            return;
        }

        try {
            // Load table columns
            const res = await App.API.get(`/servers/${App.config.SERVER_ID}/databases/${currentDb.id}/schemas/${encodeURIComponent(schema)}/tables/${encodeURIComponent(table)}/columns`);
            if (res.success && res.data) {
                activeTableColumns = res.data;
                let html = '<div class="grid grid-cols-1 gap-4">';
                activeTableColumns.forEach(col => {
                    const isSerial = col.type.toLowerCase().includes('serial') || col.type.toLowerCase().includes('auto_increment') || col.name.toLowerCase() === 'id';
                    html += `
                        <div class="space-y-1.5">
                            <label class="block text-gray-300 text-xs font-semibold uppercase tracking-wider">${escapeHTML(col.name)} <span class="text-[10px] text-gray-500">(${escapeHTML(col.type)})</span></label>
                            <input type="text" data-col-name="${escapeHTML(col.name)}" ${isSerial ? 'placeholder="AUTO_INCREMENT (Optional)"' : 'placeholder="Enter value"'} class="db-insert-row-field w-full bg-gray-900 border border-gray-700 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:border-indigo-500 font-mono">
                        </div>
                    `;
                });
                html += '</div>';
                container.innerHTML = html;
            } else {
                container.innerHTML = '<div class="text-center py-6 text-red-400">Failed to load table structure.</div>';
            }
        } catch (e) {
            container.innerHTML = '<div class="text-center py-6 text-red-400">Connection error.</div>';
        }
    }

    function closeInsertRowModal() {
        closeModal('db-insert-row-modal');
    }

    async function submitInsertRow() {
        if (!currentDb || !explorerState.activeSchema || !explorerState.activeTable) return;

        const schema = explorerState.activeSchema;
        const table = explorerState.activeTable;
        const isNoSQL = currentDb.type === 'mongodb' || currentDb.type === 'redis';

        let data = {};

        if (isNoSQL) {
            const jsonText = document.getElementById('db-insert-row-json').value;
            try {
                data = JSON.parse(jsonText);
            } catch (e) {
                App.UI.toast('error', 'Invalid JSON syntax.');
                return;
            }
        } else {
            // Read from SQL inputs
            const inputs = document.querySelectorAll('.db-insert-row-field');
            inputs.forEach(input => {
                const colName = input.dataset.colName;
                const val = input.value.trim();
                if (val !== "") {
                    data[colName] = val;
                }
            });
        }

        try {
            const res = await App.API.post(`/servers/${App.config.SERVER_ID}/databases/${currentDb.id}/schemas/${encodeURIComponent(schema)}/tables/${encodeURIComponent(table)}/rows`, {
                data
            });

            if (res.success) {
                App.UI.toast('success', 'Record successfully inserted.');
                closeInsertRowModal();
                loadTableData(schema, table, explorerState.page);
            } else {
                App.UI.toast('error', `Insert failed: ${res.message}`);
            }
        } catch (e) {
            App.UI.toast('error', 'Connection error while inserting record.');
        }
    }

    async function deleteRow(schema, table, pkCol, pkVal) {
        if (!currentDb) return;
        if (!confirm('Are you sure you want to delete this record?')) {
            return;
        }

        try {
            const res = await App.API.delete(`/servers/${App.config.SERVER_ID}/databases/${currentDb.id}/schemas/${encodeURIComponent(schema)}/tables/${encodeURIComponent(table)}/rows?pk_col=${encodeURIComponent(pkCol)}&pk_val=${encodeURIComponent(pkVal)}`);
            if (res.success) {
                App.UI.toast('success', 'Record deleted successfully.');
                loadTableData(schema, table, explorerState.page);
            } else {
                App.UI.toast('error', `Delete failed: ${res.message}`);
            }
        } catch (e) {
            App.UI.toast('error', 'Connection error while deleting record.');
        }
    }

    // ---- Keyboard Shortcut: Ctrl/Cmd+Enter to run query ----
    document.addEventListener('keydown', function(e) {
        if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') {
            const queryEl = document.getElementById('db-console-query');
            if (queryEl && document.activeElement === queryEl) {
                e.preventDefault();
                runQuery();
            }
        }
    });

    // ---- Database Users Management ----

    async function loadUsers() {
        if (!currentDb) return;
        const tbody = document.getElementById('db-users-table-body');
        const emptyState = document.getElementById('db-users-empty');
        if (!tbody) return;

        const panel = document.getElementById('db-panel-users');
        const existingRedisBanner = document.getElementById('db-redis-warning-banner');
        if (existingRedisBanner) existingRedisBanner.remove();

        const addUserBtn = panel ? panel.querySelector('button[onclick="App.Databases.openAddUserModal()"]') : null;
        if (addUserBtn) addUserBtn.classList.remove('hidden');

        if (currentDb.type === 'redis') {
            const banner = document.createElement('div');
            banner.id = 'db-redis-warning-banner';
            banner.className = "p-4 mb-4 bg-red-500/10 border border-red-500/30 rounded-xl text-red-400 text-xs flex items-start space-x-3 shadow-lg shadow-red-500/5 backdrop-blur-md";
            banner.innerHTML = `
                <div class="p-1.5 bg-red-500/20 rounded-lg text-red-300">
                    <i class="fas fa-exclamation-triangle text-base"></i>
                </div>
                <div>
                    <h4 class="font-bold text-sm text-white mb-0.5">Redis Cluster Security Notice</h4>
                    <p class="text-red-300/80 leading-relaxed">Redis operates natively using a <strong>single global connection password</strong>. Dynamic multi-user provisioning and scoped schema access are bypassed by default. External clients can connect using the connection string generated above.</p>
                </div>
            `;
            if (panel) panel.insertBefore(banner, panel.firstChild);
            if (addUserBtn) addUserBtn.classList.add('hidden');
        }

        if (currentDb.type === 'sqlite') {
            const banner = document.createElement('div');
            banner.id = 'db-redis-warning-banner';
            banner.className = "p-4 mb-4 bg-sky-500/10 border border-sky-500/30 rounded-xl text-sky-400 text-xs flex items-start space-x-3 shadow-lg shadow-sky-500/5 backdrop-blur-md";
            banner.innerHTML = `
                <div class="p-1.5 bg-sky-500/20 rounded-lg text-sky-300">
                    <i class="fas fa-info-circle text-base"></i>
                </div>
                <div>
                    <h4 class="font-bold text-sm text-white mb-0.5">SQLite File-Based Security</h4>
                    <p class="text-sky-300/80 leading-relaxed">SQLite is a <strong>file-based embedded database</strong> with no built-in user authentication or role management. Access is controlled by file-system permissions on the server. User provisioning is not applicable for this engine type.</p>
                </div>
            `;
            if (panel) panel.insertBefore(banner, panel.firstChild);
            if (addUserBtn) addUserBtn.classList.add('hidden');
        }

        if (currentDb.type === 'pocketbase') {
            const banner = document.createElement('div');
            banner.id = 'db-redis-warning-banner';
            banner.className = "p-4 mb-4 bg-orange-500/10 border border-orange-500/30 rounded-xl text-orange-400 text-xs flex items-start space-x-3 shadow-lg shadow-orange-500/5 backdrop-blur-md";
            banner.innerHTML = `
                <div class="p-1.5 bg-orange-500/20 rounded-lg text-orange-300">
                    <i class="fas fa-info-circle text-base"></i>
                </div>
                <div>
                    <h4 class="font-bold text-sm text-white mb-0.5">PocketBase BaaS Auth Control</h4>
                    <p class="text-orange-300/80 leading-relaxed">PocketBase manages its own administrator accounts, users, and API authorization rules directly. Multi-user provisioning inside GoPulse is bypassed. Please use the PocketBase Admin panel to configure database permissions and roles.</p>
                </div>
            `;
            if (panel) panel.insertBefore(banner, panel.firstChild);
            if (addUserBtn) addUserBtn.classList.add('hidden');
        }

        tbody.innerHTML = '<tr><td colspan="5" class="px-6 py-8 text-center text-gray-500 text-sm"><i class="fas fa-spinner fa-spin mr-2"></i>Loading access users...</td></tr>';
        if (emptyState) emptyState.classList.add('hidden');

        try {
            const res = await App.API.get(`/servers/${App.config.SERVER_ID}/databases/${currentDb.id}/users`);
            if (!res.success || !res.data || res.data.length === 0) {
                tbody.innerHTML = '';
                if (emptyState) emptyState.classList.remove('hidden');
                return;
            }

            const hostIP = (App.System && App.System.currentServer && App.System.currentServer.host) 
                || document.getElementById('settings-server-host')?.value 
                || window.location.hostname 
                || "127.0.0.1";
            tbody.innerHTML = '';
            res.data.forEach(user => {
                const tr = document.createElement('tr');
                tr.className = "hover:bg-gray-800/20 text-sm text-gray-300 transition-colors";
                
                let badgeClass = "bg-blue-900/50 text-blue-400 border border-blue-500/20";
                if (user.scope === 'readWrite') {
                    badgeClass = "bg-green-900/50 text-green-400 border border-green-500/20";
                } else if (user.scope === 'admin') {
                    badgeClass = "bg-purple-900/50 text-purple-400 border border-purple-500/20";
                }

                // Exposed URL Connection String for each specific user
                let targetDb = user.database;
                if (!targetDb || targetDb === '* (All)') {
                    targetDb = currentDb.database_name;
                }
                if (currentDb.type === 'mongodb' && (!user.database || user.database === '* (All)')) {
                    targetDb = 'admin';
                }

                let userURI = "";
                if (currentDb.type === 'mysql') {
                    userURI = `mysql://${user.username}:<password>@${hostIP}:${currentDb.host_port}/${targetDb}`;
                } else if (currentDb.type === 'postgres') {
                    userURI = `postgresql://${user.username}:<password>@${hostIP}:${currentDb.host_port}/${targetDb}?sslmode=disable`;
                } else if (currentDb.type === 'mongodb') {
                    userURI = `mongodb://${user.username}:<password>@${hostIP}:${currentDb.host_port}/${targetDb}?authSource=admin`;
                }

                tr.innerHTML = `
                    <td class="px-6 py-4 font-mono font-semibold text-white">${escapeHTML(user.username)}</td>
                    <td class="px-6 py-4">
                        <span class="px-2.5 py-0.5 text-xs font-bold rounded-md uppercase ${badgeClass}">${user.scope}</span>
                    </td>
                    <td class="px-6 py-4 font-mono text-xs text-gray-400">${escapeHTML(user.database || 'public')}</td>
                    <td class="px-6 py-4">
                        <div class="flex items-center space-x-2">
                            <input type="text" readonly value="${escapeHTML(userURI)}" class="bg-gray-900 border border-gray-700/60 rounded px-2 py-1 text-xs font-mono text-gray-400 w-64 focus:outline-none select-all" onclick="this.select()">
                            <button onclick="App.Databases.copyUserURI(this)" class="p-1.5 bg-gray-700 hover:bg-gray-650 text-gray-300 hover:text-white rounded border border-gray-600 transition-all text-xs" title="Copy Connection String">
                                <i class="far fa-copy"></i>
                            </button>
                        </div>
                    </td>
                    <td class="px-6 py-4 text-right">
                        <button onclick="App.Databases.deleteUser('${escapeHTML(user.username)}')" class="p-2 bg-red-950/20 hover:bg-red-950/60 border border-red-500/10 hover:border-red-500/30 text-red-400 hover:text-red-300 rounded-lg transition-all text-xs" title="Remove User Access">
                            <i class="far fa-trash-alt"></i>
                        </button>
                    </td>
                `;
                tbody.appendChild(tr);
            });
        } catch (e) {
            tbody.innerHTML = '<tr><td colspan="5" class="px-6 py-8 text-center text-red-400 text-sm"><i class="fas fa-exclamation-triangle mr-2"></i>Failed to load access users.</td></tr>';
        }
    }

    function openAddUserModal() {
        const modal = document.getElementById('db-add-user-modal');
        if (!modal) return;

        const usernameEl = document.getElementById('db-user-username');
        const passwordEl = document.getElementById('db-user-password');
        const scopeEl = document.getElementById('db-user-scope');
        const databaseEl = document.getElementById('db-user-database');
        
        if (usernameEl) usernameEl.value = '';
        if (passwordEl) passwordEl.value = '';
        if (scopeEl) scopeEl.value = 'readWrite';

        // Dynamically populate active schemas as Target Databases dropdown list
        if (databaseEl) {
            databaseEl.innerHTML = '<option value="*">* (All Databases)</option>';
            if (explorerState && explorerState.schemas && explorerState.schemas.length > 0) {
                explorerState.schemas.forEach(schema => {
                    const opt = document.createElement('option');
                    opt.value = schema;
                    opt.innerText = schema;
                    if (schema === currentDb.database_name) opt.selected = true;
                    databaseEl.appendChild(opt);
                });
            } else {
                App.API.get(`/servers/${App.config.SERVER_ID}/databases/${currentDb.id}/schemas`).then(res => {
                    if (res.success && res.data) {
                        res.data.forEach(schema => {
                            const opt = document.createElement('option');
                            opt.value = schema;
                            opt.innerText = schema;
                            if (schema === currentDb.database_name) opt.selected = true;
                            databaseEl.appendChild(opt);
                        });
                    }
                }).catch(err => console.error("Failed to load schemas inside user picker modal", err));
            }
        }

        modal.classList.remove('hidden');
        setTimeout(() => {
            modal.classList.add('opacity-100');
            const inner = modal.querySelector('.scale-95') || modal.children[0];
            if (inner) {
                inner.classList.remove('scale-95');
                inner.classList.add('scale-100');
            }
        }, 10);
    }

    function closeAddUserModal() {
        const modal = document.getElementById('db-add-user-modal');
        if (!modal) return;

        modal.classList.remove('opacity-100');
        const inner = modal.children[0];
        if (inner) {
            inner.classList.remove('scale-100');
            inner.classList.add('scale-95');
        }
        setTimeout(() => modal.classList.add('hidden'), 300);
    }

    function toggleUserPassVisibility(btn) {
        const input = document.getElementById('db-user-password');
        if (!input) return;

        const icon = btn.querySelector('i');
        if (input.type === 'password') {
            input.type = 'text';
            if (icon) {
                icon.classList.remove('fa-eye');
                icon.classList.add('fa-eye-slash');
            }
        } else {
            input.type = 'password';
            if (icon) {
                icon.classList.remove('fa-eye-slash');
                icon.classList.add('fa-eye');
            }
        }
    }

    function copyUserURI(btn) {
        const input = btn.previousElementSibling;
        if (!input) return;
        
        const text = input.value;
        if (navigator.clipboard && navigator.clipboard.writeText) {
            navigator.clipboard.writeText(text).then(() => {
                App.UI.toast('success', 'User connection URI copied to clipboard!');
            });
        } else {
            input.select();
            document.execCommand('copy');
            App.UI.toast('success', 'User connection URI copied to clipboard!');
        }
    }

    async function submitAddUser() {
        if (!currentDb) return;
        const usernameEl = document.getElementById('db-user-username');
        const passwordEl = document.getElementById('db-user-password');
        const scopeEl = document.getElementById('db-user-scope');
        const databaseEl = document.getElementById('db-user-database');

        if (!usernameEl || !passwordEl || !scopeEl) return;

        const username = usernameEl.value.trim();
        const password = passwordEl.value;
        const scope = scopeEl.value;
        const database = databaseEl ? databaseEl.value : '*';

        if (!username) {
            App.UI.toast('error', 'Username is required.');
            return;
        }
        if (!password) {
            App.UI.toast('error', 'Password is required.');
            return;
        }

        try {
            App.UI.toast('info', 'Provisioning database user...');
            const res = await App.API.post(`/servers/${App.config.SERVER_ID}/databases/${currentDb.id}/users`, {
                username,
                password,
                scope,
                database
            });

            if (res.success) {
                App.UI.toast('success', 'User added successfully!');
                closeAddUserModal();
                loadUsers();
            } else {
                App.UI.toast('error', `Failed: ${res.message || res.error}`);
            }
        } catch (e) {
            App.UI.toast('error', 'Failed to provision database user.');
        }
    }

    async function deleteUser(username) {
        if (!currentDb) return;
        if (!confirm(`Are you sure you want to revoke database access for user "${username}"?`)) return;

        try {
            App.UI.toast('info', `Revoking access for user ${username}...`);
            const res = await App.API.delete(`/servers/${App.config.SERVER_ID}/databases/${currentDb.id}/users/${username}`);
            if (res.success) {
                App.UI.toast('success', `Access revoked for user ${username}`);
                loadUsers();
            } else {
                App.UI.toast('error', `Failed: ${res.message || res.error}`);
            }
        } catch (e) {
            App.UI.toast('error', `Failed to revoke database access.`);
        }
    }

    return {
        load,
        openProvisionModal,
        closeProvisionModal,
        onProvTypeChange,
        generatePass,
        submitProvision,
        togglePower,
        quickCheckHealth,
        showListView,
        showManageView,
        switchManageTab,
        copyConnURI,
        runQuery,
        createBackup,
        loadBackups,
        restoreBackup,
        importBackup,
        saveAutoBackupConfig,
        deleteDatabase,
        // Explorer
        loadSchemas,
        toggleSchema,
        selectTable,
        explorerPrevPage,
        explorerNextPage,
        refreshExplorer,
        openCreateSchemaModal,
        closeSchemaModal,
        submitCreateSchema,
        deleteSchema,
        openCreateTableModal,
        closeCreateTableModal,
        addSchemaBuilderColumn,
        submitCreateTable,
        deleteTable,
        openInsertRowModal,
        closeInsertRowModal,
        submitInsertRow,
        deleteRow,
        // Users Management
        loadUsers,
        openAddUserModal,
        closeAddUserModal,
        toggleUserPassVisibility,
        copyUserURI,
        submitAddUser,
        deleteUser
    };
})();
