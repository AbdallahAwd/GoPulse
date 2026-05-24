/**
 * GoPulse App - Editor Module
 */

App.Editor = {
    async open(path) {
        App.state.currentEditPath = path;
        
        const ext = path.split('.').pop().toLowerCase();
        const isImage = ['png', 'jpg', 'jpeg', 'gif', 'bmp', 'svg', 'webp'].includes(ext);
        const isAudio = ['mp3', 'wav', 'ogg', 'm4a'].includes(ext);
        const isMedia = isImage || isAudio;

        const els = {
            filename: App.UI.el('editor-filename'),
            content: App.UI.el('file-editor-content'),
            imageContainer: App.UI.el('file-editor-image'),
            audioContainer: App.UI.el('file-editor-audio'),
            modal: App.UI.el('file-editor-modal'),
            skeleton: App.UI.el('editor-skeleton'),
            saveBtn: App.UI.el('editor-save-btn')
        };

        if (els.filename) els.filename.textContent = path.split('/').pop();
        
        // Reset Views
        if (els.content) { els.content.value = ''; els.content.classList.add('hidden'); }
        if (els.imageContainer) { els.imageContainer.classList.add('hidden'); els.imageContainer.querySelector('img').src = ''; }
        if (els.audioContainer) { els.audioContainer.classList.add('hidden'); els.audioContainer.querySelector('audio').src = ''; }
        if (els.skeleton) els.skeleton.classList.remove('hidden'); // Show loader
        
        // Toggle Save Button
        if (els.saveBtn) {
            if (isMedia) {
                els.saveBtn.classList.add('hidden');
            } else {
                els.saveBtn.classList.remove('hidden');
            }
        }

        if (els.modal) {
            els.modal.classList.remove('hidden', 'opacity-0');
            els.modal.querySelector('div').classList.remove('scale-95');
        }

        try {
            const url = `/files/content?path=${encodeURIComponent(path)}`;
            // Base URL includes /api/v1 prefix, ensure we construct full URL correctly for fetch/src
            // App.API.url returns relative to API base if passed path, so...
            // Actually, we can check how download constructs it:
            // `${App.config.API_BASE}/servers/${App.config.SERVER_ID}/files/content`
            
            const serverId = App.config.SERVER_ID;
            const fullUrl = `${App.config.API_BASE}/servers/${serverId}/files/content?path=${encodeURIComponent(path)}`;

            if (isMedia) {
                // For media, just set the SRC
                // We need to wait a bit or just hide skeleton immediately? 
                // Let's rely on image onload if possible, but for now simple hidden toggle
                
                if (isImage) {
                    const img = els.imageContainer.querySelector('img');
                    img.src = fullUrl;
                    img.onload = () => els.skeleton.classList.add('hidden');
                    img.onerror = () => App.UI.toast('error', 'Failed to load image');
                    els.imageContainer.classList.remove('hidden');
                    // Fallback cleanup if onload doesn't fire fast or error
                    setTimeout(() => els.skeleton.classList.add('hidden'), 500); 
                } else if (isAudio) {
                    const audio = els.audioContainer.querySelector('audio');
                    audio.src = fullUrl;
                    els.audioContainer.classList.remove('hidden');
                    els.skeleton.classList.add('hidden');
                }
            } else {
                // Text content
                const res = await fetch(fullUrl);
                if (!res.ok) throw new Error(`HTTP ${res.status}`);
                const text = await res.text();

                if (els.skeleton) els.skeleton.classList.add('hidden');
                if (els.content) {
                    els.content.value = text;
                    els.content.classList.remove('hidden');
                }
            }
        } catch (e) {
            App.UI.toast('error', `Failed to open: ${e.message}`);
            this.close();
        }
    },

    async save() {
        const content = App.UI.el('file-editor-content')?.value || '';
        const btn = App.UI.el('editor-save-btn');
        const originalHtml = btn?.innerHTML;

        if (btn) {
            btn.innerHTML = `<i class="fas fa-circle-notch fa-spin mr-2"></i> Saving...`;
            btn.disabled = true;
        }

        try {
            await App.API.post('/files/save', { path: App.state.currentEditPath, content });
            if (btn) btn.innerHTML = `<i class="fas fa-check mr-2"></i> Saved!`;
            App.UI.toast('success', 'File saved');
            setTimeout(() => {
                this.close();
                App.Files.list(App.state.currentPath);
            }, 800);
        } catch (e) {
            if (btn) {
                btn.innerHTML = originalHtml;
                btn.disabled = false;
            }
        }
    },

    close() {
        const modal = App.UI.el('file-editor-modal');
        if (!modal) return;
        
        modal.classList.add('opacity-0');
        modal.querySelector('div').classList.add('scale-95');
        setTimeout(() => {
            modal.classList.add('hidden');
            App.state.currentEditPath = '';
            const btn = App.UI.el('editor-save-btn');
            if (btn) {
                btn.disabled = false;
                btn.innerHTML = `<i class="fas fa-save mr-2"></i> Save Changes`;
            }
        }, 300);
    }
};

window.closeFileEditor = () => App.Editor.close();
window.saveFileEditor = () => App.Editor.save();
