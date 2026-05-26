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

    async fetchData() {
        try {
            const res = await App.API.get(`/stats/history?range=${this.currentRange}&server_id=${App.config.SERVER_ID}`);
            if (res.success && res.data) {
                this.renderCharts(res.data);
            }
        } catch (error) {
            console.error('Failed to load performance history:', error);
        }
    },

    setRange(range) {
        this.currentRange = range;
        
        // Update range buttons UI
        ['1h', '6h', '24h'].forEach(r => {
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

    renderCharts(data) {
        const labels = data.map(d => {
            const date = new Date(d.timestamp);
            return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
        });

        const cpuData = data.map(d => d.cpu_percent);
        const memData = data.map(d => d.mem_percent);
        const diskData = data.map(d => d.disk_percent);

        // Calculate Network Speed (Rx & Tx) in KB/s from cumulative bytes
        let rxSpeeds = [];
        let txSpeeds = [];
        for (let i = 0; i < data.length; i++) {
            if (i === 0) {
                rxSpeeds.push(0);
                txSpeeds.push(0);
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
            maintainAspectRatio: false,
            plugins: {
                legend: { display: false },
                tooltip: {
                    mode: 'index',
                    intersect: false,
                    backgroundColor: '#1f2937',
                    titleColor: '#9ca3af',
                    bodyColor: '#ffffff',
                    borderColor: '#374151',
                    borderWidth: 1,
                    padding: 10,
                    cornerRadius: 6,
                    font: { family: 'Inter, sans-serif' }
                }
            },
            scales: {
                x: {
                    grid: { color: '#374151', drawBorder: false, borderDash: [2, 4] },
                    ticks: { color: '#9ca3af', font: { size: 10 } }
                },
                y: {
                    grid: { color: '#374151', drawBorder: false, borderDash: [2, 4] },
                    ticks: { color: '#9ca3af', font: { size: 10 } }
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
                borderColor: '#3b82f6',
                borderWidth: 2,
                backgroundColor: (context) => this.createGradient(context.chart.ctx, '#3b82f6'),
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
                borderColor: '#a855f7',
                borderWidth: 2,
                backgroundColor: (context) => this.createGradient(context.chart.ctx, '#a855f7'),
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
                borderColor: '#eab308',
                borderWidth: 2,
                backgroundColor: (context) => this.createGradient(context.chart.ctx, '#eab308'),
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
                    borderColor: '#10b981',
                    borderWidth: 2,
                    backgroundColor: (context) => this.createGradient(context.chart.ctx, '#10b981'),
                    fill: true,
                    tension: 0.3,
                    pointRadius: 0,
                    pointHoverRadius: 4
                },
                {
                    label: 'Upload (Tx)',
                    data: txSpeeds,
                    borderColor: '#f43f5e',
                    borderWidth: 2,
                    backgroundColor: (context) => this.createGradient(context.chart.ctx, '#f43f5e'),
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
                legend: { display: true, labels: { color: '#9ca3af', font: { size: 10 } } }
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
