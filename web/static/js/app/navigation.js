// Workspace navigation and addressable server sections. Existing feature modules
// remain responsible for resource actions and their authorization.
App.Navigation = {
    titles: {
        functions:'Functions', 'remote-backups':'Remote Backups', alerts: 'Alerts', overview: 'Overview', projects: 'Applications', databases: 'Databases',
        domains: 'Domains and TLS', 'load-balancers': 'Load balancers',
        processes: 'Processes', docker: 'Docker', cron: 'Cron jobs', files: 'Files', terminal: 'Terminal',
        'system-users': 'Server users', firewall: 'Firewall', settings: 'Server settings'
    },

    current: null,
    generation: 0,

    resolve(search, hash = '') {
        const requested = new URLSearchParams(search).get('view') || hash.replace(/^#/, '') || 'overview';
        return Object.hasOwn(this.titles, requested) ? requested : 'overview';
    },

    sectionURL(view, currentURL = window.location.href) {
        const url = new URL(currentURL);
        url.searchParams.set('view', view);
        ['resource', 'panel', 'action'].forEach(key => url.searchParams.delete(key));
        url.hash = '';
        return url;
    },

    async activate(view, push = false) {
        if (!Object.hasOwn(this.titles, view) || !document.getElementById(`${view}-tab`)) view = 'overview';
        const generation = ++this.generation;
        this.current = view;
        if (push) history.pushState({}, '', this.sectionURL(view));
        document.querySelectorAll('.tab-content').forEach(panel => panel.classList.toggle('hidden', panel.id !== `${view}-tab`));
        document.querySelectorAll('#dashboard-sidebar-nav .tab-btn').forEach(link => {
            const active = link.dataset.tab === view;
            link.classList.toggle('border-blue-500', active);
            if (active) link.setAttribute('aria-current', 'page');
            else link.removeAttribute('aria-current');
        });
        const selector = document.getElementById('server-view-selector');
        if (selector) selector.value = view;
        const title = document.getElementById('server-section-title');
        if (title) title.textContent = this.titles[view];
        document.title = `GoPulse - ${document.getElementById('current-server-id')?.dataset.name || 'Server'} - ${this.titles[view]}`;
        const description = document.getElementById('server-section-description');
        if (description) description.textContent = {functions:'Create, deploy, and run isolated functions with triggers and logs.','remote-backups':'Schedule encrypted off-server backups and verify recovery.',alerts:'Configure alerts and inspect incidents and webhook deliveries.',overview:'Manage resources and inspect this server.',projects:'Build, deploy, and manage applications on this server.',databases:'Manage database instances, services, and backups on this server.',domains:'Manage domains and certificates on this server.','load-balancers':'Route traffic from this server to application backends.',processes:'Inspect processes and resource usage on this server.',docker:'Manage containers, images, volumes, and networks.',cron:'Schedule commands and inspect their execution on this server.',files:'Browse and edit files on this server.',terminal:'Open an interactive terminal on this server.','system-users':'Manage operating system users and their access.',firewall:'Manage network access and firewall rules.',settings:'Configure preferences for this server workspace.'}[view];
        const globalSection = 'servers';
        document.querySelectorAll('#workspace-navigation a').forEach(link => {
            if (link.dataset.section === globalSection) link.setAttribute('aria-current', 'page');
            else link.removeAttribute('aria-current');
        });
        if (view !== 'functions') App.Functions?.deactivate();
        if (view !== 'remote-backups') App.RemoteBackups?.deactivate();
        if (view !== 'alerts') App.Alerts?.deactivate();
        if (view !== 'cron') App.Cron?.deactivate();
        if (view !== 'databases') App.Databases.deactivate();
        else App.Databases.showListView(false);
        if (view !== 'overview') App.Charts.pause();
        const loaders = {
            'remote-backups': () => App.RemoteBackups.load(),
            functions: () => App.Functions.load(),
            alerts: () => App.Alerts.load(),
            overview: () => {
                App.Charts.init();
            },
            projects: () => App.Projects.load(), databases: () => App.Databases.load(),
            domains: () => App.Domains.load(), 'load-balancers': () => App.LoadBalancers.load(),
            processes: () => App.Processes.load(), docker: () => App.Docker.refresh(), cron: () => App.Cron.load(),
            files: () => App.Files.list(App.state.currentPath),
            'system-users': () => App.SystemUsers.load(), firewall: () => App.Firewall.load()
        };
        try {
            if (view === 'terminal') App.Terminal.open();
            else App.Terminal.close();
            await loaders[view]?.();
            if (generation !== this.generation) return;
            const params = new URLSearchParams(window.location.search);
            const resource = params.get('resource');
            if (resource && view === 'projects' && App.Projects.list.some(item => item.id === resource)) {
                App.Projects.activeOpsTab = params.get('panel') === 'releases' ? 'releases' : 'overview';
                App.Projects.showProjectOps(resource);
            }
            if (resource && view === 'databases') await App.Databases.showManageView(resource);
            if (resource && view === 'load-balancers' && App.LoadBalancers.loadBalancers.some(item => item.id === resource)) App.LoadBalancers.showDetailsPanel(resource);
            if (params.get('action') === 'create') {
                if (view === 'cron') App.Cron.openEditor();
                if (view === 'projects') App.Projects.showCreateModal();
                if (view === 'databases') App.Databases.openProvisionModal('clusters');
                params.delete('action');
                history.replaceState({}, '', `${window.location.pathname}?${params}`);
            }
        } catch (error) {
            if (generation === this.generation) App.UI.toast('error', `Unable to open ${this.titles[view]}: ${error.message}`);
        }
    },

    init() {
        const toggle = document.getElementById('workspace-menu-toggle');
        const rail = document.getElementById('console-rail');
        const scrim = document.getElementById('console-mobile-scrim');
        if (toggle && rail) {
            const desktop = window.matchMedia('(min-width: 1024px)');
            rail.inert = !desktop.matches;
            const close = () => {
                if (rail.contains(document.activeElement) && !desktop.matches) toggle.focus();
                rail.inert = !desktop.matches;
                rail.dataset.open = 'false';
                if (scrim) scrim.hidden = true;
                toggle.setAttribute('aria-expanded', 'false');
                toggle.setAttribute('aria-label', 'Open navigation');
                document.body.classList.remove('console-menu-open');
            };
            toggle.addEventListener('click', () => {
                const open = rail.dataset.open !== 'true';
                if (!open) { close(); return; }
                rail.inert = false;
                rail.dataset.open = 'true';
                if (scrim) scrim.hidden = false;
                toggle.setAttribute('aria-expanded', 'true');
                toggle.setAttribute('aria-label', 'Close navigation');
                document.body.classList.add('console-menu-open');
                rail.querySelector('nav a')?.focus();
            });
            scrim?.addEventListener('click', () => { close(); toggle.focus(); });
            document.addEventListener('keydown', event => {
                if (event.key === 'Escape' && rail.dataset.open === 'true') { close(); toggle.focus(); }
                if (event.key === 'Tab' && rail.dataset.open === 'true') {
                    const links = Array.from(rail.querySelectorAll('a, button'));
                    const first = links[0], last = links[links.length - 1];
                    if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus(); }
                    else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
                }
            });
            rail.querySelectorAll('.tab-btn').forEach(link => link.addEventListener('click', close));
            desktop.addEventListener('change', () => close());
        }
        if (!App.config.SERVER_ID) return;
        document.querySelectorAll('#dashboard-sidebar-nav .tab-btn').forEach(link => {
            link.addEventListener('click', event => {
                if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
                event.preventDefault();
                this.activate(link.dataset.tab, true);
            });
        });
        document.getElementById('server-view-selector')?.addEventListener('change', event => this.activate(event.target.value, true));
        window.addEventListener('popstate', () => this.activate(this.resolve(window.location.search, window.location.hash)));
        const initial = this.resolve(window.location.search, window.location.hash);
        if (window.location.hash) {
            const url = new URL(window.location.href);
            url.searchParams.set('view', initial); url.hash = '';
            history.replaceState({}, '', url);
        }
        this.activate(initial);
    }
};
