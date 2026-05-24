/**
 * GoPulse App - Files Module
 */

App.Files = {
    async list(path = '/') {
        if (!App.config.SERVER_ID) return;
        const overlay = App.UI.el('file-loading-overlay');
        if (overlay) overlay.classList.remove('hidden');

        try {
            const response = await App.API.get(`/files/list?path=${encodeURIComponent(path)}`);
            if (response.success) {
                App.state.currentPath = response.data.path;
                this.render(response.data.files);
                this.renderBreadcrumbs(App.state.currentPath);
            }
        } catch (e) {
            const tbody = App.UI.el('files-tbody');
            if (tbody) tbody.innerHTML = `<tr><td colspan="5" class="px-4 py-8 text-center text-red-400">Failed to load directory</td></tr>`;
        } finally {
            if (overlay) overlay.classList.add('hidden');
        }
    },

    render(files) {
        const container = App.UI.el('file-list-container');
        if (!container) return;

        const fileList = files || []; // Handle null/undefined files
        const path = App.state.currentPath;

        const loadingOverlay = `<div id="file-loading-overlay"
                    class="absolute inset-0 bg-white bg-opacity-10 backdrop-blur-sm flex items-center justify-center z-10 hidden transition-all duration-300">
                    <div class="flex flex-col items-center p-4 rounded-lg  bg-opacity-75 shadow-xl ">
                        <i class="fas fa-circle-notch fa-spin text-3xl mb-3 text-blue-500"></i>
                        <p class="text-white font-medium text-sm">Loading contents...</p>
                    </div>
                </div>`;

        // Parent directory row
        let parentRow = '';
        if (path !== '/' && path !== '') {
            const parentPath = path.substring(0, path.lastIndexOf('/')) || '/';
            parentRow = `
                <tr class="border-b border-gray-700/50 hover:bg-gray-700/50 transition-colors group cursor-pointer"
                    onclick="App.Files.list('${parentPath}')">
                    <td class="py-3 px-4 text-center"><i class="fas fa-level-up-alt text-lg text-gray-500"></i></td>
                    <td class="py-3 px-4 font-medium text-gray-400 group-hover:text-blue-400 transition-colors">..</td>
                    <td class="py-3 px-4 text-gray-500 font-mono text-xs">-</td>
                    <td class="py-3 px-4 text-gray-500 text-xs">-</td>
                    <td class="py-3 px-4 text-right"></td>
                </tr>`;
        }

        const tableContent = fileList.map(file => {
            const icon = file.is_dir ? 'fa-folder text-yellow-400' : 'fa-file-code text-blue-400';
            const size = file.is_dir ? '-' : App.UI.formatBytes(file.size);
            const date = new Date(file.mod_time).toLocaleString();
            const fullPath = path === '/' ? `/${file.name}` : `${path}/${file.name}`;
            
            return `
            <tr class="border-b border-gray-700/50 hover:bg-gray-700/50 transition-colors group cursor-pointer file-item"
                data-path="${fullPath}" data-type="${file.is_dir ? 'dir' : 'file'}" data-name="${file.name}">
                <td class="py-3 px-4 text-center"><i class="fas ${icon} text-lg"></i></td>
                <td class="py-3 px-4 font-medium text-white group-hover:text-blue-400 transition-colors">
                    <a href="#" onclick="event.preventDefault(); ${file.is_dir ? 
                        `App.Files.list('${fullPath}')` : 
                        `App.Editor.open('${fullPath}')`}" class="block">
                        ${file.name}
                    </a>
                </td>
                <td class="py-3 px-4 text-gray-400 font-mono text-xs">${size}</td>
                <td class="py-3 px-4 text-gray-400 text-xs">${date}</td>
                <td class="py-3 px-4 text-right opacity-0 group-hover:opacity-100 transition-opacity">
                    <button onclick="App.Files.delete('${fullPath}')" class="text-gray-400 hover:text-red-400 transition-colors p-1" title="Delete">
                        <i class="fas fa-trash-alt"></i>
                    </button>
                    ${file.is_dir ? `<button onclick="App.Files.zip('${fullPath}')" class="ml-2 text-gray-400 hover:text-yellow-400 transition-colors p-1" title="Zip Folder">
                        <i class="fas fa-file-zipper"></i>
                    </button>` : ''}
                    ${!file.is_dir ? `<button onclick="App.Files.download('${fullPath}')" class="ml-2 text-gray-400 hover:text-green-400 transition-colors p-1" title="Download">
                        <i class="fas fa-download"></i>
                    </button>` : ''}
                    ${!file.is_dir && file.name.endsWith('.zip') ? `<button onclick="App.Files.unzip('${fullPath}')" class="ml-2 text-gray-400 hover:text-blue-400 transition-colors p-1" title="Unzip">
                        <i class="fas fa-box-open"></i>
                    </button>` : ''}
                </td>
            </tr>`;
        }).join('');

        const tableHtml = (fileList.length === 0 && !parentRow)
            ? `<div class="text-center text-gray-500 py-12 flex flex-col items-center">
                    <i class="fas fa-folder-open text-4xl mb-3 opacity-50"></i>
                    <p>Empty directory</p>
               </div>`
            : `<table class="w-full text-left border-collapse">
                <thead>
                    <tr class="text-gray-400 border-b border-gray-700 text-sm uppercase bg-gray-900/50">
                        <th class="py-3 px-4 font-medium w-12 text-center"><i class="fas fa-file"></i></th>
                        <th class="py-3 px-4 font-medium">Name</th>
                        <th class="py-3 px-4 font-medium w-32">Size</th>
                        <th class="py-3 px-4 font-medium w-48">Modified</th>
                        <th class="py-3 px-4 font-medium w-32 text-right">Actions</th>
                    </tr>
                </thead>
                <tbody class="text-gray-300 text-sm">
                    ${parentRow}
                    ${tableContent}
                </tbody>
            </table>`;
        
        container.innerHTML = loadingOverlay + tableHtml;
        this.attachContextMenu();
    },

    renderBreadcrumbs(path) {
        const input = App.UI.el('file-path-input');
        console.log('renderBreadcrumbs called with path:', path, 'input element found:', !!input);
        if (input) {
            console.log('Setting input value to:', path);
            input.value = path;
            input.setAttribute('value', path);
            console.log('Input value after setting:', input.value);
        } else {
            console.warn('file-path-input element not found!');
        }
    },

    create(type) {
        const modal = App.UI.el('create-item-modal');
        const title = App.UI.el('create-item-title');
        const typeInput = App.UI.el('create-item-type');
        const nameInput = App.UI.el('create-item-name');
        const icon = App.UI.el('create-item-icon');
        const iconWrapper = App.UI.el('create-item-icon-wrapper');

        if (!modal || !typeInput || !nameInput) return;

        // Reset and Setup
        typeInput.value = type;
        nameInput.value = '';
        
        if (type === 'directory') {
            title.textContent = 'Create New Folder';
            icon.className = 'fas fa-folder-plus text-yellow-400';
            iconWrapper.className = 'p-2 bg-yellow-900 bg-opacity-50 rounded-lg mr-3';
        } else {
            title.textContent = 'Create New File';
            icon.className = 'fas fa-file-plus text-blue-400';
            iconWrapper.className = 'p-2 bg-blue-900 bg-opacity-50 rounded-lg mr-3';
        }

        // Show Modal
        modal.classList.remove('hidden');
        // Small delay for transition
        setTimeout(() => {
            modal.classList.remove('opacity-0');
            modal.querySelector('div').classList.remove('scale-95', 'opacity-0');
            nameInput.focus();
        }, 10);
    },

    closeCreateModal() {
        const modal = App.UI.el('create-item-modal');
        if (!modal) return;

        modal.classList.add('opacity-0');
        modal.querySelector('div').classList.add('scale-95');
        
        setTimeout(() => {
            modal.classList.add('hidden');
        }, 300);
    },

    async submitCreate() {
        const type = App.UI.el('create-item-type').value;
        const name = App.UI.el('create-item-name').value;

        if (!name || name.includes('/') || name.includes('\\')) {
            if (name) App.UI.toast('error', `Invalid ${type} name`);
            return;
        }

        const fullPath = App.state.currentPath === '/' ? `/${name}` : `${App.state.currentPath}/${name}`;
        
        // Button Loading State
        const btn = document.querySelector('#create-item-modal button:last-child');
        const originalText = btn.innerHTML;
        btn.innerHTML = '<i class="fas fa-spinner fa-spin"></i> Creating...';
        btn.disabled = true;

        try {
            if (type === 'directory') {
                await App.API.post('/files/action', { action: 'mkdir', path: fullPath });
            } else {
                 await App.API.post('/files/save', { path: fullPath, content: "" });
            }
            App.UI.toast('success', `${type === 'directory' ? 'Folder' : 'File'} created`);
            this.list(App.state.currentPath);
            this.closeCreateModal();
        } catch (e) { 
            // Error handled by API
        } finally {
            btn.innerHTML = originalText;
            btn.disabled = false;
        }
    },

    async delete(path) {
        if (!confirm(`Delete ${path}?`)) return;
        try {
            await App.API.post('/files/action', { action: 'delete', path });
            App.UI.toast('success', 'Item deleted');
            this.list(App.state.currentPath);
        } catch (e) { /* Handled */ }
    },

    async zip(path) {
        try {
            const btn = document.querySelector(`button[onclick="App.Files.zip('${path}')"] i`);
            if (btn) btn.className = 'fas fa-spinner fa-spin';
            
            await App.API.post('/files/action', { action: 'compress', path });
            App.UI.toast('success', 'Folder zipped successfully');
            this.list(App.state.currentPath);
        } catch (e) { 
             App.UI.toast('error', 'Failed to zip folder');
        }
    },

    async unzip(path) {
        try {
            const btn = document.querySelector(`button[onclick="App.Files.unzip('${path}')"] i`);
            if (btn) btn.className = 'fas fa-spinner fa-spin';

            await App.API.post('/files/action', { action: 'decompress', path });
            App.UI.toast('success', 'File unzipped successfully');
            this.list(App.state.currentPath);
        } catch (e) {
            App.UI.toast('error', 'Failed to unzip file');
        }
    },

    download(path) {
        const url = App.config.SERVER_ID 
            ? `${App.config.API_BASE}/servers/${App.config.SERVER_ID}/files/content?path=${encodeURIComponent(path)}`
            : `${App.config.API_BASE}/files/content?path=${encodeURIComponent(path)}`;
        window.location.href = url;
    },

    async upload() {
        const input = App.UI.el('file-upload-input');
        if (!input?.files.length) return;

        const formData = new FormData();
        formData.append('file', input.files[0]);
        
        const btn = App.UI.el('upload-btn');
        const originalText = btn.innerHTML;
        btn.innerHTML = `<i class="fas fa-spinner fa-spin mr-2"></i>Uploading 0%...`;
        btn.disabled = true;

        const onProgress = (percent) => {
            btn.innerHTML = `<i class="fas fa-spinner fa-spin mr-2"></i>Uploading ${Math.round(percent)}%...`;
        };

        try {
            const uploadUrl = `/files/upload?path=${encodeURIComponent(App.state.currentPath)}`;
            await App.API.upload(uploadUrl, formData, onProgress);
            App.UI.toast('success', 'File uploaded');
            this.list(App.state.currentPath);
            input.value = '';
        } catch (e) { /* Handled */ } 
        finally {
             btn.innerHTML = originalText;
             btn.disabled = false;
        }
    },

    // --- Terminal ---
    openTerminal() {
        const modal = App.UI.el('terminal-modal');
        const pathEl = App.UI.el('terminal-path');
        const input = App.UI.el('terminal-input');
        
        if (!modal || !pathEl || !input) return;

        pathEl.textContent = App.state.currentPath;
        modal.classList.remove('hidden');
        
        // Transition
        setTimeout(() => {
            modal.classList.remove('opacity-0');
            modal.querySelector('div').classList.remove('scale-95');
            input.focus();
        }, 10);

        // Attach Enter listener if not already attached (simple way: remove then add, or check flag)
        // Better: handle onkeydown in HTML or re-assign here
        input.onkeydown = async (e) => {
            if (e.key === 'Enter') {
                const cmd = input.value.trim();
                if (!cmd) return;
                
                // Echo command
                this.appendTerminalOutput(`$ ${cmd}`, 'text-white font-bold');
                input.value = '';
                
                await this.executeCommand(cmd);
            }
        };
    },

    closeTerminal() {
        const modal = App.UI.el('terminal-modal');
        if (!modal) return;
        
        modal.classList.add('opacity-0');
        modal.querySelector('div').classList.add('scale-95');
        setTimeout(() => modal.classList.add('hidden'), 300);
    },

    clearTerminal() {
        const output = App.UI.el('terminal-output');
        if (output) {
            output.innerHTML = '';
            // Restore welcome message if desired, or leave empty
            this.appendTerminalOutput('# Terminal cleared', 'text-gray-500 italic');
        }
    },

    appendTerminalOutput(text, className = 'text-gray-300') {
        const output = App.UI.el('terminal-output');
        if (!output) return;

        const div = document.createElement('div');
        div.className = className + ' whitespace-pre-wrap break-all';
        div.textContent = text;
        output.appendChild(div);
        output.scrollTop = output.scrollHeight;
    },

    async executeCommand(command) {
        try {
            const response = await App.API.post('/files/exec', {
                path: App.state.currentPath,
                command: command
            });

            if (response.success) {
                if (response.data.output) {
                    this.appendTerminalOutput(response.data.output);
                } else {
                    // No output (e.g. silent command)
                }
            } else {
                 if (response.data && response.data.output) {
                     this.appendTerminalOutput(response.data.output, 'text-red-400');
                 }
                 this.appendTerminalOutput(`Error: ${response.message}`, 'text-red-500');
            }
        } catch (e) {
            this.appendTerminalOutput(`Execution failed: ${e.message}`, 'text-red-500');
        }
    }
};

window.loadFileBrowser = (path) => App.Files.list(path);
window.createDirectory = () => App.Files.create('directory');
window.createFile = () => App.Files.create('file');
window.uploadFile = () => App.Files.upload();
window.downloadFile = (path) => App.Files.download(path);
window.deleteFile = (path) => App.Files.delete(path);
window.zipFile = (path) => App.Files.zip(path);
window.unzipFile = (path) => App.Files.unzip(path);
window.openTerminal = () => App.Files.openTerminal();
window.closeTerminal = () => App.Files.closeTerminal();
window.clearTerminal = () => App.Files.clearTerminal();
