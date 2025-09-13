// Simplified Zmodem detection and notification
class ZmodemIntegration {
    constructor(terminal, websocket) {
        this.terminal = terminal;
        this.ws = websocket;
        this.transferActive = false;
        this.buffer = new Uint8Array(0);
        
        if (window.DEBUG) console.log('ZmodemIntegration initialized');
    }
    
    // Feed data from websocket to detector
    consume(data) {
        // Convert data to Uint8Array if needed
        let bytes;
        if (typeof data === 'string') {
            bytes = new Uint8Array(data.length);
            for (let i = 0; i < data.length; i++) {
                bytes[i] = data.charCodeAt(i);
            }
        } else if (data instanceof ArrayBuffer) {
            bytes = new Uint8Array(data);
        } else if (data instanceof Uint8Array) {
            bytes = data;
        } else {
            // Log unexpected data type
            if (window.DEBUG) console.log('ZmodemIntegration: Unexpected data type:', typeof data, data);
            bytes = new Uint8Array(0);
        }
        
        // Log first few bytes for debugging
        if (bytes.length > 0) {
            const sample = Array.from(bytes.slice(0, Math.min(20, bytes.length)))
                .map(b => b.toString(16).padStart(2, '0'))
                .join(' ');
            if (window.DEBUG) console.log('ZmodemIntegration: Processing bytes:', sample);
        }
        
        // Append to buffer
        const newBuffer = new Uint8Array(this.buffer.length + bytes.length);
        newBuffer.set(this.buffer, 0);
        newBuffer.set(bytes, this.buffer.length);
        this.buffer = newBuffer;
        
        // Check for ZMODEM signatures
        if (this.detectZmodemStart(this.buffer)) {
            if (window.DEBUG) console.log('ZmodemIntegration: ZMODEM start detected!');
            if (!this.transferActive) {
                this.transferActive = true;
                this.handleZmodemDetection();
            }
            // Keep buffering during transfer
            if (this.buffer.length > 10000) {
                // Prevent excessive buffering
                this.buffer = this.buffer.slice(-5000);
            }
            return null; // Don't display to terminal during transfer
        }
        
        // Check for end of transfer
        if (this.transferActive && this.detectZmodemEnd(this.buffer)) {
            this.transferActive = false;
            this.hideNotification();
            this.buffer = new Uint8Array(0);
        }
        
        // Clear buffer if no transfer
        if (!this.transferActive) {
            this.buffer = new Uint8Array(0);
            return data; // Pass through to terminal
        }
        
        return null; // Don't display during transfer
    }
    
    detectZmodemStart(data) {
        // ZRQINIT: **\x18B00000000000000\r\x8a
        // Look for the ZMODEM start sequences
        const patterns = [
            [0x2A, 0x2A, 0x18, 0x42, 0x30, 0x30], // **\x18B00
            [0x2A, 0x18, 0x43],                    // *\x18C (ZRINIT)
            [0x72, 0x7A, 0x0D]                     // rz\r
        ];
        
        for (const pattern of patterns) {
            if (this.findPattern(data, pattern)) {
                return true;
            }
        }
        return false;
    }
    
    detectZmodemEnd(data) {
        // Look for OO (two capital O's) which often indicates end
        const patterns = [
            [0x4F, 0x4F],           // OO
            [0x18, 0x18, 0x18, 0x18, 0x18] // Multiple cancels
        ];
        
        for (const pattern of patterns) {
            if (this.findPattern(data, pattern)) {
                return true;
            }
        }
        return false;
    }
    
    findPattern(data, pattern) {
        if (data.length < pattern.length) return false;
        
        for (let i = 0; i <= data.length - pattern.length; i++) {
            let match = true;
            for (let j = 0; j < pattern.length; j++) {
                if (data[i + j] !== pattern[j]) {
                    match = false;
                    break;
                }
            }
            if (match) return true;
        }
        return false;
    }
    
    handleZmodemDetection() {
        if (window.DEBUG) console.log('ZMODEM transfer detected');
        this.showTransferNotification('File transfer in progress...');
    }
    
    showTransferNotification(message) {
        // Remove existing notification
        this.hideNotification();
        
        const notification = document.createElement('div');
        notification.id = 'zmodem-notification';
        notification.style.cssText = `
            position: fixed;
            top: 20px;
            right: 20px;
            background: rgba(0, 100, 0, 0.95);
            color: #00ff00;
            padding: 15px 20px;
            border: 2px solid #00ff00;
            border-radius: 5px;
            font-family: 'Courier New', monospace;
            z-index: 10000;
            min-width: 350px;
            max-width: 500px;
            box-shadow: 0 0 20px rgba(0, 255, 0, 0.5);
        `;
        
        notification.innerHTML = `
            <div style="margin-bottom: 10px; font-weight: bold;">📥 ZMODEM FILE TRANSFER</div>
            <div id="zmodem-message" style="margin-bottom: 15px;">${message}</div>
            <div style="font-size: 12px; color: #00cc00;">
                The file will download automatically when transfer completes.<br>
                Press Ctrl+X multiple times to cancel if needed.
            </div>
        `;
        
        document.body.appendChild(notification);
    }
    
    showNotification(message) {
        this.showTransferNotification(message);
    }
    
    hideNotification() {
        const notification = document.getElementById('zmodem-notification');
        if (notification) {
            notification.remove();
        }
    }
    
    abort() {
        this.transferActive = false;
        this.buffer = new Uint8Array(0);
        this.hideNotification();
    }
}

// Export for use
window.ZmodemIntegration = ZmodemIntegration;
