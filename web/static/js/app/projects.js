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

    renderList: function(projects) {
        const container = document.getElementById('projects-list');
        if (!container) return;

        if (!projects || projects.length === 0) {
            container.innerHTML = `
                <div class="text-center py-12">
                    <div class="bg-gray-800 rounded-full h-20 w-20 flex items-center justify-center mx-auto mb-4">
                        <i class="fas fa-project-diagram text-4xl text-gray-600"></i>
                    </div>
                    <h3 class="text-xl font-medium text-white mb-2">No Projects Yet</h3>
                    <p class="text-gray-400 mb-6">Create your first project to get started.</p>
                    <button onclick="App.Projects.showCreateModal()" class="bg-blue-600 hover:bg-blue-700 text-white px-6 py-2 rounded-lg font-medium transition-colors">
                        <i class="fas fa-plus mr-2"></i>New Project
                    </button>
                </div>
            `;
            return;
        }

        let html = `
            <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
        `;

        projects.forEach(p => {
            const statusColor = p.status === 'running' ? 'text-green-400' : 'text-gray-400';
            const typeIcon = p.type === 'node' ? 'fab fa-node-js' : 'fas fa-code';
            
            // Check if built (basic check for zero time or null)
            const isBuilt = p.last_build && !p.last_build.startsWith('0001');

            const hasWebhook = p.webhook_enabled;
            const webhookStatus = hasWebhook ? 'Active' : 'Configure';
            const webhookIcon = hasWebhook ? 'fa-toggle-on text-blue-500' : 'fa-toggle-off text-gray-500';
            const lastDeploy = p.last_deploy && !p.last_deploy.startsWith('0001') ? new Date(p.last_deploy).toLocaleString() : 'Never';

            let actionButtons = '';
            if (isBuilt) {
                 actionButtons += `
                    <button onclick="App.Projects.action('${p.id}', 'start', this)" class="flex-1 bg-green-900/20 text-green-400 hover:bg-green-900/40 py-2 rounded text-sm font-medium transition-colors">
                        <i class="fas fa-play mr-1"></i> Start
                    </button>
                    <button onclick="App.Projects.action('${p.id}', 'stop', this)" class="flex-1 bg-red-900/20 text-red-400 hover:bg-red-900/40 py-2 rounded text-sm font-medium transition-colors">
                        <i class="fas fa-stop mr-1"></i> Stop
                    </button>
                `;
            }

            actionButtons += `
                <button onclick="App.Projects.action('${p.id}', 'build', this)" class="flex-1 bg-blue-900/20 text-blue-400 hover:bg-blue-900/40 py-2 rounded text-sm font-medium transition-colors">
                    <i class="fas fa-hammer mr-1"></i> Build
                </button>
            `;

            html += `
                <div class="bg-gray-800 rounded-lg p-6 border border-gray-700 hover:border-blue-500 transition-colors flex flex-col justify-between h-[360px]">
                    <div>
                        <div class="flex justify-between items-start mb-4">
                            <div class="flex items-center space-x-3">
                                <div class="bg-gray-700 p-2 rounded-lg">
                                    <i class="${typeIcon} text-2xl text-blue-400"></i>
                                </div>
                                <div>
                                    <h3 class="font-medium text-white">${p.name}</h3>
                                    <div class="text-xs text-gray-400">${p.type} • Port ${p.port}</div>
                                </div>
                            </div>
                            <div class="${statusColor} text-sm flex items-center">
                                <i class="fas fa-circle text-[8px] mr-2"></i>
                                ${p.status.toUpperCase()}
                            </div>
                        </div>
                        
                        <div class="space-y-2 mb-4">
                            <div class="flex justify-between text-sm">
                                <span class="text-gray-400">Build Type</span>
                                <span class="text-gray-300">${p.build_type || 'N/A'}</span>
                            </div>
                            <div class="flex justify-between text-sm">
                                <span class="text-gray-400">Last Build</span>
                                <span class="text-gray-300 font-mono text-xs">${isBuilt ? new Date(p.last_build).toLocaleString() : 'Never'}</span>
                            </div>
                            <div class="flex justify-between text-sm">
                                <span class="text-gray-400">Git Webhook</span>
                                <button onclick="App.Projects.showWebhookModal('${p.id}')" class="text-xs flex items-center space-x-1 hover:text-white transition-colors">
                                    <i class="fas ${webhookIcon} text-base"></i>
                                    <span class="font-medium">${webhookStatus}</span>
                                </button>
                            </div>
                            <div class="flex justify-between text-sm">
                                <span class="text-gray-400">Last Deploy</span>
                                <span class="text-gray-300 font-mono text-xs">${lastDeploy}</span>
                            </div>
                        </div>
                    </div>
                    
                    <div>
                        <div class="flex space-x-2 mb-2">
                            ${actionButtons}
                        </div>
                        <div class="flex space-x-2">
                            <button onclick="App.Projects.showEnvModal('${p.id}')" class="flex-1 bg-gray-700/60 hover:bg-gray-600 text-gray-300 py-2 rounded text-sm font-medium transition-colors flex items-center justify-center">
                                <i class="fas fa-key mr-1.5"></i> Env Vars
                            </button>
                            <button onclick="App.Projects.openFiles('${p.id}')" class="px-3 bg-gray-700/60 text-gray-400 hover:bg-gray-600 hover:text-yellow-300 rounded text-sm transition-colors" title="Open project files">
                                <i class="fas fa-folder-open"></i>
                            </button>
                            <button onclick="App.Projects.editProject('${p.id}')" class="px-3 bg-gray-700/60 text-gray-400 hover:bg-gray-600 rounded text-sm transition-colors" title="Edit">
                                <i class="fas fa-edit"></i>
                            </button>
                            <button onclick="App.Projects.action('${p.id}', 'delete', this)" class="px-3 bg-gray-700/60 text-gray-400 hover:bg-gray-600 rounded text-sm transition-colors text-red-400 hover:text-red-300" title="Delete">
                                <i class="fas fa-trash"></i>
                            </button>
                        </div>
                    </div>
                </div>
            `;
        });

        html += `
                	<!-- Add New Card -->
                <button onclick="App.Projects.showCreateModal()" class="bg-gray-800/50 border-2 border-dashed border-gray-700 rounded-lg p-6 flex flex-col items-center justify-center hover:border-gray-500 hover:bg-gray-800 transition-all group">
                    <div class="bg-gray-800 p-4 rounded-full mb-3 group-hover:scale-110 transition-transform">
                        <i class="fas fa-plus text-xl text-gray-400 group-hover:text-white"></i>
                    </div>
                    <span class="text-gray-400 font-medium group-hover:text-white">New Project</span>
                </button>
            </div>
        `;

        container.innerHTML = html;
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
                this.load();
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
