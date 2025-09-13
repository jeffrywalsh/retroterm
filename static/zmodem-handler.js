// Simplified Zmodem detection and pass-through
class ZmodemHandler {
    constructor(terminal, websocket) {
        this.terminal = terminal;
        this.ws = websocket;
        this.receiving = false;
        this.buffer = [];
        this.filename = '';
        
        // Zmodem signature patterns
        this.ZRQINIT = new Uint8Array([42, 42, 24, 66, 48, 48]); // **\x18B00
    }
    
    detectZmodem(data) {
        // Check if data contains Zmodem start sequence
        if (data.indexOf('**\x18B00') !== -1 || 
            data.indexOf('rz\r**\x18B') !== -1) {
            return true;
        }
        return false;
    }
    
    startReceive() {
        this.receiving = true;
        this.buffer = [];
        this.showNotification('Zmodem transfer detected - preparing to receive...');
        
        // Send acknowledgment
        this.ws.send(JSON.stringify({
            type: 'data',
            data: btoa('**\x18B01000000000000\r\n')
        }));
    }
    
    processData(data) {
        if (!this.receiving) {
            if (this.detectZmodem(data)) {
                this.startReceive();
                return true; // Handled
            }
            return false; // Not Zmodem
        }
        
        // Collecting Zmodem data
        this.buffer.push(data);
        
        // Check for end of transfer
        if (data.indexOf('\x18\x18\x18\x18\x18') !== -1) {
            this.completeReceive();
        }
        
        return true; // Handled
    }
    
    completeReceive() {
        this.receiving = false;
        
        // For now, just notify that transfer was attempted
        this.showNotification('Zmodem transfer attempted - full implementation needed');
        
        setTimeout(() => {
            this.hideNotification();
        }, 3000);
    }
    
    showNotification(message) {
        // Remove any existing notification
        this.hideNotification();
        
        const notification = document.createElement('div');
        notification.id = 'zmodem-notification';
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
            <div style="margin-bottom: 10px; font-weight: bold;">📥 FILE TRANSFER</div>
            <div>${message}</div>
            <div style="margin-top: 10px; font-size: 12px;">
                Note: Full Zmodem support requires additional implementation.<br>
                For now, use alternative transfer methods like HTTP/FTP if available.
            </div>
        `;
        
        document.body.appendChild(notification);
    }
    
    hideNotification() {
        const notification = document.getElementById('zmodem-notification');
        if (notification) {
            notification.remove();
        }
    }
    
    cancel() {
        if (this.receiving) {
            this.receiving = false;
            this.buffer = [];
            // Send cancel sequence
            this.ws.send(JSON.stringify({
                type: 'data',
                data: btoa('\x18\x18\x18\x18\x18\x18\x18\x18')
            }));
            this.hideNotification();
        }
    }
}

// Export for use in app.js
window.ZmodemHandler = ZmodemHandler;