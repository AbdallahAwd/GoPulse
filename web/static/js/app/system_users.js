/**
 * GoPulse App - Server System Users Module
 * Manage remote Linux system users and groups
 */

App.SystemUsers = {
    async load() {
        const list = App.UI.el('linux-users-list');
        if (!list) return;

        list.innerHTML = `
            <div class="col-span-full text-center py-12 text-gray-500">
                <i class="fas fa-circle-notch fa-spin text-3xl text-purple-500 mb-4"></i>
                <p>Reading passwd file on remote server...</p>
            </div>
        `;

        try {
            const res = await App.API.get('/system/users');
            if (res.success && res.data) {
                // Show users, sort non-system users to the top
                const sortedUsers = res.data.sort((a, b) => {
                    if (a.is_system && !b.is_system) return 1;
                    if (!a.is_system && b.is_system) return -1;
                    return a.username.localeCompare(b.username);
                });

                list.innerHTML = sortedUsers.map(user => {
                    const isSystem = user.is_system;
                    const badgeColor = isSystem 
                        ? 'bg-gray-500/10 text-gray-400 border border-gray-500/20' 
                        : 'bg-purple-500/10 text-purple-400 border border-purple-500/20';
                    const badgeText = isSystem ? 'System Account' : 'User Account';

                    // Sudo status representation
                    const isRoot = user.uid === '0';

                    return `
                        <div class="bg-gray-800/40 backdrop-blur-md border border-gray-700/40 rounded-xl p-5 shadow-lg relative group hover:border-purple-500/20 transition-all duration-300">
                            <div class="flex justify-between items-start mb-3">
                                <div class="flex items-center space-x-3">
                                    <div class="p-2.5 rounded-lg bg-gray-900/60 text-purple-400 border border-purple-500/15">
                                        <i class="fas fa-user-gear text-lg"></i>
                                    </div>
                                    <div>
                                        <h4 class="text-base font-bold text-white flex items-center space-x-2">
                                            <span>${user.username}</span>
                                            ${isRoot ? '<span class="px-1.5 py-0.5 text-[8px] bg-red-500/20 text-red-400 border border-red-500/30 rounded font-black">ROOT</span>' : ''}
                                        </h4>
                                        <span class="inline-block px-1.5 py-0.5 rounded text-[9px] font-semibold uppercase ${badgeColor} mt-1">
                                            ${badgeText}
                                        </span>
                                    </div>
                                </div>
                                
                                ${(!isSystem && !isRoot) ? `
                                <button onclick="App.SystemUsers.deleteUser('${user.username}')" 
                                    class="text-gray-500 hover:text-red-500 p-2 rounded-lg hover:bg-red-500/10 border border-transparent hover:border-red-500/25 transition-all"
                                    title="Delete User">
                                    <i class="fas fa-trash-alt text-xs"></i>
                                </button>
                                ` : ''}
                            </div>

                            <div class="space-y-2.5 border-t border-gray-700/30 pt-3 mt-1.5 text-xs text-gray-400 font-medium">
                                <div class="flex justify-between">
                                    <span class="text-gray-500 font-semibold">UID / GID</span>
                                    <span class="font-mono text-white">${user.uid} / ${user.gid}</span>
                                </div>
                                <div class="flex justify-between">
                                    <span class="text-gray-500 font-semibold">Home Directory</span>
                                    <span class="font-mono text-white truncate max-w-[180px]" title="${user.home_dir}">${user.home_dir}</span>
                                </div>
                                <div class="flex justify-between">
                                    <span class="text-gray-500 font-semibold">Shell</span>
                                    <span class="font-mono text-white">${user.shell}</span>
                                </div>
                                ${user.comment ? `
                                <div class="flex justify-between border-t border-gray-700/20 pt-2 mt-1">
                                    <span class="text-gray-500 font-semibold">Comment</span>
                                    <span class="text-gray-300 italic truncate max-w-[180px]" title="${user.comment}">${user.comment}</span>
                                </div>
                                ` : ''}
                            </div>
                        </div>
                    `;
                }).join('');
            } else {
                list.innerHTML = `<div class="col-span-full text-center text-gray-500 py-8">No users found.</div>`;
            }
        } catch (error) {
            console.error('Failed to load Linux users:', error);
            list.innerHTML = `<div class="col-span-full text-center text-red-500 py-8 font-semibold"><i class="fas fa-exclamation-triangle mr-2"></i>Failed to read users database on remote VPS</div>`;
        }
    },

    openAddUserModal() {
        App.UI.el('add-linux-user-modal').classList.remove('hidden');
    },

    closeAddUserModal() {
        App.UI.el('add-linux-user-modal').classList.add('hidden');
        App.UI.el('add-linux-user-form').reset();
    },

    async handleCreateUser(e) {
        e.preventDefault();
        const btn = App.UI.el('linux-save-btn');
        const originalText = btn.innerHTML;

        btn.innerHTML = '<i class="fas fa-spinner fa-spin mr-2"></i>Creating...';
        btn.disabled = true;

        const username = App.UI.el('linux-username').value;
        const password = App.UI.el('linux-password').value;
        const shell = App.UI.el('linux-shell').value;
        const isSudoer = App.UI.el('linux-is-sudoer').checked;

        try {
            const res = await App.API.post('/system/users', { username, password, shell, is_sudoer: isSudoer });
            if (res.success) {
                this.closeAddUserModal();
                this.load();
                App.UI.toast('success', `Linux user '${username}' created successfully.`);
            } else {
                alert('Failed to create remote user: ' + (res.message || 'Unknown error'));
            }
        } catch (error) {
            console.error('Failed to create Linux user:', error);
            alert('An error occurred while provisioning the Linux user.');
        } finally {
            btn.innerHTML = originalText;
            btn.disabled = false;
        }
    },

    async deleteUser(username) {
        if (!confirm(`Are you absolutely sure you want to delete remote Linux user '${username}' and their home directory?`)) return;

        try {
            const res = await App.API.delete(`/system/users/${username}`);
            if (res.success) {
                this.load();
                App.UI.toast('success', `Linux user '${username}' deleted successfully.`);
            } else {
                alert('Failed to delete remote user: ' + (res.message || 'Unknown error'));
            }
        } catch (error) {
            console.error('Failed to delete Linux user:', error);
            alert('An error occurred while deleting the remote Linux user.');
        }
    }
};
