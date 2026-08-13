(function () {
  const STYLE_ID = 'sot-style';
  function ensureStyle() {
    if (document.getElementById(STYLE_ID)) return;
    const style = document.createElement('style');
    style.id = STYLE_ID;
    style.textContent = `
      .sot-wrap{position:absolute;inset:0;background:radial-gradient(circle at 50% 0%,#bdeaff,#8fd3ff 70%);
        color:#16233d;display:flex;flex-direction:column;align-items:center;padding:2rem;overflow:hidden}
      .sot-title{font-size:1.6rem;font-weight:900;margin-bottom:2rem}
      .sot-lanes{display:flex;flex-direction:column;gap:2.2rem;width:100%;max-width:760px}
      .sot-lane{display:flex;align-items:center;gap:1rem}
      .sot-lane img{width:56px;height:56px;border-radius:50%;background:#fff;flex-shrink:0}
      .sot-lane-info{flex:1;min-width:0}
      .sot-lane-name{font-weight:800;margin-bottom:.4rem;display:flex;justify-content:space-between}
      .sot-lane-name .attempt{color:var(--text-dim);font-weight:600;font-size:.85rem}
      .sot-segs{display:flex;gap:.4rem}
      .sot-seg{flex:1;height:22px;border-radius:8px;background:var(--card-light);position:relative;overflow:hidden}
      .sot-seg .fill{position:absolute;inset:0;width:0%;background:linear-gradient(90deg,#3fd0c9,#3ddc84);
        transition:width .12s linear}
      .sot-seg.done .fill{width:100%!important;background:linear-gradient(90deg,#ffd23f,#ff9f3f)}
      .sot-msg{position:absolute;inset:0;display:flex;align-items:center;justify-content:center;
        flex-direction:column;gap:.6rem;background:rgba(10,6,26,.75);color:#fff;z-index:5}
      .sot-msg .big{font-size:3rem;font-weight:900;text-shadow:0 4px 0 rgba(0,0,0,.4)}
      .sot-msg .sub{color:rgba(255,255,255,.65);font-size:1.1rem;text-align:center;max-width:480px}
      .sot-result-actions{display:flex;gap:1rem;margin-top:1rem}
    `;
    document.head.appendChild(style);
  }

  function avatarUrl(avatarId) {
    const a = window.MP_getAvatar(avatarId);
    return 'data:image/svg+xml;utf8,' + encodeURIComponent(a.svg);
  }

  const SOT_COUNTDOWN_MS = 3000; // mirrors server's SOT_COUNTDOWN_MS

  function start(root, ctx) {
    ensureStyle();
    const { socket, roomCode, players, onExit } = ctx;
    const TRACKS = window.MP_TRACKS;
    let timers = [];
    const setT = (fn, ms) => { const id = setTimeout(fn, ms); timers.push(id); return id; };

    function laneHtml(p) {
      const segs = TRACKS.map(() => '<div class="sot-seg"><div class="fill"></div></div>').join('');
      return `
        <div class="sot-lane" data-player="${p.id}">
          <img src="${avatarUrl(p.avatarId)}" />
          <div class="sot-lane-info">
            <div class="sot-lane-name"><span>${escapeHtml(p.name)}</span><span class="attempt" data-attempt></span></div>
            <div class="sot-segs" data-segs>${segs}</div>
          </div>
        </div>`;
    }

    root.innerHTML = `
      <div class="sot-wrap">
        <div class="sot-title">🛤️ Stay on Track</div>
        <div class="sot-lanes">${players.map(laneHtml).join('')}</div>
        <div class="sot-msg" id="sot-msg">
          <div class="big" id="sot-msg-big">Get Ready…</div>
          <div class="sub">Hold your phone flat to start! Then tilt forward/back to speed up or reverse, and left/right to steer. Fall off and you restart that track — Track 4 loops!</div>
        </div>
      </div>
    `;
    const wrap = root.querySelector('.sot-wrap');
    const msgBox = root.querySelector('#sot-msg');
    const msgBig = root.querySelector('#sot-msg-big');

    setT(() => (msgBig.textContent = '3…'), SOT_COUNTDOWN_MS - 2400);
    setT(() => (msgBig.textContent = '2…'), SOT_COUNTDOWN_MS - 1600);
    setT(() => (msgBig.textContent = '1…'), SOT_COUNTDOWN_MS - 800);

    function laneEl(playerId) {
      return wrap.querySelector(`.sot-lane[data-player="${playerId}"]`);
    }

    function updateLane(playerId, { trackIndex, progress, attempts }) {
      const lane = laneEl(playerId);
      if (!lane) return;
      const segEls = lane.querySelectorAll('[data-segs] .sot-seg');
      segEls.forEach((seg, i) => {
        const fill = seg.querySelector('.fill');
        if (i < trackIndex) {
          seg.classList.add('done');
        } else if (i === trackIndex) {
          seg.classList.remove('done');
          fill.style.width = Math.min(100, progress * 100) + '%';
        } else {
          seg.classList.remove('done');
          fill.style.width = '0%';
        }
      });
      const attemptEl = lane.querySelector('[data-attempt]');
      if (attemptEl) attemptEl.textContent = attempts > 1 ? `Attempt ${attempts}` : '';
    }

    function onInputRelay(data) {
      if (data.gameEvent === 'stayontrack:progress') {
        updateLane(data.playerId, data.payload);
      }
    }
    socket.on('input:relay', onInputRelay);

    function onGo() {
      msgBig.textContent = 'GO!';
      setT(() => msgBox.classList.add('hide-msg'), 500);
      msgBox.style.transition = 'opacity .3s';
      setT(() => (msgBox.style.opacity = '0'), 500);
      setT(() => (msgBox.style.display = 'none'), 900);
    }
    socket.on('stayontrack:go', onGo);

    function onResult(result) {
      socket.off('input:relay', onInputRelay);
      const byId = Object.fromEntries(players.map((p) => [p.id, p]));
      const winner = byId[result.winnerId];
      msgBox.style.display = 'flex';
      msgBox.style.opacity = '1';
      msgBig.textContent = winner ? `🏆 ${winner.name} wins!` : 'Race over!';
      const sub = document.createElement('div');
      sub.className = 'sub';
      sub.textContent = winner ? `Finished all 4 tracks in ${(result.winnerTimeMs / 1000).toFixed(1)}s` : '';
      const actions = document.createElement('div');
      actions.className = 'sot-result-actions';
      actions.innerHTML = `<button class="btn ghost" id="sot-menu">Back to Menu</button><button class="btn primary" id="sot-again">Play Again</button>`;
      msgBox.appendChild(sub);
      msgBox.appendChild(actions);
      msgBox.querySelector('#sot-menu').addEventListener('click', onExit);
      msgBox.querySelector('#sot-again').addEventListener('click', () => {
        socket.emit('host:startMatch', { code: roomCode, game: 'stayontrack' });
      });
    }
    socket.on('stayontrack:result', onResult);

    return {
      stop() {
        timers.forEach(clearTimeout);
        socket.off('input:relay', onInputRelay);
        socket.off('stayontrack:go', onGo);
        socket.off('stayontrack:result', onResult);
      },
    };
  }

  function escapeHtml(s) {
    return String(s).replace(/[&<>"']/g, (c) => ({
      '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
    }[c]));
  }

  window.MP_GAMES = window.MP_GAMES || {};
  window.MP_GAMES.stayontrack = { start };
})();
