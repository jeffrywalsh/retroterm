// Minimal DirectoryManager shim (custom directory UI removed)
class DirectoryManager {
  constructor() {
    this.isConnected = false;
  }
  setConnectionStatus(connected) {
    this.isConnected = connected;
    if (window.DEBUG) console.log('DirectoryManager: connection =', connected);
  }
}

// Initialize minimal manager for compatibility
let directoryManager;
document.addEventListener('DOMContentLoaded', () => {
  directoryManager = new DirectoryManager();
});

