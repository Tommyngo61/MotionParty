(function () {
  const STYLE_ID = 'trc-style';
  function ensureStyle() {
    if (document.getElementById(STYLE_ID)) return;
    const style = document.createElement('style');
    style.id = STYLE_ID;
    style.textContent = `
      .trc{width:100%;min-height:80vh;display:flex;flex-direction:column;align-items:center;
        justify-content:center;text-align:center;border-radius:24px;padding:1.2rem;color:#fff;
        background:radial-gradient(circle,#1e3a8a,#0d1b4c)}
      .trc-icon{font-size:3.5rem;margin-bottom:.3rem}
      .trc-title{font-size:1.5rem;font-weight:900;text-shadow:0 3px 0 rgba(0,0,0,.3)}
      .trc-sub{margin-top:.5rem;font-size:1.05rem;color:rgba(255,255,255,.85);max-width:340px}
      .trc-team{margin-top:.4rem;font-size:.85rem;font-weight:800;padding:.2rem .7rem;border-radius:8px}
      .trc-team.A{background:#1e88e5}
      .trc-team.B{background:#e53935}

      .trc-grid{display:grid;grid-template-columns:repeat(5,1fr);gap:.4rem;width:100%;margin-top:1rem}
      .trc-grid-cat{font-size:.65rem;font-weight:800;text-transform:uppercase;opacity:.75;
        display:flex;align-items:center;justify-content:center;text-align:center;padding:.2rem}
      .trc-grid-tile{background:rgba(255,255,255,.12);border:none;border-radius:8px;color:var(--accent-yellow,#ffd23f);
        font-weight:900;font-size:1rem;padding:.7rem 0;cursor:pointer}
      .trc-grid-tile:disabled{background:rgba(255,255,255,.03);color:transparent;cursor:default}

      .trc-buzz-btn{width:200px;height:200px;border-radius:50%;border:none;background:radial-gradient(circle,#ff6b6b,#7a1f1f);
        color:#fff;font-size:1.6rem;font-weight:900;box-shadow:0 8px 0 #5c1414;margin-top:1rem;cursor:pointer}
      .trc-buzz-btn:active{transform:translateY(6px);box-shadow:0 2px 0 #5c1414}
      .trc-buzz-btn:disabled{opacity:.3;box-shadow:none;cursor:default}

      .trc-wager-row{display:flex;gap:.6rem;align-items:center;margin-top:1rem}
      .trc-wager-row input{width:120px;font-size:1.3rem;text-align:center;padding:.6rem;border-radius:10px;
        border:none;font-weight:800}

      .trc-textinput{width:90%;max-width:340px;font-size:1.2rem;padding:.8rem;border-radius:12px;
        border:none;margin-top:1rem;text-align:center}
    `;
    document.head.appendChild(style);
  }

  function start(root, ctx) {
    ensureStyle();
    const { socket, playerId, mode: startMode, teams: startTeams } = ctx;
    let mode = startMode === 'teams' ? 'teams' : 'ffa';
    let teams = startTeams || null;
    let categories = [];
    let cleared = [];
    let activeEntity = null;
    let myEntity = mode === 'teams' ? (teams && teams[playerId]) || 'A' : playerId;
    let wrongThisClue = false;
    let currentCat = null;

    root.innerHTML = `<div class="trc" id="trc-box"></div>`;
    const box = root.querySelector('#trc-box');

    function myTurn() { return activeEntity === myEntity; }

    function render(html) {
      box.innerHTML = html;
    }

    function teamBadge() {
      if (mode !== 'teams') return '';
      return `<div class="trc-team ${myEntity}">Team ${myEntity}</div>`;
    }

    render(`
      <div class="trc-icon">🧠</div>
      <div class="trc-title">Trivia Throwdown</div>
      <div class="trc-sub">Get ready…</div>
      ${teamBadge()}
    `);

    function renderBoardScreen(myPick) {
      wrongThisClue = false;
      if (!myPick) {
        render(`
          <div class="trc-icon">🧠</div>
          <div class="trc-title">Waiting…</div>
          <div class="trc-sub">Watch the TV — it's someone else's turn to pick.</div>
          ${teamBadge()}
        `);
        return;
      }
      let grid = categories.map((c) => `<div class="trc-grid-cat">${escapeHtml(c.name)}</div>`).join('');
      for (let vi = 0; vi < 5; vi++) {
        categories.forEach((c, ci) => {
          const isCleared = cleared[ci] && cleared[ci][vi];
          grid += `<button class="trc-grid-tile" data-cat="${ci}" data-val="${vi}" ${isCleared ? 'disabled' : ''}>${isCleared ? '' : '$' + c.values[vi]}</button>`;
        });
      }
      render(`
        <div class="trc-title">Your pick!</div>
        <div class="trc-sub">Tap a category and value.</div>
        <div class="trc-grid">${grid}</div>
      `);
      box.querySelectorAll('.trc-grid-tile:not(:disabled)').forEach((btn) => {
        btn.addEventListener('click', () => {
          if (window.MP_Feedback) window.MP_Feedback.play('tick');
          socket.emit('trivia:pickTile', { catIndex: Number(btn.dataset.cat), valIndex: Number(btn.dataset.val) });
          render(`<div class="trc-title">Locking it in…</div>`);
        });
      });
    }

    function onBoard(data) {
      categories = data.categories;
      cleared = data.cleared;
      activeEntity = data.activeEntity;
      mode = data.mode;
      teams = data.teams;
      myEntity = mode === 'teams' ? (teams && teams[playerId]) || 'A' : playerId;
      renderBoardScreen(myTurn());
    }
    socket.on('trivia:board', onBoard);

    function onTurn(data) {
      activeEntity = data.entity;
      renderBoardScreen(myTurn());
    }
    socket.on('trivia:turn', onTurn);

    function onDailyDouble(data) {
      currentCat = categories[data.catIndex];
      if (data.entity !== myEntity) {
        render(`
          <div class="trc-icon">💰</div>
          <div class="trc-title">Daily Double!</div>
          <div class="trc-sub">Waiting for the wager…</div>
        `);
        return;
      }
      if (window.MP_Feedback) window.MP_Feedback.play('success');
      render(`
        <div class="trc-icon">💰</div>
        <div class="trc-title">Daily Double!</div>
        <div class="trc-sub">Wager up to $${data.maxWager}.</div>
        <div class="trc-wager-row">
          <input type="number" id="trc-wager-input" min="0" max="${data.maxWager}" value="${Math.min(200, data.maxWager)}" />
          <button class="btn primary" id="trc-wager-btn">Wager</button>
        </div>
      `);
      const btn = box.querySelector('#trc-wager-btn');
      const input = box.querySelector('#trc-wager-input');
      btn.addEventListener('click', () => {
        const amount = Math.max(0, Math.min(data.maxWager, Math.round(Number(input.value) || 0)));
        socket.emit('trivia:wager', { amount });
        render(`<div class="trc-title">Wagered $${amount}!</div><div class="trc-sub">Here comes the clue…</div>`);
      });
    }
    socket.on('trivia:dailyDouble', onDailyDouble);

    function onClue(data) {
      currentCat = categories[data.catIndex];
      wrongThisClue = false;
      const canBuzz = !data.isDailyDouble || data.entity === myEntity;
      render(`
        <div class="trc-sub">${escapeHtml(currentCat.name)} — ${data.isDailyDouble ? `Wagered $${data.wager}` : '$' + data.value}</div>
        <button class="trc-buzz-btn" id="trc-buzz-btn" ${canBuzz ? '' : 'disabled'}>${canBuzz ? 'BUZZ!' : 'Wait…'}</button>
      `);
      const btn = box.querySelector('#trc-buzz-btn');
      if (btn && canBuzz) {
        btn.addEventListener('click', () => {
          btn.disabled = true;
          if (window.MP_Feedback) window.MP_Feedback.play('fire');
          if (navigator.vibrate) navigator.vibrate(30);
          socket.emit('trivia:buzz');
        }, { once: true });
      }
    }
    socket.on('trivia:clue', onClue);

    function onBuzzResult(data) {
      const btn = box.querySelector('#trc-buzz-btn');
      if (btn) btn.disabled = true;
      if (data.entity === myEntity) {
        if (window.MP_Feedback) window.MP_Feedback.play('go');
        if (navigator.vibrate) navigator.vibrate(60);
        render(`<div class="trc-title">You buzzed in!</div><div class="trc-sub">Answer out loud now.</div>`);
      } else {
        render(`<div class="trc-title">Buzzed!</div><div class="trc-sub">Someone else got there first — hold tight.</div>`);
      }
    }
    socket.on('trivia:buzzResult', onBuzzResult);

    function onReopenBuzz(data) {
      if (data.wrongEntity === myEntity) wrongThisClue = true;
      if (wrongThisClue) {
        render(`<div class="trc-title">Not this one</div><div class="trc-sub">Wait for the next clue.</div>`);
        return;
      }
      render(`
        <div class="trc-sub">${escapeHtml(currentCat ? currentCat.name : '')}</div>
        <button class="trc-buzz-btn" id="trc-buzz-btn">BUZZ!</button>
      `);
      box.querySelector('#trc-buzz-btn').addEventListener('click', () => {
        box.querySelector('#trc-buzz-btn').disabled = true;
        if (window.MP_Feedback) window.MP_Feedback.play('fire');
        if (navigator.vibrate) navigator.vibrate(30);
        socket.emit('trivia:buzz');
      }, { once: true });
    }
    socket.on('trivia:reopenBuzz', onReopenBuzz);

    function onClueResolved(data) {
      const iAnswered = data.entity === myEntity;
      if (iAnswered && window.MP_Feedback) window.MP_Feedback.play(data.correct ? 'win' : 'lose');
      if (iAnswered && navigator.vibrate) navigator.vibrate(data.correct ? [0, 60] : [0, 120, 60, 120]);
      render(`
        <div class="trc-title">${data.scored ? (data.correct ? '✅ Correct!' : '❌ Wrong') : 'Look at the TV'}</div>
        <div class="trc-sub">Look at the TV for the answer.</div>
      `);
    }
    socket.on('trivia:clueResolved', onClueResolved);

    function onFaceoffStart(data) {
      const inFaceoff = data.finalists.includes(myEntity);
      if (window.MP_Feedback) window.MP_Feedback.play('go');
      render(`
        <div class="trc-icon">🏆</div>
        <div class="trc-title">Face-Off!</div>
        <div class="trc-sub">${inFaceoff ? "You made the final round — get ready!" : 'Watch the TV for the final round.'}</div>
      `);
    }
    socket.on('trivia:faceoffStart', onFaceoffStart);

    function onFaceoffQuestion(data) {
      const inFaceoff = data.finalists.includes(myEntity);
      if (!inFaceoff) {
        render(`<div class="trc-title">Face-Off</div><div class="trc-sub">Watching the finalists answer on the TV.</div>`);
        return;
      }
      render(`
        <div class="trc-sub">${escapeHtml(data.prompt)}</div>
        <input class="trc-textinput" id="trc-fo-input" maxlength="40" placeholder="Type your answer…" autocomplete="off" />
        <button class="btn primary" id="trc-fo-btn" style="margin-top:.8rem">Submit</button>
      `);
      const submit = () => {
        const input = box.querySelector('#trc-fo-input');
        const text = input ? input.value.trim() : '';
        if (!text) return;
        socket.emit('trivia:faceoffSubmit', { text });
        if (window.MP_Feedback) window.MP_Feedback.play('tick');
        render(`<div class="trc-title">Submitted!</div><div class="trc-sub">"${escapeHtml(text)}" — waiting on the other finalist…</div>`);
      };
      box.querySelector('#trc-fo-btn').addEventListener('click', submit);
    }
    socket.on('trivia:faceoffQuestion', onFaceoffQuestion);

    function onFaceoffResult(data) {
      const mine = data.answers[myEntity];
      if (!mine) return; // not a finalist - stay on the "watching" screen
      if (window.MP_Feedback) window.MP_Feedback.play(mine.points ? 'success' : 'tick');
      render(`
        <div class="trc-title">${mine.points ? `+$${mine.points}!` : 'No match'}</div>
        <div class="trc-sub">You said "${escapeHtml(mine.text || '')}"</div>
      `);
    }
    socket.on('trivia:faceoffResult', onFaceoffResult);

    function onMatchResult(data) {
      const won = data.winner === myEntity;
      if (window.MP_Feedback) window.MP_Feedback.play(data.tie ? 'error' : won ? 'win' : 'lose');
      if (navigator.vibrate) navigator.vibrate(won ? [0, 60] : [0, 120, 60, 120]);
      render(`
        <div class="trc-icon">${data.tie ? '🤝' : won ? '🏆' : '💥'}</div>
        <div class="trc-title">${data.tie ? "It's a tie!" : won ? 'YOU WIN!' : 'Game over'}</div>
        <div class="trc-sub">Look at the TV for the final scores.</div>
      `);
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

  window.MP_CONTROLLERS = window.MP_CONTROLLERS || {};
  window.MP_CONTROLLERS.trivia = { start };
})();
