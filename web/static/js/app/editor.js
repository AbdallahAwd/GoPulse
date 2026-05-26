/**
 * GoPulse App - Editor Module
 */

let aceEditor = null;

App.Editor = {
    getModeByExtension(ext) {
        const modes = {
            'js': 'javascript',
            'ts': 'typescript',
            'jsx': 'javascript',
            'tsx': 'typescript',
            'html': 'html',
            'htm': 'html',
            'css': 'css',
            'scss': 'scss',
            'less': 'less',
            'go': 'golang',
            'py': 'python',
            'json': 'json',
            'yaml': 'yaml',
            'yml': 'yaml',
            'toml': 'toml',
            'md': 'markdown',
            'sh': 'sh',
            'bash': 'sh',
            'sql': 'sql',
            'php': 'php',
            'rb': 'ruby',
            'rs': 'rust',
            'c': 'c_cpp',
            'cpp': 'c_cpp',
            'h': 'c_cpp',
            'hpp': 'c_cpp',
            'java': 'java',
            'xml': 'xml',
            'ini': 'ini',
            'conf': 'ini'
        };
        return modes[ext] || 'text';
    },

    initAce() {
        if (aceEditor) return aceEditor;
        if (typeof ace === 'undefined') return null;

        const container = document.getElementById('file-editor-ace');
        if (!container) return null;

        // Initialize Ace
        aceEditor = ace.edit("file-editor-ace");
        aceEditor.setTheme("ace/theme/tomorrow_night"); // modern dark theme
        aceEditor.setOptions({
            fontSize: "13px",
            fontFamily: "Menlo, Monaco, Consolas, 'Courier New', monospace",
            showPrintMargin: false,
            enableBasicAutocompletion: true,
            enableLiveAutocompletion: true,
            tabSize: 4,
            useSoftTabs: true,
            wrap: true
        });

        // Add Premium Keybindings: Save on Ctrl+S / Cmd+S
        aceEditor.commands.addCommand({
            name: 'saveFile',
            bindKey: { win: 'Ctrl-S', mac: 'Command-S' },
            exec: function() {
                App.Editor.save();
            },
            readOnly: false
        });

        return aceEditor;
    },

    async open(path) {
        App.state.currentEditPath = path;
        
        const ext = path.split('.').pop().toLowerCase();
        const isImage = ['png', 'jpg', 'jpeg', 'gif', 'bmp', 'svg', 'webp'].includes(ext);
        const isAudio = ['mp3', 'wav', 'ogg', 'm4a'].includes(ext);
        const isMedia = isImage || isAudio;

        const els = {
            filename: App.UI.el('editor-filename'),
            content: App.UI.el('file-editor-content'),
            aceContainer: document.getElementById('file-editor-ace'),
            imageContainer: App.UI.el('file-editor-image'),
            audioContainer: App.UI.el('file-editor-audio'),
            modal: App.UI.el('file-editor-modal'),
            skeleton: App.UI.el('editor-skeleton'),
            saveBtn: App.UI.el('editor-save-btn')
        };

        if (els.filename) els.filename.textContent = path.split('/').pop();
        
        // Reset Views
        if (els.content) { els.content.value = ''; els.content.classList.add('hidden'); }
        if (els.aceContainer) els.aceContainer.classList.add('hidden');
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
            const serverId = App.config.SERVER_ID;
            const fullUrl = `${App.config.API_BASE}/servers/${serverId}/files/content?path=${encodeURIComponent(path)}`;

            if (isMedia) {
                if (isImage) {
                    const img = els.imageContainer.querySelector('img');
                    img.src = fullUrl;
                    img.onload = () => els.skeleton.classList.add('hidden');
                    img.onerror = () => App.UI.toast('error', 'Failed to load image');
                    els.imageContainer.classList.remove('hidden');
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
                
                const editor = this.initAce();
                if (editor && els.aceContainer) {
                    // Populate Ace Editor
                    editor.setValue(text, -1);
                    editor.session.getUndoManager().reset();
                    
                    // Apply language modes
                    const mode = this.getModeByExtension(ext);
                    editor.session.setMode(`ace/mode/${mode}`);
                    
                    els.aceContainer.classList.remove('hidden');
                } else {
                    // Fallback to plain Textarea if Ace is not loaded
                    if (els.content) {
                        els.content.value = text;
                        els.content.classList.remove('hidden');
                    }
                }
            }
        } catch (e) {
            App.UI.toast('error', `Failed to open: ${e.message}`);
            this.close();
        }
    },

    async save() {
        const els = {
            aceContainer: document.getElementById('file-editor-ace'),
            textarea: App.UI.el('file-editor-content')
        };

        let content = '';
        if (aceEditor && els.aceContainer && !els.aceContainer.classList.contains('hidden')) {
            content = aceEditor.getValue();
        } else if (els.textarea) {
            content = els.textarea.value;
        }

        const btn = App.UI.el('editor-save-btn');
        const originalHtml = btn?.innerHTML;

        if (btn) {
            btn.innerHTML = `<i class="fas fa-circle-notch fa-spin mr-2"></i> Saving...`;
            btn.disabled = true;
        }

        try {
            await App.API.post('/files/save', { path: App.state.currentEditPath, content });
            if (btn) btn.innerHTML = `<i class="fas fa-check mr-2"></i> Saved!`;
            App.UI.toast('success', 'File saved successfully');
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
            
            // Free some editor resources/clear session when closed
            if (aceEditor) {
                aceEditor.setValue('', -1);
            }
        }, 300);
    }
};

window.closeFileEditor = () => App.Editor.close();
window.saveFileEditor = () => App.Editor.save();
