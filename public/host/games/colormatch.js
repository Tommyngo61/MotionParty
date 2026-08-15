(function () {
  const STYLE_ID = 'cm-style';
  function ensureStyle() {
    if (document.getElementById(STYLE_ID)) return;
    const style = document.createElement('style');
    style.id = STYLE_ID;
    style.textContent = `
      .cm-wrap{position:absolute;inset:0;background:radial-gradient(circle at 50% 0%,#bdeaff,#8fd3ff 70%);
        color:#16233d;display:flex;flex-direction:column;align-items:center;padding:2rem;overflow:auto}
      .cm-title{font-size:1.6rem;font-weight:900;margin-bottom:1rem}
      .cm-target{display:flex;align-items:center;gap:1rem;background:#fff;border-radius:20px;
        padding:1rem 1.6rem;box-shadow:0 2px 14px rgba(22,35,61,.12);margin-bottom:.6rem}
      .cm-target-swatch{width:64px;height:64px;border-radius:50%;border:3px solid rgba(22,35,61,.15);flex-shrink:0}
      .cm-target-name{font-size:1.4rem;font-weight:900}
      .cm-timer{font-size:1.1rem;font-weight:800;color:var(--text-dim);margin-bottom:1.6rem}
      .cm-lanes{display:flex;flex-wrap:wrap;gap:1.4rem;justify-content:center;width:100%;max-width:900px}
      .cm-lane{display:flex;align-items:center;gap:.8rem;background:#fff;border-radius:16px;padding:.7rem 1.1rem;
        box-shadow:0 2px 12px rgba(22,35,61,.1);min-width:220px}
      .cm-lane img{width:44px;height:44px;border-radius:50%;background:#fff;flex-shrink:0}
      .cm-lane-info{flex:1;min-width:0}
      .cm-lane-name{font-weight:800}
      .cm-lane-status{font-size:.85rem;color:var(--text-dim)}
      .cm-lane-swatch{width:32px;height:32px;border-radius:50%;border:2px solid rgba(22,35,61,.15);
        background:#eee;flex-shrink:0}
      .cm-msg{position:absolute;inset:0;display:flex;align-items:center;justify-content:center;
        flex-direction:column;gap:.6rem;background:rgba(10,6,26,.75);color:#fff;z-index:5;padding:2rem}
      .cm-msg .big{font-size:3rem;font-weight:900;text-shadow:0 4px 0 rgba(0,0,0,.4);text-align:center}
      .cm-msg .sub{color:rgba(255,255,255,.65);font-size:1.1rem;text-align:center;max-width:480px}
      .cm-compare{display:flex;flex-wrap:wrap;gap:1rem;justify-content:center;margin-top:1rem;max-width:700px}
      .cm-compare-card{background:rgba(255,255,255,.1);border-radius:12px;padding:.6rem .9rem;
        display:flex;align-items:center;gap:.6rem;font-weight:700}
      .cm-compare-swatch{width:26px;height:26px;border-radius:50%;border:2px solid rgba(255,255,255,.5)}
      .cm-result-actions{display:flex;gap:1rem;margin-top:1.2rem}
    `;
    document.head.appendChild(style);
  }

  function avatarUrl(avatarId) {
    const a = window.MP_getAvatar(avatarId);
    return 'data:image/svg+xml;utf8,' + encodeURIComponent(a.svg);
  }

  const CM_COUNTDOWN_MS = 3000; // mirrors server's CM_COUNTDOWN_MS
  const CM_ROUND_MS = 5000; // mirrors server's CM_ROUND_MS

  function start(root, ctx) {
    ensureStyle();
    const { socket, roomCode, players, onExit } = ctx;
    const { similarityPct } = window.MP_COLORS;
    let timers = [];
    const setT = (fn, ms) => { const id = setTimeout(fn, ms); timers.push(id); return id; };

    function laneHtml(p) {
      return `
        <div class="cm-lane" data-player="${p.id}">
          <img src="${avatarUrl(p.avatarId)}" />
          <div class="cm-lane-info">
            <div class="cm-lane-name">${escapeHtml(p.name)}</div>
            <div class="cm-lane-status" data-status>Get ready…</div>
          </div>
          <div class="cm-lane-swatch" data-swatch></div>
        </div>`;
    }

    root.innerHTML = `
      <div class="cm-wrap">
        <div class="cm-title">🎨 Color Match Relay</div>
        <div class="cm-target" id="cm-target" style="visibility:hidden">
          <div class="cm-target-swatch" id="cm-target-swatch"></div>
          <div class="cm-target-name" id="cm-target-name">?</div>
        </div>
        <div class="cm-timer" id="cm-timer">Get ready…</div>
        <div class="cm-lanes">${players.map(laneHtml).join('')}</div>
        <div class="cm-msg" id="cm-msg">
          <div class="big" id="cm-msg-big">Get Ready…</div>
          <div class="sub">A color will flash - find something that color in the room and photograph it! Everyone races at once; first close-enough match wins.</div>
        </div>
      </div>
    `;
    const wrap = root.querySelector('.cm-wrap');
    const targetBox = root.querySelector('#cm-target');
    const targetSwatch = root.querySelector('#cm-target-swatch');
    const targetName = root.querySelector('#cm-target-name');
    const timerLabel = root.querySelector('#cm-timer');
    const msgBox = root.querySelector('#cm-msg');
    const msgBig = root.querySelector('#cm-msg-big');

    setT(() => (msgBig.textContent = '3…'), CM_COUNTDOWN_MS - 2400);
    setT(() => (msgBig.textContent = '2…'), CM_COUNTDOWN_MS - 1600);
    setT(() => (msgBig.textContent = '1…'), CM_COUNTDOWN_MS - 800);

    function laneEl(playerId) {
      return wrap.querySelector(`.cm-lane[data-player="${playerId}"]`);
    }

    let countdownRafId = null;
    let roundEndsAt = 0;
    function tickCountdown() {
      const remain = Math.max(0, roundEndsAt - performance.now());
      timerLabel.textContent = (remain / 1000).toFixed(1) + 's';
      if (remain > 0) countdownRafId = requestAnimationFrame(tickCountdown);
    }

    function onGo({ target, roundMs }) {
      targetSwatch.style.background = target.hex;
      targetName.textContent = target.name;
      targetBox.style.visibility = 'visible';
      msgBox.style.display = 'none';
      roundEndsAt = performance.now() + roundMs;
      tickCountdown();
      players.forEach((p) => {
        const status = laneEl(p.id) && laneEl(p.id).querySelector('[data-status]');
        if (status) status.textContent = 'Searching…';
      });
    }
    socket.on('colormatch:go', onGo);

    function onInputRelay(data) {
      if (data.gameEvent !== 'colormatch:attempt') return;
      const lane = laneEl(data.playerId);
      if (!lane) return;
      const { hex, distance } = data.payload;
      lane.querySelector('[data-swatch]').style.background = hex;
      lane.querySelector('[data-status]').textContent = `${similarityPct(distance)}% match`;
    }
    socket.on('input:relay', onInputRelay);

    function onResult(result) {
      cancelAnimationFrame(countdownRafId);
      socket.off('input:relay', onInputRelay);
      const byId = Object.fromEntries(players.map((p) => [p.id, p]));
      const winner = result.winnerId ? byId[result.winnerId] : null;

      msgBox.style.display = 'flex';
      msgBig.textContent = winner ? `🏆 ${winner.name} matches best!` : 'Nobody found a close enough match!';

      const sub = document.createElement('div');
      sub.className = 'sub';
      sub.textContent = winner && result.winnerDistance != null
        ? `${similarityPct(result.winnerDistance)}% match to the target color.`
        : 'Try again with a faster eye next time!';
      msgBox.appendChild(sub);

      const compare = document.createElement('div');
      compare.className = 'cm-compare';
      const attempts = result.attempts || {};
      players
        .map((p) => ({ p, a: attempts[p.id] }))
        .filter((x) => x.a)
        .sort((x, y) => x.a.distance - y.a.distance)
        .forEach(({ p, a }) => {
          const card = document.createElement('div');
          card.className = 'cm-compare-card';
          card.innerHTML = `<div class="cm-compare-swatch" style="background:${escapeHtml(a.hex)}"></div>`;
          card.appendChild(document.createTextNode(`${p.name}: ${similarityPct(a.distance)}%`));
          compare.appendChild(card);
        });
      msgBox.appendChild(compare);

      const actions = document.createElement('div');
      actions.className = 'cm-result-actions';
      actions.innerHTML = `<button class="btn ghost" id="cm-menu">Back to Menu</button><button class="btn primary" id="cm-again">Play Again</button>`;
      msgBox.appendChild(actions);
      msgBox.querySelector('#cm-menu').addEventListener('click', onExit);
      msgBox.querySelector('#cm-again').addEventListener('click', () => {
        socket.emit('host:startMatch', { code: roomCode, game: 'colormatch' });
      });
    }
    socket.on('colormatch:result', onResult);

    return {
      stop() {
        timers.forEach(clearTimeout);
        cancelAnimationFrame(countdownRafId);
        socket.off('colormatch:go', onGo);
        socket.off('input:relay', onInputRelay);
        socket.off('colormatch:result', onResult);
      },
    };
  }

  function escapeHtml(s) {
    return String(s).replace(/[&<>"']/g, (c) => ({
      '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
    }[c]));
  }

  window.MP_GAMES = window.MP_GAMES || {};
  window.MP_GAMES.colormatch = { start };
})();
