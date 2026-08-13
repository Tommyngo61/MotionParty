(function () {
  const STYLE_ID = 'tm-style';
  function ensureStyle() {
    if (document.getElementById(STYLE_ID)) return;
    const style = document.createElement('style');
    style.id = STYLE_ID;
    style.textContent = `
      .tm-wrap{position:absolute;inset:0;background:radial-gradient(circle at 50% 0%,#bdeaff,#8fd3ff 70%);
        color:#16233d;display:flex;flex-direction:column;align-items:center;padding:2rem;overflow:hidden}
      .tm-title{font-size:1.6rem;font-weight:900;margin-bottom:2rem}
      .tm-lanes{display:flex;flex-direction:column;gap:2.2rem;width:100%;max-width:760px}
      .tm-lane{display:flex;align-items:center;gap:1rem}
      .tm-lane img{width:56px;height:56px;border-radius:50%;background:#fff;flex-shrink:0}
      .tm-lane-info{flex:1;min-width:0}
      .tm-lane-name{font-weight:800;margin-bottom:.4rem}
      .tm-segs{display:flex;gap:.4rem}
      .tm-seg{flex:1;height:22px;border-radius:8px;background:var(--card-light);position:relative;overflow:hidden}
      .tm-seg .fill{position:absolute;inset:0;width:0%;background:linear-gradient(90deg,#3fd0c9,#3ddc84);
        transition:width .12s linear}
      .tm-seg.done .fill{width:100%!important;background:linear-gradient(90deg,#ffd23f,#ff9f3f)}
      .tm-msg{position:absolute;inset:0;display:flex;align-items:center;justify-content:center;
        flex-direction:column;gap:.6rem;background:rgba(10,6,26,.75);color:#fff;z-index:5}
      .tm-msg .big{font-size:3rem;font-weight:900;text-shadow:0 4px 0 rgba(0,0,0,.4)}
      .tm-msg .sub{color:rgba(255,255,255,.65);font-size:1.1rem;text-align:center;max-width:480px}
      .tm-result-actions{display:flex;gap:1rem;margin-top:1rem}
    `;
    document.head.appendChild(style);
  }

  function avatarUrl(avatarId) {
    const a = window.MP_getAvatar(avatarId);
    return 'data:image/svg+xml;utf8,' + encodeURIComponent(a.svg);
  }

  const TM_COUNTDOWN_MS = 3000; // mirrors server's TM_COUNTDOWN_MS

  function start(root, ctx) {
    ensureStyle();
    const { socket, roomCode, players, onExit } = ctx;
    const MAZES = window.MP_MAZES;
    let timers = [];
    const setT = (fn, ms) => { const id = setTimeout(fn, ms); timers.push(id); return id; };

    function laneHtml(p) {
      const segs = MAZES.map(() => '<div class="tm-seg"><div class="fill"></div></div>').join('');
      return `
        <div class="tm-lane" data-player="${p.id}">
          <img src="${avatarUrl(p.avatarId)}" />
          <div class="tm-lane-info">
            <div class="tm-lane-name">${escapeHtml(p.name)}</div>
            <div class="tm-segs" data-segs>${segs}</div>
          </div>
        </div>`;
    }

    root.innerHTML = `
      <div class="tm-wrap">
        <div class="tm-title">🧭 Tilt Maze</div>
        <div class="tm-lanes">${players.map(laneHtml).join('')}</div>
        <div class="tm-msg" id="tm-msg">
          <div class="big" id="tm-msg-big">Get Ready…</div>
          <div class="sub">Hold your phone flat to start! Tilt it to roll the iron ball through the maze to the hole — first to solve both mazes wins.</div>
        </div>
      </div>
    `;
    const wrap = root.querySelector('.tm-wrap');
    const msgBox = root.querySelector('#tm-msg');
    const msgBig = root.querySelector('#tm-msg-big');

    setT(() => (msgBig.textContent = '3…'), TM_COUNTDOWN_MS - 2400);
    setT(() => (msgBig.textContent = '2…'), TM_COUNTDOWN_MS - 1600);
    setT(() => (msgBig.textContent = '1…'), TM_COUNTDOWN_MS - 800);

    function laneEl(playerId) {
      return wrap.querySelector(`.tm-lane[data-player="${playerId}"]`);
    }

    function updateLane(playerId, { mazeIndex, progress }) {
      const lane = laneEl(playerId);
      if (!lane) return;
      const segEls = lane.querySelectorAll('[data-segs] .tm-seg');
      segEls.forEach((seg, i) => {
        const fill = seg.querySelector('.fill');
        if (i < mazeIndex) {
          seg.classList.add('done');
        } else if (i === mazeIndex) {
          seg.classList.remove('done');
          fill.style.width = Math.min(100, progress * 100) + '%';
        } else {
          seg.classList.remove('done');
          fill.style.width = '0%';
        }
      });
    }

    function onInputRelay(data) {
      if (data.gameEvent === 'tiltmaze:progress') {
        updateLane(data.playerId, data.payload);
      }
    }
    socket.on('input:relay', onInputRelay);

    function onGo() {
      msgBig.textContent = 'GO!';
      msgBox.style.transition = 'opacity .3s';
      setT(() => (msgBox.style.opacity = '0'), 500);
      setT(() => (msgBox.style.display = 'none'), 900);
    }
    socket.on('tiltmaze:go', onGo);

    function onResult(result) {
      socket.off('input:relay', onInputRelay);
      const byId = Object.fromEntries(players.map((p) => [p.id, p]));
      const winner = byId[result.winnerId];
      msgBox.style.display = 'flex';
      msgBox.style.opacity = '1';
      msgBig.textContent = winner ? `🏆 ${winner.name} wins!` : 'Race over!';
      const sub = document.createElement('div');
      sub.className = 'sub';
      sub.textContent = winner ? `Solved both mazes in ${(result.winnerTimeMs / 1000).toFixed(1)}s` : '';
      const actions = document.createElement('div');
      actions.className = 'tm-result-actions';
      actions.innerHTML = `<button class="btn ghost" id="tm-menu">Back to Menu</button><button class="btn primary" id="tm-again">Play Again</button>`;
      msgBox.appendChild(sub);
      msgBox.appendChild(actions);
      msgBox.querySelector('#tm-menu').addEventListener('click', onExit);
      msgBox.querySelector('#tm-again').addEventListener('click', () => {
        socket.emit('host:startMatch', { code: roomCode, game: 'tiltmaze' });
      });
    }
    socket.on('tiltmaze:result', onResult);

    return {
      stop() {
        timers.forEach(clearTimeout);
        socket.off('input:relay', onInputRelay);
        socket.off('tiltmaze:go', onGo);
        socket.off('tiltmaze:result', onResult);
      },
    };
  }

  function escapeHtml(s) {
    return String(s).replace(/[&<>"']/g, (c) => ({
      '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
    }[c]));
  }

  window.MP_GAMES = window.MP_GAMES || {};
  window.MP_GAMES.tiltmaze = { start };
})();
