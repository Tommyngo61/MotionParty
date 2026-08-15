// Shared sound + vibration helper for both the host (TV) and player (phone) apps.
// All sound effects are synthesized in-browser via Web Audio (oscillators + a
// noise buffer) - no audio files to download, license, or ship, matching the
// project's "hand-drawn, nothing pre-made" approach to avatars.
(function () {
  let ctx = null;

  function getCtx() {
    const AC = window.AudioContext || window.webkitAudioContext;
    if (!AC) return null;
    if (!ctx) ctx = new AC();
    if (ctx.state === 'suspended') ctx.resume().catch(() => {});
    return ctx;
  }

  // Call this from the first real user gesture on the page (a tap/click) so iOS
  // Safari and Chrome autoplay policies let the AudioContext actually produce sound.
  function unlock() {
    getCtx();
  }

  function tone(freq, durMs, opts) {
    const c = getCtx();
    if (!c) return;
    const { type = 'sine', vol = 0.18, delayMs = 0, glideTo = null, attack = 0.01, release = 0.08 } = opts || {};
    const t0 = c.currentTime + delayMs / 1000;
    const dur = durMs / 1000;
    const osc = c.createOscillator();
    const gain = c.createGain();
    osc.type = type;
    osc.frequency.setValueAtTime(freq, t0);
    if (glideTo) osc.frequency.exponentialRampToValueAtTime(Math.max(1, glideTo), t0 + dur);
    gain.gain.setValueAtTime(0, t0);
    gain.gain.linearRampToValueAtTime(vol, t0 + attack);
    gain.gain.linearRampToValueAtTime(0, t0 + dur + release);
    osc.connect(gain).connect(c.destination);
    osc.start(t0);
    osc.stop(t0 + dur + release + 0.03);
  }

  // Filtered white noise burst - used for whooshes, hits, and gunshot-ish transients
  // that a pure oscillator tone can't convincingly produce.
  function noise(durMs, opts) {
    const c = getCtx();
    if (!c) return;
    const { vol = 0.15, delayMs = 0, filterFreq = 1200 } = opts || {};
    const t0 = c.currentTime + delayMs / 1000;
    const dur = durMs / 1000;
    const bufferSize = Math.max(1, Math.floor(c.sampleRate * dur));
    const buffer = c.createBuffer(1, bufferSize, c.sampleRate);
    const data = buffer.getChannelData(0);
    for (let i = 0; i < bufferSize; i++) data[i] = Math.random() * 2 - 1;
    const src = c.createBufferSource();
    src.buffer = buffer;
    const filter = c.createBiquadFilter();
    filter.type = 'lowpass';
    filter.frequency.value = filterFreq;
    const gain = c.createGain();
    gain.gain.setValueAtTime(0, t0);
    gain.gain.linearRampToValueAtTime(vol, t0 + 0.005);
    gain.gain.linearRampToValueAtTime(0, t0 + dur);
    src.connect(filter).connect(gain).connect(c.destination);
    src.start(t0);
    src.stop(t0 + dur + 0.03);
  }

  // Plays tones back-to-back. Each note may override type/vol/etc like tone() does,
  // plus an optional `gap` (ms of silence before the *next* note starts, default 40).
  function sequence(notes) {
    let t = 0;
    (notes || []).forEach((n) => {
      tone(n.freq, n.dur, Object.assign({}, n, { delayMs: t + (n.delayMs || 0) }));
      t += n.dur + (n.gap != null ? n.gap : 40);
    });
  }

  function vibrate(pattern) {
    try {
      if (navigator.vibrate) navigator.vibrate(pattern);
    } catch (e) {
      /* unsupported - silently ignore */
    }
  }

  // Named presets so game code can just call MP_Feedback.play('win') instead of
  // hand-rolling oscillator settings everywhere. Add to this as new moments need
  // a cue - keep names generic (not per-game) so future games can reuse them too.
  const PRESETS = {
    tick: () => tone(880, 60, { type: 'square', vol: 0.12 }),
    go: () => {
      sequence([{ freq: 660, dur: 90, type: 'triangle', vol: 0.2 }, { freq: 990, dur: 150, type: 'triangle', vol: 0.22 }]);
      vibrate(40);
    },
    ready: () => tone(392, 140, { type: 'sine', vol: 0.16 }),
    set: () => tone(523, 140, { type: 'sine', vol: 0.18 }),
    fire: () => {
      tone(880, 90, { type: 'square', vol: 0.22 });
      noise(140, { vol: 0.2, filterFreq: 2200 });
      vibrate(40);
    },
    swing: () => noise(90, { vol: 0.14, filterFreq: 3000 }),
    hit: () => {
      noise(70, { vol: 0.18, filterFreq: 2600 });
      tone(220, 60, { type: 'triangle', vol: 0.12 });
    },
    point: () => sequence([{ freq: 784, dur: 80, type: 'sine', vol: 0.18 }, { freq: 988, dur: 130, type: 'sine', vol: 0.2 }]),
    success: () => sequence([{ freq: 659, dur: 80, type: 'sine', vol: 0.18 }, { freq: 988, dur: 140, type: 'sine', vol: 0.2 }]),
    fall: () => tone(220, 220, { type: 'sawtooth', vol: 0.16, glideTo: 80 }),
    finish: () => sequence([
      { freq: 523, dur: 100, type: 'triangle', vol: 0.2 },
      { freq: 659, dur: 100, type: 'triangle', vol: 0.2 },
      { freq: 784, dur: 220, type: 'triangle', vol: 0.22 },
    ]),
    win: () => {
      sequence([
        { freq: 523, dur: 110, type: 'triangle', vol: 0.2 },
        { freq: 659, dur: 110, type: 'triangle', vol: 0.2 },
        { freq: 784, dur: 110, type: 'triangle', vol: 0.2 },
        { freq: 1047, dur: 280, type: 'triangle', vol: 0.24 },
      ]);
      vibrate([0, 60]);
    },
    lose: () => {
      sequence([{ freq: 330, dur: 160, type: 'sawtooth', vol: 0.18 }, { freq: 220, dur: 280, type: 'sawtooth', vol: 0.18 }]);
      vibrate([0, 120, 60, 120]);
    },
    error: () => {
      tone(160, 220, { type: 'sawtooth', vol: 0.2 });
      vibrate([0, 90, 60, 90]);
    },
  };

  function play(name, opts) {
    const fn = PRESETS[name];
    if (fn) fn(opts);
  }

  window.MP_Feedback = { tone, noise, sequence, vibrate, play, unlock };
})();
