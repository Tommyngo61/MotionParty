(function () {
  const STYLE_ID = 'tmc-style';
  function ensureStyle() {
    if (document.getElementById(STYLE_ID)) return;
    const style = document.createElement('style');
    style.id = STYLE_ID;
    style.textContent = `
      .tmc-wrap{position:relative;width:100%;min-height:82vh;display:flex;flex-direction:column;
        border-radius:20px;overflow:hidden;background:linear-gradient(180deg,#8fd3ff 0%,#cdeeff 100%)}
      .tmc-hud{display:flex;justify-content:space-between;align-items:center;padding:.6rem 1rem;
        background:rgba(0,0,0,.35);color:#fff;font-weight:800;z-index:4}
      .tmc-canvas-wrap{position:relative;flex:1}
      canvas{position:absolute;inset:0;width:100%;height:100%;display:block}
      .tmc-msg{position:absolute;inset:0;display:flex;align-items:center;justify-content:center;
        flex-direction:column;gap:.4rem;text-align:center;padding:1.5rem;background:rgba(10,6,26,0);color:#fff;
        pointer-events:none;transition:background .15s;z-index:5}
      .tmc-msg.show{background:rgba(10,6,26,.72)}
      .tmc-msg .big{font-size:2rem;font-weight:900;text-shadow:0 3px 0 rgba(0,0,0,.4)}
      .tmc-msg .sub{color:rgba(255,255,255,.8)}
    `;
    document.head.appendChild(style);
  }

  const MAX_TILT_DEG = 24;
  // Same "rolling ball" feel as Stay on Track: low damping + a velocity cap so the
  // ball carries its own momentum instead of snapping to a stop when leveled.
  const ACCEL = 3.8;
  const DAMPING = 0.9;
  const MAX_VEL = 2.4;
  const START_FLAT_DEG = 12; // both axes must be within this of level to start the race
  const BALL_R = 0.16;  // ball radius, in maze cell-units
  const WALL_HALF = 0.06; // wall half-thickness, in maze cell-units
  const FINISH_R = 0.32; // capture radius around the finish hole, in cell-units
  const PROGRESS_RELAY_MS = 180;

  function clamp(v, lo, hi) { return Math.max(lo, Math.min(hi, v)); }

  function start(root, ctx) {
    ensureStyle();
    const { socket, opponents } = ctx;
    const MAZES = window.MP_MAZES;

    root.innerHTML = `
      <div class="tmc-wrap">
        <div class="tmc-hud">
          <span id="tmc-maze">Maze 1/${MAZES.length}</span>
          <span id="tmc-timer">0.0s</span>
        </div>
        <div class="tmc-canvas-wrap">
          <canvas id="tmc-canvas"></canvas>
          <div class="tmc-msg" id="tmc-msg"><div class="big" id="tmc-msg-big"></div><div class="sub" id="tmc-msg-sub"></div></div>
        </div>
      </div>
    `;
    const canvas = root.querySelector('#tmc-canvas');
    const c2d = canvas.getContext('2d');
    const mazeLabel = root.querySelector('#tmc-maze');
    const timerLabel = root.querySelector('#tmc-timer');
    const msgBox = root.querySelector('#tmc-msg');
    const msgBig = root.querySelector('#tmc-msg-big');
    const msgSub = root.querySelector('#tmc-msg-sub');

    function setMsg(big, sub, show) {
      msgBig.textContent = big || '';
      msgSub.textContent = sub || '';
      msgBox.classList.toggle('show', !!show);
    }
    const raceIntro = opponents.length === 0 ? 'Racing through 2 mazes'
      : opponents.length === 1 ? `Racing ${opponents[0].name} through 2 mazes`
      : `Racing ${opponents.length} others through 2 mazes`;
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

    function isFlatEnough() {
      return Math.abs(lastBeta) < START_FLAT_DEG && Math.abs(lastGamma) < START_FLAT_DEG;
    }

    // ---------- race state ----------
    let raceState = 'waiting'; // waiting | racing | finished
    let pendingGo = false;
    let mazeIndex = 0;
    let ballX = 0, ballY = 0, velX = 0, velY = 0;
    let initialDist = 1;
    let raceStartTs = 0;
    let lastRelay = 0;
    let rafId = null;
    let lastFrameTs = 0;

    function resetBall(idx) {
      const maze = MAZES[idx];
      ballX = maze.start.x;
      ballY = maze.start.y;
      velX = 0;
      velY = 0;
      initialDist = Math.hypot(maze.finish.x - ballX, maze.finish.y - ballY) || 1;
    }

    function relayProgress(force, distToFinish) {
      const now = performance.now();
      if (!force && now - lastRelay < PROGRESS_RELAY_MS) return;
      lastRelay = now;
      const progress = clamp(1 - distToFinish / initialDist, 0, 1);
      socket.emit('player:input', {
        gameEvent: 'tiltmaze:progress',
        payload: { mazeIndex, progress },
      });
    }

    function beginRace() {
      raceStartTs = performance.now();
      mazeIndex = 0;
      resetBall(0);
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
    socket.on('tiltmaze:go', onGo);

    let toastTimer = null;
    function flashToast(text) {
      setMsg(text, '', true);
      clearTimeout(toastTimer);
      toastTimer = setTimeout(() => { if (raceState === 'racing') setMsg('', '', false); }, 700);
    }

    function completeMaze() {
      if (mazeIndex < MAZES.length - 1) {
        mazeIndex++;
        resetBall(mazeIndex);
        mazeLabel.textContent = `Maze ${mazeIndex + 1}/${MAZES.length}`;
        flashToast(`${MAZES[mazeIndex - 1].name} solved!`);
        relayProgress(true, initialDist);
      } else {
        const totalTimeMs = performance.now() - raceStartTs;
        raceState = 'finished';
        setMsg('🏁 SOLVED!', `Your time: ${(totalTimeMs / 1000).toFixed(1)}s — waiting for the result…`, true);
        socket.emit('tiltmaze:finish', { totalTimeMs });
        relayProgress(true, 0);
      }
    }

    function onResult(result) {
      raceState = 'finished';
      const myId = ctx.playerId;
      const won = result.winnerId === myId;
      if (won) {
        setMsg('🏆 YOU WIN!', `Solved it in ${(result.winnerTimeMs / 1000).toFixed(1)}s`, true);
      } else {
        const winner = opponents.find((p) => p.id === result.winnerId);
        setMsg('Someone finished first', `${winner ? winner.name : 'Another racer'} solved both mazes first.`, true);
      }
      if (navigator.vibrate) navigator.vibrate(won ? [0, 60] : [0, 120, 60, 120]);
    }
    socket.on('tiltmaze:result', onResult);

    // ---------- wall collision (circle vs. line segment, with sliding) ----------
    function resolveCollisions(walls) {
      for (let pass = 0; pass < 2; pass++) {
        for (const seg of walls) {
          const x1 = seg[0], y1 = seg[1], x2 = seg[2], y2 = seg[3];
          const dx = x2 - x1, dy = y2 - y1;
          const lenSq = dx * dx + dy * dy;
          let t = lenSq > 0 ? ((ballX - x1) * dx + (ballY - y1) * dy) / lenSq : 0;
          t = clamp(t, 0, 1);
          const cx = x1 + t * dx, cy = y1 + t * dy;
          const distX = ballX - cx, distY = ballY - cy;
          const dist = Math.hypot(distX, distY);
          const minDist = BALL_R + WALL_HALF;
          if (dist < minDist) {
            const safe = dist > 1e-6;
            const nx = safe ? distX / dist : 1;
            const ny = safe ? distY / dist : 0;
            const pen = minDist - (safe ? dist : 0);
            ballX += nx * pen;
            ballY += ny * pen;
            const vn = velX * nx + velY * ny;
            if (vn < 0) {
              velX -= vn * nx;
              velY -= vn * ny;
            }
          }
        }
      }
    }

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
      if (raceState !== 'racing') return;

      const maze = MAZES[mazeIndex];

      // Tilting left/right moves the ball left/right; tilting the top of the phone
      // away from you (same convention fixed for Stay on Track) rolls it "forward" -
      // up and away on screen.
      const tiltX = clamp(lastGamma / MAX_TILT_DEG, -1, 1);
      const tiltY = clamp(lastBeta / MAX_TILT_DEG, -1, 1);
      velX += tiltX * ACCEL * dt;
      velY += tiltY * ACCEL * dt;
      const damp = Math.max(0, 1 - DAMPING * dt);
      velX *= damp;
      velY *= damp;
      const speed = Math.hypot(velX, velY);
      if (speed > MAX_VEL) {
        velX = (velX / speed) * MAX_VEL;
        velY = (velY / speed) * MAX_VEL;
      }
      ballX += velX * dt;
      ballY += velY * dt;
      resolveCollisions(maze.walls);
      // Defensive clamp - the outer maze boundary is always walled, so this should
      // never actually engage, but it guarantees the ball can never leave the grid.
      ballX = clamp(ballX, BALL_R, maze.cols - BALL_R);
      ballY = clamp(ballY, BALL_R, maze.rows - BALL_R);

      const distToFinish = Math.hypot(maze.finish.x - ballX, maze.finish.y - ballY);
      if (distToFinish < FINISH_R) {
        completeMaze();
        return;
      }
      timerLabel.textContent = ((now - raceStartTs) / 1000).toFixed(1) + 's';
      relayProgress(false, distToFinish);
      // Read-only snapshot for automated testing/debugging; never read by gameplay itself.
      window.MP_TM_DEBUG = { mazeIndex, ballX, ballY, velX, velY, raceState };
    }

    function draw() {
      const maze = MAZES[mazeIndex];
      c2d.fillStyle = '#bdeaff';
      c2d.fillRect(0, 0, W, H);

      const cellPx = Math.min(W / maze.cols, H / maze.rows) * 0.92;
      const gridW = cellPx * maze.cols, gridH = cellPx * maze.rows;
      const offX = (W - gridW) / 2, offY = (H - gridH) / 2;
      const sx = (x) => offX + x * cellPx;
      const sy = (y) => offY + y * cellPx;

      c2d.fillStyle = '#fff8e1';
      c2d.fillRect(sx(0), sy(0), gridW, gridH);

      // finish hole
      c2d.beginPath();
      c2d.arc(sx(maze.finish.x), sy(maze.finish.y), cellPx * FINISH_R, 0, Math.PI * 2);
      c2d.fillStyle = '#16233d';
      c2d.fill();

      // start marker
      c2d.beginPath();
      c2d.arc(sx(maze.start.x), sy(maze.start.y), cellPx * 0.12, 0, Math.PI * 2);
      c2d.strokeStyle = '#3ddc84';
      c2d.lineWidth = 3;
      c2d.stroke();

      // walls
      c2d.strokeStyle = '#16233d';
      c2d.lineWidth = Math.max(2, cellPx * WALL_HALF * 2);
      c2d.lineCap = 'round';
      c2d.beginPath();
      for (const seg of maze.walls) {
        c2d.moveTo(sx(seg[0]), sy(seg[1]));
        c2d.lineTo(sx(seg[2]), sy(seg[3]));
      }
      c2d.stroke();

      if (raceState === 'racing') {
        const bx = sx(ballX), by = sy(ballY);
        const r = cellPx * BALL_R;
        c2d.beginPath();
        c2d.ellipse(bx, by + r * 0.6, r * 1.1, r * 0.4, 0, 0, Math.PI * 2);
        c2d.fillStyle = 'rgba(0,0,0,.3)';
        c2d.fill();
        const grad = c2d.createRadialGradient(bx - r * 0.35, by - r * 0.35, r * 0.15, bx, by, r);
        grad.addColorStop(0, '#eef0f2');
        grad.addColorStop(1, '#7c8288');
        c2d.beginPath();
        c2d.arc(bx, by, r, 0, Math.PI * 2);
        c2d.fillStyle = grad;
        c2d.fill();
        c2d.strokeStyle = '#4a4e52';
        c2d.lineWidth = 1;
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
        socket.off('tiltmaze:go', onGo);
        socket.off('tiltmaze:result', onResult);
      },
    };
  }

  window.MP_CONTROLLERS = window.MP_CONTROLLERS || {};
  window.MP_CONTROLLERS.tiltmaze = { start };
})();
