/**
 * GoPulse App - Images Module
 */

App.Docker = App.Docker || {};

App.Docker.Images = {
    async load() {
        if (!App.config.SERVER_ID) return;
        const tbody = App.UI.el('images-tbody');
        if (!tbody) return;

        try {
            const response = await App.API.get('/images');
            const images = response.data || [];

            // Cache fallback logic
            if (images.length === 0 && App.state.lastImages.length > 0) {
                 App.UI.toast('error', 'Received empty image list, using cached data');
                 return;
            }
            App.state.lastImages = images;

            this.render(images);
        } catch (e) {
             // Keep showing old data if available? Or just show error
             // For now, standard error behavior but maybe we should keep old data visible
             if (App.state.lastImages.length === 0) {
                tbody.innerHTML = '<tr><td colspan="5" class="text-center py-4 text-red-400">Failed to load images</td></tr>';
             } else {
                App.UI.toast('error', 'Failed to refresh images, keeping cached data');
             }
        }
    },

    render(images) {
        const tbody = App.UI.el('images-tbody');
        if (!tbody) return;

        if (images.length === 0) {
            tbody.innerHTML = '<tr><td colspan="5" class="text-center py-4 text-gray-400">No images found</td></tr>';
            return;
        }

        tbody.innerHTML = images.map(img => {
            const repoTag = img.repo_tags && img.repo_tags.length > 0 ? img.repo_tags[0] : '<none>:<none>';
            const [repo, tag] = repoTag.split(':');
            const sizeMB = (img.size / (1024 * 1024)).toFixed(2);

            return `
            <tr class="hover:bg-gray-700 transition-colors">
                <td class="px-4 py-3 font-mono text-xs text-gray-400" title="${img.id}">${img.id.substring(0, 12)}</td>
                <td class="px-4 py-3 text-white font-medium">${repo}</td>
                <td class="px-4 py-3 text-gray-300"><span class="bg-gray-600 px-2 py-1 rounded text-xs">${tag}</span></td>
                <td class="px-4 py-3 text-gray-400">${sizeMB} MB</td>
                <td class="px-4 py-3 text-right space-x-2">
                    <button onclick="App.Docker.Run.openModal('${repoTag}')" class="text-green-400 hover:text-green-300 hover:bg-green-900/30 p-2 rounded transition-colors" title="Run Image">
                        <i class="fas fa-play"></i>
                    </button>
                    <button onclick="App.Docker.Images.remove('${img.id}')" class="text-red-400 hover:text-red-300 hover:bg-red-900/30 p-2 rounded transition-colors" title="Remove Image">
                        <i class="fas fa-trash-alt"></i>
                    </button>
                </td>
            </tr>
            `;
        }).join('');
    },

    async remove(id) {
        if (!confirm('Are you sure you want to remove this image?')) return;
        try {
            await App.API.delete(`/images/${id}`);
            App.UI.toast('success', 'Image removed successfully');
            this.load();
        } catch (e) { /* Handled */ }
    },

    showPullModal() {
        this.openPullModal();
    },

    openPullModal() {
        const modal = document.getElementById('pull-modal');
        if (modal) {
            modal.classList.remove('hidden');
            const input = document.getElementById('pull-image-name');
            if (input) {
                input.value = '';
                input.focus();
            }
            this.clearPullTerminal();
        }
    },

    closePullModal() {
        const modal = document.getElementById('pull-modal');
        if (modal) modal.classList.add('hidden');
    },

    clearPullTerminal() {
        const term = document.getElementById('pull-terminal');
        if (term) term.innerText = '';
    },

    appendPullLog(text) {
        const term = document.getElementById('pull-terminal');
        if (!term) return;
        term.innerText += text;
        term.scrollTop = term.scrollHeight;
    },

    async startPull() {
        const input = document.getElementById('pull-image-name');
        const image = input ? input.value.trim() : '';
        
        if (!image) {
            App.UI.toast('error', 'Image name is required');
            return;
        }

        this.appendPullLog(`[System] Starting pull for ${image}...\n`);

        try {
            const response = await fetch(`/api/servers/${App.config.SERVER_ID}/images/pull`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ image: image })
            });

            if (!response.ok) {
                 const err = await response.json();
                 throw new Error(err.error || 'Pull failed to start');
            }

            const reader = response.body.getReader();
            const decoder = new TextDecoder();

            while (true) {
                const { done, value } = await reader.read();
                if (done) break;
                const text = decoder.decode(value, { stream: true });
                this.appendPullLog(text);
            }
            
            this.appendPullLog('\n[System] Pull completed successfully.\n');
            App.UI.toast('success', `Image ${image} pulled successfully`);
            this.load();

        } catch (e) {
            this.appendPullLog(`\n[Error] ${e.message}\n`);
            App.UI.toast('error', 'Pull failed: ' + e.message);
        }
    }
};
