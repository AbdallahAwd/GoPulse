/**
 * GoPulse App - Performance History Charts Module
 * Renders beautiful, sleek dark-themed gradient area charts using Chart.js
 */

App.Charts = {
    charts: {},
    currentRange: '1h',
    isCollapsed: false,
    refreshInterval: null,

    init() {
        if (!App.config.SERVER_ID) return;
        
        // Setup initial charts and fetch data
        this.fetchData();

        // Start refresh interval (60 seconds)
        if (this.refreshInterval) clearInterval(this.refreshInterval);
        this.refreshInterval = setInterval(() => {
            this.fetchData();
        }, 60000);
    },

    pause() {
        if (this.refreshInterval) clearInterval(this.refreshInterval);
        this.refreshInterval = null;
    },

    async fetchData() {
        if (this.loading || (App.Navigation && App.Navigation.current !== 'overview')) return;
        this.loading = true;
        const requestedRange = this.currentRange;
        try {
            const res = await App.API.get(`/stats/history?range=${this.currentRange}&server_id=${App.config.SERVER_ID}`);
            if (res.success && res.data && requestedRange === this.currentRange) {
                this.renderCharts(res.data);
                const feedback = document.getElementById('history-feedback');
                if (feedback) feedback.textContent = res.data.length ? 'Saved metric summaries · 30-day retention · gaps indicate missing readings.' : 'No saved readings in this range yet. History appears after metrics are collected.';
            }
        } catch (error) {
            const feedback = document.getElementById('history-feedback');
            if (feedback) feedback.textContent = 'Unable to load metric history. Try refreshing.';
            console.error('Failed to load performance history:', error);
        } finally { this.loading = false; if (requestedRange !== this.currentRange) this.fetchData(); }
    },

    setRange(range) {
        this.currentRange = range;
        
        // Update range buttons UI
        ['1h', '6h', '24h', '7d', '30d'].forEach(r => {
            const btn = document.getElementById(`btn-chart-${r}`);
            if (btn) {
                if (r === range) {
                    btn.className = 'px-3 py-1.5 rounded-md text-gray-300 hover:text-white transition-all bg-gray-800 text-white';
                } else {
                    btn.className = 'px-3 py-1.5 rounded-md text-gray-400 hover:text-white transition-all';
                }
            }
        });

        this.fetchData();
    },

    toggleCollapse() {
        const container = document.getElementById('charts-container');
        const icon = document.querySelector('#btn-charts-collapse i');
        if (!container) return;

        this.isCollapsed = !this.isCollapsed;
        if (this.isCollapsed) {
            container.classList.add('hidden');
            if (icon) icon.className = 'fas fa-chevron-down';
        } else {
            container.classList.remove('hidden');
            if (icon) icon.className = 'fas fa-chevron-up';
            // Refit/redraw charts since size might have changed
            Object.values(this.charts).forEach(chart => chart.resize());
        }
    },

    createGradient(ctx, color) {
        const gradient = ctx.createLinearGradient(0, 0, 0, 200);
        gradient.addColorStop(0, color + '33'); // 20% opacity
        gradient.addColorStop(1, color + '00'); // 0% opacity
        return gradient;
    },

    prepareData(data) {
        const result = [];
        const step = Math.floor(({ '1h':60, '6h':360, '24h':1440, '7d':10080, '30d':43200 }[this.currentRange] || 1440) / 1000) + 1;
        for (const point of data) {
            const previous = result[result.length - 1];
            if (previous && new Date(point.timestamp) - new Date(previous.timestamp) > step * 90000) {
                result.push({ timestamp:new Date(new Date(previous.timestamp).getTime() + step * 60000).toISOString(), unavailable:true });
            }
            result.push(point);
        }
        return result;
    },

    renderCharts(data) {
        data = this.prepareData(data);
        const labels = data.map(d => {
            const date = new Date(d.timestamp);
            return ['7d','30d'].includes(this.currentRange) ? date.toLocaleString([], {month:'short',day:'numeric',hour:'2-digit',minute:'2-digit'}) : date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
        });

        const cpuData = data.map(d => d.unavailable ? null : d.cpu_percent);
        const memData = data.map(d => d.unavailable ? null : d.mem_percent);
        const diskData = data.map(d => d.unavailable ? null : d.disk_percent);

        // Calculate Network Speed (Rx & Tx) in KB/s from cumulative bytes
        let rxSpeeds = [];
        let txSpeeds = [];
        for (let i = 0; i < data.length; i++) {
            if (i === 0 || data[i].unavailable || data[i-1].unavailable) {
                rxSpeeds.push(null);
                txSpeeds.push(null);
            } else {
                const tDiff = (new Date(data[i].timestamp) - new Date(data[i-1].timestamp)) / 1000;
                if (tDiff > 0) {
                    const rxDiff = data[i].net_rx_bytes - data[i-1].net_rx_bytes;
                    const txDiff = data[i].net_tx_bytes - data[i-1].net_tx_bytes;
                    // Protect against server reboots resetting stats counters
                    rxSpeeds.push(rxDiff >= 0 ? rxDiff / 1024 / tDiff : 0);
                    txSpeeds.push(txDiff >= 0 ? txDiff / 1024 / tDiff : 0);
                } else {
                    rxSpeeds.push(0);
                    txSpeeds.push(0);
                }
            }
        }

        // Shared chart options for consistent premium style
        const baseOptions = {
            responsive: true,
            spanGaps: false,
            maintainAspectRatio: false,
            plugins: {
                legend: { display: false },
                tooltip: {
                    mode: 'index',
                    intersect: false,
                    backgroundColor: '#202125',
                    titleColor: '#96999f',
                    bodyColor: '#ffffff',
                    borderColor: '#2b2d32',
                    borderWidth: 1,
                    padding: 10,
                    cornerRadius: 6,
                    font: { family: 'Inter, sans-serif' }
                }
            },
            scales: {
                x: {
                    grid: { color: '#2b2d32', drawBorder: false, borderDash: [2, 4] },
                    ticks: { color: '#96999f', font: { size: 10 } }
                },
                y: {
                    grid: { color: '#2b2d32', drawBorder: false, borderDash: [2, 4] },
                    ticks: { color: '#96999f', font: { size: 10 } }
                }
            },
            interaction: { intersect: false, mode: 'index' }
        };

        // Render CPU Chart
        this.updateOrCreateChart('cpu-chart', {
            labels: labels,
            datasets: [{
                label: 'CPU',
                data: cpuData,
                borderColor: '#c3f36b',
                borderWidth: 2,
                backgroundColor: (context) => this.createGradient(context.chart.ctx, '#c3f36b'),
                fill: true,
                tension: 0.3,
                pointRadius: 0,
                pointHoverRadius: 4
            }]
        }, {
            ...baseOptions,
            scales: {
                ...baseOptions.scales,
                y: { ...baseOptions.scales.y, min: 0, max: 100 }
            }
        });

        // Render Memory Chart
        this.updateOrCreateChart('memory-chart', {
            labels: labels,
            datasets: [{
                label: 'Memory',
                data: memData,
                borderColor: '#a8b8d0',
                borderWidth: 2,
                backgroundColor: (context) => this.createGradient(context.chart.ctx, '#a8b8d0'),
                fill: true,
                tension: 0.3,
                pointRadius: 0,
                pointHoverRadius: 4
            }]
        }, {
            ...baseOptions,
            scales: {
                ...baseOptions.scales,
                y: { ...baseOptions.scales.y, min: 0, max: 100 }
            }
        });

        // Render Disk Chart
        this.updateOrCreateChart('disk-chart', {
            labels: labels,
            datasets: [{
                label: 'Disk',
                data: diskData,
                borderColor: '#e4bd78',
                borderWidth: 2,
                backgroundColor: (context) => this.createGradient(context.chart.ctx, '#e4bd78'),
                fill: true,
                tension: 0.3,
                pointRadius: 0,
                pointHoverRadius: 4
            }]
        }, {
            ...baseOptions,
            scales: {
                ...baseOptions.scales,
                y: { ...baseOptions.scales.y, min: 0, max: 100 }
            }
        });

        // Render Network Chart
        this.updateOrCreateChart('network-chart', {
            labels: labels,
            datasets: [
                {
                    label: 'Download (Rx)',
                    data: rxSpeeds,
                    borderColor: '#88c7b0',
                    borderWidth: 2,
                    backgroundColor: (context) => this.createGradient(context.chart.ctx, '#88c7b0'),
                    fill: true,
                    tension: 0.3,
                    pointRadius: 0,
                    pointHoverRadius: 4
                },
                {
                    label: 'Upload (Tx)',
                    data: txSpeeds,
                    borderColor: '#b4a5d4',
                    borderWidth: 2,
                    backgroundColor: (context) => this.createGradient(context.chart.ctx, '#b4a5d4'),
                    fill: true,
                    tension: 0.3,
                    pointRadius: 0,
                    pointHoverRadius: 4
                }
            ]
        }, {
            ...baseOptions,
            plugins: {
                ...baseOptions.plugins,
                legend: { display: true, labels: { color: '#96999f', font: { size: 10 } } }
            }
        });
    },

    updateOrCreateChart(canvasId, chartData, options) {
        const ctx = document.getElementById(canvasId)?.getContext('2d');
        if (!ctx) return;

        if (this.charts[canvasId]) {
            this.charts[canvasId].data = chartData;
            this.charts[canvasId].update('none'); // Update without animation for speed
        } else {
            this.charts[canvasId] = new Chart(ctx, {
                type: 'line',
                data: chartData,
                options: options
            });
        }
    }
};
