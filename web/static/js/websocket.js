// WebSocket connection management
let ws = null;
let reconnectInterval = null;
const wsProtocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
const wsUrl = `${wsProtocol}//${window.location.host}/api/stats/ws`;

function connectWebSocket() {
    try {
        ws = new WebSocket(wsUrl);

        ws.onopen = function() {
            console.log('WebSocket connected');
            updateWSStatus('connected');
            
            // Clear reconnect interval if exists
            if (reconnectInterval) {
                clearInterval(reconnectInterval);
                reconnectInterval = null;
            }

            // Send ping every 30 seconds
            setInterval(() => {
                if (ws && ws.readyState === WebSocket.OPEN) {
                    ws.send(JSON.stringify({ type: 'ping' }));
                }
            }, 30000);
        };

        ws.onmessage = function(event) {
            try {
                const data = JSON.parse(event.data);
                handleWebSocketMessage(data);
            } catch (err) {
                console.error('Error parsing WebSocket message:', err);
            }
        };

        ws.onerror = function(error) {
            console.error('WebSocket error:', error);
            updateWSStatus('error');
        };

        ws.onclose = function() {
            console.log('WebSocket closed');
            updateWSStatus('disconnected');
            
            // Attempt to reconnect after 5 seconds
            if (!reconnectInterval) {
                reconnectInterval = setInterval(() => {
                    console.log('Attempting to reconnect WebSocket...');
                    connectWebSocket();
                }, 5000);
            }
        };
    } catch (error) {
        console.error('Error creating WebSocket:', error);
        updateWSStatus('error');
    }
}

function updateWSStatus(status) {
    const indicator = document.getElementById('ws-indicator');
    const statusText = document.querySelector('#ws-status span:last-child');

    switch(status) {
        case 'connected':
            indicator.className = 'h-2 w-2 rounded-full bg-green-500 pulse';
            statusText.textContent = 'Connected';
            statusText.className = 'text-sm text-green-400';
            break;
        case 'disconnected':
            indicator.className = 'h-2 w-2 rounded-full bg-yellow-500';
            statusText.textContent = 'Disconnected';
            statusText.className = 'text-sm text-yellow-400';
            break;
        case 'error':
            indicator.className = 'h-2 w-2 rounded-full bg-red-500';
            statusText.textContent = 'Error';
            statusText.className = 'text-sm text-red-400';
            break;
    }
}

function handleWebSocketMessage(message) {
    if (message.type === 'stats_update') {
        const currentServerID = document.getElementById('current-server-id')?.value;
        // If we are on a specific server dashboard, filter by ID
        if (currentServerID && message.data.server_id !== currentServerID) {
            return;
        }
        // If we are on the server list page (no currentServerID), we might want to update the list status?
        // But the list page doesn't use these stats yet.
        
        if (message.data.stats) {
            updateSystemStats(message.data.stats);
        }
    } else {
        console.log('Unknown message type:', message.type);
    }
}

// Initialize WebSocket connection when page loads
connectWebSocket();
