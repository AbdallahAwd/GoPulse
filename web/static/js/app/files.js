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
                App.state.lastFilesList = response.data.files || [];
                this.suggestionsCache = this.suggestionsCache || {};
                this.suggestionsCache[App.state.currentPath] = App.state.lastFilesList;
                this.render(response.data.files);
                this.renderBreadcrumbs(App.state.currentPath);
            }
        } catch (e) {
            const tbody = App.UI.el('files-tbody');
            if (tbody) {
                tbody.innerHTML = `<tr><td colspan="5" class="px-4 py-8 text-center text-red-400">Failed to load directory</td></tr>`;
            } else {
                const container = App.UI.el('file-list-container');
                if (container) {
                    container.innerHTML = `<div class="text-center text-red-400 py-12">Failed to load directory</div>`;
                }
            }
        } finally {
            if (overlay) overlay.classList.add('hidden');
        }
    },

    getFileIcon(name, isDir) {
        if (isDir) {
            return {
                icon: 'fa-folder',
                color: 'text-yellow-400',
                bgColor: 'bg-yellow-400/10'
            };
        }
        
        const ext = name.split('.').pop().toLowerCase();
        
        const fileTypes = {
            // Archives
            'zip': { icon: 'fa-file-archive', color: 'text-orange-400', bgColor: 'bg-orange-400/10' },
            'tar': { icon: 'fa-file-archive', color: 'text-orange-400', bgColor: 'bg-orange-400/10' },
            'gz': { icon: 'fa-file-archive', color: 'text-orange-400', bgColor: 'bg-orange-400/10' },
            'rar': { icon: 'fa-file-archive', color: 'text-orange-400', bgColor: 'bg-orange-400/10' },
            
            // Code
            'js': { icon: 'fa-file-code', color: 'text-yellow-300', bgColor: 'bg-yellow-300/10' },
            'ts': { icon: 'fa-file-code', color: 'text-blue-400', bgColor: 'bg-blue-400/10' },
            'jsx': { icon: 'fa-file-code', color: 'text-cyan-400', bgColor: 'bg-cyan-400/10' },
            'tsx': { icon: 'fa-file-code', color: 'text-blue-500', bgColor: 'bg-blue-500/10' },
            'html': { icon: 'fa-file-code', color: 'text-orange-500', bgColor: 'bg-orange-500/10' },
            'css': { icon: 'fa-file-code', color: 'text-blue-400', bgColor: 'bg-blue-400/10' },
            'scss': { icon: 'fa-file-code', color: 'text-pink-400', bgColor: 'bg-pink-400/10' },
            'php': { icon: 'fa-file-code', color: 'text-indigo-400', bgColor: 'bg-indigo-400/10' },
            'go': { icon: 'fa-file-code', color: 'text-cyan-400', bgColor: 'bg-cyan-400/10' },
            'py': { icon: 'fa-file-code', color: 'text-green-400', bgColor: 'bg-green-400/10' },
            'rb': { icon: 'fa-file-code', color: 'text-red-500', bgColor: 'bg-red-500/10' },
            'rs': { icon: 'fa-file-code', color: 'text-amber-600', bgColor: 'bg-amber-600/10' },
            
            // Scripts
            'sh': { icon: 'fa-terminal', color: 'text-emerald-400', bgColor: 'bg-emerald-400/10' },
            'bash': { icon: 'fa-terminal', color: 'text-emerald-400', bgColor: 'bg-emerald-400/10' },
            
            // Config/Data
            'json': { icon: 'fa-file-signature', color: 'text-purple-400', bgColor: 'bg-purple-400/10' },
            'yaml': { icon: 'fa-file-signature', color: 'text-purple-400', bgColor: 'bg-purple-400/10' },
            'yml': { icon: 'fa-file-signature', color: 'text-purple-400', bgColor: 'bg-purple-400/10' },
            'toml': { icon: 'fa-file-signature', color: 'text-purple-400', bgColor: 'bg-purple-400/10' },
            'lock': { icon: 'fa-lock', color: 'text-amber-500', bgColor: 'bg-amber-500/10' },
            'env': { icon: 'fa-sliders-h', color: 'text-pink-400', bgColor: 'bg-pink-400/10' },
            
            // Media
            'png': { icon: 'fa-file-image', color: 'text-emerald-400', bgColor: 'bg-emerald-400/10' },
            'jpg': { icon: 'fa-file-image', color: 'text-emerald-400', bgColor: 'bg-emerald-400/10' },
            'jpeg': { icon: 'fa-file-image', color: 'text-emerald-400', bgColor: 'bg-emerald-400/10' },
            'gif': { icon: 'fa-file-image', color: 'text-emerald-400', bgColor: 'bg-emerald-400/10' },
            'svg': { icon: 'fa-file-image', color: 'text-emerald-400', bgColor: 'bg-emerald-400/10' },
            'ico': { icon: 'fa-file-image', color: 'text-emerald-400', bgColor: 'bg-emerald-400/10' },
            
            // Docs
            'md': { icon: 'fa-file-alt', color: 'text-teal-400', bgColor: 'bg-teal-400/10' },
            'txt': { icon: 'fa-file-alt', color: 'text-slate-300', bgColor: 'bg-slate-300/10' },
            'pdf': { icon: 'fa-file-pdf', color: 'text-rose-500', bgColor: 'bg-rose-500/10' },
            
            // Database
            'sql': { icon: 'fa-database', color: 'text-blue-500', bgColor: 'bg-blue-500/10' },
            'db': { icon: 'fa-database', color: 'text-blue-500', bgColor: 'bg-blue-500/10' },
            'sqlite': { icon: 'fa-database', color: 'text-blue-500', bgColor: 'bg-blue-500/10' }
        };

        if (name.startsWith('.env')) {
            return fileTypes['env'];
        }

        return fileTypes[ext] || {
            icon: 'fa-file',
            color: 'text-slate-400',
            bgColor: 'bg-slate-400/10'
        };
    },

    setViewMode(mode) {
        this.viewMode = mode;
        localStorage.setItem('gopulse_files_view_mode', mode);
        
        // Update toggle UI
        const btnList = App.UI.el('btn-view-list');
        const btnGrid = App.UI.el('btn-view-grid');
        
        if (btnList && btnGrid) {
            if (mode === 'list') {
                btnList.className = 'px-3 py-1.5 rounded-md text-xs font-semibold flex items-center space-x-1.5 transition-all text-white bg-blue-600 shadow';
                btnGrid.className = 'px-3 py-1.5 rounded-md text-xs font-semibold flex items-center space-x-1.5 transition-all text-gray-400 hover:text-white';
            } else {
                btnGrid.className = 'px-3 py-1.5 rounded-md text-xs font-semibold flex items-center space-x-1.5 transition-all text-white bg-blue-600 shadow';
                btnList.className = 'px-3 py-1.5 rounded-md text-xs font-semibold flex items-center space-x-1.5 transition-all text-gray-400 hover:text-white';
            }
        }
        
        // Re-render
        if (App.state.lastFilesList) {
            this.render(App.state.lastFilesList);
        }
    },

    render(files) {
        const container = App.UI.el('file-list-container');
        if (!container) return;

        const fileList = files || []; // Handle null/undefined files
        const path = App.state.currentPath;
        
        // Initialize view mode
        if (!this.viewMode) {
            this.viewMode = localStorage.getItem('gopulse_files_view_mode') || 'list';
        }

        const loadingOverlay = `<div id="file-loading-overlay"
                    class="absolute inset-0 bg-black/40 backdrop-blur-sm flex items-center justify-center z-10 hidden transition-all duration-300">
                    <div class="flex flex-col items-center p-6 rounded-2xl bg-gray-900/95 border border-gray-800/80 shadow-2xl text-center">
                        <i class="fas fa-circle-notch fa-spin text-3xl mb-3.5 text-blue-500"></i>
                        <p class="text-white font-medium text-sm">Loading contents...</p>
                    </div>
                </div>`;

        // Empty state HTML
        const emptyStateHtml = `<div class="text-center text-gray-500 py-16 flex flex-col items-center justify-center bg-gray-900/10 rounded-xl border border-dashed border-gray-800/50 p-8 w-full">
                <div class="w-16 h-16 rounded-full bg-gray-800/30 flex items-center justify-center mb-4 text-gray-600 border border-gray-850">
                    <i class="fas fa-folder-open text-2xl"></i>
                </div>
                <h3 class="text-white font-semibold text-base">This folder is empty</h3>
                <p class="text-xs text-gray-500 mt-1 max-w-xs">There are no files or directories inside this path. You can create one or upload a file using the toolbar.</p>
           </div>`;

        if (this.viewMode === 'grid') {
            // Render Grid View
            let parentGridItem = '';
            if (path !== '/' && path !== '') {
                const parentPath = path.substring(0, path.lastIndexOf('/')) || '/';
                parentGridItem = `
                    <div class="relative bg-gray-900/20 hover:bg-gray-700/10 border border-gray-800/40 hover:border-gray-700/60 rounded-xl p-4 transition-all duration-200 group flex flex-col items-center justify-center text-center cursor-pointer min-h-[140px]"
                        onclick="App.Files.list('${parentPath}')">
                        <div class="w-16 h-16 rounded-2xl flex items-center justify-center bg-gray-800/50 text-gray-400 group-hover:text-blue-400 mb-3 transition-transform duration-300 group-hover:scale-105">
                            <i class="fas fa-level-up-alt text-2xl"></i>
                        </div>
                        <span class="text-gray-400 group-hover:text-blue-400 transition-colors font-semibold text-xs">
                            Go Back (..)
                        </span>
                    </div>`;
            }

            const gridContent = fileList.map(file => {
                const fileMeta = this.getFileIcon(file.name, file.is_dir);
                const size = file.is_dir ? 'Folder' : App.UI.formatBytes(file.size);
                const fullPath = path === '/' ? `/${file.name}` : `${path}/${file.name}`;
                
                return `
                <div class="relative bg-gray-900/30 hover:bg-gray-700/20 border border-gray-800/60 hover:border-gray-700/80 rounded-xl p-4 transition-all duration-200 group flex flex-col items-center text-center cursor-pointer select-none file-grid-item"
                    data-path="${fullPath}" data-type="${file.is_dir ? 'dir' : 'file'}" data-name="${file.name}"
                    onclick="event.preventDefault(); ${file.is_dir ? 
                        `App.Files.list('${fullPath}')` : 
                        `App.Editor.open('${fullPath}')`}">
                    
                    <!-- Quick Actions Hover Overlay -->
                    <div class="absolute top-2 right-2 flex items-center space-x-1 opacity-0 group-hover:opacity-100 transition-all duration-200 z-10">
                        ${!file.is_dir ? `
                        <button onclick="event.stopPropagation(); App.Files.download('${fullPath}')" 
                            class="w-7 h-7 flex items-center justify-center bg-gray-950/80 hover:bg-emerald-950/60 border border-gray-800 hover:border-emerald-500/40 text-gray-400 hover:text-emerald-400 rounded-full transition-all" title="Download">
                            <i class="fas fa-download text-[10px]"></i>
                        </button>` : ''}
                        ${file.is_dir ? `
                        <button onclick="event.stopPropagation(); App.Files.zip('${fullPath}')" 
                            class="w-7 h-7 flex items-center justify-center bg-gray-950/80 hover:bg-amber-950/60 border border-gray-800 hover:border-amber-500/40 text-gray-400 hover:text-amber-400 rounded-full transition-all" title="Zip Folder">
                            <i class="fas fa-file-zipper text-[10px]"></i>
                        </button>` : ''}
                        <button onclick="event.stopPropagation(); App.Files.delete('${fullPath}')" 
                            class="w-7 h-7 flex items-center justify-center bg-gray-950/80 hover:bg-red-950/60 border border-gray-800 hover:border-red-500/40 text-gray-400 hover:text-red-400 rounded-full transition-all" title="Delete">
                            <i class="fas fa-trash-alt text-[10px]"></i>
                        </button>
                    </div>

                    <!-- Icon -->
                    <div class="w-16 h-16 rounded-2xl flex items-center justify-center ${fileMeta.bgColor} ${fileMeta.color} mb-3.5 transition-transform duration-300 group-hover:scale-110 shadow-lg">
                        <i class="fas ${fileMeta.icon} text-3xl"></i>
                    </div>

                    <!-- Details -->
                    <span class="text-gray-200 group-hover:text-blue-400 transition-colors font-medium text-xs break-all line-clamp-2 px-1 w-full">
                        ${file.name}
                    </span>
                    <span class="text-[10px] text-gray-500 font-mono mt-1">
                        ${size}
                    </span>
                </div>`;
            }).join('');

            const gridHtml = (fileList.length === 0 && !parentGridItem)
                ? emptyStateHtml
                : `<div class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 gap-4 p-1">
                        ${parentGridItem}
                        ${gridContent}
                   </div>`;

            container.innerHTML = loadingOverlay + gridHtml;
        } else {
            // Render List View (Sleek Table)
            let parentRow = '';
            if (path !== '/' && path !== '') {
                const parentPath = path.substring(0, path.lastIndexOf('/')) || '/';
                parentRow = `
                    <tr class="border-b border-gray-800/40 hover:bg-gray-700/20 transition-colors group cursor-pointer"
                        onclick="App.Files.list('${parentPath}')">
                        <td class="py-3 px-4 text-center">
                            <div class="w-9 h-9 rounded-lg flex items-center justify-center bg-gray-800/40 text-gray-500">
                                <i class="fas fa-level-up-alt text-sm"></i>
                            </div>
                        </td>
                        <td class="py-3 px-4 font-semibold text-gray-400 group-hover:text-blue-400 transition-colors">..</td>
                        <td class="py-3 px-4 text-gray-500 font-mono text-xs">-</td>
                        <td class="py-3 px-4 text-gray-500 text-xs">-</td>
                        <td class="py-3 px-4 text-right"></td>
                    </tr>`;
            }

            const tableContent = fileList.map(file => {
                const fileMeta = this.getFileIcon(file.name, file.is_dir);
                const size = file.is_dir ? '-' : App.UI.formatBytes(file.size);
                const date = new Date(file.mod_time).toLocaleString();
                const fullPath = path === '/' ? `/${file.name}` : `${path}/${file.name}`;
                
                return `
                <tr class="border-b border-gray-800/40 hover:bg-gray-700/20 transition-colors group cursor-pointer file-item"
                    data-path="${fullPath}" data-type="${file.is_dir ? 'dir' : 'file'}" data-name="${file.name}">
                    <td class="py-2.5 px-4 text-center">
                        <div class="w-9 h-9 rounded-lg flex items-center justify-center ${fileMeta.bgColor} ${fileMeta.color} transition-transform group-hover:scale-105">
                            <i class="fas ${fileMeta.icon} text-base"></i>
                        </div>
                    </td>
                    <td class="py-2.5 px-4 font-semibold text-gray-200 group-hover:text-blue-400 transition-colors">
                        <a href="#" onclick="event.preventDefault(); ${file.is_dir ? 
                            `App.Files.list('${fullPath}')` : 
                            `App.Editor.open('${fullPath}')`}" class="block py-1">
                            ${file.name}
                        </a>
                    </td>
                    <td class="py-2.5 px-4 text-gray-400 font-mono text-xs">${size}</td>
                    <td class="py-2.5 px-4 text-gray-400 text-xs">${date}</td>
                    <td class="py-2.5 px-4 text-right">
                        <div class="flex items-center justify-end space-x-1.5 opacity-0 group-hover:opacity-100 transition-all duration-200">
                            ${!file.is_dir ? `
                            <button onclick="event.stopPropagation(); App.Files.download('${fullPath}')" 
                                class="p-1.5 bg-gray-900/80 hover:bg-emerald-950/40 border border-gray-800 hover:border-emerald-500/30 text-gray-400 hover:text-emerald-400 rounded-lg transition-all" title="Download">
                                <i class="fas fa-download text-xs"></i>
                            </button>` : ''}
                            ${file.is_dir ? `
                            <button onclick="event.stopPropagation(); App.Files.zip('${fullPath}')" 
                                class="p-1.5 bg-gray-900/80 hover:bg-amber-950/40 border border-gray-800 hover:border-amber-500/30 text-gray-400 hover:text-amber-400 rounded-lg transition-all" title="Zip Folder">
                                <i class="fas fa-file-zipper text-xs"></i>
                            </button>` : ''}
                            ${!file.is_dir && file.name.endsWith('.zip') ? `
                            <button onclick="event.stopPropagation(); App.Files.unzip('${fullPath}')" 
                                class="p-1.5 bg-gray-900/80 hover:bg-blue-950/40 border border-gray-800 hover:border-blue-500/30 text-gray-400 hover:text-blue-400 rounded-lg transition-all" title="Unzip">
                                <i class="fas fa-box-open text-xs"></i>
                            </button>` : ''}
                            <button onclick="event.stopPropagation(); App.Files.delete('${fullPath}')" 
                                class="p-1.5 bg-gray-900/80 hover:bg-red-950/40 border border-gray-800 hover:border-red-500/30 text-gray-400 hover:text-red-400 rounded-lg transition-all" title="Delete">
                                <i class="fas fa-trash-alt text-xs"></i>
                            </button>
                        </div>
                    </td>
                </tr>`;
            }).join('');

            const tableHtml = (fileList.length === 0 && !parentRow)
                ? emptyStateHtml
                : `<table class="w-full text-left border-collapse">
                    <thead>
                        <tr class="text-gray-400 border-b border-gray-800/85 text-xs uppercase tracking-wider bg-gray-900/40">
                            <th class="py-3 px-4 font-semibold w-16 text-center">Type</th>
                            <th class="py-3 px-4 font-semibold">Name</th>
                            <th class="py-3 px-4 font-semibold w-32">Size</th>
                            <th class="py-3 px-4 font-semibold w-52">Modified</th>
                            <th class="py-3 px-4 font-semibold w-40 text-right">Actions</th>
                        </tr>
                    </thead>
                    <tbody class="text-gray-300 text-sm">
                        ${parentRow}
                        ${tableContent}
                    </tbody>
                </table>`;

            container.innerHTML = loadingOverlay + tableHtml;
        }

        this.attachContextMenu();
    },

    renderBreadcrumbs(path) {
        const input = App.UI.el('file-path-input');
        if (input) {
            input.value = path;
            input.setAttribute('value', path);
        }

        const display = App.UI.el('file-breadcrumbs-display');
        if (!display) return;

        // Ensure path starts with / and has no trailing / (unless it's just /)
        let normalizedPath = path.startsWith('/') ? path : '/' + path;
        if (normalizedPath.endsWith('/') && normalizedPath.length > 1) {
            normalizedPath = normalizedPath.slice(0, -1);
        }

        const parts = normalizedPath.split('/').filter(p => p !== '');
        
        let html = `
            <button onclick="App.Files.navigateToSegment('/', event)" 
                class="flex items-center text-gray-400 hover:text-blue-400 font-semibold transition-colors focus:outline-none shrink-0">
                <i class="fas fa-hdd mr-1.5"></i>root
            </button>
        `;

        let currentBuildPath = '';
        parts.forEach((part, index) => {
            currentBuildPath += '/' + part;
            const isLast = index === parts.length - 1;
            
            html += `
                <span class="text-gray-650 px-1 select-none shrink-0 font-mono text-[10px]">/</span>
                <button onclick="App.Files.navigateToSegment('${currentBuildPath}', event)" 
                    class="font-semibold transition-colors focus:outline-none truncate max-w-[120px] ${
                        isLast 
                        ? 'text-white font-bold cursor-default pointer-events-none' 
                        : 'text-gray-400 hover:text-blue-400'
                    }">
                    ${part}
                </button>
            `;
        });

        display.innerHTML = html;
    },

    navigateToSegment(path, event) {
        if (event) event.stopPropagation(); // Prevent focusing input
        this.list(path);
    },

    focusPathInput(event) {
        // Only trigger if we didn't click on a button inside display
        if (event && event.target.closest('button')) return;

        const display = App.UI.el('file-breadcrumbs-display');
        const form = App.UI.el('file-path-form');
        const input = App.UI.el('file-path-input');

        if (!display || !form || !input) return;

        display.classList.add('hidden');
        form.classList.remove('hidden');
        input.setAttribute('readonly', 'readonly');
        input.focus({ preventScroll: true });
        input.select();
        requestAnimationFrame(() => {
            input.removeAttribute('readonly');
        });
    },

    blurPathInput() {
        // Set a slight timeout so clicking navigation buttons inside breadcrumbs works first
        setTimeout(() => {
            const display = App.UI.el('file-breadcrumbs-display');
            const form = App.UI.el('file-path-form');
            if (!display || !form) return;

            form.classList.add('hidden');
            display.classList.remove('hidden');
            this.closeSuggestions();
        }, 150);
    },

    submitPath(event) {
        if (event) event.preventDefault();
        const input = App.UI.el('file-path-input');
        if (!input) return;

        const path = input.value.trim() || '/';
        this.list(path);
        
        const form = App.UI.el('file-path-form');
        const display = App.UI.el('file-breadcrumbs-display');
        if (form && display) {
            form.classList.add('hidden');
            display.classList.remove('hidden');
        }
        this.closeSuggestions();
    },

    attachContextMenu() {
        const menu = App.UI.el('file-context-menu');
        if (!menu) return;

        // Hide menu on document click
        const hideMenu = () => menu.classList.add('hidden');
        document.removeEventListener('click', hideMenu);
        document.addEventListener('click', hideMenu);

        // Hide menu on esc key
        const escMenu = (e) => {
            if (e.key === 'Escape') menu.classList.add('hidden');
        };
        document.removeEventListener('keydown', escMenu);
        document.addEventListener('keydown', escMenu);

        // Find all file items
        const items = document.querySelectorAll('.file-item, .file-grid-item');
        items.forEach(item => {
            item.addEventListener('contextmenu', (e) => {
                e.preventDefault();
                e.stopPropagation();

                const path = item.getAttribute('data-path');
                const type = item.getAttribute('data-type');
                const name = item.getAttribute('data-name');

                // Position menu at cursor
                menu.style.left = `${e.clientX}px`;
                menu.style.top = `${e.clientY}px`;
                menu.classList.remove('hidden');

                // Configure menu actions
                const editAction = menu.querySelector('[data-action="edit"]');
                const downloadAction = menu.querySelector('[data-action="download"]');
                const compressAction = menu.querySelector('[data-action="compress"]');
                const deleteAction = menu.querySelector('[data-action="delete"]');

                // Toggle visibility of actions based on type
                if (editAction) {
                    if (type === 'dir') {
                        editAction.classList.add('hidden');
                    } else {
                        editAction.classList.remove('hidden');
                        editAction.onclick = (event) => {
                            event.preventDefault();
                            App.Editor.open(path);
                        };
                    }
                }

                if (downloadAction) {
                    if (type === 'dir') {
                        downloadAction.classList.add('hidden');
                    } else {
                        downloadAction.classList.remove('hidden');
                        downloadAction.onclick = (event) => {
                            event.preventDefault();
                            App.Files.download(path);
                        };
                    }
                }

                if (compressAction) {
                    // For dir or zip decompress
                    if (type === 'dir') {
                        compressAction.innerHTML = '<i class="fas fa-file-archive w-5 text-yellow-400"></i> Compress (Zip)';
                        compressAction.onclick = (event) => {
                            event.preventDefault();
                            App.Files.zip(path);
                        };
                    } else if (name.endsWith('.zip')) {
                        compressAction.innerHTML = '<i class="fas fa-box-open w-5 text-blue-400"></i> Decompress (Unzip)';
                        compressAction.onclick = (event) => {
                            event.preventDefault();
                            App.Files.unzip(path);
                        };
                    } else {
                        compressAction.innerHTML = '<i class="fas fa-file-archive w-5 text-yellow-400"></i> Compress (Zip)';
                        compressAction.onclick = (event) => {
                            event.preventDefault();
                            App.Files.zip(path);
                        };
                    }
                }

                if (deleteAction) {
                    deleteAction.onclick = (event) => {
                        event.preventDefault();
                        App.Files.delete(path);
                    };
                }
            });
        });
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
            this.invalidatePathSuggestions(App.state.currentPath);
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
        if (!await App.UI.confirm(`Delete ${path}?`)) return;
        try {
            await App.API.post('/files/action', { action: 'delete', path });
            App.UI.toast('success', 'Item deleted');
            this.invalidatePathSuggestions(App.state.currentPath);
            this.list(App.state.currentPath);
        } catch (e) { /* Handled */ }
    },

    async zip(path) {
        try {
            const btn = document.querySelector(`button[onclick="App.Files.zip('${path}')"] i`);
            if (btn) btn.className = 'fas fa-spinner fa-spin';
            
            await App.API.post('/files/action', { action: 'compress', path });
            App.UI.toast('success', 'Folder zipped successfully');
            this.invalidatePathSuggestions(App.state.currentPath);
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
            this.invalidatePathSuggestions(App.state.currentPath);
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
            this.invalidatePathSuggestions(App.state.currentPath);
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
    },

    init() {
        if (this.initialized) return;
        this.initialized = true;
        this.suggestionsCache = {};
        this.activeSuggestionIndex = -1;
        this.currentSuggestions = [];
        this.suggestionTimeout = null;
        this.suggestionRequestSeq = 0;
        this.initSuggestions();
    },

    initSuggestions() {
        const input = App.UI.el('file-path-input');
        const suggestionsEl = App.UI.el('file-path-suggestions');
        if (!input || !suggestionsEl) return;
        if (input.dataset.suggestionsBound === 'true') return;
        input.dataset.suggestionsBound = 'true';
        input.setAttribute('autocomplete', 'new-password');
        input.setAttribute('name', `gopulse-file-path-${Date.now()}`);
        input.setAttribute('autocapitalize', 'off');
        input.setAttribute('autocorrect', 'off');
        input.setAttribute('spellcheck', 'false');

        input.addEventListener('input', () => {
            this.handlePathInput();
        });

        input.addEventListener('focus', () => {
            this.handlePathInput();
        });

        input.addEventListener('keydown', (e) => {
            this.handleSuggestionsKeydown(e);
        });

        suggestionsEl.addEventListener('mousedown', (e) => {
            e.preventDefault();
            e.stopPropagation();
        });

        suggestionsEl.addEventListener('click', (e) => {
            const item = e.target.closest('.suggestion-item');
            if (!item) return;

            e.preventDefault();
            e.stopPropagation();

            const index = Number(item.dataset.index);
            const suggestion = this.currentSuggestions[index];
            if (suggestion) {
                this.selectSuggestion(suggestion.path, suggestion.isDir);
            }
        });

        document.addEventListener('click', (e) => {
            if (!e.target.closest('#file-breadcrumbs-container')) {
                this.closeSuggestions();
            }
        });
    },

    invalidatePathSuggestions(path = App.state.currentPath) {
        if (!this.suggestionsCache) return;

        delete this.suggestionsCache[path || '/'];
        const parentPath = this.getPathSuggestionState(path || '/').parentPath;
        delete this.suggestionsCache[parentPath];
    },

    getPathSuggestionState(value) {
        let fullPath = (value || '').trim();
        if (fullPath === '') fullPath = '/';

        if (!fullPath.startsWith('/')) {
            const base = App.state.currentPath || '/';
            fullPath = base === '/' ? `/${fullPath}` : `${base.replace(/\/$/, '')}/${fullPath}`;
        }

        fullPath = fullPath.replace(/\/+/g, '/');

        const lastSlash = fullPath.lastIndexOf('/');
        const parentPath = lastSlash <= 0 ? '/' : fullPath.substring(0, lastSlash);
        const prefix = fullPath.substring(lastSlash + 1);

        return { fullPath, parentPath, prefix };
    },

    handlePathInput() {
        const input = App.UI.el('file-path-input');
        if (!input) return;

        const form = App.UI.el('file-path-form');
        if (form && form.classList.contains('hidden')) return;

        const { parentPath, prefix } = this.getPathSuggestionState(input.value);
        const normalizedPrefix = prefix.toLowerCase();

        if (this.suggestionsCache && this.suggestionsCache[parentPath]) {
            this.showMatchingSuggestions(this.suggestionsCache[parentPath], normalizedPrefix, parentPath);
            return;
        }

        if (parentPath === App.state.currentPath && App.state.lastFilesList) {
            this.suggestionsCache = this.suggestionsCache || {};
            this.suggestionsCache[parentPath] = App.state.lastFilesList;
            this.showMatchingSuggestions(App.state.lastFilesList, normalizedPrefix, parentPath);
            return;
        }

        clearTimeout(this.suggestionTimeout);
        const requestSeq = ++this.suggestionRequestSeq;
        this.suggestionTimeout = setTimeout(async () => {
            try {
                const response = await App.API.get(`/files/list?path=${encodeURIComponent(parentPath)}`);
                if (requestSeq !== this.suggestionRequestSeq) return;

                if (response.success && response.data && response.data.files) {
                    const files = response.data.files;
                    this.suggestionsCache = this.suggestionsCache || {};
                    this.suggestionsCache[parentPath] = files;

                    const currentState = this.getPathSuggestionState(input.value);
                    if (currentState.parentPath === parentPath) {
                        this.showMatchingSuggestions(files, currentState.prefix.toLowerCase(), parentPath);
                    }
                }
            } catch (e) {
                this.closeSuggestions();
            }
        }, 120);
    },

    showMatchingSuggestions(files, prefix, parentPath) {
        const suggestionsEl = App.UI.el('file-path-suggestions');
        if (!suggestionsEl) return;

        const matches = (files || []).filter(f => {
            if (f.name === '.' || f.name === '..') return false;
            return f.name.toLowerCase().startsWith(prefix);
        }).sort((a, b) => {
            if (a.is_dir !== b.is_dir) return a.is_dir ? -1 : 1;
            return a.name.localeCompare(b.name);
        }).slice(0, 15);

        if (matches.length === 0) {
            this.closeSuggestions();
            return;
        }

        this.currentSuggestions = matches.map(f => {
            const sep = parentPath.endsWith('/') ? '' : '/';
            return {
                name: f.name,
                isDir: !!f.is_dir,
                path: parentPath + sep + f.name + (f.is_dir ? '/' : '')
            };
        });
        this.activeSuggestionIndex = -1;

        suggestionsEl.innerHTML = '';
        const fragment = document.createDocumentFragment();

        this.currentSuggestions.forEach((suggestion, idx) => {
            const fileMeta = this.getFileIcon(suggestion.name, suggestion.isDir);
            const name = suggestion.name;
            const typedLen = prefix.length;
            const matchPart = name.substring(0, typedLen);
            const restPart = name.substring(typedLen);

            const item = document.createElement('div');
            item.className = 'suggestion-item flex items-center px-4 py-2 hover:bg-gray-800/80 cursor-pointer text-sm font-mono transition-all border-b border-gray-800/40 last:border-0';
            item.dataset.index = String(idx);

            const iconWrap = document.createElement('div');
            iconWrap.className = `w-6 h-6 flex items-center justify-center rounded ${fileMeta.bgColor} ${fileMeta.color} mr-3`;
            const icon = document.createElement('i');
            icon.className = `fas ${fileMeta.icon} text-xs`;
            iconWrap.appendChild(icon);

            const label = document.createElement('span');
            label.className = 'flex-1 truncate font-medium';

            if (typedLen > 0) {
                const typed = document.createElement('span');
                typed.className = 'text-blue-400 font-bold';
                typed.textContent = matchPart;

                const rest = document.createElement('span');
                rest.className = 'text-gray-300';
                rest.textContent = restPart;

                label.appendChild(typed);
                label.appendChild(rest);
            } else {
                label.textContent = name;
                label.classList.add('text-gray-300');
            }

            const badge = document.createElement('span');
            badge.className = suggestion.isDir
                ? 'text-[10px] bg-amber-500/10 text-amber-400 px-1.5 py-0.5 rounded font-sans uppercase font-bold tracking-wider'
                : 'text-[10px] bg-blue-500/10 text-blue-400 px-1.5 py-0.5 rounded font-sans uppercase font-bold tracking-wider';
            badge.textContent = suggestion.isDir ? 'dir' : 'file';

            item.appendChild(iconWrap);
            item.appendChild(label);
            item.appendChild(badge);
            fragment.appendChild(item);
        });

        suggestionsEl.appendChild(fragment);
        suggestionsEl.classList.remove('hidden');
    },

    selectSuggestion(completedPath, isDir, event) {
        if (event) {
            event.preventDefault();
            event.stopPropagation();
        }
        const input = App.UI.el('file-path-input');
        if (!input) return;

        input.value = completedPath;

        if (isDir) {
            input.focus();
            input.setSelectionRange(completedPath.length, completedPath.length);
            this.handlePathInput();
        } else {
            this.closeSuggestions();
            const form = App.UI.el('file-path-form');
            const display = App.UI.el('file-breadcrumbs-display');
            if (form && display) {
                form.classList.add('hidden');
                display.classList.remove('hidden');
            }
            App.Editor.open(completedPath);
        }
    },

    handleSuggestionsKeydown(e) {
        const suggestionsEl = App.UI.el('file-path-suggestions');
        if (!suggestionsEl || suggestionsEl.classList.contains('hidden')) {
            return;
        }

        const items = suggestionsEl.querySelectorAll('.suggestion-item');
        if (items.length === 0) return;

        if (e.key === 'ArrowDown') {
            e.preventDefault();
            this.activeSuggestionIndex = (this.activeSuggestionIndex + 1) % items.length;
            this.highlightSuggestion(items);
        } else if (e.key === 'ArrowUp') {
            e.preventDefault();
            this.activeSuggestionIndex = (this.activeSuggestionIndex - 1 + items.length) % items.length;
            this.highlightSuggestion(items);
        } else if (e.key === 'Tab') {
            e.preventDefault();
            const index = this.activeSuggestionIndex >= 0 ? this.activeSuggestionIndex : 0;
            const item = items[index];
            const suggestion = this.currentSuggestions[index];
            if (item && suggestion) {
                this.selectSuggestion(suggestion.path, suggestion.isDir);
            }
        } else if (e.key === 'Enter') {
            if (this.activeSuggestionIndex >= 0) {
                e.preventDefault();
                e.stopPropagation();
                const item = items[this.activeSuggestionIndex];
                const suggestion = this.currentSuggestions[this.activeSuggestionIndex];
                if (item && suggestion) {
                    this.selectSuggestion(suggestion.path, suggestion.isDir);
                }
            }
        } else if (e.key === 'Escape') {
            e.preventDefault();
            this.closeSuggestions();
        }
    },

    highlightSuggestion(items) {
        items.forEach((item, idx) => {
            if (idx === this.activeSuggestionIndex) {
                item.classList.add('bg-blue-600/20', 'border-l-2', 'border-blue-500');
                item.scrollIntoView({ block: 'nearest' });
            } else {
                item.classList.remove('bg-blue-600/20', 'border-l-2', 'border-blue-500');
            }
        });
    },

    closeSuggestions() {
        clearTimeout(this.suggestionTimeout);
        const suggestionsEl = App.UI.el('file-path-suggestions');
        if (suggestionsEl) {
            suggestionsEl.classList.add('hidden');
            suggestionsEl.innerHTML = '';
        }
        this.activeSuggestionIndex = -1;
        this.currentSuggestions = [];
    }
};

// Initialize when DOM is ready
document.addEventListener('DOMContentLoaded', () => {
    App.Files.init();
});

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
