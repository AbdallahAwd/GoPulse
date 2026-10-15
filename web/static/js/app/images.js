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
                tbody.innerHTML = '<tr><td colspan="4" class="text-center py-4 text-red-400">Failed to load images</td></tr>';
             } else {
                App.UI.toast('error', 'Failed to refresh images, keeping cached data');
             }
        }
    },

    render(images) {
        const tbody = App.UI.el('images-tbody');
        if (!tbody) return;

        if (images.length === 0) {
            tbody.innerHTML = '<tr><td colspan="4" class="text-center py-4 text-gray-400">No images found</td></tr>';
            return;
        }

        const text = value => App.Docker.escapeHTML(value);
        const arg = value => App.Docker.handlerArgument(value);
        tbody.innerHTML = images.map(img => {
            const repoTag = img.repo_tags?.[0] || '<none>:<none>';
            const tagged = repoTag !== '<none>:<none>';
            const separator = repoTag.lastIndexOf(':');
            const hasTag = separator > repoTag.lastIndexOf('/');
            const repo = tagged ? (hasTag ? repoTag.slice(0,separator) : repoTag) : 'Untagged image';
            const tag = tagged ? (hasTag ? repoTag.slice(separator+1) : 'latest') : 'none';
            const sizeMB = (img.size / (1024 * 1024)).toFixed(2);
            const imageReference = tagged ? repoTag : img.id;
            return `<tr><td><div class="docker-resource-name">${text(repo)}</div><div class="docker-resource-id" title="${text(img.id)}">${text(String(img.id).substring(0,12))}</div></td><td><span class="docker-tag">${text(tag)}</span></td><td class="docker-table-muted">${sizeMB} MB</td><td><div class="docker-row-actions"><button onclick="App.Docker.Run.openModal(${arg(imageReference)})" class="docker-inline-button"><i class="fas fa-play" aria-hidden="true"></i>Run</button><button onclick="App.Docker.Images.remove(${arg(img.id)})" class="docker-icon-button docker-danger-button" title="Remove image" aria-label="Remove ${text(imageReference)}"><i class="far fa-trash-alt" aria-hidden="true"></i></button></div></td></tr>`;
        }).join('');
    },

    async remove(id) {
        if (!await App.UI.confirm('Are you sure you want to remove this image?')) return;
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
