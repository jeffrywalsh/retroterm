// Global debug flag (set to true in console to enable verbose logs)
window.DEBUG = window.DEBUG || false;

class BBSTerminal {
    constructor() {
        this.terminal = null;
        this.fitAddon = null;
        this.ws = null;
        this.isConnected = false;
        this.currentSize = { cols: 80, rows: 25 };
        this.fontSize = 16;
        this.mobileMode = 'auto'; // 'auto' | 'on' | 'off'
        this.allowManualConnection = true;
        this.zmodem = null;
        this._indeterminateTimer = null;
        this._indeterminateActive = false;
        this.currentBBS = null;
        this.lastConnection = null;
        this.music = null;
        // fullscreen state removed
        
        this.loadConfig();
        this.initTerminal();
        this.initEventListeners();
        this.loadSettings();
        this.setupResponsive();

        // Expose debug helpers for hexdumps via console
        this.installDebugDumpHelpers();

        // Auto-connect WebSocket for replay and other features
        this.connectWebSocket();
    }

    // Minimal WS connect used for TEST_MODE Fake Connect (no telnet/ssh connect)
    connectWebSocket() {
        if (this.ws && this.ws.readyState === WebSocket.OPEN) return;
        const wsProtocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const wsUrl = `${wsProtocol}//${window.location.host}/ws`;
        this.ws = new WebSocket(wsUrl);
        window.ws = this.ws; // Expose globally for replay functionality
        this.ws.onopen = () => {
            this.updateStatus('Connected', 'connected');
            this.terminal.writeln('\x1b[32mWebSocket connected (test mode)\x1b[0m');
            // Initialize Zmodem if available (harmless; may do nothing)
            if (typeof ZmodemIntegration !== 'undefined') {
                try {
                    this.zmodem = new ZmodemIntegration(this.terminal, this.ws);
                } catch {}
            }
        };
        this.setupWebSocketHandlers();
    }

    async loadConfig() {
        try {
            const response = await fetch('/api/config');
            const config = await response.json();
            this.allowManualConnection = false; // Always false now, no manual connections
            this.testMode = !!config.testMode;
            if (this.testMode) {
                const btn = document.getElementById('fake-connect-btn');
                if (btn) {
                    btn.style.display = 'inline-block';
                    btn.addEventListener('click', () => {
                        if (this.ws && this.ws.readyState === WebSocket.OPEN) {
                            this.terminal.writeln('\x1b[36mPlaying captured test stream...\x1b[0m');
                            this.ws.send(JSON.stringify({ type: 'playCapture' }));
                        } else {
                            this.terminal.writeln('\x1b[33mNot connected — opening WS...\x1b[0m');
                            this.connectWebSocket();
                            setTimeout(() => {
                                if (this.ws && this.ws.readyState === WebSocket.OPEN) {
                                    this.ws.send(JSON.stringify({ type: 'playCapture' }));
                                }
                            }, 500);
                        }
                    });
                }
            }
        } catch (error) {
            console.error('Failed to load config:', error);
        }
    }

    // Debug: install console-callable helpers to hexdump on-screen cells
    installDebugDumpHelpers() {
        const self = this;
        // Dump a single cell (row, col are 1-based in viewport coordinates)
        window.dumpCell = function(row, col) {
            if (!self.terminal) return null;
            const buf = self.terminal.buffer.active;
            const top = buf.viewportY;
            const line = buf.getLine(top + (row - 1));
            if (!line) return null;
            const cell = line.getCell(col - 1);
            if (!cell) return null;
            const ch = cell.getChars();
            const cp = ch && ch.length ? ch.codePointAt(0) : 0;
            return {
                row, col,
                char: ch,
                codePoint: cp,
                codePointHex: cp ? '0x' + cp.toString(16) : null,
                width: cell.getWidth()
            };
        };

        // Dump a line's text and hex code points
        window.dumpLine = function(row) {
            if (!self.terminal) return null;
            const buf = self.terminal.buffer.active;
            const top = buf.viewportY;
            const line = buf.getLine(top + (row - 1));
            if (!line) return null;
            const text = line.translateToString(true);
            const codes = [];
            for (let x = 0; x < self.terminal.cols; x++) {
                const cell = line.getCell(x);
                if (!cell) break;
                const ch = cell.getChars();
                const cp = ch && ch.length ? ch.codePointAt(0) : 0;
                codes.push(cp ? cp.toString(16).padStart(2,'0') : '00');
            }
            const hex = codes.join(' ');
            const out = { row, text, hex };
            console.log('dumpLine', out);
            return out;
        };

        // Dump a rectangular region (rows/cols are 1-based, inclusive)
        window.dumpRegion = function(r1, c1, r2, c2) {
            if (!self.terminal) return null;
            const buf = self.terminal.buffer.active;
            const top = buf.viewportY;
            const rows = self.terminal.rows;
            const cols = self.terminal.cols;
            r1 = Math.max(1, Math.min(rows, r1|0));
            r2 = Math.max(1, Math.min(rows, r2|0));
            c1 = Math.max(1, Math.min(cols, c1|0));
            c2 = Math.max(1, Math.min(cols, c2|0));
            if (r2 < r1) [r1, r2] = [r2, r1];
            if (c2 < c1) [c1, c2] = [c2, c1];
            const lines = [];
            for (let ry = r1; ry <= r2; ry++) {
                const line = buf.getLine(top + (ry - 1));
                if (!line) break;
                const text = [];
                const hex = [];
                for (let cx = c1; cx <= c2; cx++) {
                    const cell = line.getCell(cx - 1);
                    if (!cell) break;
                    const ch = cell.getChars() || '';
                    const cp = ch.length ? ch.codePointAt(0) : 0;
                    text.push(ch || ' ');
                    hex.push(cp ? cp.toString(16).padStart(2,'0') : '00');
                }
                lines.push({ row: ry, text: text.join(''), hex: hex.join(' ') });
            }
            console.log('dumpRegion', { r1, c1, r2, c2, lines });
            return { r1, c1, r2, c2, lines };
        };
    }

    initTerminal() {
        this.terminal = new Terminal({
            cursorBlink: true,
            fontSize: this.fontSize,
            // Prefer a font with PETSCII/ATASCII glyph coverage (see static/fonts)
            // Fallback to standard monospace if unavailable
            fontFamily: 'RetroTermLegacy, Courier New, Courier, monospace',
            
            // Match the classic IBM PC VGA 16-color palette for ANSI art accuracy
            drawBoldTextInBrightColors: true,
            convertEol: false,  // Don't convert line endings
            cursorStyle: 'block',
            theme: {
                background: '#000000',
                foreground: '#AAAAAA',  // DOS default attribute 7 (light gray)
                cursor: '#AAAAAA',
                cursorAccent: '#000000',
                // DOS/VGA 16-color palette
                black: '#000000',        // 0
                red: '#AA0000',          // 1
                green: '#00AA00',        // 2
                yellow: '#AA5500',       // 3 (brown in DOS)
                blue: '#0000AA',         // 4
                magenta: '#AA00AA',      // 5
                cyan: '#00AAAA',         // 6
                white: '#AAAAAA',        // 7 (light gray)
                brightBlack: '#555555',  // 8
                brightRed: '#FF5555',    // 9
                brightGreen: '#55FF55',  // 10
                brightYellow: '#FFFF55', // 11
                brightBlue: '#5555FF',   // 12
                brightMagenta: '#FF55FF',// 13
                brightCyan: '#55FFFF',   // 14
                brightWhite: '#FFFFFF'   // 15
            },
            cols: this.currentSize.cols,
            rows: this.currentSize.rows,
            scrollback: 10000
        });

        const container = document.getElementById('terminal-container');
        const terminalDiv = document.createElement('div');
        terminalDiv.className = 'terminal';
        container.appendChild(terminalDiv);
        
        this.terminal.open(terminalDiv);
        // Original font in use; no dynamic font switching

        // Try to use WebGL renderer
        try {
            const webglAddon = new WebglAddon.WebglAddon();
            this.terminal.loadAddon(webglAddon);
        } catch (e) {
            if (window.DEBUG) console.log('WebGL not available, using canvas renderer');
        }

        // FitAddon for responsive/mobile fitting
        try {
            this.fitAddon = new FitAddon.FitAddon();
            this.terminal.loadAddon(this.fitAddon);
        } catch (e) {
            if (window.DEBUG) console.warn('FitAddon not available');
        }

        this.terminal.writeln('\x1b[1;32mRetroTerm Ready\x1b[0m');
        this.terminal.writeln('\x1b[36mSelect a BBS from Quick Connect or Browse the directory\x1b[0m');
        this.terminal.writeln('');
        // Init ANSI music player
        try {
            if (window.AnsiMusicPlayer) this.music = new AnsiMusicPlayer();
        } catch {}
    }

    initEventListeners() {
        // Only disconnect button in header remains
        const disconnectBtn = document.getElementById('disconnect-btn-header');
        if (disconnectBtn) {
            disconnectBtn.addEventListener('click', () => this.disconnect());
        }
        const reconnectBtn = document.getElementById('reconnect-btn-header');
        if (reconnectBtn) {
            reconnectBtn.addEventListener('click', () => this.reconnect());
        }

        // Header menu dropdown
        const menuButton = document.getElementById('menu-button');
        const menuDropdown = document.getElementById('menu-dropdown');
        if (menuButton && menuDropdown) {
            const closeMenu = () => {
                menuDropdown.classList.remove('open');
                menuButton.setAttribute('aria-expanded', 'false');
                menuDropdown.setAttribute('aria-hidden', 'true');
            };
            const openMenu = () => {
                menuDropdown.classList.add('open');
                menuButton.setAttribute('aria-expanded', 'true');
                menuDropdown.setAttribute('aria-hidden', 'false');
            };
            menuButton.addEventListener('click', (e) => {
                e.stopPropagation();
                if (menuDropdown.classList.contains('open')) closeMenu(); else openMenu();
            });
            document.addEventListener('click', (e) => {
                if (!menuDropdown.contains(e.target) && e.target !== menuButton) closeMenu();
            });
            document.addEventListener('keydown', (e) => {
                if (e.key === 'Escape') closeMenu();
            });
            const dirBtn = document.getElementById('browse-directory-btn');
            const setBtn = document.getElementById('open-settings-btn');
            if (dirBtn) dirBtn.addEventListener('click', closeMenu);
            if (setBtn) setBtn.addEventListener('click', closeMenu);
        }

        // Directory manager integration is handled in directory.js

        // Terminal size change
        const terminalSize = document.getElementById('terminal-size');
        if (terminalSize) {
            terminalSize.addEventListener('change', (e) => {
                const [cols, rows] = e.target.value.split('x').map(Number);
                this.resizeTerminal(cols, rows);
            });
        }

        // Fullscreen removed

        // Font size change
        const fontSize = document.getElementById('font-size');
        if (fontSize) {
            fontSize.addEventListener('input', (e) => {
                this.fontSize = parseInt(e.target.value);
                const fontSizeValue = document.getElementById('font-size-value');
                if (fontSizeValue) {
                    fontSizeValue.textContent = `${this.fontSize}px`;
                }
                this.terminal.options.fontSize = this.fontSize;
                this.saveSettings();
                // Only refit automatically in mobile mode
                if (this.isMobileModeEnabled()) this.fitToContainerDebounced();
            });
        }

        // No font family toggle

        // Charset change: override connection encoding on the fly
        const charsetSel = document.getElementById('charset');
        if (charsetSel) {
            charsetSel.addEventListener('change', (e) => {
                const newCharset = e.target.value || 'CP437';
                // Persist preference
                this.saveSettings();
                // Inform server if connected so future data path updates
                if (this.ws && this.ws.readyState === WebSocket.OPEN) {
                    this.ws.send(JSON.stringify({ type: 'setCharset', charset: newCharset }));
                    this.terminal.writeln(`\x1b[36mSwitched character encoding to: ${newCharset}\x1b[0m`);
                }
                // For PETSCII/ATASCII, prefer 40x25 for authenticity
                if (newCharset === 'PETSCIIU' || newCharset === 'PETSCIIL' || newCharset === 'ATASCII') {
                    const sel = document.getElementById('terminal-size');
                    if (sel) sel.value = '40x25';
                    this.resizeTerminal(40, 25);
                }
                // For AUTO, let the server auto-detect and adjust size
            });
        }

        // Modal close buttons
        document.querySelectorAll('.modal .modal-close').forEach(btn => {
            btn.addEventListener('click', (e) => {
                const modal = e.target.closest('.modal');
                if (modal) modal.style.display = 'none';
            });
        });

        // Mobile mode toggle
        const mmSelect = document.getElementById('mobile-mode-toggle');
        if (mmSelect) {
            mmSelect.addEventListener('change', (e) => {
                this.mobileMode = e.target.value || 'auto';
                this.saveSettings();
                this.applyResponsive();
            });
        }

        // Keyboard shortcuts: d=Directory, s=Settings, Esc=close
        document.addEventListener('keydown', (e) => {
            const activeTag = document.activeElement && document.activeElement.tagName.toLowerCase();
            const typing = activeTag === 'input' || activeTag === 'textarea';
            if (typing || e.metaKey || e.ctrlKey || e.altKey) return;
            if (e.key === 'd' || e.key === 'D') {
                const btn = document.getElementById('browse-directory-btn');
                if (btn) btn.click();
            } else if (e.key === 's' || e.key === 'S') {
                const btn = document.getElementById('open-settings-btn');
                if (btn) btn.click();
            } else if (e.key === 'Escape') {
                document.querySelectorAll('.modal').forEach(m => m.style.display = 'none');
                const dd = document.getElementById('menu-dropdown');
                if (dd) dd.classList.remove('open');
            }
        });

        // Open Settings modal
        const openSettings = document.getElementById('open-settings-btn');
        if (openSettings) {
            openSettings.addEventListener('click', () => {
                const modal = document.getElementById('settings-modal');
                if (modal) modal.style.display = 'block';
            });
        }

        // Terminal input
        this.terminal.onData((data) => {
            if (this.ws && this.ws.readyState === WebSocket.OPEN) {
                this.ws.send(JSON.stringify({
                    type: 'data',
                    data: data
                }));
            }
        });
    }

    connectToBBS(host, port, protocol, charset) {
        // Direct connection method for BBS directory
        if (!host || !port) {
            this.terminal.writeln('\x1b[31mError: Invalid BBS configuration\x1b[0m');
            return;
        }

        this.updateStatus('Connecting...', 'warning');
        const connectBtn = document.getElementById('connect-btn');
        const disconnectBtn = document.getElementById('disconnect-btn');
        if (connectBtn) connectBtn.disabled = true;
        if (disconnectBtn) disconnectBtn.disabled = false;

        // Connect WebSocket
        const wsProtocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        this.ws = new WebSocket(`${wsProtocol}//${window.location.host}/ws`);
        window.ws = this.ws; // Expose globally for replay functionality

        this.ws.onopen = () => {
            this.terminal.clear();
            this.terminal.writeln(`\x1b[33mConnecting to ${protocol}://${host}:${port}...\x1b[0m`);
            this.terminal.writeln(`\x1b[36mCharacter encoding: ${charset}\x1b[0m`);
            
            // Initialize Zmodem if available
            if (typeof ZmodemIntegration !== 'undefined') {
                try {
                    this.zmodem = new ZmodemIntegration(this.terminal, this.ws);
                    this.terminal.writeln(`\x1b[32mZmodem file transfer enabled\x1b[0m`);
                    console.log('Zmodem initialized successfully');
                } catch (e) {
                    console.error('Zmodem init failed:', e);
                    this.terminal.writeln(`\x1b[31mZmodem initialization failed\x1b[0m`);
                }
            } else {
                if (window.DEBUG) console.warn('ZmodemIntegration not found - Zmodem disabled');
                this.terminal.writeln(`\x1b[33mZmodem file transfer not available\x1b[0m`);
            }
            
            this.ws.send(JSON.stringify({
                type: 'connect',
                protocol: protocol,
                host: host,
                port: port,
                username: '',
                password: '',
                charset: charset
            }));
        };

        this.setupWebSocketHandlers();
    }

    // Deprecated - kept for compatibility
    connect(isDirectoryConnection = false) {
        this.terminal.writeln('\x1b[31mError: Please use the BBS directory to connect.\x1b[0m');
        return;
    }

    setupWebSocketHandlers() {
        this.ws.onmessage = (event) => {
            const msg = JSON.parse(event.data);
            
            switch(msg.type) {
                case 'connected':
                    this.isConnected = true;
                    this.updateStatus('Connected', 'connected');
                    this.terminal.writeln(`\x1b[32m${msg.message}\x1b[0m`);
                    document.getElementById('disconnect-btn-header').style.display = 'inline-block';
                    {
                        const rb = document.getElementById('reconnect-btn-header');
                        if (rb) rb.style.display = 'none';
                    }
                    if (typeof directoryManager !== 'undefined') {
                        directoryManager.setConnectionStatus(true);
                    }
                    break;
                    
                case 'data':
                    let bytes;
                    if (msg.encoding === 'base64') {
                        const decoded = atob(msg.data);
                        bytes = new Uint8Array(decoded.length);
                        for (let i = 0; i < decoded.length; i++) {
                            bytes[i] = decoded.charCodeAt(i);
                        }
                    } else if (typeof msg.data === 'string') {
                        // Treat plain string as UTF-8 text
                        this.terminal.write(msg.data);
                        break;
                    } else {
                        // Unknown payload; do nothing
                        break;
                    }

                    // Pass through Zmodem detector if available
                    if (this.zmodem && this.zmodem.consume) {
                        const remaining = this.zmodem.consume(bytes);
                        if (remaining && remaining.length) {
                            const text = new TextDecoder('utf-8', { fatal: false }).decode(remaining);
                            this.terminal.write(text);
                        }
                    } else {
                        const text = new TextDecoder('utf-8', { fatal: false }).decode(bytes);
                        this.terminal.write(text);
                    }
                    break;

                case 'music':
                    if (this.music && msg.message) {
                        this.music.parseAndQueue(msg.message);
                    }
                    break;
                    
                case 'error':
                    this.terminal.writeln(`\x1b[31mError: ${msg.message}\x1b[0m`);
                    this.updateStatus('Disconnected', 'disconnected');
                    this.resetButtons();
                    break;
                
                case 'downloadStart':
                    this.showDownloadNotification('File transfer starting...');
                    break;
                
                case 'downloadInfo':
                    this.updateDownloadMessage(`Receiving: ${msg.message}`);
                    break;
                
                case 'downloadProgress':
                    this.updateDownloadProgress(msg.message);
                    break;
                
                case 'downloadComplete':
                    this.completeDownload(msg.message, msg.data);
                    break;
                
                case 'downloadCancelled':
                    this.hideDownloadNotification();
                    this.terminal.writeln(`\x1b[33mDownload cancelled\x1b[0m`);
                    break;
                
                case 'fileDownload':
                    // Complete file download from server
                    if (window.DEBUG) console.log('Received fileDownload message for:', msg.message);
                    this.completeDownload(msg.message, msg.data);
                    break;
                    
                case 'zmodemStatus':
                    // Show Zmodem status messages (non-destructive update)
                    this.updateDownloadMessage(msg.message);
                    break;
                    
                case 'zmodemProgress':
                    // Update notification text without recreating UI
                    this.updateDownloadMessage(msg.message);
                    break;
                    
                case 'disconnected':
                    this.isConnected = false;
                    this.updateStatus('Disconnected', 'disconnected');
                    this.terminal.writeln('\x1b[33mConnection closed\x1b[0m');
                    this.resetButtons();
                    document.getElementById('disconnect-btn-header').style.display = 'none';
                    {
                        const rb = document.getElementById('reconnect-btn-header');
                        if (rb) rb.style.display = this.lastConnection ? 'inline-block' : 'none';
                    }
                    if (typeof directoryManager !== 'undefined') {
                        directoryManager.setConnectionStatus(false);
                    }
                    break;

                case 'captureList':
                    // Handle capture list for replay
                    if (window.replayFunctions && window.replayFunctions.handleMessage) {
                        window.replayFunctions.handleMessage(msg);
                    }
                    break;

                case 'status':
                    // Display status messages from replay
                    if (msg.message) {
                        this.updateStatus(msg.message, 'info');
                    }
                    break;
            }
        };

        this.ws.onerror = (error) => {
            console.error('WebSocket error:', error);
            this.terminal.writeln('\x1b[31mConnection error\x1b[0m');
            this.updateStatus('Disconnected', 'disconnected');
            this.resetButtons();
            {
                const rb = document.getElementById('reconnect-btn-header');
                if (rb) rb.style.display = this.lastConnection ? 'inline-block' : 'none';
            }
            if (typeof directoryManager !== 'undefined') {
                directoryManager.setConnectionStatus(false);
            }
        };

        this.ws.onclose = () => {
            if (this.isConnected) {
                this.terminal.writeln('\x1b[33mConnection lost\x1b[0m');
            }
            this.isConnected = false;
            this.updateStatus('Disconnected', 'disconnected');
            // Keep last BBS info visible for easy reconnect
            if (this.lastConnection) {
                const { host, port, protocol } = this.lastConnection;
                this.setCurrentBBSInfo({ name: this.currentBBS?.name, host, port, protocol });
            } else {
                this.setCurrentBBSInfo(null);
            }
            this.resetButtons();
            {
                const rb = document.getElementById('reconnect-btn-header');
                if (rb) rb.style.display = this.lastConnection ? 'inline-block' : 'none';
            }
            if (typeof directoryManager !== 'undefined') {
                directoryManager.setConnectionStatus(false);
            }
        };
    }

    

    

    // Direct connection method that doesn't require form elements
    directConnect(host, port, protocol = 'telnet', username = '', password = '', charset = 'CP437') {
        if (window.DEBUG) console.log('directConnect called:', { host, port, protocol, charset });
        
        if (!host || !port) {
            this.terminal.writeln('\x1b[31mError: Host and port are required\x1b[0m');
            return;
        }

        // Ensure header shows at least host:port if name wasn't set by directory
        if (!this.currentBBS || !this.currentBBS.name) {
            this.setCurrentBBSInfo({ name: `${host}:${port}`, host, port, protocol });
        }

        // Disconnect any existing connection
        if (this.ws && this.ws.readyState === WebSocket.OPEN) {
            this.ws.close();
        }

        this.updateStatus('Connecting...', 'warning');
        
        // Show disconnect button
        const disconnectBtn = document.getElementById('disconnect-btn-header');
        if (disconnectBtn) {
            disconnectBtn.style.display = 'inline-block';
        }
        
        // Before connecting, remember details and hide Reconnect while connecting/connected
        this.lastConnection = { host, port, protocol, username, password, charset };
        const reconnectBtn = document.getElementById('reconnect-btn-header');
        if (reconnectBtn) reconnectBtn.style.display = 'none';

        // Connect WebSocket
        const wsProtocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const wsUrl = `${wsProtocol}//${window.location.host}/ws`;
        
        this.ws = new WebSocket(wsUrl);
        window.ws = this.ws; // Expose globally for replay functionality

        this.ws.onopen = () => {
            if (window.DEBUG) console.log('WebSocket connected, sending connect command');
            this.terminal.clear();
            this.terminal.writeln(`\x1b[33mConnecting to ${protocol}://${host}:${port}...\x1b[0m`);
            this.terminal.writeln(`\x1b[36mCharacter encoding: ${charset}\x1b[0m`);
            // Fresh decoder per connection
            this.decoder = new TextDecoder('utf-8');
            // lastConnection and button already set above
            
            // Initialize Zmodem if available
            if (typeof ZmodemIntegration !== 'undefined') {
                try {
                    this.zmodem = new ZmodemIntegration(this.terminal, this.ws);
                    this.terminal.writeln(`\x1b[32mZmodem file transfer enabled\x1b[0m`);
                    if (window.DEBUG) console.log('Zmodem initialized successfully');
                } catch (e) {
                    console.error('Zmodem init failed:', e);
                    this.terminal.writeln(`\x1b[31mZmodem initialization failed\x1b[0m`);
                }
            } else {
                if (window.DEBUG) console.warn('ZmodemIntegration not found - Zmodem disabled');
                this.terminal.writeln(`\x1b[33mZmodem file transfer not available\x1b[0m`);
            }
            
            this.ws.send(JSON.stringify({
                type: 'connect',
                protocol: protocol,
                host: host,
                port: port,
                username: username,
                password: password,
                charset: charset
            }));
        };
        
        this.setupWebSocketHandlers();
    }

    reconnect() {
        if (!this.lastConnection) {
            this.terminal.writeln('\x1b[33mNo previous connection to reconnect.\x1b[0m');
            return;
        }
        const charsetEl = document.getElementById('charset');
        const charset = (charsetEl && charsetEl.value) ? charsetEl.value : this.lastConnection.charset || 'CP437';
        const { host, port, protocol, username, password } = this.lastConnection;
        this.directConnect(host, port, protocol || 'telnet', username || '', password || '', charset);
    }

    disconnect() {
        if (window.DEBUG) console.log('Disconnect called - current status:', this.isConnected);
        if (this.ws && this.ws.readyState === WebSocket.OPEN) {
            this.ws.send(JSON.stringify({ type: 'disconnect' }));
            this.ws.close();
        }
        this.isConnected = false;
        this.updateStatus('Disconnected', 'disconnected');
        // Keep last BBS info visible for easy reconnect
        if (this.lastConnection) {
            const { host, port, protocol } = this.lastConnection;
            this.setCurrentBBSInfo({ name: this.currentBBS?.name, host, port, protocol });
        } else {
            this.setCurrentBBSInfo(null);
        }
        this.resetButtons();
        {
            const rb = document.getElementById('reconnect-btn-header');
            if (rb) rb.style.display = this.lastConnection ? 'inline-block' : 'none';
        }
        if (typeof directoryManager !== 'undefined') {
            directoryManager.setConnectionStatus(false);
        }
        if (window.DEBUG) console.log('Disconnect complete - new status:', this.isConnected);
    }

    resizeTerminal(cols, rows) {
        this.currentSize = { cols, rows };
        if (this.terminal) {
            this.terminal.resize(cols, rows);
        }
        const terminalDimensions = document.getElementById('terminal-dimensions');
        if (terminalDimensions) {
            terminalDimensions.textContent = `${cols}×${rows}`;
        }
        
        if (this.ws && this.ws.readyState === WebSocket.OPEN) {
            this.ws.send(JSON.stringify({
                type: 'resize',
                cols: cols,
                rows: rows
            }));
        }
        
        this.saveSettings();
    }

    // Fullscreen helpers removed

    updateStatus(text, className) {
        const status = document.getElementById('connection-status');
        if (status) {
            status.textContent = text;
            status.className = `status-value ${className}`;
        }
    }

    setCurrentBBSInfo(info) {
        this.currentBBS = info || null;
        const el = document.getElementById('current-bbs');
        if (el) {
            if (info && (info.name || (info.host && info.port))) {
                el.textContent = info.name || `${info.host}:${info.port}`;
            } else {
                el.textContent = 'None';
            }
        }
    }

    resetButtons() {
        const disconnectBtnHeader = document.getElementById('disconnect-btn-header');
        if (disconnectBtnHeader) disconnectBtnHeader.style.display = 'none';
    }

    saveSettings() {
        const charsetEl = document.getElementById('charset');
        const charset = (charsetEl && charsetEl.value) ? charsetEl.value : 'CP437';
        localStorage.setItem('terminalSettings', JSON.stringify({
            fontSize: this.fontSize,
            size: `${this.currentSize.cols}x${this.currentSize.rows}`,
            charset: charset,
            mobileMode: this.mobileMode
        }));
    }

    loadSettings() {
        const settings = localStorage.getItem('terminalSettings');
        if (settings) {
            const parsed = JSON.parse(settings);
            if (parsed.fontSize) {
                this.fontSize = parsed.fontSize;
                const fontSizeEl = document.getElementById('font-size');
                if (fontSizeEl) {
                    fontSizeEl.value = this.fontSize;
                }
                const fontSizeValue = document.getElementById('font-size-value');
                if (fontSizeValue) {
                    fontSizeValue.textContent = `${this.fontSize}px`;
                }
                if (this.terminal) {
                    this.terminal.options.fontSize = this.fontSize;
                }
            }
            if (parsed.size) {
                const terminalSizeEl = document.getElementById('terminal-size');
                if (terminalSizeEl) {
                    terminalSizeEl.value = parsed.size;
                }
                const [cols, rows] = parsed.size.split('x').map(Number);
                this.resizeTerminal(cols, rows);
            }
            // Apply saved charset to UI if available; default to CP437
            const charsetEl = document.getElementById('charset');
            if (charsetEl) {
                charsetEl.value = parsed.charset || 'CP437';
            }
            // Mobile mode setting
            if (parsed.mobileMode) {
                this.mobileMode = parsed.mobileMode;
            }
            const mmSelect = document.getElementById('mobile-mode-toggle');
            if (mmSelect) mmSelect.value = this.mobileMode;
        } else {
            const charsetEl = document.getElementById('charset');
            if (charsetEl) {
                charsetEl.value = 'CP437';
            }
            const mmSelect = document.getElementById('mobile-mode-toggle');
            if (mmSelect) mmSelect.value = this.mobileMode;
        }
    }

    setupResponsive() {
        // Media query for small screens
        this._mmQuery = window.matchMedia('(max-width: 899px)');
        if (this._mmQuery && this._mmQuery.addEventListener) {
            this._mmQuery.addEventListener('change', () => this.applyResponsive());
        }
        window.addEventListener('resize', () => { if (this.isMobileModeEnabled()) this.fitToContainerDebounced(); });
        window.addEventListener('orientationchange', () => {
            setTimeout(() => this.applyResponsive(), 100);
        });
        // Initial apply
        this.applyResponsive();
    }

    applyResponsive() {
        const enable = this.isMobileModeEnabled();
        document.body.classList.toggle('mobile-mode', !!enable);
        // Disable font size slider in Mobile Mode
        const fontSizeEl = document.getElementById('font-size');
        if (fontSizeEl) fontSizeEl.disabled = !!enable;
        const fontSizeValue = document.getElementById('font-size-value');
        if (fontSizeValue) fontSizeValue.style.opacity = enable ? '0.6' : '1.0';
        if (enable) {
            this.fitToContainerDebounced();
        } else {
            // Always revert to classic 80x25 when leaving Mobile Mode
            const sel = document.getElementById('terminal-size');
            if (sel) sel.value = '80x25';
            this.resizeTerminal(80, 25);
        }
    }

    fitToContainerDebounced() {
        clearTimeout(this._fitTimer);
        this._fitTimer = setTimeout(() => this.fitToContainer(), 60);
    }

    fitToContainer() {
        if (!this.fitAddon || !this.terminal) return;
        if (!this.isMobileModeEnabled()) return; // never fit in desktop mode
        try {
            this.fitAddon.fit();
            const dims = this.fitAddon.proposeDimensions ? this.fitAddon.proposeDimensions() : null;
            if (dims && dims.cols && dims.rows) {
                if (dims.cols !== this.currentSize.cols || dims.rows !== this.currentSize.rows) {
                    this.resizeTerminal(dims.cols, dims.rows);
                }
            }
        } catch (e) {
            if (window.DEBUG) console.warn('Fit failed', e);
        }
    }

    isMobileModeEnabled() {
        const small = this._mmQuery ? this._mmQuery.matches : (window.innerWidth <= 899);
        return (this.mobileMode === 'on') || (this.mobileMode === 'auto' && small);
    }
    
    
    showDownloadNotification(message) {
        // Remove any existing notification
        this.hideDownloadNotification();
        
        // Create download notification UI
        const notification = document.createElement('div');
        notification.id = 'download-notification';
        notification.style.cssText = `
            position: fixed;
            top: 20px;
            right: 20px;
            background: rgba(0, 100, 0, 0.9);
            color: #00ff00;
            padding: 15px 20px;
            border: 2px solid #00ff00;
            border-radius: 5px;
            font-family: 'Courier New', monospace;
            z-index: 10000;
            min-width: 300px;
            box-shadow: 0 0 20px rgba(0, 255, 0, 0.5);
        `;
        
        notification.innerHTML = `
            <div style="margin-bottom: 10px; font-weight: bold;">📥 ZMODEM TRANSFER</div>
            <div id="download-message">${message}</div>
            <div id="download-progress-bar" style="display: block; margin-top: 10px;">
                <div style="background: #003300; border: 1px solid #00ff00; height: 20px; position: relative;">
                    <div id="download-progress-fill" style="background: #00ff00; height: 100%; width: 0%; transition: width 0.3s;"></div>
                    <div id="download-progress-text" style="position: absolute; top: 0; left: 0; right: 0; text-align: center; line-height: 20px;">0%</div>
                </div>
            </div>
            <button id="cancel-download" style="margin-top: 10px; background: #330000; color: #ff0000; border: 1px solid #ff0000; padding: 5px 10px; cursor: pointer; font-family: inherit;">Cancel</button>
        `;
        
        document.body.appendChild(notification);
        
        // Add cancel handler
        document.getElementById('cancel-download').addEventListener('click', () => {
            this.cancelDownload();
        });

        // Start indeterminate progress until we get real percentages
        this.startIndeterminateProgress();
    }

    updateDownloadMessage(message) {
        const existing = document.getElementById('download-notification');
        if (!existing) {
            this.showDownloadNotification(message);
            return;
        }
        const msgEl = document.getElementById('download-message');
        if (msgEl) {
            msgEl.textContent = message;
        }
    }
    
    updateDownloadProgress(progress) {
        const progressBar = document.getElementById('download-progress-bar');
        const progressFill = document.getElementById('download-progress-fill');
        const progressText = document.getElementById('download-progress-text');
        
        if (progressBar) {
            // Stop indeterminate animation on first definite progress
            this.stopIndeterminateProgress();
            const pct = Math.max(0, Math.min(100, parseInt(progress, 10) || 0));
            progressBar.style.display = 'block';
            progressFill.style.width = `${pct}%`;
            progressText.textContent = `${pct}%`;
        }
    }
    
    completeDownload(filename, base64Data) {
        // Convert base64 to blob and trigger download
        try {
            const byteCharacters = atob(base64Data);
            const byteNumbers = new Array(byteCharacters.length);
            for (let i = 0; i < byteCharacters.length; i++) {
                byteNumbers[i] = byteCharacters.charCodeAt(i);
            }
            const byteArray = new Uint8Array(byteNumbers);
            const blob = new Blob([byteArray], { type: 'application/octet-stream' });
            
            // Create download link
            const url = window.URL.createObjectURL(blob);
            const a = document.createElement('a');
            a.href = url;
            a.download = filename;
            document.body.appendChild(a);
            a.click();
            document.body.removeChild(a);
            window.URL.revokeObjectURL(url);
            
            // Show completion message
            const message = document.getElementById('download-message');
            if (message) {
                message.innerHTML = `✅ Downloaded: ${filename}`;
            }
            
            // Hide notification after 3 seconds
            setTimeout(() => {
                this.hideDownloadNotification();
            }, 3000);
            
            this.terminal.writeln(`\x1b[32mFile downloaded: ${filename}\x1b[0m`);
        } catch (error) {
            console.error('Download error:', error);
            this.terminal.writeln(`\x1b[31mDownload failed: ${error.message}\x1b[0m`);
            this.hideDownloadNotification();
        }
    }
    
    hideDownloadNotification() {
        const notification = document.getElementById('download-notification');
        if (notification) {
            this.stopIndeterminateProgress();
            notification.remove();
        }
    }
    
    cancelDownload() {
        if (this.ws && this.ws.readyState === WebSocket.OPEN) {
            this.ws.send(JSON.stringify({
                type: 'cancelDownload'
            }));
        }
        this.hideDownloadNotification();
    }

    startIndeterminateProgress() {
        this.stopIndeterminateProgress();
        const progressBar = document.getElementById('download-progress-bar');
        const progressFill = document.getElementById('download-progress-fill');
        const progressText = document.getElementById('download-progress-text');
        if (!progressBar || !progressFill || !progressText) return;
        this._indeterminateActive = true;
        let pct = 5;
        let dir = 1;
        progressBar.style.display = 'block';
        progressText.textContent = '...';
        this._indeterminateTimer = setInterval(() => {
            if (!this._indeterminateActive) return;
            pct += dir * 3;
            if (pct >= 90) { dir = -1; }
            if (pct <= 5) { dir = 1; }
            progressFill.style.width = `${pct}%`;
        }, 250);
    }

    stopIndeterminateProgress() {
        this._indeterminateActive = false;
        if (this._indeterminateTimer) {
            clearInterval(this._indeterminateTimer);
            this._indeterminateTimer = null;
        }
        const progressText = document.getElementById('download-progress-text');
        if (progressText && progressText.textContent === '...') {
            progressText.textContent = '0%';
        }
    }
}

// Initialize when DOM is ready
document.addEventListener('DOMContentLoaded', () => {
    window.bbsTerminal = new BBSTerminal();

    // Initialize replay functionality
    if (window.replayFunctions && window.replayFunctions.init) {
        window.replayFunctions.init();
    }
});
