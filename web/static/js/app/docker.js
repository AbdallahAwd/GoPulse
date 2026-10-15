/**
 * GoPulse App - Docker Module
 * Manages sub-navigation and shared Docker logic
 */

App.Docker = {
    activeView: 'containers-view',

    init() {
        // Sub-navigation
        document.querySelectorAll('.docker-nav-btn').forEach(btn => {
            btn.addEventListener('click', (e) => {
                const target = e.currentTarget.dataset.target;
                this.switchView(target);
            });
        });

        // Initial load if Docker tab is active (handled by main.js, but good to have)
    },

    switchView(viewId) {
        this.activeView = viewId;

        // Keep the existing view IDs and loaders; selection is expressed by aria-pressed.
        document.querySelectorAll('.docker-nav-btn').forEach(btn => {
            btn.setAttribute('aria-pressed', String(btn.dataset.target === viewId));
        });
        const menu = document.getElementById('docker-manage-menu');
        if (menu) { menu.open = false; menu.dataset.active = String(viewId === 'networks-view'); }
        const label = document.getElementById('docker-manage-label');
        if (label) label.textContent = viewId === 'networks-view' ? 'Networks' : 'Manage';

        // Update Views
        document.querySelectorAll('.docker-view').forEach(el => el.classList.add('hidden'));
        document.getElementById(viewId).classList.remove('hidden');

        // Load Data
        this.refresh();
    },

    escapeHTML(value) {
        return String(value ?? '').replace(/[&<>"']/g, char => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[char]));
    },

    handlerArgument(value) {
        return this.escapeHTML(JSON.stringify(String(value ?? '')));
    },

    async refresh() {
        const btn = document.getElementById('docker-refresh-btn');
        let originalContent = '';
        if (btn) {
            originalContent = btn.innerHTML;
            btn.innerHTML = `<i class="fas fa-sync-alt fa-spin mr-2"></i>Refreshing...`;
            btn.disabled = true;
            btn.classList.add('opacity-75', 'cursor-not-allowed');
        }

        try {
            // Check if Docker is installed first
            const installed = await this.checkDockerInstalled();
            if (!installed) return;

            switch(this.activeView) {
                case 'containers-view':
                    if (App.Docker.Containers) await App.Docker.Containers.load();
                    break;
                case 'images-view':
                    if (App.Docker.Images) await App.Docker.Images.load();
                    break;
                case 'volumes-view':
                    if (App.Docker.Volumes) await App.Docker.Volumes.load();
                    break;
                case 'networks-view':
                    if (App.Docker.Networks) await App.Docker.Networks.load();
                    break;
            }
        } finally {
            if (btn) {
                btn.innerHTML = originalContent;
                btn.disabled = false;
                btn.classList.remove('opacity-75', 'cursor-not-allowed');
            }
        }
    },

    async checkDockerInstalled() {
        const overlay = document.getElementById('docker-not-installed');
        const content = document.getElementById('docker-content');
        if (!overlay || !content) return true;

        try {
            const res = await App.API.get('/system/check/docker');
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
            // If check fails, show content anyway (might be a local server)
            overlay.classList.add('hidden');
            content.classList.remove('hidden');
            return true;
        }
    },

    async installDocker() {
        const btn = document.getElementById('install-docker-btn');
        const terminal = document.getElementById('docker-install-terminal');
        const output = document.getElementById('docker-install-output');

        if (!btn || !terminal || !output) return;

        // Loading state
        btn.innerHTML = '<i class="fas fa-circle-notch fa-spin mr-2"></i>Installing Docker...';
        btn.disabled = true;
        btn.classList.add('opacity-75', 'cursor-not-allowed');
        terminal.classList.remove('hidden');
        output.textContent = '[System] Starting Docker installation...\n';

        try {
            const response = await fetch(`${App.config.API_BASE}/servers/${App.config.SERVER_ID}/system/install/docker`, {
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

            App.UI.toast('success', 'Docker installed successfully!');

            // Re-check and switch to docker content
            setTimeout(() => {
                btn.innerHTML = '<i class="fas fa-check mr-2"></i>Installed!';
                btn.classList.remove('opacity-75', 'cursor-not-allowed');
                btn.classList.replace('bg-blue-600', 'bg-green-600');
                this.checkDockerInstalled();
                this.refresh();
            }, 1500);
        } catch (e) {
            output.textContent += `\n[Error] Installation failed: ${e.message}\n`;
            btn.innerHTML = '<i class="fab fa-docker mr-2"></i>Retry Install';
            btn.disabled = false;
            btn.classList.remove('opacity-75', 'cursor-not-allowed');
            App.UI.toast('error', 'Docker installation failed');
        }
    },

    showBuildModal() {
        App.Docker.Build.openModal();
    }
};

App.Docker.Run = {
    currentImage: '',
    
    openModal(image) {
        this.currentImage = image;
        document.getElementById('run-image-name').value = image;
        document.getElementById('run-container-name').value = '';
        document.getElementById('run-container-ports').value = '';
        
        const modal = document.getElementById('run-image-modal');
        modal.classList.remove('hidden');
        setTimeout(() => {
            modal.querySelector('div').classList.remove('scale-95');
            modal.querySelector('div').classList.remove('opacity-0');
        }, 10);
    },

    closeModal() {
        const modal = document.getElementById('run-image-modal');
        modal.classList.add('hidden');
    },

    async submit() {
        const name = document.getElementById('run-container-name').value;
        const ports = document.getElementById('run-container-ports').value;
        const btn = document.getElementById('run-image-submit');
        
        const originalText = btn.innerHTML;
        btn.innerHTML = '<i class="fas fa-circle-notch fa-spin"></i> Running...';
        btn.disabled = true;

        try {
            const response = await App.API.post(`/servers/${App.config.SERVER_ID}/images/run`, {
                image: this.currentImage,
                name: name,
                ports: ports
            });

            if (response.success) {
                App.UI.toast('success', 'Container started successfully');
                this.closeModal();
                // Switch to containers view to see it
                App.Docker.switchView('containers-view');
            } else {
                App.UI.toast('error', response.error || 'Failed to run image');
            }
        } catch (e) {
            App.UI.toast('error', 'Failed to run image: ' + e.message);
        } finally {
            btn.innerHTML = originalText;
            btn.disabled = false;
        }
    }
};


App.Docker.Build = {
    openModal() {
        document.getElementById('build-modal').classList.remove('hidden');
        document.getElementById('build-terminal-container').classList.add('hidden'); // Ensure terminal is hidden initially
        
        // Ensure content/path areas are reset based on current selection
        this.toggleType();
        this.clearTerminal();
    },

    closeModal() {
        document.getElementById('build-modal').classList.add('hidden');
    },

    toggleType() {
        const type = document.getElementById('build-type').value;
        const source = document.getElementById('build-source').value;

        const pathGroup = document.getElementById('build-path-group');
        const composeGroup = document.getElementById('compose-file-group');
        const pathWrapper = document.getElementById('build-path-wrapper');
        const contentGroup = document.getElementById('build-content-group');
        const tagGroup = document.getElementById('build-tag-group');

        // Logic split by source
        if (source === 'content') {
            pathWrapper.classList.add('hidden');
            contentGroup.classList.remove('hidden');
        } else {
            pathWrapper.classList.remove('hidden');
            contentGroup.classList.add('hidden');
        }

        // Logic split by type (sub-logic)
        if (type === 'dockerfile') {
            if (source === 'path') {
                pathGroup.classList.remove('hidden');
                composeGroup.classList.add('hidden');
            }
            tagGroup.classList.remove('hidden');
        } else {
            // Compose
            if (source === 'path') {
                // Use pathGroup (directory) for compose as well now
                pathGroup.classList.remove('hidden');
                composeGroup.classList.add('hidden');
            }
            tagGroup.classList.add('hidden');
        }
    },

    loadFromFile(input) {
        if (input.files && input.files[0]) {
            const reader = new FileReader();
            reader.onload = (e) => {
                document.getElementById('build-content').value = e.target.result;
            };
            reader.readAsText(input.files[0]);
        }
    },

    clearTerminal() {
        const term = document.getElementById('build-terminal');
        if (term) term.innerText = '';
    },

    async start() {
        const type = document.getElementById('build-type').value;
        const source = document.getElementById('build-source').value;
        const term = document.getElementById('build-terminal');
        
        let url = '';
        let body = {};

        if (type === 'dockerfile') {
            const tag = document.getElementById('build-tag').value;
            if (!tag) {
                App.UI.toast('error', 'Tag is required');
                return;
            }
            url = '/images/build';
            body = { tag: tag };

            if (source === 'path') {
                const path = document.getElementById('build-path').value;
                if (!path) {
                    App.UI.toast('error', 'Path is required');
                    return;
                }
                body.path = path;
            } else {
                const content = document.getElementById('build-content').value;
                if (!content) {
                    App.UI.toast('error', 'Content is required');
                    return;
                }
                body.content = content;
            }
        } else {
            // Compose
            url = '/compose/build';
            if (source === 'path') {
                const path = document.getElementById('build-path').value;
                if (!path) {
                     App.UI.toast('error', 'Project path is required');
                     return;
                }
                body.file = path; // Backend expects 'file' but treats it as dir/path
            } else {
                const content = document.getElementById('build-content').value;
                 if (!content) {
                    App.UI.toast('error', 'Content is required');
                    return;
                }
                body.content = content;
            }
        }

        this.appendLog('[System] Starting build job...\n');

        // UI Updates on Start
        document.getElementById('build-terminal-container').classList.remove('hidden');
        
        // Hide content group if in content mode to give space to terminal
        if (source === 'content') {
            document.getElementById('build-content-group').classList.add('hidden');
        }

        try {
            const response = await fetch(`/api/servers/${App.config.SERVER_ID}${url}`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(body)
            });

            if (!response.ok) {
                 const err = await response.json();
                 throw new Error(err.error || 'Build failed to start');
            }

            const reader = response.body.getReader();
            const decoder = new TextDecoder();

            while (true) {
                const { done, value } = await reader.read();
                if (done) break;
                const text = decoder.decode(value, { stream: true });
                this.appendLog(text);
            }
            
            this.appendLog('\n[System] Build process completed.\n');
            App.UI.toast('success', 'Build completed');
             // Refresh lists
            if (type === 'dockerfile') App.Docker.Images.load();
            App.Docker.refresh();

        } catch (e) {
            this.appendLog(`\n[Error] ${e.message}\n`);
            App.UI.toast('error', 'Build failed: ' + e.message);
        }
    },

    appendLog(text) {
        const term = document.getElementById('build-terminal');
        if (!term) return;
        term.innerText += text;
        term.scrollTop = term.scrollHeight;
    }
};

App.Docker.FilePicker = {
    currentPath: '/',
    targetInput: null,
    mode: 'file', // 'file' or 'directory'

    open(inputId, mode = 'file') {
        this.targetInput = inputId;
        this.mode = mode;
        const modal = document.getElementById('file-picker-modal');
        const title = document.getElementById('file-picker-title');
        
        if (title) title.textContent = mode === 'directory' ? 'Select Directory' : 'Select File';
        
        if (modal) {
            modal.classList.remove('hidden');
            setTimeout(() => {
                modal.classList.remove('opacity-0');
                modal.querySelector('div').classList.remove('scale-95');
            }, 10);
            this.load(this.currentPath);
        }
    },

    close() {
        const modal = document.getElementById('file-picker-modal');
        if (modal) {
            modal.classList.add('opacity-0');
            modal.querySelector('div').classList.add('scale-95');
            setTimeout(() => modal.classList.add('hidden'), 300);
        }
    },

    async load(path) {
        const loading = document.getElementById('file-picker-loading');
        if (loading) loading.classList.remove('hidden');

        try {
            // Reusing the existing files API
            const response = await fetch(`${App.config.API_BASE}/servers/${App.config.SERVER_ID}/files/list?path=${encodeURIComponent(path)}`);
            const json = await response.json();

            if (json.success) {
                this.currentPath = json.data.path;
                this.render(json.data.files);
                this.updatePathDisplay();
            } else {
                App.UI.toast('error', 'Failed to load directory');
            }
        } catch (e) {
            console.error(e);
            App.UI.toast('error', 'Failed to load directory');
        } finally {
            if (loading) loading.classList.add('hidden');
        }
    },

    render(files) {
        const tbody = document.getElementById('file-picker-tbody');
        if (!tbody) return;

        tbody.innerHTML = '';
        const sortedFiles = (files || []).sort((a, b) => {
            if (a.is_dir === b.is_dir) return a.name.localeCompare(b.name);
            return a.is_dir ? -1 : 1;
        });

        sortedFiles.forEach(file => {
            // Filter: if mode is 'directory', maybe we still want to show files but disable them? 
            // Or just show everything and only allow selecting directories?
            // For now, let's show everything but style accordingly.
            
            const icon = file.is_dir ? 'fa-folder docker-picker-icon' : 'fa-file-code docker-picker-icon';
            const isSelectable = this.mode === 'directory' ? file.is_dir : true;
            
            // If mode is directory, files should probably be greyed out or just not selectable
            const cursorClass = isSelectable ? 'cursor-pointer hover:bg-gray-700' : 'cursor-default opacity-50';
            
            const tr = document.createElement('tr');
            tr.className = `border-b border-gray-700/50 transition-colors ${cursorClass}`;
            tr.innerHTML = `
                <td class="py-3 px-4 w-12 text-center"><i class="fas ${icon}"></i></td>
                <td class="py-3 px-4 font-medium text-white">${App.Docker.escapeHTML(file.name)}</td>
                <td class="py-3 px-4 text-right">
                    ${isSelectable ? 
                        `<button class="docker-inline-button">Select</button>` :
                        ''}
                </td>
            `;

            // Row click handling
            tr.addEventListener('click', (e) => {
                // If it's a specific button click, let it handle separately if needed, 
                // but here we can just use the row click for navigation or selection
                
                // If directory, navigate on click (unless it's the select button?)
                // Let's mimic standard behavior: Click name/row -> Navigate (if dir). 
                // We need a specific "Select" action.
                
                // If clicking the Select button:
                if (e.target.tagName === 'BUTTON' || e.target.closest('button')) {
                    e.stopPropagation();
                    this.select(file.name);
                    return;
                }

                // Navigation
                if (file.is_dir) {
                    const fullPath = this.currentPath === '/' ? `/${file.name}` : `${this.currentPath}/${file.name}`;
                    this.load(fullPath);
                }
            });

            tbody.appendChild(tr);
        });
        
        // If current path is not root, show existing "Select Current Directory" option if in directory mode?
        // Actually, usually you select a folder by being IN it or clicking it. 
        // Let's add a "Select Current Folder" button in the header or top of list if in directory mode.
        if (this.mode === 'directory') {
             const tr = document.createElement('tr');
             tr.className = 'docker-picker-current cursor-pointer';
             tr.innerHTML = `
                <td class="py-3 px-4 text-center"><i class="fas fa-check text-green-400"></i></td>
                <td class="py-3 px-4">Use current directory</td>
                <td class="py-3 px-4 text-right">
                     <button class="docker-inline-button select-current-btn">Select</button>
                </td>
             `;
             tr.onclick = (e) => {
                  this.select('.');
             };
             tbody.insertBefore(tr, tbody.firstChild);
        }
    },

    updatePathDisplay() {
        const el = document.getElementById('file-picker-path');
        if (el) el.textContent = this.currentPath;
    },

    up() {
        if (this.currentPath === '/') return;
        const parts = this.currentPath.split('/');
        parts.pop();
        const newPath = parts.join('/') || '/';
        this.load(newPath);
    },

    select(filename) {
        let fullPath;
        if (filename === '.') {
            fullPath = this.currentPath;
        } else {
            fullPath = this.currentPath === '/' ? `/${filename}` : `${this.currentPath}/${filename}`;
        }
        
        const input = document.getElementById(this.targetInput);
        if (input) {
            input.value = fullPath;
            // Update display
            const display = document.getElementById(`${this.targetInput}-display`);
            if (display) {
                display.textContent = fullPath;
                display.classList.remove('italic', 'text-gray-400');
                display.classList.add('text-white', 'font-mono');
            }
        }
        this.close();
    }
};

// Initialize when DOM is ready (or called from main.js)
document.addEventListener('DOMContentLoaded', () => {
    App.Docker.init();
});

document.addEventListener('click', event => {
    const menu = document.getElementById('docker-manage-menu');
    if (menu?.open && !menu.contains(event.target)) menu.open = false;
});

document.addEventListener('click', event => {
    document.querySelectorAll('.docker-container-menu[open]').forEach(menu => {
        if (!menu.contains(event.target) || event.target.closest('button')) menu.open = false;
    });
});
document.addEventListener('keydown', event => {
    if (event.key !== 'Escape') return;
    document.querySelectorAll('.docker-container-menu[open], #docker-manage-menu[open]').forEach(menu => { menu.open = false; menu.querySelector('summary')?.focus(); });
});
