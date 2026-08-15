(function () {
  const STYLE_ID = 'qsc-style';
  function ensureStyle() {
    if (document.getElementById(STYLE_ID)) return;
    const style = document.createElement('style');
    style.id = STYLE_ID;
    style.textContent = `
      .qsc{width:100%;min-height:80vh;display:flex;flex-direction:column;align-items:center;
        justify-content:center;text-align:center;border-radius:24px;transition:background .2s,color .2s;
        background:#bfe3ff;color:#0b2b4a}
      .qsc.red{background:radial-gradient(circle,#ff6b6b,#7a1f1f);color:#fff}
      .qsc.countdown{background:radial-gradient(circle,#ff8f5e,#7a3a1f);color:#fff}
      .qsc.green{background:radial-gradient(circle,#5cf0a0,#0f6d3f);color:#fff}
      .qsc.win{background:radial-gradient(circle,#ffe15e,#8a6a00);color:#fff}
      .qsc.lose{background:radial-gradient(circle,#8f8f8f,#2a2a2a);color:#fff}
      .qsc .qsc-vs{font-size:1rem;opacity:.7;margin-bottom:.4rem}
      .qsc .qsc-title{font-size:2rem;font-weight:900;text-shadow:0 3px 0 rgba(0,0,0,.2)}
      .qsc .qsc-sub{margin-top:.6rem;font-size:1.1rem;opacity:.85}
      .qsc-icon{font-size:4rem;margin-bottom:.4rem}
      .qsc-countdown-num{font-size:4.5rem;font-weight:900;margin-top:.4rem;text-shadow:0 4px 0 rgba(0,0,0,.35)}
    `;
    document.head.appendChild(style);
  }

  const READY_THRESHOLD = 55;   // deg/s rotation, or accel delta - trips false-start check
  const DRAW_THRESHOLD = 70;
  const POINT_DOWN_Y = -7;      // accelerationIncludingGravity.y this negative == phone held vertical, top pointed at the ground
  const POINT_DOWN_HOLD_MS = 250; // must sustain the pose this long before we call it confirmed

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
    let armed = false; // true once 'countdown' received -> false-start watch active
    let resolved = false;
    let fireLocalTs = 0;
    let aimSignaled = false; // true once we've told the server we're holding the aim-down pose
    let pointDownSince = null;
    const baseline = { value: 9.8, samples: 0 };

    root.innerHTML = `
      <div class="qsc" id="qsc-box">
        <div class="qsc-icon">🤠</div>
        <div class="qsc-vs">${opponent ? 'Duel vs ' + escapeHtml(opponent.name) : 'Quick Draw Duel'}</div>
        <div class="qsc-title" id="qsc-title">Get in position…</div>
        <div class="qsc-sub" id="qsc-sub">Point your phone straight down at your side and hold it there.</div>
        <div class="qsc-countdown-num" id="qsc-countdown-num"></div>
      </div>
    `;
    const box = root.querySelector('#qsc-box');
    const title = root.querySelector('#qsc-title');
    const sub = root.querySelector('#qsc-sub');
    const countdownNum = root.querySelector('#qsc-countdown-num');
    let countdownInterval = null;

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

      // Phone held vertically, top pointed at the ground (like a holstered gun) -
      // accelerationIncludingGravity.y reads strongly negative in that pose. Held
      // for a beat so a brief pass-through mid-swing doesn't count.
      if (phase === 'walk' && !aimSignaled) {
        const g = e.accelerationIncludingGravity;
        if (g && g.y != null && g.y < POINT_DOWN_Y) {
          if (pointDownSince == null) pointDownSince = performance.now();
          else if (performance.now() - pointDownSince >= POINT_DOWN_HOLD_MS) {
            aimSignaled = true;
            socket.emit('quickshoot:aimReady');
            if (window.MP_Feedback) window.MP_Feedback.play('success');
            title.textContent = 'Aimed!';
            sub.textContent = 'Hold still — waiting for opponent…';
          }
        } else {
          pointDownSince = null;
        }
      }

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
        if (window.MP_Feedback) window.MP_Feedback.play('error');
        box.className = 'qsc red';
        title.textContent = 'TOO EARLY!';
        sub.textContent = "You moved before FIRE.";
      }
    }
    window.addEventListener('devicemotion', onMotion);

    function onState({ phase: p, ts, durationMs }) {
      phase = p;
      clearInterval(countdownInterval);
      if (p === 'countdown') {
        armed = true;
        box.className = 'qsc countdown';
        title.textContent = 'Get ready…';
        sub.textContent = 'Hold still until FIRE!';
        const endsAt = ts + durationMs;
        let lastSecond = null;
        const tick = () => {
          const secondsLeft = Math.max(1, Math.ceil((endsAt - Date.now()) / 1000));
          if (secondsLeft !== lastSecond) {
            lastSecond = secondsLeft;
            if (window.MP_Feedback) window.MP_Feedback.play('tick');
          }
          countdownNum.textContent = secondsLeft;
        };
        tick();
        countdownInterval = setInterval(tick, 200);
      } else if (p === 'fire') {
        fireLocalTs = performance.now();
        box.className = 'qsc green';
        title.textContent = 'FIRE!! 🔫';
        sub.textContent = 'Flick your phone up now!';
        countdownNum.textContent = '';
        if (window.MP_Feedback) window.MP_Feedback.play('fire');
        if (navigator.vibrate) navigator.vibrate(40);
      }
    }

    function onResult(result) {
      resolved = true;
      clearInterval(countdownInterval);
      const myId = ctx.playerId;
      const won = result.winnerId === myId;
      const lost = result.loserId === myId;
      box.className = 'qsc ' + (won ? 'win' : lost ? 'lose' : '');
      countdownNum.textContent = '';
      if (result.reason === 'falseStart') {
        title.textContent = lost ? 'You drew too early!' : (won ? 'Opponent jumped the gun — You Win!' : 'False start');
      } else if (result.reason === 'noShow') {
        title.textContent = "Nobody drew — it's a wash";
      } else {
        title.textContent = won ? '🏆 YOU WIN!' : lost ? '💥 YOU LOSE' : 'Duel over';
      }
      sub.textContent = 'Look at the TV for the replay.';
      if (window.MP_Feedback) window.MP_Feedback.play(won ? 'win' : lost ? 'lose' : 'error');
      if (navigator.vibrate) navigator.vibrate(lost ? [0, 120, 60, 120] : won ? [0, 60] : []);
    }

    socket.on('quickshoot:state', onState);
    socket.on('quickshoot:result', onResult);

    return {
      stop() {
        clearInterval(countdownInterval);
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
