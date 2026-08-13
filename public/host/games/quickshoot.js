(function () {
  const STYLE_ID = 'qs-style';
  function ensureStyle() {
    if (document.getElementById(STYLE_ID)) return;
    const style = document.createElement('style');
    style.id = STYLE_ID;
    style.textContent = `
      .qs-wrap{position:absolute;inset:0;background:linear-gradient(180deg,#3a1f14 0%,#1a0f36 55%);color:#fff;
        display:flex;flex-direction:column;overflow:hidden}
      .qs-caption{text-align:center;font-size:1.6rem;font-weight:800;padding:1.2rem 0 .4rem;
        text-shadow:0 3px 0 rgba(0,0,0,.4);min-height:2.4rem;transition:color .2s}
      .qs-arena{flex:1;position:relative}
      .qs-char{position:absolute;top:50%;left:50%;width:160px;margin-left:-80px;margin-top:-110px;
        text-align:center;transition:left 1.3s cubic-bezier(.35,.7,.3,1);will-change:left}
      .qs-char .qs-avatar{width:140px;height:140px;border-radius:50%;background:#fff;
        box-shadow:0 10px 24px rgba(0,0,0,.4);animation:qs-bob 0.5s ease-in-out infinite}
      .qs-char.qs-still .qs-avatar{animation:none}
      .qs-char .qs-label{margin-top:.5rem;font-weight:800;font-size:1.1rem;text-shadow:0 2px 0 #000}
      .qs-char .qs-time{margin-top:.2rem;font-size:1.4rem;font-weight:900;color:var(--accent-yellow)}
      @keyframes qs-bob{0%,100%{transform:translateY(0)}50%{transform:translateY(-8px)}}
      .qs-light{position:absolute;top:14px;left:50%;transform:translateX(-50%);width:34px;height:34px;
        border-radius:50%;background:#555;box-shadow:0 0 0 6px rgba(0,0,0,.25);transition:background .15s,box-shadow .15s}
      .qs-light.red{background:var(--accent-red);box-shadow:0 0 24px 8px rgba(255,77,77,.7)}
      .qs-light.orange{background:var(--accent-orange);box-shadow:0 0 24px 8px rgba(255,159,63,.7)}
      .qs-light.green{background:var(--accent-green);box-shadow:0 0 30px 12px rgba(61,220,132,.85)}
      .qs-flash{position:absolute;inset:0;background:#3ddc84;opacity:0;pointer-events:none;transition:opacity .08s}
      .qs-flash.on{opacity:.35}
      .qs-char.qs-shot .qs-avatar{animation:qs-fall 0.7s forwards;filter:grayscale(.3)}
      @keyframes qs-fall{to{transform:rotate(78deg) translateY(30px);opacity:.55}}
      .qs-char.qs-winner .qs-avatar{animation:qs-victory .6s ease-in-out infinite}
      @keyframes qs-victory{0%,100%{transform:translateY(0) scale(1)}50%{transform:translateY(-14px) scale(1.06)}}
      .qs-pow{position:absolute;top:20%;font-size:3rem;font-weight:900;color:var(--accent-yellow);
        text-shadow:0 0 12px rgba(0,0,0,.6);opacity:0;transform:scale(.4) rotate(-10deg)}
      .qs-pow.show{animation:qs-pow .6s ease-out forwards}
      @keyframes qs-pow{0%{opacity:0;transform:scale(.4) rotate(-10deg)}30%{opacity:1;transform:scale(1.2) rotate(6deg)}100%{opacity:1;transform:scale(1) rotate(0)}}
      .qs-result{position:absolute;inset:0;background:rgba(10,6,26,.9);display:flex;flex-direction:column;
        align-items:center;justify-content:center;gap:1.2rem;text-align:center;padding:2rem}
      .qs-result h1{font-size:2.4rem;margin:0}
      .qs-result .qs-sub{color:rgba(255,255,255,.65);font-size:1.1rem}
      .qs-result .qs-times{display:flex;gap:3rem;margin-top:.5rem}
      .qs-result .qs-times div{font-size:1.3rem;font-weight:700}
      .qs-result .qs-actions{display:flex;gap:1rem;margin-top:1rem}
    `;
    document.head.appendChild(style);
  }

  function avatarUrl(avatarId) {
    const a = window.MP_getAvatar(avatarId);
    return 'data:image/svg+xml;utf8,' + encodeURIComponent(a.svg);
  }

  function start(root, ctx) {
    ensureStyle();
    const { socket, roomCode, players, onExit, bracketMode, onMatchResult } = ctx;
    const [p1, p2] = players;
    let timers = [];
    const setT = (fn, ms) => { const id = setTimeout(fn, ms); timers.push(id); return id; };
    const clearTimers = () => { timers.forEach(clearTimeout); timers = []; };

    root.innerHTML = `
      <div class="qs-wrap">
        <div class="qs-caption" id="qs-caption">Back to back…</div>
        <div class="qs-arena">
          <div class="qs-flash" id="qs-flash"></div>
          <div class="qs-light" id="qs-light"></div>
          <div class="qs-char" id="qs-p1" style="left:50%">
            <img class="qs-avatar" src="${avatarUrl(p1.avatarId)}" />
            <div class="qs-label">${escapeHtml(p1.name)}</div>
            <div class="qs-time" id="qs-time-1"></div>
          </div>
          <div class="qs-char" id="qs-p2" style="left:50%">
            <img class="qs-avatar" src="${avatarUrl(p2.avatarId)}" />
            <div class="qs-label">${escapeHtml(p2.name)}</div>
            <div class="qs-time" id="qs-time-2"></div>
          </div>
        </div>
      </div>
    `;

    const caption = root.querySelector('#qs-caption');
    const light = root.querySelector('#qs-light');
    const flash = root.querySelector('#qs-flash');
    const c1 = root.querySelector('#qs-p1');
    const c2 = root.querySelector('#qs-p2');

    // Walk-apart choreography (purely visual; timing mirrors the server's QS_WALK_DURATION)
    setT(() => { caption.textContent = 'Pace 1…'; }, 250);
    setT(() => { c1.style.left = '30%'; c2.style.left = '70%'; }, 300);
    setT(() => { caption.textContent = 'Pace 2…'; }, 900);
    setT(() => { caption.textContent = 'Pace 3…'; }, 1500);
    setT(() => {
      c1.style.left = '20%';
      c2.style.left = '80%';
    }, 1600);
    setT(() => {
      caption.textContent = 'Turn and face your rival!';
      c1.classList.add('qs-still');
      c2.classList.add('qs-still');
    }, 2600);
    setT(() => { caption.textContent = 'Wait for it…'; }, 3100);

    function setLight(color, text) {
      light.className = 'qs-light ' + (color || '');
      caption.textContent = text || '';
    }

    function onState({ phase }) {
      if (phase === 'ready') setLight('red', 'READY…');
      else if (phase === 'set') setLight('orange', 'SET…');
      else if (phase === 'fire') {
        setLight('green', 'FIRE!! 🔫');
        flash.classList.add('on');
        setT(() => flash.classList.remove('on'), 150);
      }
    }

    function onResult(result) {
      clearTimers();
      showResult(root, result, { p1, p2, socket, roomCode, onExit, c1, c2, bracketMode, onMatchResult });
    }

    socket.on('quickshoot:state', onState);
    socket.on('quickshoot:result', onResult);

    return {
      stop() {
        clearTimers();
        socket.off('quickshoot:state', onState);
        socket.off('quickshoot:result', onResult);
      },
    };
  }

  function showResult(root, result, { p1, p2, socket, roomCode, onExit, c1, c2, bracketMode, onMatchResult }) {
    const wrap = root.querySelector('.qs-wrap');
    const byId = { [p1.id]: p1, [p2.id]: p2 };
    const { reason, winnerId, loserId, times } = result;

    if (winnerId && byId[winnerId]) {
      (winnerId === p1.id ? c1 : c2).classList.add('qs-winner');
    }
    if (loserId && byId[loserId]) {
      (loserId === p1.id ? c1 : c2).classList.add('qs-shot');
      const pow = document.createElement('div');
      pow.className = 'qs-pow show';
      pow.textContent = 'BANG!';
      pow.style.left = (loserId === p1.id ? '14%' : '68%');
      root.querySelector('.qs-arena').appendChild(pow);
    }
    if (times) {
      if (times[p1.id] != null) root.querySelector('#qs-time-1').textContent = times[p1.id] + ' ms';
      if (times[p2.id] != null) root.querySelector('#qs-time-2').textContent = times[p2.id] + ' ms';
    }

    let headline, sub;
    if (reason === 'falseStart') {
      headline = `🚫 ${byId[loserId] ? byId[loserId].name : 'Someone'} drew too early!`;
      sub = `${byId[winnerId] ? byId[winnerId].name : 'Opponent'} wins by default.`;
    } else if (reason === 'noShow') {
      headline = "😴 Nobody drew!";
      sub = "It's a draw — try again.";
    } else if (reason === 'timeout' && byId[winnerId] && (!times || times[loserId] == null)) {
      headline = `🏆 ${byId[winnerId].name} wins!`;
      sub = `${byId[loserId] ? byId[loserId].name : 'Opponent'} never fired.`;
    } else {
      headline = `🏆 ${byId[winnerId] ? byId[winnerId].name : ''} wins the duel!`;
      sub = 'Fastest reaction takes it.';
    }

    const overlay = document.createElement('div');
    overlay.className = 'qs-result';
    if (bracketMode) {
      overlay.innerHTML = `
        <h1>${escapeHtml(headline)}</h1>
        <div class="qs-sub">${escapeHtml(sub)}</div>
        <div class="qs-sub">${winnerId ? 'Advancing the bracket…' : 'Replaying this match…'}</div>
      `;
      wrap.appendChild(overlay);
      setTimeout(() => onMatchResult && onMatchResult(winnerId || null), 2600);
      return;
    }
    overlay.innerHTML = `
      <h1>${escapeHtml(headline)}</h1>
      <div class="qs-sub">${escapeHtml(sub)}</div>
      <div class="qs-actions">
        <button class="btn ghost" id="qs-menu">Back to Menu</button>
        <button class="btn primary" id="qs-again">Play Again</button>
      </div>
    `;
    wrap.appendChild(overlay);

    overlay.querySelector('#qs-menu').addEventListener('click', onExit);
    overlay.querySelector('#qs-again').addEventListener('click', () => {
      socket.emit('host:startMatch', { code: roomCode, game: 'quickshoot' });
    });
  }

  function escapeHtml(s) {
    return String(s).replace(/[&<>"']/g, (c) => ({
      '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
    }[c]));
  }

  window.MP_GAMES = window.MP_GAMES || {};
  window.MP_GAMES.quickshoot = { start };
})();
