/**
 * GoPulse App - Web SSH Terminal Module
 * Uses xterm.js to establish interactive SSH PTY over WebSockets
 */

App.Terminal = {
    term: null,
    fitAddon: null,
    socket: null,
    isOpen: false,

    init() {
        // Nothing heavy needed here since third-party libs are loaded in base.html
    },

    open() {
        if (this.isOpen) return;
        
        const container = document.getElementById('terminal-container');
        if (!container) return;

        container.innerHTML = ''; // Clear previous instances
        this.isOpen = true;

        // Initialize xterm
        this.term = new Terminal({
            cursorBlink: true,
            theme: {
                background: '#000000',
                foreground: '#a6e22e', // retro green
                cursor: '#ffffff',
            },
            fontFamily: 'Courier New, Courier, monospace',
            fontSize: 14,
        });

        // Initialize FitAddon
        this.fitAddon = new FitAddon.FitAddon();
        this.term.loadAddon(this.fitAddon);

        // Open terminal in container
        this.term.open(container);
        this.fitAddon.fit();

        const cols = this.term.cols;
        const rows = this.term.rows;

        // Establish WebSocket Connection
        const protocol = window.location.protocol === 'https:' ? 'wss' : 'ws';
        const wsUrl = `${protocol}://${window.location.host}/api/servers/${App.config.SERVER_ID}/terminal?cols=${cols}&rows=${rows}`;
        
        this.term.writeln('\x1b[33mConnecting to server SSH terminal...\x1b[0m');

        this.socket = new WebSocket(wsUrl);

        // WebSocket Event Handlers
        this.socket.onopen = () => {
            this.term.writeln('\x1b[32mConnection established.\x1b[0m\r\n');
            
            // Send resize message on init to sync with exact PTY size
            this.resizePTY();
        };

        this.socket.onmessage = (event) => {
            // Write binary/text data to the terminal
            if (typeof event.data === 'string') {
                this.term.write(event.data);
            } else {
                const reader = new FileReader();
                reader.onload = () => {
                    this.term.write(new Uint8Array(reader.result));
                };
                reader.readAsArrayBuffer(event.data);
            }
        };

        this.socket.onerror = (error) => {
            console.error('Terminal WebSocket error:', error);
            this.term.writeln('\r\n\x1b[31mTerminal connection error.\x1b[0m');
        };

        this.socket.onclose = (event) => {
            this.term.writeln('\r\n\x1b[31mTerminal connection closed by remote host.\x1b[0m');
            this.isOpen = false;
        };

        // Handle Keyboard input
        this.term.onData((data) => {
            if (this.socket && this.socket.readyState === WebSocket.OPEN) {
                this.socket.send(JSON.stringify({
                    type: 'input',
                    data: data
                }));
            }
        });

        // Handle Window resize
        window.addEventListener('resize', this.onWindowResize);

        // Periodically verify fit
        this.resizeInterval = setInterval(() => {
            if (this.isOpen && this.fitAddon) {
                this.fitAddon.fit();
            }
        }, 1000);
    },

    onWindowResize: () => {
        if (App.Terminal.isOpen && App.Terminal.fitAddon) {
            App.Terminal.fitAddon.fit();
            App.Terminal.resizePTY();
        }
    },

    resizePTY() {
        if (this.socket && this.socket.readyState === WebSocket.OPEN && this.term) {
            const cols = this.term.cols;
            const rows = this.term.rows;
            this.socket.send(JSON.stringify({
                type: 'resize',
                cols: cols,
                rows: rows
            }));
        }
    },

    close() {
        if (!this.isOpen) return;

        window.removeEventListener('resize', this.onWindowResize);
        if (this.resizeInterval) {
            clearInterval(this.resizeInterval);
        }

        if (this.socket) {
            this.socket.close();
            this.socket = null;
        }

        if (this.term) {
            this.term.dispose();
            this.term = null;
        }

        this.isOpen = false;
    },

    reconnect() {
        this.close();
        setTimeout(() => this.open(), 200);
    }
};
