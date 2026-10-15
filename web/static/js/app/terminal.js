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
                background: '#101113',
                foreground: '#d6d8dc',
                cursor: '#c3f36b',
            },
            fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace',
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

        const socket = this.socket;
        // Ignore events from a socket disposed during a section change.
        this.socket.onopen = () => {
            if (this.socket !== socket || !this.term) return;
            this.term.writeln('\x1b[32mConnection established.\x1b[0m\r\n');
            
            // Send resize message on init to sync with exact PTY size
            this.resizePTY();
        };

        this.socket.onmessage = (event) => {
            if (this.socket !== socket || !this.term) return;
            // Write binary/text data to the terminal
            if (typeof event.data === 'string') {
                this.term.write(event.data);
            } else {
                const reader = new FileReader();
                reader.onload = () => {
                    if (this.socket !== socket || !this.term) return;
                    this.term.write(new Uint8Array(reader.result));
                };
                reader.readAsArrayBuffer(event.data);
            }
        };

        this.socket.onerror = (error) => {
            if (this.socket !== socket || !this.term) return;
            console.error('Terminal WebSocket error:', error);
            this.term.writeln('\r\n\x1b[31mTerminal connection error.\x1b[0m');
        };

        this.socket.onclose = (event) => {
            if (this.socket !== socket || !this.term) return;
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
        window.removeEventListener('resize', this.onWindowResize);
        if (this.resizeInterval) {
            clearInterval(this.resizeInterval);
        }

        if (this.socket) {
            const socket = this.socket;
            this.socket = null;
            socket.close();
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
