(function () {
  const STYLE_ID = 'sotc-style';
  function ensureStyle() {
    if (document.getElementById(STYLE_ID)) return;
    const style = document.createElement('style');
    style.id = STYLE_ID;
    style.textContent = `
      .sotc-wrap{position:relative;width:100%;min-height:82vh;display:flex;flex-direction:column;
        border-radius:20px;overflow:hidden;background:linear-gradient(180deg,#8fd3ff 0%,#cdeeff 100%)}
      .sotc-hud{display:flex;justify-content:space-between;align-items:center;padding:.6rem 1rem;
        background:rgba(0,0,0,.35);color:#fff;font-weight:800;z-index:4}
      .sotc-canvas-wrap{position:relative;flex:1}
      canvas{position:absolute;inset:0;width:100%;height:100%;display:block}
      .sotc-msg{position:absolute;inset:0;display:flex;align-items:center;justify-content:center;
        flex-direction:column;gap:.4rem;text-align:center;padding:1.5rem;background:rgba(10,6,26,0);color:#fff;
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

  const MAX_TILT_DEG = 24;      // gamma (left/right) range mapped to full steering accel
  // Low damping + a velocity cap instead of high damping - the ball keeps rolling on
  // its own like it's actually got weight, instead of snapping to a stop the instant
  // you level the phone. You have to tilt the other way to actually kill its momentum.
  const STEER_ACCEL = 3.8;
  const STEER_DAMPING = 0.9;
  const MAX_STEER_VEL = 1.8;
  const MAX_PITCH_DEG = 30;     // beta (forward/back) range from level mapped to full speed
  const PITCH_DEADZONE_DEG = 4; // ignore tiny unintentional pitch near level
  // Tilting the top of the phone away from you speeds the ball up; flip this to 1 if
  // it feels backwards on your device.
  const PITCH_SIGN = -1;
  const START_FLAT_DEG = 12;    // both axes must be within this of level to start the race
  const FALL_PAUSE_MS = 900;
  const FALL_DROP_PX = 160;
  const PROGRESS_RELAY_MS = 180;
  const BALL_Y_FRAC = 0.74;
  const VISIBLE_SCALE = 0.6;
  const LOOP_RX_FRAC = 0.34;
  const LOOP_RY_FRAC = 0.3;

  function start(root, ctx) {
    ensureStyle();
    const { socket, opponents } = ctx;
    const TRACKS = window.MP_TRACKS;
    const centerlineX = window.MP_trackCenterlineX;

    root.innerHTML = `
      <div class="sotc-wrap">
        <div class="sotc-hud">
          <span id="sotc-track">Track 1/${TRACKS.length}</span>
          <span id="sotc-timer">0.0s</span>
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
    const flash = root.querySelector('#sotc-flash');
    const msgBox = root.querySelector('#sotc-msg');
    const msgBig = root.querySelector('#sotc-msg-big');
    const msgSub = root.querySelector('#sotc-msg-sub');

    function setMsg(big, sub, show) {
      msgBig.textContent = big || '';
      msgSub.textContent = sub || '';
      msgBox.classList.toggle('show', !!show);
    }
    const raceIntro = opponents.length === 0 ? 'Racing across 4 tracks'
      : opponents.length === 1 ? `Racing ${opponents[0].name} across 4 tracks`
      : `Racing ${opponents.length} others across 4 tracks`;
    setMsg('Get ready…', raceIntro, true);

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

    function pitchSpeedMult() {
      const mag = Math.abs(lastBeta);
      if (mag < PITCH_DEADZONE_DEG) return 0;
      const norm = Math.min(1, (mag - PITCH_DEADZONE_DEG) / (MAX_PITCH_DEG - PITCH_DEADZONE_DEG));
      return PITCH_SIGN * Math.sign(lastBeta) * norm;
    }

    function isFlatEnough() {
      return Math.abs(lastBeta) < START_FLAT_DEG && Math.abs(lastGamma) < START_FLAT_DEG;
    }

    // ---------- race state ----------
    let raceState = 'waiting'; // waiting | racing | falling | finished
    let pendingGo = false; // server said go, but waiting on the phone to be held flat first
    let trackIndex = 0;
    let progress = 0; // linear tracks: 0..1. loop tracks: 0..laps
    let ballX = 0; // offset from centerline: horizontal for linear tracks, radial for loop tracks
    let ballVelX = 0;
    let attempts = TRACKS.map(() => 1);
    let fallAt = 0;
    let raceStartTs = 0;
    let lastRelay = 0;
    let rafId = null;
    let lastFrameTs = 0;

    function scaleX() { return W * 0.42; }
    function screenXFor(xNorm) { return W / 2 + xNorm * scaleX(); }
    function loopRX() { return W * LOOP_RX_FRAC; }
    function loopRY() { return H * LOOP_RY_FRAC; }

    function requiredProgressFor(track) { return track.type === 'loop' ? track.laps : 1; }

    function relayProgress(force) {
      const now = performance.now();
      if (!force && now - lastRelay < PROGRESS_RELAY_MS) return;
      lastRelay = now;
      const track = TRACKS[trackIndex];
      socket.emit('player:input', {
        gameEvent: 'stayontrack:progress',
        payload: { trackIndex, progress: Math.min(1, progress / requiredProgressFor(track)), attempts: attempts[trackIndex] },
      });
    }

    function fallOff() {
      raceState = 'falling';
      fallAt = performance.now();
      attempts[trackIndex]++;
      flash.classList.add('on');
      setTimeout(() => flash.classList.remove('on'), 180);
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

    function beginRace() {
      raceStartTs = performance.now();
      trackIndex = 0;
      progress = 0;
      ballVelX = 0;
      ballX = centerlineX(0, 0);
      raceState = 'racing';
      pendingGo = false;
      setMsg('', '', false);
    }

    function onGo() {
      if (isFlatEnough()) {
        beginRace();
      } else {
        pendingGo = true;
        setMsg('Level your phone!', 'Hold it flat to start the race', true);
      }
    }
    socket.on('stayontrack:go', onGo);

    function onResult(result) {
      raceState = 'finished';
      const myId = ctx.playerId;
      const won = result.winnerId === myId;
      if (won) {
        setMsg('🏆 YOU WIN!', `Finished in ${(result.winnerTimeMs / 1000).toFixed(1)}s`, true);
      } else {
        const winner = opponents.find((p) => p.id === result.winnerId);
        setMsg('Someone finished first', `${winner ? winner.name : 'Another racer'} won this race.`, true);
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
      if (pendingGo) {
        if (isFlatEnough()) beginRace();
        return;
      }
      if (raceState === 'falling') {
        if (now - fallAt >= FALL_PAUSE_MS) resumeAfterFall();
        return;
      }
      if (raceState !== 'racing') return;

      const track = TRACKS[trackIndex];

      // left/right tilt steers (offset from centerline) - low damping + a velocity
      // cap so the ball keeps rolling with its own momentum, like it has real weight
      const steer = Math.max(-1, Math.min(1, lastGamma / MAX_TILT_DEG));
      ballVelX += steer * STEER_ACCEL * dt;
      ballVelX *= Math.max(0, 1 - STEER_DAMPING * dt);
      ballVelX = Math.max(-MAX_STEER_VEL, Math.min(MAX_STEER_VEL, ballVelX));
      ballX += ballVelX * dt;

      // forward/back tilt controls speed - forward speeds up, back slows/reverses
      progress = Math.max(0, progress + pitchSpeedMult() * track.speed * dt);

      const required = requiredProgressFor(track);
      if (progress >= required) {
        completeTrack();
        return;
      }
      const lapT = track.type === 'loop' ? progress % 1 : progress;
      const cl = centerlineX(trackIndex, lapT);
      if (Math.abs(ballX - cl) > track.width / 2) {
        fallOff();
        return;
      }
      timerLabel.textContent = ((now - raceStartTs) / 1000).toFixed(1) + 's';
      trackLabel.textContent = track.type === 'loop'
        ? `Track ${trackIndex + 1}/${TRACKS.length} • Lap ${Math.min(track.laps, Math.floor(progress) + 1)}/${track.laps}`
        : `Track ${trackIndex + 1}/${TRACKS.length}`;
      relayProgress(false);
      // Read-only snapshot for automated testing/debugging; never read by gameplay itself.
      window.MP_SOT_DEBUG = { trackIndex, progress, ballX, ballVelX, raceState };
    }

    function ballScreenPos(track) {
      if (track.type === 'loop') {
        const lapT = ((progress % 1) + 1) % 1;
        const theta = lapT * Math.PI * 2;
        const r = 1 + ballX;
        return { x: W / 2 + Math.cos(theta) * r * loopRX(), y: H / 2 + Math.sin(theta) * r * loopRY() };
      }
      return { x: screenXFor(ballX), y: BALL_Y_FRAC * H };
    }

    function drawBallAt(bx, by, scale, alpha, isFalling) {
      c2d.save();
      c2d.globalAlpha = alpha;
      c2d.beginPath();
      c2d.ellipse(bx, by + 10 * scale, 14 * scale, 5 * scale, 0, 0, Math.PI * 2);
      c2d.fillStyle = 'rgba(0,0,0,.35)';
      c2d.fill();
      c2d.beginPath();
      c2d.arc(bx, by, 13 * scale, 0, Math.PI * 2);
      c2d.fillStyle = isFalling ? '#ff4d4d' : '#ffe15e';
      c2d.fill();
      c2d.strokeStyle = '#8a6a00';
      c2d.lineWidth = 2;
      c2d.stroke();
      c2d.restore();
    }

    function drawLinear(track) {
      c2d.fillStyle = '#bdeaff';
      c2d.fillRect(0, 0, W, H);
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
    }

    function drawLoop(track) {
      c2d.fillStyle = '#bdeaff';
      c2d.fillRect(0, 0, W, H);
      const cx = W / 2, cy = H / 2;
      const rx = loopRX(), ry = loopRY();
      const half = track.width / 2;
      const rows = 96;
      c2d.beginPath();
      for (let i = 0; i <= rows; i++) {
        const t = i / rows;
        const theta = t * Math.PI * 2;
        const r = 1 + centerlineX(trackIndex, t) + half;
        const x = cx + Math.cos(theta) * r * rx;
        const y = cy + Math.sin(theta) * r * ry;
        i === 0 ? c2d.moveTo(x, y) : c2d.lineTo(x, y);
      }
      for (let i = rows; i >= 0; i--) {
        const t = i / rows;
        const theta = t * Math.PI * 2;
        const r = 1 + centerlineX(trackIndex, t) - half;
        const x = cx + Math.cos(theta) * r * rx;
        const y = cy + Math.sin(theta) * r * ry;
        c2d.lineTo(x, y);
      }
      c2d.closePath();
      c2d.fillStyle = '#2b6f5c';
      c2d.fill();
      c2d.strokeStyle = '#ffd23f';
      c2d.lineWidth = 2;
      c2d.stroke();

      // start/finish marker at lap-progress 0
      const cl0 = centerlineX(trackIndex, 0);
      const rInner = 1 + cl0 - half, rOuter = 1 + cl0 + half;
      c2d.strokeStyle = 'rgba(255,255,255,.85)';
      c2d.lineWidth = 3;
      c2d.beginPath();
      c2d.moveTo(cx + rInner * rx, cy);
      c2d.lineTo(cx + rOuter * rx, cy);
      c2d.stroke();

      if (raceState === 'racing' || raceState === 'falling') {
        c2d.font = '700 14px system-ui, sans-serif';
        c2d.textAlign = 'center';
        const lapNum = Math.min(track.laps, Math.floor(progress) + 1);
        const label = `Lap ${lapNum}/${track.laps}`;
        c2d.strokeStyle = 'rgba(255,255,255,.85)';
        c2d.lineWidth = 3;
        c2d.strokeText(label, cx, cy);
        c2d.fillStyle = '#16233d';
        c2d.fillText(label, cx, cy);
      }
    }

    function draw() {
      const track = TRACKS[trackIndex];
      if (track.type === 'loop') drawLoop(track); else drawLinear(track);

      if (raceState === 'racing' || raceState === 'falling') {
        const pos = ballScreenPos(track);
        let scale = 1, alpha = 1, dropY = 0;
        if (raceState === 'falling') {
          const t = Math.min(1, (performance.now() - fallAt) / FALL_PAUSE_MS);
          const eased = t * t;
          dropY = eased * FALL_DROP_PX;
          scale = Math.max(0.15, 1 - 0.75 * eased);
          alpha = Math.max(0, 1 - eased);
        }
        drawBallAt(pos.x, pos.y + dropY, scale, alpha, raceState === 'falling');
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
