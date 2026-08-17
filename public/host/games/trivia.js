(function () {
  const STYLE_ID = 'tr-style';
  function ensureStyle() {
    if (document.getElementById(STYLE_ID)) return;
    const style = document.createElement('style');
    style.id = STYLE_ID;
    style.textContent = `
      .tr-wrap{position:absolute;inset:0;background:linear-gradient(180deg,#0d1b4c 0%,#1a0f36 100%);
        color:#fff;display:flex;flex-direction:column;padding:1.2rem 1.6rem;overflow:hidden}
      .tr-scoreboard{display:flex;gap:.8rem;justify-content:center;flex-wrap:wrap;margin-bottom:1rem}
      .tr-score-card{background:rgba(255,255,255,.08);border-radius:14px;padding:.5rem 1rem;
        display:flex;align-items:center;gap:.5rem;border:2px solid transparent;transition:border-color .2s}
      .tr-score-card.active{border-color:var(--accent-yellow)}
      .tr-score-card img{width:32px;height:32px;border-radius:50%;background:#fff}
      .tr-score-card .name{font-weight:800;font-size:.95rem}
      .tr-score-card .pts{font-weight:900;font-size:1.1rem;color:var(--accent-yellow)}
      .tr-score-card .pts.negative{color:var(--accent-red)}

      .tr-board{flex:1;display:grid;grid-template-columns:repeat(5,1fr);gap:.5rem;min-height:0}
      .tr-cat-head{background:#0a1440;border-radius:8px;display:flex;align-items:center;justify-content:center;
        text-align:center;font-weight:900;font-size:1rem;padding:.5rem .3rem;text-transform:uppercase;
        letter-spacing:.3px}
      .tr-tile{background:linear-gradient(180deg,#1e3a8a,#152a63);border-radius:8px;display:flex;
        align-items:center;justify-content:center;font-size:1.8rem;font-weight:900;color:var(--accent-yellow);
        text-shadow:0 2px 0 rgba(0,0,0,.4);transition:transform .1s}
      .tr-tile.cleared{background:rgba(255,255,255,.04);color:transparent}

      .tr-msg{position:absolute;inset:0;display:flex;align-items:center;justify-content:center;
        flex-direction:column;gap:.8rem;text-align:center;padding:2rem;background:rgba(10,6,26,.85);z-index:5}
      .tr-msg .big{font-size:2rem;font-weight:900;text-shadow:0 3px 0 rgba(0,0,0,.4)}
      .tr-msg .sub{color:rgba(255,255,255,.75);font-size:1.15rem;max-width:640px}

      .tr-clue-cat{color:var(--accent-yellow);font-weight:800;letter-spacing:.5px;text-transform:uppercase;
        margin-bottom:.6rem}
      .tr-clue-text{font-size:1.8rem;font-weight:800;max-width:800px;line-height:1.35}
      .tr-clue-value{margin-top:1rem;font-size:1.3rem;font-weight:900;color:var(--accent-yellow)}
      .tr-buzz-status{margin-top:1.4rem;font-size:1.2rem;font-weight:800}
      .tr-judge{display:flex;gap:1rem;margin-top:1.4rem}
      .tr-judge button{font-size:1.1rem;padding:.8rem 1.6rem}

      .tr-dd-badge{font-size:2.2rem;font-weight:900;color:var(--accent-yellow);text-shadow:0 3px 0 rgba(0,0,0,.4)}

      .tr-faceoff-vs{display:flex;gap:2rem;align-items:center;justify-content:center;margin:1rem 0}
      .tr-faceoff-vs img{width:80px;height:80px;border-radius:50%;background:#fff}
      .tr-faceoff-answers{display:flex;gap:2rem;justify-content:center;margin-top:1.2rem;flex-wrap:wrap}
      .tr-faceoff-card{background:rgba(255,255,255,.08);border-radius:14px;padding:1rem 1.4rem;min-width:200px}
      .tr-faceoff-card .who{font-weight:800;margin-bottom:.4rem}
      .tr-faceoff-card .ans{font-size:1.1rem}
      .tr-faceoff-card .pts{font-weight:900;color:var(--accent-yellow);margin-top:.3rem}

      .tr-actions{display:flex;gap:1rem;margin-top:1.2rem}
    `;
    document.head.appendChild(style);
  }

  const CAT_COLORS = ['#e53935', '#1e88e5', '#43a047', '#fb8c00', '#8e24aa'];

  function avatarUrl(avatarId) {
    const a = window.MP_getAvatar(avatarId);
    return 'data:image/svg+xml;utf8,' + encodeURIComponent(a.svg);
  }

  function start(root, ctx) {
    ensureStyle();
    const { socket, roomCode, players, onExit, mode: startMode, teams: startTeams, deductWrong } = ctx;
    const byId = Object.fromEntries(players.map((p) => [p.id, p]));
    let mode = startMode === 'teams' ? 'teams' : 'ffa';
    let teams = startTeams || null; // playerId -> 'A'|'B', once trivia:board arrives

    root.innerHTML = `
      <div class="tr-wrap">
        <div class="tr-scoreboard" id="tr-scoreboard"></div>
        <div class="tr-board" id="tr-board"></div>
      </div>
    `;
    const wrap = root.querySelector('.tr-wrap');
    const scoreboardEl = root.querySelector('#tr-scoreboard');
    const boardEl = root.querySelector('#tr-board');

    let categories = [];
    let cleared = [];
    let scores = {};
    let activeEntity = null;
    let overlay = null;

    function entityLabel(entity) {
      if (mode === 'teams') {
        const members = players.filter((p) => teams && teams[p.id] === entity).map((p) => p.name);
        return `Team ${entity}` + (members.length ? ` (${members.join(' & ')})` : '');
      }
      const p = byId[entity];
      return p ? p.name : entity;
    }

    function entityAvatar(entity) {
      if (mode === 'teams') {
        const member = players.find((p) => teams && teams[p.id] === entity);
        return member ? avatarUrl(member.avatarId) : '';
      }
      const p = byId[entity];
      return p ? avatarUrl(p.avatarId) : '';
    }

    function renderScoreboard() {
      const entities = mode === 'teams' ? ['A', 'B'] : players.map((p) => p.id);
      scoreboardEl.innerHTML = entities.map((e) => {
        const pts = scores[e] || 0;
        const img = entityAvatar(e);
        return `
          <div class="tr-score-card${e === activeEntity ? ' active' : ''}">
            ${img ? `<img src="${img}" />` : ''}
            <span class="name">${escapeHtml(entityLabel(e))}</span>
            <span class="pts${pts < 0 ? ' negative' : ''}">$${pts}</span>
          </div>`;
      }).join('');
    }

    function renderBoard() {
      let html = '';
      categories.forEach((c, ci) => {
        html += `<div class="tr-cat-head" style="border-bottom:4px solid ${CAT_COLORS[ci % CAT_COLORS.length]}">${escapeHtml(c.name)}</div>`;
      });
      for (let vi = 0; vi < 5; vi++) {
        categories.forEach((c, ci) => {
          const isCleared = cleared[ci] && cleared[ci][vi];
          html += `<div class="tr-tile${isCleared ? ' cleared' : ''}">${isCleared ? '' : '$' + c.values[vi]}</div>`;
        });
      }
      boardEl.innerHTML = html;
    }

    function closeOverlay() {
      if (overlay) { overlay.remove(); overlay = null; }
    }

    function showOverlay(html) {
      closeOverlay();
      overlay = document.createElement('div');
      overlay.className = 'tr-msg';
      overlay.innerHTML = html;
      wrap.appendChild(overlay);
      return overlay;
    }

    function onCountdownTick() {
      showOverlay('<div class="big">Get Ready!</div><div class="sub">Trivia Throwdown is about to begin…</div>');
    }
    onCountdownTick();

    function onBoard(data) {
      categories = data.categories;
      cleared = data.cleared;
      scores = data.scores;
      activeEntity = data.activeEntity;
      mode = data.mode;
      teams = data.teams;
      closeOverlay();
      renderScoreboard();
      renderBoard();
      if (window.MP_Feedback) window.MP_Feedback.play('go');
    }
    socket.on('trivia:board', onBoard);

    function onTurn(data) {
      activeEntity = data.entity;
      renderScoreboard();
      showOverlay(`<div class="big">${escapeHtml(entityLabel(activeEntity))}'s pick!</div>`);
      setTimeout(closeOverlay, 1400);
    }
    socket.on('trivia:turn', onTurn);

    function onDailyDouble(data) {
      if (window.MP_Feedback) window.MP_Feedback.play('success');
      showOverlay(`
        <div class="tr-dd-badge">💰 DAILY DOUBLE! 💰</div>
        <div class="sub">${escapeHtml(entityLabel(data.entity))} is wagering up to $${data.maxWager}…</div>
      `);
    }
    socket.on('trivia:dailyDouble', onDailyDouble);

    function onClue(data) {
      closeOverlay();
      const cat = categories[data.catIndex];
      const box = showOverlay(`
        <div class="tr-clue-cat">${escapeHtml(cat.name)}</div>
        <div class="tr-clue-text">${escapeHtml(data.text)}</div>
        <div class="tr-clue-value">${data.isDailyDouble ? `Wagered $${data.wager}` : '$' + data.value}</div>
        <div class="tr-buzz-status" id="tr-buzz-status">${data.isDailyDouble
          ? `Only ${escapeHtml(entityLabel(data.entity))} can answer…`
          : 'Buzz in!'}</div>
      `);
      if (window.MP_Feedback) window.MP_Feedback.play('ready');
    }
    socket.on('trivia:clue', onClue);

    function onBuzzResult(data) {
      if (window.MP_Feedback) window.MP_Feedback.play('tick');
      const status = overlay && overlay.querySelector('#tr-buzz-status');
      if (status) status.textContent = `${entityLabel(data.entity)} buzzed in!`;
      const judge = document.createElement('div');
      judge.className = 'tr-judge';
      judge.innerHTML = `
        <button class="btn primary" id="tr-correct">✅ Correct</button>
        <button class="btn ghost" id="tr-wrong">❌ Wrong</button>
      `;
      overlay.appendChild(judge);
      judge.querySelector('#tr-correct').addEventListener('click', () => {
        judge.remove();
        socket.emit('trivia:judge', { code: roomCode, correct: true });
      });
      judge.querySelector('#tr-wrong').addEventListener('click', () => {
        judge.remove();
        socket.emit('trivia:judge', { code: roomCode, correct: false });
      });
    }
    socket.on('trivia:buzzResult', onBuzzResult);

    function onReopenBuzz(data) {
      scores = data.scores;
      renderScoreboard();
      if (window.MP_Feedback) window.MP_Feedback.play('error');
      const status = overlay && overlay.querySelector('#tr-buzz-status');
      if (status) status.textContent = `${entityLabel(data.wrongEntity)} was wrong — buzz in!`;
    }
    socket.on('trivia:reopenBuzz', onReopenBuzz);

    function onClueResolved(data) {
      scores = data.scores;
      cleared[data.catIndex][data.valIndex] = true;
      renderScoreboard();
      renderBoard();
      if (window.MP_Feedback) window.MP_Feedback.play(data.scored && data.correct ? 'win' : 'tick');
      showOverlay(`
        <div class="big">${data.scored ? (data.correct ? '✅ Correct!' : '❌ Not quite') : '⏱️ Time\'s up'}</div>
        <div class="sub">${escapeHtml(data.answer)}</div>
      `);
      setTimeout(() => { if (!data.boardDone) closeOverlay(); }, 1800);
    }
    socket.on('trivia:clueResolved', onClueResolved);

    function onFaceoffStart(data) {
      scores = data.scores;
      const [a, b] = data.finalists;
      showOverlay(`
        <div class="big">🏆 Face-Off Round! 🏆</div>
        <div class="tr-faceoff-vs">
          <div><img src="${entityAvatar(a)}" /><div>${escapeHtml(entityLabel(a))}</div></div>
          <div style="font-size:1.5rem;font-weight:900">VS</div>
          <div><img src="${entityAvatar(b)}" /><div>${escapeHtml(entityLabel(b))}</div></div>
        </div>
        <div class="sub">Highest combined score after the Face-Off wins it all!</div>
      `);
    }
    socket.on('trivia:faceoffStart', onFaceoffStart);

    function onFaceoffQuestion(data) {
      showOverlay(`
        <div class="tr-clue-cat">Question ${data.index + 1} of ${data.total}</div>
        <div class="tr-clue-text">${escapeHtml(data.prompt)}</div>
        <div class="tr-buzz-status">Answer on your phones!</div>
      `);
      if (window.MP_Feedback) window.MP_Feedback.play('ready');
    }
    socket.on('trivia:faceoffQuestion', onFaceoffQuestion);

    function onFaceoffResult(data) {
      scores = data.scores;
      const cards = Object.entries(data.answers).map(([entity, a]) => `
        <div class="tr-faceoff-card">
          <div class="who">${escapeHtml(entityLabel(entity))}</div>
          <div class="ans">"${escapeHtml(a.text || '—')}"</div>
          <div class="pts">${a.points ? '+$' + a.points : 'No match'}</div>
        </div>
      `).join('');
      if (window.MP_Feedback) window.MP_Feedback.play('success');
      showOverlay(`<div class="big">Results</div><div class="tr-faceoff-answers">${cards}</div>`);
    }
    socket.on('trivia:faceoffResult', onFaceoffResult);

    function onMatchResult(data) {
      scores = data.scores;
      if (window.MP_Feedback) window.MP_Feedback.play(data.tie ? 'error' : 'win');
      const box = showOverlay(`
        <div class="big">${data.tie ? "🤝 It's a tie!" : `🏆 ${escapeHtml(entityLabel(data.winner))} wins!`}</div>
        <div class="tr-actions">
          <button class="btn ghost" id="tr-menu">Back to Menu</button>
          <button class="btn primary" id="tr-again">Play Again</button>
        </div>
      `);
      box.querySelector('#tr-menu').addEventListener('click', onExit);
      box.querySelector('#tr-again').addEventListener('click', () => {
        socket.emit('host:startMatch', { code: roomCode, game: 'trivia', mode, teams, deductWrong });
      });
    }
    socket.on('trivia:matchResult', onMatchResult);

    return {
      stop() {
        socket.off('trivia:board', onBoard);
        socket.off('trivia:turn', onTurn);
        socket.off('trivia:dailyDouble', onDailyDouble);
        socket.off('trivia:clue', onClue);
        socket.off('trivia:buzzResult', onBuzzResult);
        socket.off('trivia:reopenBuzz', onReopenBuzz);
        socket.off('trivia:clueResolved', onClueResolved);
        socket.off('trivia:faceoffStart', onFaceoffStart);
        socket.off('trivia:faceoffQuestion', onFaceoffQuestion);
        socket.off('trivia:faceoffResult', onFaceoffResult);
        socket.off('trivia:matchResult', onMatchResult);
      },
    };
  }

  function escapeHtml(s) {
    return String(s).replace(/[&<>"']/g, (c) => ({
      '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
    }[c]));
  }

  // ---------- How to Play tutorial ----------
  const TUT_STYLE_ID = 'tr-tut-style';
  function ensureTutStyle() {
    if (document.getElementById(TUT_STYLE_ID)) return;
    const style = document.createElement('style');
    style.id = TUT_STYLE_ID;
    style.textContent = `
      .tr-tut-grid{position:absolute;top:16%;left:20%;right:20%;bottom:34%;display:grid;
        grid-template-columns:repeat(3,1fr);grid-template-rows:repeat(2,1fr);gap:4px}
      .tr-tut-grid div{background:rgba(30,58,138,.5);border-radius:3px}
      .tr-tut-grid div.tr-tut-picked{animation:tr-tut-pick 3s ease-in-out infinite}
      @keyframes tr-tut-pick{0%,20%{background:rgba(30,58,138,.5)}25%,45%{background:#ffd23f}50%,100%{background:transparent}}
      .tr-tut-buzzer{position:absolute;bottom:14%;left:50%;transform:translateX(-50%);width:34px;height:34px;
        border-radius:50%;background:#7a1f1f;animation:tr-tut-buzz 3s ease-in-out infinite}
      @keyframes tr-tut-buzz{0%,50%{background:#7a1f1f;box-shadow:none}
        55%,70%{background:#ff4d4d;box-shadow:0 0 14px 4px rgba(255,77,77,.7);transform:translateX(-50%) scale(1.15)}
        75%,100%{background:#7a1f1f;box-shadow:none;transform:translateX(-50%) scale(1)}}
    `;
    document.head.appendChild(style);
  }

  function animateTutorial(stage) {
    ensureTutStyle();
    stage.innerHTML = `
      <div class="tr-tut-grid">
        <div></div><div class="tr-tut-picked"></div><div></div>
        <div></div><div></div><div></div>
      </div>
      <div class="tr-tut-buzzer"></div>
    `;
    return () => {};
  }

  window.MP_TUTORIALS = window.MP_TUTORIALS || {};
  window.MP_TUTORIALS.trivia = {
    emoji: '🧠',
    title: 'Trivia Throwdown',
    render: animateTutorial,
  };

  window.MP_GAMES = window.MP_GAMES || {};
  window.MP_GAMES.trivia = { start };
})();
