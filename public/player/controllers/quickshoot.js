(function () {
  const STYLE_ID = 'qsc-style';
  function ensureStyle() {
    if (document.getElementById(STYLE_ID)) return;
    const style = document.createElement('style');
    style.id = STYLE_ID;
    style.textContent = `
      .qsc{width:100%;min-height:80vh;display:flex;flex-direction:column;align-items:center;
        justify-content:center;text-align:center;border-radius:24px;transition:background .2s;color:#fff;
        background:#333}
      .qsc.red{background:radial-gradient(circle,#ff6b6b,#7a1f1f)}
      .qsc.orange{background:radial-gradient(circle,#ffb85e,#7a4a1f)}
      .qsc.green{background:radial-gradient(circle,#5cf0a0,#0f6d3f)}
      .qsc.win{background:radial-gradient(circle,#ffe15e,#8a6a00)}
      .qsc.lose{background:radial-gradient(circle,#8f8f8f,#2a2a2a)}
      .qsc .qsc-vs{font-size:1rem;color:rgba(255,255,255,.7);margin-bottom:.4rem}
      .qsc .qsc-title{font-size:2rem;font-weight:900;text-shadow:0 3px 0 rgba(0,0,0,.4)}
      .qsc .qsc-sub{margin-top:.6rem;font-size:1.1rem;color:rgba(255,255,255,.85)}
      .qsc .qsc-time{margin-top:1rem;font-size:1.6rem;font-weight:800}
      .qsc-icon{font-size:4rem;margin-bottom:.4rem}
    `;
    document.head.appendChild(style);
  }

  const READY_THRESHOLD = 55;   // deg/s rotation, or accel delta - trips false-start check
  const DRAW_THRESHOLD = 70;

  function motionEnergy(e, baseline) {
    let rotMag = 0;
    if (e.rotationRate) {
      const r = e.rotationRate;
      rotMag = Math.sqrt((r.alpha || 0) ** 2 + (r.beta || 0) ** 2 + (r.gamma || 0) ** 2);
    }
    let accMag = 0;
    const a = e.acceleration && e.acceleration.x != null ? e.acceleration : e.accelerationIncludingGravity;
    if (a) {
      accMag = Math.sqrt((a.x || 0) ** 2 + (a.y || 0) ** 2 + (a.z || 0) ** 2);
      accMag = Math.abs(accMag - baseline.value) * 6; // scale roughly into the rotMag range
    }
    return Math.max(rotMag, accMag);
  }

  function start(root, ctx) {
    ensureStyle();
    const { socket, opponent } = ctx;
    let phase = 'walk';
    let armed = false; // true once 'ready' received -> false-start watch active
    let resolved = false;
    let fireLocalTs = 0;
    const baseline = { value: 9.8, samples: 0 };

    root.innerHTML = `
      <div class="qsc" id="qsc-box">
        <div class="qsc-icon">🤠</div>
        <div class="qsc-vs">${opponent ? 'Duel vs ' + escapeHtml(opponent.name) : 'Quick Draw Duel'}</div>
        <div class="qsc-title" id="qsc-title">Get in position…</div>
        <div class="qsc-sub" id="qsc-sub">Hold your phone flat and still.</div>
        <div class="qsc-time" id="qsc-time"></div>
      </div>
    `;
    const box = root.querySelector('#qsc-box');
    const title = root.querySelector('#qsc-title');
    const sub = root.querySelector('#qsc-sub');
    const timeEl = root.querySelector('#qsc-time');

    function onMotion(e) {
      if (resolved) return;
      // Calibrate a resting baseline briefly at the very start.
      const a = e.acceleration && e.acceleration.x != null ? e.acceleration : e.accelerationIncludingGravity;
      if (baseline.samples < 20 && a) {
        const mag = Math.sqrt((a.x || 0) ** 2 + (a.y || 0) ** 2 + (a.z || 0) ** 2);
        baseline.value = (baseline.value * baseline.samples + mag) / (baseline.samples + 1);
        baseline.samples++;
      }
      const energy = motionEnergy(e, baseline);

      if (phase === 'fire') {
        if (energy > DRAW_THRESHOLD) {
          resolved = true;
          const reactionTime = performance.now() - fireLocalTs;
          socket.emit('quickshoot:draw', { reactionTime });
          title.textContent = 'DRAWN!';
          sub.textContent = 'Waiting for the result…';
        }
      } else if (armed && energy > READY_THRESHOLD) {
        resolved = true;
        socket.emit('quickshoot:falseStart');
        box.className = 'qsc red';
        title.textContent = 'TOO EARLY!';
        sub.textContent = "You moved before FIRE.";
      }
    }
    window.addEventListener('devicemotion', onMotion);

    function onState({ phase: p }) {
      phase = p;
      if (p === 'ready') {
        armed = true;
        box.className = 'qsc red';
        title.textContent = 'READY…';
        sub.textContent = 'Hold still!';
      } else if (p === 'set') {
        box.className = 'qsc orange';
        title.textContent = 'SET…';
        sub.textContent = 'Almost there — stay still!';
      } else if (p === 'fire') {
        fireLocalTs = performance.now();
        box.className = 'qsc green';
        title.textContent = 'FIRE!! 🔫';
        sub.textContent = 'Flick your phone up now!';
        if (navigator.vibrate) navigator.vibrate(40);
      }
    }

    function onResult(result) {
      resolved = true;
      const myId = ctx.playerId;
      const won = result.winnerId === myId;
      const lost = result.loserId === myId;
      box.className = 'qsc ' + (won ? 'win' : lost ? 'lose' : '');
      if (result.reason === 'falseStart') {
        title.textContent = lost ? 'You drew too early!' : (won ? 'Opponent jumped the gun — You Win!' : 'False start');
      } else if (result.reason === 'noShow') {
        title.textContent = "Nobody drew — it's a wash";
      } else {
        title.textContent = won ? '🏆 YOU WIN!' : lost ? '💥 YOU LOSE' : 'Duel over';
      }
      sub.textContent = 'Look at the TV for the replay.';
      if (result.times && result.times[myId] != null) {
        timeEl.textContent = result.times[myId] + ' ms';
      }
      if (navigator.vibrate) navigator.vibrate(lost ? [0, 120, 60, 120] : won ? [0, 60] : []);
    }

    socket.on('quickshoot:state', onState);
    socket.on('quickshoot:result', onResult);

    return {
      stop() {
        window.removeEventListener('devicemotion', onMotion);
        socket.off('quickshoot:state', onState);
        socket.off('quickshoot:result', onResult);
      },
    };
  }

  function escapeHtml(s) {
    return String(s).replace(/[&<>"']/g, (c) => ({
      '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
    }[c]));
  }

  window.MP_CONTROLLERS = window.MP_CONTROLLERS || {};
  window.MP_CONTROLLERS.quickshoot = { start };
})();
