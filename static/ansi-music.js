// Minimal ANSI Music player for SyncTERM-style CSI | sequences.
// Payload grammar varies wildly; for now, support a tiny subset:
//  - Tn   : tempo (BPM) e.g., T120
//  - On   : octave 0-8
//  - Ln   : default note length (e.g., 4 = quarter, 8 = eighth)
//  - Notes: C D E F G A B, with optional #, and optional length after note (e.g., C8)
//  - Rn   : rest with optional length
// Unknown tokens are ignored. This is intentionally conservative.

class AnsiMusicPlayer {
  constructor() {
    this.ctx = null;
    this.tempo = 120; // BPM
    this.octave = 4;  // Default octave
    this.defLen = 4;  // Default length denominator
    this.queue = Promise.resolve();
  }

  ensureCtx() {
    if (!this.ctx) {
      const AC = window.AudioContext || window.webkitAudioContext;
      if (!AC) return false;
      this.ctx = new AC();
    }
    return true;
  }

  noteFreq(note, octave) {
    const base = { C:0, 'C#':1, D:2, 'D#':3, E:4, F:5, 'F#':6, G:7, 'G#':8, A:9, 'A#':10, B:11 };
    const semi = base[note];
    if (semi == null) return null;
    // A4 = 440 Hz, note number 69
    const n = semi + (octave+1)*12; // C0=12
    return 440 * Math.pow(2, (n - 69)/12);
  }

  durMs(den) {
    const beatMs = 60000/Math.max(30, Math.min(300, this.tempo));
    return (4/den) * beatMs;
  }

  playTone(freq, ms) {
    if (!this.ensureCtx()) return Promise.resolve();
    const ctx = this.ctx;
    const t0 = ctx.currentTime;
    const osc = ctx.createOscillator();
    const gain = ctx.createGain();
    osc.type = 'square';
    osc.frequency.value = freq;
    gain.gain.setValueAtTime(0.0001, t0);
    gain.gain.exponentialRampToValueAtTime(0.2, t0 + 0.01);
    gain.gain.exponentialRampToValueAtTime(0.0001, t0 + ms/1000);
    osc.connect(gain).connect(ctx.destination);
    osc.start(t0);
    osc.stop(t0 + ms/1000 + 0.02);
    return new Promise(res => setTimeout(res, ms * 0.95));
  }

  rest(ms) {
    return new Promise(res => setTimeout(res, ms));
  }

  parseAndQueue(payload) {
    // Ensure context and try to resume on first use
    if (this.ensureCtx() && this.ctx.state === 'suspended') {
      try { this.ctx.resume(); } catch {}
    }
    // Tokenize by simple regex: commands like T120, O4, L8, R4, C#4, etc.
    const re = /(T\d+|O\d+|L\d+|R\d+|[A-G]#?\d*)/gi;
    const tokens = payload.match(re) || [];
    for (const tok of tokens) {
      const T = tok.toUpperCase();
      if (T[0] === 'T') {
        const n = parseInt(T.slice(1), 10); if (!isNaN(n)) this.tempo = Math.max(30, Math.min(300, n));
      } else if (T[0] === 'O') {
        const n = parseInt(T.slice(1), 10); if (!isNaN(n)) this.octave = Math.max(0, Math.min(8, n));
      } else if (T[0] === 'L') {
        const n = parseInt(T.slice(1), 10); if (!isNaN(n)) this.defLen = Math.max(1, Math.min(64, n));
      } else if (T[0] === 'R') {
        const n = parseInt(T.slice(1), 10) || this.defLen;
        const ms = this.durMs(n);
        this.queue = this.queue.then(() => this.rest(ms));
      } else {
        // Note
        const m = /^([A-G]#?)(\d+)?$/.exec(T);
        if (!m) continue;
        const name = m[1];
        const len = m[2] ? parseInt(m[2], 10) : this.defLen;
        const freq = this.noteFreq(name, this.octave);
        if (!freq) continue;
        const ms = this.durMs(len);
        this.queue = this.queue.then(() => this.playTone(freq, ms));
      }
    }
  }
}

window.AnsiMusicPlayer = AnsiMusicPlayer;
