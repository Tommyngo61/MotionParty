(function () {
  const STYLE_ID = 'tn-style';
  function ensureStyle() {
    if (document.getElementById(STYLE_ID)) return;
    const style = document.createElement('style');
    style.id = STYLE_ID;
    style.textContent = `
      .tn-wrap{position:absolute;inset:0;background:linear-gradient(180deg,#8fd3ff 0%,#bdeaff 38%,#2c7a3d 38%);
        overflow:hidden}
      .tn-scorebar{position:absolute;top:10px;left:50%;transform:translateX(-50%);
        background:rgba(10,6,26,.55);padding:.5rem 1.4rem;border-radius:14px;font-weight:800;
        font-size:1.3rem;z-index:5;display:flex;gap:1rem;align-items:center}
      .tn-scorebar .tn-vs{opacity:.6;font-weight:600;font-size:1rem}
      canvas{position:absolute;inset:0;width:100%;height:100%}
      .tn-msg{position:absolute;top:38%;left:50%;transform:translate(-50%,-50%);
        font-size:2.4rem;font-weight:900;text-shadow:0 4px 0 rgba(0,0,0,.35);z-index:5;pointer-events:none;
        opacity:0;transition:opacity .15s}
      .tn-msg.show{opacity:1}
      .tn-result{position:absolute;inset:0;background:rgba(10,6,26,.9);display:flex;flex-direction:column;
        align-items:center;justify-content:center;gap:1rem;text-align:center;z-index:10}
      .tn-result h1{font-size:2.4rem;margin:0}
      .tn-result .tn-actions{display:flex;gap:1rem;margin-top:1rem}
    `;
    document.head.appendChild(style);
  }

  const WIN_SCORE = 5;
  const BASE_TRAVEL_MS = 1150;
  const MIN_TRAVEL_MS = 780;
  const TRAVEL_DECAY = 30;
  const PEAK_HEIGHT = 0.85;
  const GRACE_MS = 480;
  const SERVE_DELAY_MS = 900;
  const SWING_WINDOW_T = 0.55; // segment-A fraction after which a swing counts

  function loadAvatarImg(avatarId) {
    const a = window.MP_getAvatar(avatarId);
    const img = new Image();
    img.src = 'data:image/svg+xml;utf8,' + encodeURIComponent(a.svg);
    return img;
  }

  function start(root, ctx) {
    ensureStyle();
    const { socket, roomCode, players, onExit } = ctx;
    root.innerHTML = `
      <div class="tn-wrap">
        <div class="tn-scorebar">
          <span id="tn-p1name"></span><span id="tn-score1">0</span>
          <span class="tn-vs">-</span>
          <span id="tn-score2">0</span><span id="tn-p2name"></span>
        </div>
        <canvas id="tn-canvas"></canvas>
        <div class="tn-msg" id="tn-msg"></div>
      </div>
    `;
    const wrap = root.querySelector('.tn-wrap');
    const canvas = root.querySelector('#tn-canvas');
    const cx2d = canvas.getContext('2d');
    const msgEl = root.querySelector('#tn-msg');
    root.querySelector('#tn-p1name').textContent = players[0].name;
    root.querySelector('#tn-p2name').textContent = players[1].name;

    const imgs = [loadAvatarImg(players[0].avatarId), loadAvatarImg(players[1].avatarId)];
    const depth = [0.92, 0.08]; // baseline depth for player 0 (near) and 1 (far)
    const avatarX = [0, 0];     // current eased on-court x position, -1..1
    const swingFlash = [0, 0];  // timestamps until which the swing flash shows

    const score = [0, 0];
    let running = true;
    let W = 0, H = 0;

    function resize() {
      const rect = wrap.getBoundingClientRect();
      W = canvas.width = rect.width;
      H = canvas.height = rect.height;
    }
    resize();
    window.addEventListener('resize', resize);

    // ---------- projection helpers ----------
    const courtTopY = () => H * 0.13;
    const courtBottomY = () => H * 0.9;
    const nearHalfW = () => W * 0.4;
    const farHalfW = () => W * 0.13;
    function projX(x, d) {
      const hw = farHalfW() + d * (nearHalfW() - farHalfW());
      return W / 2 + x * hw;
    }
    function projY(d) { return courtTopY() + d * (courtBottomY() - courtTopY()); }
    function projScale(d) { return 0.45 + d * 1.0; }

    // ---------- ball / rally state ----------
    let phase = 'serving'; // serving | flight | grace | matchover
    let serverIndex = 0;
    let hitterIndex = 0, receiverIndex = 1;
    let t = 0, travelMs = BASE_TRAVEL_MS, flightStart = 0;
    let x0 = 0, x1 = 0;
    let graceStart = 0;
    let serveTimer = null;

    function showMsg(text, ms) {
      msgEl.textContent = text;
      msgEl.classList.add('show');
      if (ms) setTimeout(() => { if (running) msgEl.classList.remove('show'); }, ms);
    }

    function queueServe() {
      phase = 'serving';
      showMsg(`${players[serverIndex].name} serves…`, SERVE_DELAY_MS - 100);
      serveTimer = setTimeout(() => {
        if (!running) return;
        hitterIndex = serverIndex;
        receiverIndex = 1 - serverIndex;
        x0 = avatarX[hitterIndex];
        x1 = (Math.random() * 1.4 - 0.7);
        travelMs = BASE_TRAVEL_MS;
        t = 0;
        flightStart = performance.now();
        phase = 'flight';
        msgEl.classList.remove('show');
      }, SERVE_DELAY_MS);
    }

    function ballHeightForT(tt) {
      if (tt <= 1) return PEAK_HEIGHT * Math.sin(Math.PI * Math.min(1, Math.max(0, tt)));
      return 0;
    }

    function awardPoint(winnerIdx) {
      score[winnerIdx]++;
      root.querySelector('#tn-score1').textContent = score[0];
      root.querySelector('#tn-score2').textContent = score[1];
      if (score[winnerIdx] >= WIN_SCORE) {
        endMatch(winnerIdx);
        return;
      }
      showMsg(winnerIdx === hitterIndex ? 'OUT!' : 'POINT!', 900);
      serverIndex = 1 - serverIndex;
      setTimeout(() => { if (running) queueServe(); }, 1000);
    }

    function registerHit(byIndex, tilt) {
      hitterIndex = byIndex;
      receiverIndex = 1 - byIndex;
      x0 = clamp(currentBallX(), -1, 1);
      const tiltInfluence = clamp(tilt || 0, -1, 1) * 0.55;
      x1 = clamp((Math.random() * 1.0 - 0.5) + tiltInfluence, -0.9, 0.9);
      travelMs = Math.max(MIN_TRAVEL_MS, travelMs - TRAVEL_DECAY);
      t = 0;
      flightStart = performance.now();
      phase = 'flight';
      swingFlash[byIndex] = performance.now() + 220;
    }

    function currentBallX() {
      const tt = Math.min(1, t);
      return x0 + (x1 - x0) * tt;
    }

    function onSwing({ playerId, payload }) {
      const idx = players.findIndex((p) => p.id === playerId);
      if (idx === -1) return;
      swingFlash[idx] = performance.now() + 180;
      if (phase === 'matchover') return;
      const activeReceiverWindow =
        (phase === 'flight' && idx === receiverIndex && t >= SWING_WINDOW_T) ||
        (phase === 'grace' && idx === receiverIndex);
      if (activeReceiverWindow) {
        registerHit(idx, payload && payload.tilt);
      }
    }

    function onInputRelay(data) {
      if (data.gameEvent === 'tennis:swing') onSwing(data);
    }
    socket.on('input:relay', onInputRelay);

    // ---------- main loop ----------
    let rafId = null;
    function frame(now) {
      if (!running) return;
      update(now);
      draw();
      rafId = requestAnimationFrame(frame);
    }

    function update(now) {
      if (phase === 'flight') {
        t = (now - flightStart) / travelMs;
        if (t >= 1) {
          phase = 'grace';
          graceStart = now;
          t = 1;
        }
      } else if (phase === 'grace') {
        if (now - graceStart >= GRACE_MS) {
          phase = 'scored';
          awardPoint(hitterIndex);
        }
      }

      // Auto-move: ease avatars toward where the ball is headed (Wii-style assisted positioning)
      for (let i = 0; i < 2; i++) {
        let target = 0;
        if ((phase === 'flight' || phase === 'grace') && receiverIndex === i) target = x1;
        else if (phase === 'flight' && hitterIndex === i) target = x0;
        avatarX[i] += (target - avatarX[i]) * 0.06;
      }
    }

    function draw() {
      cx2d.clearRect(0, 0, W, H);
      drawCourt();
      drawNet();
      // draw far player first (behind), then ball mid-air check, then near player
      drawPlayer(1);
      drawBallShadowAndBall();
      drawPlayer(0);
    }

    function drawCourt() {
      cx2d.fillStyle = '#2c7a3d';
      cx2d.fillRect(0, courtTopY() - H * 0.02, W, H);
      cx2d.strokeStyle = 'rgba(255,255,255,.85)';
      cx2d.lineWidth = 3;
      cx2d.beginPath();
      cx2d.moveTo(projX(-1, 0), projY(0));
      cx2d.lineTo(projX(1, 0), projY(0));
      cx2d.lineTo(projX(1, 1), projY(1));
      cx2d.lineTo(projX(-1, 1), projY(1));
      cx2d.closePath();
      cx2d.stroke();
      cx2d.beginPath();
      cx2d.moveTo(projX(0, 0), projY(0));
      cx2d.lineTo(projX(0, 1), projY(1));
      cx2d.stroke();
    }

    function drawNet() {
      const d = 0.5;
      const y = projY(d) - H * 0.06;
      cx2d.strokeStyle = '#eee';
      cx2d.lineWidth = 3;
      cx2d.beginPath();
      cx2d.moveTo(projX(-1.05, d), y);
      cx2d.lineTo(projX(1.05, d), y);
      cx2d.stroke();
      cx2d.strokeStyle = 'rgba(255,255,255,.5)';
      cx2d.lineWidth = 1;
      for (let x = -1; x <= 1; x += 0.15) {
        cx2d.beginPath();
        cx2d.moveTo(projX(x, d), y);
        cx2d.lineTo(projX(x, d), y + H * 0.05);
        cx2d.stroke();
      }
    }

    function drawPlayer(i) {
      const img = imgs[i];
      if (!img.complete) return;
      const d = depth[i];
      const scale = projScale(d);
      let size = W * 0.11 * scale;
      const flashing = performance.now() < swingFlash[i];
      if (flashing) size *= 1.22;
      const px = projX(avatarX[i], d);
      const py = projY(d);
      cx2d.save();
      cx2d.translate(px, py);
      if (flashing) cx2d.rotate((i === 0 ? -1 : 1) * 0.18);
      cx2d.beginPath();
      cx2d.ellipse(0, size * 0.08, size * 0.55, size * 0.14, 0, 0, Math.PI * 2);
      cx2d.fillStyle = 'rgba(0,0,0,.25)';
      cx2d.fill();
      cx2d.beginPath();
      cx2d.arc(0, -size / 2, size / 2, 0, Math.PI * 2);
      cx2d.closePath();
      cx2d.save();
      cx2d.clip();
      cx2d.drawImage(img, -size / 2, -size, size, size);
      cx2d.restore();
      cx2d.restore();
    }

    function drawBallShadowAndBall() {
      if (phase !== 'flight' && phase !== 'grace') return;
      const tt = phase === 'grace' ? 1 : t;
      const bx = currentBallX();
      const bd = lerp(depth[hitterIndex], depth[receiverIndex], Math.min(1, tt));
      const scale = projScale(bd);
      const h = phase === 'grace'
        ? PEAK_HEIGHT * 0.25 * Math.sin(Math.PI * Math.min(1, (performance.now() - graceStart) / GRACE_MS))
        : ballHeightForT(t);
      const px = projX(bx, bd);
      const groundY = projY(bd);
      const shadowAlpha = Math.max(0.15, 0.45 - h * 0.35);

      cx2d.beginPath();
      cx2d.ellipse(px, groundY, 8 * scale, 3 * scale, 0, 0, Math.PI * 2);
      cx2d.fillStyle = `rgba(0,0,0,${shadowAlpha})`;
      cx2d.fill();

      const ballY = groundY - h * H * 0.28;
      cx2d.beginPath();
      cx2d.arc(px, ballY, 6.5 * scale, 0, Math.PI * 2);
      cx2d.fillStyle = '#e8ff3f';
      cx2d.fill();
      cx2d.strokeStyle = '#b8cc20';
      cx2d.lineWidth = 1;
      cx2d.stroke();
    }

    function endMatch(winnerIdx) {
      running = false;
      cancelAnimationFrame(rafId);
      clearTimeout(serveTimer);
      const overlay = document.createElement('div');
      overlay.className = 'tn-result';
      overlay.innerHTML = `
        <h1>🏆 ${escapeHtml(players[winnerIdx].name)} wins the match!</h1>
        <div>${score[0]} - ${score[1]}</div>
        <div class="tn-actions">
          <button class="btn ghost" id="tn-menu">Back to Menu</button>
          <button class="btn primary" id="tn-again">Play Again</button>
        </div>
      `;
      wrap.appendChild(overlay);
      overlay.querySelector('#tn-menu').addEventListener('click', onExit);
      overlay.querySelector('#tn-again').addEventListener('click', () => {
        socket.emit('host:startMatch', { code: roomCode, game: 'tennis' });
      });
    }

    queueServe();
    rafId = requestAnimationFrame(frame);

    return {
      stop() {
        running = false;
        cancelAnimationFrame(rafId);
        clearTimeout(serveTimer);
        window.removeEventListener('resize', resize);
        socket.off('input:relay', onInputRelay);
      },
    };
  }

  function lerp(a, b, t) { return a + (b - a) * t; }
  function clamp(v, min, max) { return Math.max(min, Math.min(max, v)); }
  function escapeHtml(s) {
    return String(s).replace(/[&<>"']/g, (c) => ({
      '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
    }[c]));
  }

  window.MP_GAMES = window.MP_GAMES || {};
  window.MP_GAMES.tennis = { start };
})();
