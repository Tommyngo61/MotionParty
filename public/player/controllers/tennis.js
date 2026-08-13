(function () {
  const STYLE_ID = 'tnc-style';
  function ensureStyle() {
    if (document.getElementById(STYLE_ID)) return;
    const style = document.createElement('style');
    style.id = STYLE_ID;
    style.textContent = `
      .tnc{width:100%;min-height:80vh;display:flex;flex-direction:column;align-items:center;
        justify-content:center;text-align:center;border-radius:24px;
        background:radial-gradient(circle,#3fd0c9,#0f5a55);transition:transform .08s}
      .tnc.swung{transform:scale(1.04)}
      .tnc .tnc-icon{font-size:5rem}
      .tnc .tnc-title{font-size:1.6rem;font-weight:900;margin-top:.5rem;text-shadow:0 3px 0 rgba(0,0,0,.3)}
      .tnc .tnc-sub{margin-top:.5rem;color:rgba(255,255,255,.85)}
      .tnc .tnc-count{margin-top:1.2rem;font-size:1rem;color:rgba(255,255,255,.6)}
    `;
    document.head.appendChild(style);
  }

  const SWING_THRESHOLD = 16; // m/s^2 above resting magnitude
  const REFRACTORY_MS = 320;

  function start(root, ctx) {
    ensureStyle();
    const { socket, opponent } = ctx;

    root.innerHTML = `
      <div class="tnc" id="tnc-box">
        <div class="tnc-icon">🎾</div>
        <div class="tnc-title">Swing your phone!</div>
        <div class="tnc-sub">${opponent ? 'vs ' + escapeHtml(opponent.name) : 'Time your swing to the ball on screen'}</div>
        <div class="tnc-count" id="tnc-count">Swings: 0</div>
      </div>
    `;
    const box = root.querySelector('#tnc-box');
    const countEl = root.querySelector('#tnc-count');
    let swings = 0;
    let lastSwingAt = 0;
    let lastGamma = 0;

    function onOrientation(e) {
      if (typeof e.gamma === 'number') lastGamma = e.gamma;
    }
    window.addEventListener('deviceorientation', onOrientation);

    function onMotion(e) {
      const now = performance.now();
      if (now - lastSwingAt < REFRACTORY_MS) return;
      const a = e.acceleration && e.acceleration.x != null ? e.acceleration : e.accelerationIncludingGravity;
      if (!a) return;
      const mag = Math.sqrt((a.x || 0) ** 2 + (a.y || 0) ** 2 + (a.z || 0) ** 2);
      const delta = Math.abs(mag - 9.8);
      if (delta > SWING_THRESHOLD) {
        lastSwingAt = now;
        swings++;
        countEl.textContent = 'Swings: ' + swings;
        box.classList.add('swung');
        setTimeout(() => box.classList.remove('swung'), 140);
        if (navigator.vibrate) navigator.vibrate(15);
        const tilt = Math.max(-1, Math.min(1, lastGamma / 45));
        socket.emit('player:input', {
          gameEvent: 'tennis:swing',
          payload: { power: delta, tilt },
        });
      }
    }
    window.addEventListener('devicemotion', onMotion);

    return {
      stop() {
        window.removeEventListener('devicemotion', onMotion);
        window.removeEventListener('deviceorientation', onOrientation);
      },
    };
  }

  function escapeHtml(s) {
    return String(s).replace(/[&<>"']/g, (c) => ({
      '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
    }[c]));
  }

  window.MP_CONTROLLERS = window.MP_CONTROLLERS || {};
  window.MP_CONTROLLERS.tennis = { start };
})();
