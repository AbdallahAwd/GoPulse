/**
 * GoPulse App - Core Module
 * App definition, Configuration, State, API, and Shared UI
 */

window.App = {
    config: {
        API_BASE: '/api',
        SERVER_ID: document.getElementById('current-server-id')?.value,
    },
    state: {
        lastProcesses: [],
        lastContainers: [],
        lastImages: [],
        lastVolumes: [],
        currentPath: '/',
        currentEditPath: '',
        searchTimeout: null,
    },
};

// -----------------------------------------------------------------------------
// API Module - Centralized API Handling
// -----------------------------------------------------------------------------
App.API = {
    // Helper to constructing URLs with Server context if needed
    url(endpoint) {
        // Global endpoints or already scoped ones (not per-server)
        if (endpoint.startsWith('/servers') || endpoint.startsWith('/stats') || endpoint.startsWith('/github')) {
            return endpoint;
        }
        
        if (endpoint.startsWith('/') && App.config.SERVER_ID) {
            return `/servers/${App.config.SERVER_ID}${endpoint}`;
        }
        return endpoint;
    },

    async request(endpoint, options = {}) {
        const url = this.url(endpoint);
        try {
            const response = await fetch(`${App.config.API_BASE}${url}`, {
                ...options,
                headers: {
                    'Content-Type': 'application/json',
                    ...options.headers,
                },
            });

            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }

            return await response.json();
        } catch (error) {
            console.error('API call failed:', error);
            App.UI.toast('error', `API Error: ${error.message}`);
            throw error;
        }
    },

    get(endpoint) {
        return this.request(endpoint);
    },

    post(endpoint, body) {
        return this.request(endpoint, {
            method: 'POST',
            body: JSON.stringify(body),
        });
    },

    put(endpoint, body) {
        return this.request(endpoint, {
            method: 'PUT',
            body: JSON.stringify(body),
        });
    },

    delete(endpoint) {
        return this.request(endpoint, { method: 'DELETE' });
    },

    // Upload handles its own headers/body
    async upload(endpoint, formData, onProgress) {
        return new Promise((resolve, reject) => {
            const url = this.url(endpoint);
            const xhr = new XMLHttpRequest();
            
            xhr.open('POST', `${App.config.API_BASE}${url}`);
            
            if (onProgress) {
                xhr.upload.onprogress = (e) => {
                    if (e.lengthComputable) {
                        const percentComplete = (e.loaded / e.total) * 100;
                        onProgress(percentComplete);
                    }
                };
            }

            xhr.onload = () => {
                if (xhr.status >= 200 && xhr.status < 300) {
                    try {
                        const response = JSON.parse(xhr.responseText);
                        resolve(response);
                    } catch (e) {
                        // If not JSON, just return text or empty
                        resolve(xhr.responseText);
                    }
                } else {
                    App.UI.toast('error', `Upload failed: ${xhr.statusText}`);
                    reject(new Error(xhr.statusText));
                }
            };

            xhr.onerror = () => {
                App.UI.toast('error', 'Upload failed: Network error');
                reject(new Error('Network error'));
            };

            xhr.send(formData);
        });
    }
};

// -----------------------------------------------------------------------------
// UI Module - Shared UI Logic
// -----------------------------------------------------------------------------
App.UI = {
    el(id) {
        return document.getElementById(id);
    },

    toast(type, message) {
        const toast = this.el('toast');
        if (!toast) return;

        const icon = this.el('toast-icon');
        const messageEl = this.el('toast-message');

        const iconClass = {
            success: 'fa-check-circle text-green-500',
            error: 'fa-exclamation-circle text-red-500',
            info: 'fa-info-circle text-blue-500'
        }[type] || 'fa-info-circle text-blue-500';

        icon.className = `fas ${iconClass}`;
        messageEl.textContent = message;

        toast.classList.remove('hidden', 'translate-y-32');
        toast.classList.add('translate-y-0');

        setTimeout(() => {
            toast.classList.remove('translate-y-0');
            toast.classList.add('translate-y-32');
            setTimeout(() => toast.classList.add('hidden'), 300);
        }, 3000);
    },

    formatBytes(bytes, decimals = 2) {
        if (bytes === 0) return '0 Bytes';
        const k = 1024;
        const dm = decimals < 0 ? 0 : decimals;
        const sizes = ['Bytes', 'KB', 'MB', 'GB', 'TB', 'PB', 'EB', 'ZB', 'YB'];
        const i = Math.floor(Math.log(bytes) / Math.log(k));
        return parseFloat((bytes / Math.pow(k, i)).toFixed(dm)) + ' ' + sizes[i];
    },

    updateStats(stats) {
        if (!stats || !stats.cpu) return;

        const updateEl = (id, val, suffix = '%') => {
            const el = this.el(id);
            const bar = this.el(id.replace('usage', 'bar')); // Convention: cpu-usage -> cpu-bar
            if (el) el.textContent = `${val}${suffix}`;
            if (bar) bar.style.width = `${val}%`;
        };

        // CPU & Memory
        updateEl('cpu-usage', stats.cpu.percent.toFixed(1));
        updateEl('memory-usage', stats.memory.used_percent.toFixed(1));

        // Disk (First partition)
        if (stats.disk?.length > 0) {
            updateEl('disk-usage', stats.disk[0].used_percent.toFixed(1));
        }

        // Load Average
        if (stats.cpu.load_average?.length >= 3) {
            const load = stats.cpu.load_average;
            const loadEl = this.el('load-avg');
            if (loadEl) {
                loadEl.textContent = load[0].toFixed(2);
                this.el('load-details').textContent =
                    `1m: ${load[0].toFixed(2)} | 5m: ${load[1].toFixed(2)} | 15m: ${load[2].toFixed(2)}`;
            }
        }
    }
};

// Global Exports for websocket.js compatibility
window.updateSystemStats = (stats) => App.UI.updateStats(stats);
