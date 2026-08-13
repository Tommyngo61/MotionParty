(function () {
  const STYLE_ID = 'sotc-style';
  function ensureStyle() {
    if (document.getElementById(STYLE_ID)) return;
    const style = document.createElement('style');
    style.id = STYLE_ID;
    style.textContent = `
      .sotc-wrap{position:relative;width:100%;min-height:82vh;display:flex;flex-direction:column;
        border-radius:20px;overflow:hidden;background:#0d1024}
      .sotc-hud{display:flex;justify-content:space-between;align-items:center;padding:.6rem 1rem;
        background:rgba(0,0,0,.35);font-weight:800;z-index:4}
      .sotc-hud .flat-warn{color:var(--accent-orange);font-size:.85rem;opacity:0;transition:opacity .15s}
      .sotc-hud .flat-warn.show{opacity:1}
      .sotc-canvas-wrap{position:relative;flex:1}
      canvas{position:absolute;inset:0;width:100%;height:100%;display:block}
      .sotc-msg{position:absolute;inset:0;display:flex;align-items:center;justify-content:center;
        flex-direction:column;gap:.4rem;text-align:center;padding:1.5rem;background:rgba(10,6,26,0);
        pointer-events:none;transition:background .15s;z-index:5}
      .sotc-msg.show{background:rgba(10,6,26,.72)}
      .sotc-msg .big{font-size:2rem;font-weight:900;text-shadow:0 3px 0 rgba(0,0,0,.4)}
      .sotc-msg .sub{color:rgba(255,255,255,.8)}
      .sotc-fall-flash{position:absolute;inset:0;background:var(--accent-red);opacity:0;
        transition:opacity .1s;pointer-events:none;z-index:3}
      .sotc-fall-flash.on{opacity:.28}
    `;
    document.head.appendChild(style);
  }

  const MAX_TILT_DEG = 24;
  const ACCEL = 3.4;
  const DAMPING = 2.4;
  const FALL_PAUSE_MS = 700;
  const FLAT_BETA_LIMIT = 42; // degrees of forward/back tilt still considered "flat enough"
  const PROGRESS_RELAY_MS = 180;
  const BALL_Y_FRAC = 0.74;
  const VISIBLE_SCALE = 0.6;

  function start(root, ctx) {
    ensureStyle();
    const { socket, opponent } = ctx;
    const TRACKS = window.MP_TRACKS;
    const centerlineX = window.MP_trackCenterlineX;

    root.innerHTML = `
      <div class="sotc-wrap">
        <div class="sotc-hud">
          <span id="sotc-track">Track 1/${TRACKS.length}</span>
          <span id="sotc-timer">0.0s</span>
          <span class="flat-warn" id="sotc-flat-warn">📱 Keep it flat!</span>
        </div>
        <div class="sotc-canvas-wrap">
          <canvas id="sotc-canvas"></canvas>
          <div class="sotc-fall-flash" id="sotc-flash"></div>
          <div class="sotc-msg" id="sotc-msg"><div class="big" id="sotc-msg-big"></div><div class="sub" id="sotc-msg-sub"></div></div>
        </div>
      </div>
    `;
    const canvas = root.querySelector('#sotc-canvas');
    const c2d = canvas.getContext('2d');
    const trackLabel = root.querySelector('#sotc-track');
    const timerLabel = root.querySelector('#sotc-timer');
    const flatWarn = root.querySelector('#sotc-flat-warn');
    const flash = root.querySelector('#sotc-flash');
    const msgBox = root.querySelector('#sotc-msg');
    const msgBig = root.querySelector('#sotc-msg-big');
    const msgSub = root.querySelector('#sotc-msg-sub');

    function setMsg(big, sub, show) {
      msgBig.textContent = big || '';
      msgSub.textContent = sub || '';
      msgBox.classList.toggle('show', !!show);
    }
    setMsg('Get ready…', opponent ? `Racing ${opponent.name} across 4 tracks` : 'Racing across 4 tracks', true);

    let W = 0, H = 0;
    function resize() {
      const rect = canvas.parentElement.getBoundingClientRect();
      W = canvas.width = rect.width;
      H = canvas.height = rect.height;
    }
    resize();
    window.addEventListener('resize', resize);

    // ---------- orientation ----------
    let lastGamma = 0, lastBeta = 0;
    function onOrientation(e) {
      if (typeof e.gamma === 'number') lastGamma = e.gamma;
      if (typeof e.beta === 'number') lastBeta = e.beta;
    }
    window.addEventListener('deviceorientation', onOrientation);

    // ---------- race state ----------
    let raceState = 'waiting'; // waiting | racing | falling | finished
    let trackIndex = 0;
    let progress = 0;
    let ballX = 0;
    let ballVelX = 0;
    let attempts = TRACKS.map(() => 1);
    let fallAt = 0;
    let raceStartTs = 0;
    let lastRelay = 0;
    let rafId = null;
    let lastFrameTs = 0;

    function scaleX() { return W * 0.42; }
    function screenXFor(xNorm) { return W / 2 + xNorm * scaleX(); }

    function relayProgress(force) {
      const now = performance.now();
      if (!force && now - lastRelay < PROGRESS_RELAY_MS) return;
      lastRelay = now;
      socket.emit('player:input', {
        gameEvent: 'stayontrack:progress',
        payload: { trackIndex, progress: Math.min(1, progress), attempts: attempts[trackIndex] },
      });
    }

    function fallOff() {
      raceState = 'falling';
      fallAt = performance.now();
      attempts[trackIndex]++;
      flash.classList.add('on');
      setTimeout(() => flash.classList.remove('on'), 180);
      setMsg('OFF TRACK!', 'Restarting this track…', true);
      if (navigator.vibrate) navigator.vibrate([0, 90, 60, 90]);
      relayProgress(true);
    }

    function resumeAfterFall() {
      progress = 0;
      ballVelX = 0;
      ballX = centerlineX(trackIndex, 0);
      raceState = 'racing';
      setMsg('', '', false);
    }

    function completeTrack() {
      if (trackIndex < TRACKS.length - 1) {
        trackIndex++;
        progress = 0;
        ballVelX = 0;
        ballX = centerlineX(trackIndex, 0);
        trackLabel.textContent = `Track ${trackIndex + 1}/${TRACKS.length}`;
        flashToast(`${TRACKS[trackIndex - 1].name} complete!`);
        relayProgress(true);
      } else {
        const totalTimeMs = performance.now() - raceStartTs;
        raceState = 'finished';
        setMsg('🏁 FINISHED!', `Your time: ${(totalTimeMs / 1000).toFixed(1)}s — waiting for the result…`, true);
        socket.emit('stayontrack:finish', { totalTimeMs });
        relayProgress(true);
      }
    }

    let toastTimer = null;
    function flashToast(text) {
      setMsg(text, '', true);
      clearTimeout(toastTimer);
      toastTimer = setTimeout(() => { if (raceState === 'racing') setMsg('', '', false); }, 700);
    }

    function onGo() {
      raceStartTs = performance.now();
      trackIndex = 0;
      progress = 0;
      ballVelX = 0;
      ballX = centerlineX(0, 0);
      raceState = 'racing';
      setMsg('', '', false);
    }
    socket.on('stayontrack:go', onGo);

    function onResult(result) {
      raceState = 'finished';
      const myId = ctx.playerId;
      const won = result.winnerId === myId;
      if (won) {
        setMsg('🏆 YOU WIN!', `Finished in ${(result.winnerTimeMs / 1000).toFixed(1)}s`, true);
      } else {
        setMsg('Opponent finished first', `${opponent ? opponent.name : 'They'} won this race.`, true);
      }
      if (navigator.vibrate) navigator.vibrate(won ? [0, 60] : [0, 120, 60, 120]);
    }
    socket.on('stayontrack:result', onResult);

    // ---------- main loop ----------
    function frame(now) {
      const dt = Math.min(0.05, lastFrameTs ? (now - lastFrameTs) / 1000 : 0.016);
      lastFrameTs = now;
      update(now, dt);
      draw();
      rafId = requestAnimationFrame(frame);
    }

    function update(now, dt) {
      const isFlat = Math.abs(lastBeta) < FLAT_BETA_LIMIT;
      flatWarn.classList.toggle('show', raceState === 'racing' && !isFlat);

      if (raceState === 'falling') {
        if (now - fallAt >= FALL_PAUSE_MS) resumeAfterFall();
        return;
      }
      if (raceState !== 'racing') return;

      if (isFlat) {
        const tilt = Math.max(-1, Math.min(1, lastGamma / MAX_TILT_DEG));
        ballVelX += tilt * ACCEL * dt;
      }
      ballVelX *= Math.max(0, 1 - DAMPING * dt);
      ballX += ballVelX * dt;

      const track = TRACKS[trackIndex];
      progress += track.speed * dt;

      if (progress >= 1) {
        completeTrack();
        return;
      }
      const cl = centerlineX(trackIndex, progress);
      if (Math.abs(ballX - cl) > track.width / 2) {
        fallOff();
        return;
      }
      timerLabel.textContent = ((now - raceStartTs) / 1000).toFixed(1) + 's';
      relayProgress(false);
      // Read-only snapshot for automated testing/debugging; never read by gameplay itself.
      window.MP_SOT_DEBUG = { trackIndex, progress, ballX, ballVelX, raceState };
    }

    function draw() {
      c2d.clearRect(0, 0, W, H);
      const track = TRACKS[trackIndex];
      const rows = 48;
      const leftPts = [];
      const rightPts = [];
      for (let i = 0; i <= rows; i++) {
        const rf = i / rows;
        const rowProgress = progress + (BALL_Y_FRAC - rf) * VISIBLE_SCALE;
        const cl = centerlineX(trackIndex, rowProgress);
        const y = rf * H;
        leftPts.push([screenXFor(cl - track.width / 2), y]);
        rightPts.push([screenXFor(cl + track.width / 2), y]);
      }
      c2d.fillStyle = '#161a38';
      c2d.fillRect(0, 0, W, H);
      c2d.beginPath();
      c2d.moveTo(leftPts[0][0], leftPts[0][1]);
      for (const p of leftPts) c2d.lineTo(p[0], p[1]);
      for (let i = rightPts.length - 1; i >= 0; i--) c2d.lineTo(rightPts[i][0], rightPts[i][1]);
      c2d.closePath();
      c2d.fillStyle = '#2b6f5c';
      c2d.fill();
      c2d.strokeStyle = '#ffd23f';
      c2d.lineWidth = 3;
      c2d.beginPath();
      leftPts.forEach((p, i) => (i === 0 ? c2d.moveTo(p[0], p[1]) : c2d.lineTo(p[0], p[1])));
      c2d.stroke();
      c2d.beginPath();
      rightPts.forEach((p, i) => (i === 0 ? c2d.moveTo(p[0], p[1]) : c2d.lineTo(p[0], p[1])));
      c2d.stroke();

      if (raceState === 'racing' || raceState === 'falling') {
        const bx = screenXFor(ballX);
        const by = BALL_Y_FRAC * H;
        c2d.beginPath();
        c2d.ellipse(bx, by + 10, 14, 5, 0, 0, Math.PI * 2);
        c2d.fillStyle = 'rgba(0,0,0,.35)';
        c2d.fill();
        c2d.beginPath();
        c2d.arc(bx, by, 13, 0, Math.PI * 2);
        c2d.fillStyle = raceState === 'falling' ? '#ff4d4d' : '#ffe15e';
        c2d.fill();
        c2d.strokeStyle = '#8a6a00';
        c2d.lineWidth = 2;
        c2d.stroke();
      }
    }

    rafId = requestAnimationFrame(frame);

    return {
      stop() {
        cancelAnimationFrame(rafId);
        clearTimeout(toastTimer);
        window.removeEventListener('resize', resize);
        window.removeEventListener('deviceorientation', onOrientation);
        socket.off('stayontrack:go', onGo);
        socket.off('stayontrack:result', onResult);
      },
    };
  }

  window.MP_CONTROLLERS = window.MP_CONTROLLERS || {};
  window.MP_CONTROLLERS.stayontrack = { start };
})();
