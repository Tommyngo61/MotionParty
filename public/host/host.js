(function () {
  const socket = io();
  let roomCode = null;
  let players = new Map(); // id -> {id,name,avatarId}
  let pendingGame = null;
  let pendingMode = 'single'; // 'single' | 'bracket' | 'ffa'
  let selected = [];
  let activeGameHandle = null;
  let bracket = null; // { game, rounds: [[{a,b,winnerId}]], roundIndex, entrantNames } while a tournament is running

  const GAME_INFO = {
    tennis: {
      emoji: '🎾',
      name: 'Motion Tennis',
      supportsBracket: true,
      howTo: 'Your character auto-moves into position. Swing your phone like a racket when the ball is in range to hit it back — tilt left/right at the moment of the swing to steer your return. First to 5 points wins.',
    },
    quickshoot: {
      emoji: '🤠',
      name: '1-2-3 Shoot!',
      supportsBracket: true,
      howTo: 'Watch the light: red, then orange, then green. The instant it turns green, flick your phone up to draw. Draw before green and you lose instantly. Fastest reaction wins the duel.',
    },
    stayontrack: {
      emoji: '🛤️',
      name: 'Stay on Track',
      freeForAll: true,
      howTo: "Hold your phone flat to start. You move forward on your own — tilt left/right to steer a ball along a winding path without falling off — Track 4 is a loop, go around it twice. Everyone races at once; first to clear all 4 tracks wins.",
    },
    tiltmaze: {
      emoji: '🧭',
      name: 'Tilt Maze',
      freeForAll: true,
      howTo: "Hold your phone flat to start. Tilt it like a tray to roll an iron ball through a maze to the hole — it can't fall off, the maze walls contain it. Everyone races at once; first to solve the Easy maze then the Hard maze wins.",
    },
    colormatch: {
      emoji: '🎨',
      name: 'Color Match Relay',
      freeForAll: true,
      howTo: 'A color flashes on the TV — run and find something in the room that color, then photograph it with your phone camera within 5 seconds. Everyone competes at once; first close-enough match wins the round.',
    },
  };

  const el = (id) => document.getElementById(id);
  const screens = {
    lobby: el('screen-lobby'),
    select: el('screen-select'),
    game: el('screen-game'),
    bracket: el('screen-bracket'),
  };

  let stopTutorialStage = () => {};

  function showScreen(name) {
    if (name !== 'select') {
      stopTutorialStage();
      stopTutorialStage = () => {};
    }
    Object.values(screens).forEach((s) => s.classList.remove('active'));
    screens[name].classList.add('active');
  }

  function toast(msg, ms = 2500) {
    const t = el('toast');
    t.textContent = msg;
    t.classList.add('show');
    clearTimeout(t._timer);
    t._timer = setTimeout(() => t.classList.remove('show'), ms);
  }

  function avatarImg(avatarId, size) {
    const a = window.MP_getAvatar(avatarId);
    const img = document.createElement('img');
    img.src = 'data:image/svg+xml;utf8,' + encodeURIComponent(a.svg);
    if (size) { img.width = size; img.height = size; }
    return img;
  }

  // ---------- Lobby rendering ----------
  function renderPlayerList() {
    const list = el('player-list');
    list.innerHTML = '';
    el('player-count').textContent = `(${players.size})`;
    if (players.size === 0) {
      const p = document.createElement('div');
      p.className = 'player-empty';
      p.textContent = 'Waiting for players to scan the QR code…';
      list.appendChild(p);
      return;
    }
    for (const p of players.values()) {
      const chip = document.createElement('div');
      chip.className = 'player-chip';
      chip.appendChild(avatarImg(p.avatarId, 36));
      const name = document.createElement('span');
      name.className = 'pname';
      name.textContent = p.name;
      chip.appendChild(name);
      list.appendChild(chip);
    }
  }

  // ---------- Player select screen ----------
  function minPlayersFor(game, mode) {
    const info = GAME_INFO[game];
    if (info && info.freeForAll) return 1;
    return mode === 'bracket' ? 3 : 2;
  }

  function maxPlayersFor(game, mode) {
    const info = GAME_INFO[game];
    if (info && info.freeForAll) return Infinity;
    return mode === 'bracket' ? 16 : 2;
  }

  function openSelectScreen(game) {
    pendingGame = game;
    pendingMode = GAME_INFO[game] && GAME_INFO[game].freeForAll ? 'ffa' : 'single';
    selected = [];
    renderModeToggle();
    renderHowTo();
    renderSelectTitle();
    renderSelectList();
    showScreen('select');
    stopTutorialStage();
    stopTutorialStage = window.MP_renderTutorial(el('select-stage'), game);
  }

  function renderModeToggle() {
    const wrap = el('select-mode');
    const info = GAME_INFO[pendingGame];
    if (!info || !info.supportsBracket) {
      wrap.style.display = 'none';
      return;
    }
    wrap.style.display = 'flex';
    wrap.querySelectorAll('.mode-btn').forEach((btn) => {
      btn.classList.toggle('active', btn.dataset.mode === pendingMode);
    });
  }

  function renderHowTo() {
    const info = GAME_INFO[pendingGame];
    el('select-howto').textContent = info ? info.howTo : '';
  }

  function renderSelectTitle() {
    const info = GAME_INFO[pendingGame];
    const name = info ? info.name : 'Players';
    let sub;
    if (pendingMode === 'ffa') sub = `Choose Players for ${name}`;
    else if (pendingMode === 'bracket') sub = `Choose 3+ Players for a ${name} Tournament`;
    else sub = `Choose 2 Players for ${name}`;
    el('select-title').textContent = sub;
  }

  el('select-mode').querySelectorAll('.mode-btn').forEach((btn) => {
    btn.addEventListener('click', () => {
      pendingMode = btn.dataset.mode;
      selected = [];
      renderModeToggle();
      renderSelectTitle();
      renderSelectList();
    });
  });

  function renderSelectList() {
    const list = el('select-list');
    list.innerHTML = '';
    if (players.size === 0) {
      const p = document.createElement('div');
      p.className = 'player-empty';
      p.textContent = 'No players yet — have them scan the QR code first.';
      list.appendChild(p);
    }
    for (const p of players.values()) {
      const card = document.createElement('div');
      card.className = 'select-card' + (selected.includes(p.id) ? ' selected' : '');
      card.appendChild(avatarImg(p.avatarId, 64));
      const name = document.createElement('div');
      name.textContent = p.name;
      name.style.fontWeight = '700';
      card.appendChild(name);
      const badge = document.createElement('div');
      badge.className = 'badge';
      badge.textContent = 'READY';
      card.appendChild(badge);
      card.addEventListener('click', () => toggleSelect(p.id));
      list.appendChild(card);
    }
    const min = minPlayersFor(pendingGame, pendingMode);
    const max = maxPlayersFor(pendingGame, pendingMode);
    el('select-start').disabled = selected.length < min || selected.length > max;
    el('select-start').textContent = pendingMode === 'bracket' ? 'Start Tournament' : 'Start Game';
  }

  function toggleSelect(id) {
    const max = maxPlayersFor(pendingGame, pendingMode);
    if (selected.includes(id)) {
      selected = selected.filter((x) => x !== id);
    } else {
      if (selected.length >= max) {
        toast(max === 2 ? 'Only 2 players for this mode — deselect one first.' : `Only ${max} players max — deselect one first.`);
        return;
      }
      selected.push(id);
    }
    renderSelectList();
  }

  el('select-back').addEventListener('click', () => showScreen('lobby'));
  el('select-start').addEventListener('click', () => {
    const min = minPlayersFor(pendingGame, pendingMode);
    const max = maxPlayersFor(pendingGame, pendingMode);
    if (selected.length < min || selected.length > max) return;
    if (pendingMode === 'bracket') {
      startBracket(pendingGame, selected.map((id) => players.get(id)).filter(Boolean));
      return;
    }
    socket.emit('host:setMatchPlayers', { code: roomCode, playerIds: selected });
    socket.emit('host:startMatch', { code: roomCode, game: pendingGame });
  });

  // ---------- Game tiles ----------
  document.querySelectorAll('.game-tile[data-game]').forEach((tile) => {
    tile.addEventListener('click', () => {
      if (window.MP_Feedback) window.MP_Feedback.unlock(); // first tap on the TV - safe point to unlock audio
      openSelectScreen(tile.dataset.game);
    });
  });

  // ---------- Tournament bracket ----------
  function shuffle(arr) {
    const a = arr.slice();
    for (let i = a.length - 1; i > 0; i--) {
      const j = Math.floor(Math.random() * (i + 1));
      [a[i], a[j]] = [a[j], a[i]];
    }
    return a;
  }

  function nextPow2(n) {
    let p = 1;
    while (p < n) p *= 2;
    return p;
  }

  // Subsequent rounds are always built from a round's winners, which is already a
  // clean power-of-2-length list with no byes needed.
  function buildRound(entrants) {
    const round = [];
    for (let i = 0; i < entrants.length; i += 2) {
      round.push({ a: entrants[i], b: entrants[i + 1], winnerId: null });
    }
    return round;
  }

  // Round 1 is the only round that needs bye padding. Byes must be spread one per
  // match (never two byes paired together, which would create an unplayable,
  // unresolvable match with no real players in it) - since the bye count is always
  // less than half the bracket size, giving each bye its own match first and only
  // pairing byes up with a real player guarantees that.
  function buildFirstRound(entrants) {
    const n = entrants.length;
    const size = nextPow2(n);
    const byeCount = size - n;
    const round = [];
    let idx = 0;
    for (let i = 0; i < byeCount; i++) round.push({ a: entrants[idx++], b: null, winnerId: null });
    while (idx < n) round.push({ a: entrants[idx++], b: entrants[idx++], winnerId: null });
    return round;
  }

  function resolveByes() {
    const round = bracket.rounds[bracket.roundIndex];
    for (const m of round) {
      if (m.winnerId == null && (!m.a) !== (!m.b) && (m.a || m.b)) {
        m.winnerId = (m.a || m.b).id;
      }
    }
  }

  function currentMatch() {
    const round = bracket.rounds[bracket.roundIndex];
    return round.find((m) => m.winnerId == null && m.a && m.b) || null;
  }

  function isTournamentDone() {
    const round = bracket.rounds[bracket.roundIndex];
    return round.length === 1 && round[0].winnerId != null;
  }

  function advanceRoundIfComplete() {
    const round = bracket.rounds[bracket.roundIndex];
    if (round.some((m) => m.winnerId == null)) return;
    if (round.length === 1) return;
    const winners = round.map((m) => (m.a && m.winnerId === m.a.id ? m.a : m.b));
    bracket.rounds.push(buildRound(winners));
    bracket.roundIndex++;
    resolveByes();
    advanceRoundIfComplete();
  }

  function startBracket(game, entrantPlayers) {
    const shuffled = shuffle(entrantPlayers);
    bracket = {
      game,
      rounds: [buildFirstRound(shuffled)],
      roundIndex: 0,
      entrantNames: entrantPlayers.map((p) => p.name),
    };
    resolveByes();
    advanceRoundIfComplete();
    showScreen('bracket');
    renderBracket(true);
  }

  function shuffleReveal(slotEl, finalName, pool, delay) {
    const others = pool.filter((n) => n !== finalName);
    if (others.length === 0) return;
    setTimeout(() => {
      let i = 0;
      const iv = setInterval(() => {
        slotEl.textContent = others[i % others.length];
        i++;
      }, 70);
      setTimeout(() => {
        clearInterval(iv);
        slotEl.textContent = finalName;
        slotEl.classList.add('revealed');
      }, 650);
    }, delay);
  }

  function renderBracket(animate) {
    const container = el('bracket-rounds');
    container.innerHTML = '';
    bracket.rounds.forEach((round, ri) => {
      const roundEl = document.createElement('div');
      roundEl.className = 'bracket-round';
      const title = document.createElement('div');
      title.className = 'bracket-round-title';
      title.textContent = round.length === 1 ? 'Final' : `Round ${ri + 1}`;
      roundEl.appendChild(title);
      round.forEach((m) => {
        const matchEl = document.createElement('div');
        matchEl.className = 'bracket-match';
        if (bracket.roundIndex === ri && m.winnerId == null && m.a && m.b) matchEl.classList.add('current');

        const slotA = document.createElement('div');
        slotA.className = 'bracket-slot' + slotClass(m, m.a);
        const aFinal = m.a ? m.a.name : (m.b ? '' : '—');
        slotA.textContent = animate && ri === 0 && m.a && m.b ? '' : aFinal;
        matchEl.appendChild(slotA);

        const vs = document.createElement('div');
        vs.className = 'bracket-vs';
        vs.textContent = 'vs';
        matchEl.appendChild(vs);

        const slotB = document.createElement('div');
        slotB.className = 'bracket-slot' + slotClass(m, m.b);
        const bFinal = m.b ? m.b.name : (m.a ? 'BYE' : '—');
        slotB.textContent = animate && ri === 0 && m.a && m.b ? '' : bFinal;
        matchEl.appendChild(slotB);

        roundEl.appendChild(matchEl);

        if (animate && ri === 0 && m.a && m.b) {
          shuffleReveal(slotA, aFinal, bracket.entrantNames, 80);
          shuffleReveal(slotB, bFinal, bracket.entrantNames, 160);
        }
      });
      container.appendChild(roundEl);
    });

    const actions = el('bracket-actions');
    actions.innerHTML = '';
    if (isTournamentDone()) {
      const finalMatch = bracket.rounds[bracket.roundIndex][0];
      const champion = finalMatch.a && finalMatch.winnerId === finalMatch.a.id ? finalMatch.a : finalMatch.b;
      const h = document.createElement('div');
      h.className = 'bracket-champion';
      h.textContent = `🏆 ${champion ? champion.name : ''} is the champion!`;
      actions.appendChild(h);
      const btn = document.createElement('button');
      btn.className = 'btn primary';
      btn.textContent = 'Back to Menu';
      btn.addEventListener('click', exitBracket);
      actions.appendChild(btn);
    } else {
      const m = currentMatch();
      if (m) {
        const btn = document.createElement('button');
        btn.className = 'btn primary';
        btn.textContent = `Start Match: ${m.a.name} vs ${m.b.name}`;
        btn.addEventListener('click', startCurrentBracketMatch);
        actions.appendChild(btn);
      }
      const endBtn = document.createElement('button');
      endBtn.className = 'btn ghost';
      endBtn.textContent = 'End Tournament';
      endBtn.addEventListener('click', exitBracket);
      actions.appendChild(endBtn);
    }
  }

  function slotClass(m, side) {
    if (!m.winnerId || !side) return '';
    return side.id === m.winnerId ? ' winner' : ' loser';
  }

  function startCurrentBracketMatch() {
    const m = currentMatch();
    if (!m) return;
    socket.emit('host:setMatchPlayers', { code: roomCode, playerIds: [m.a.id, m.b.id] });
    socket.emit('host:startMatch', { code: roomCode, game: bracket.game });
  }

  function onBracketMatchResult(winnerId) {
    const m = currentMatch();
    stopActiveGame();
    if (m && winnerId) {
      m.winnerId = winnerId;
      advanceRoundIfComplete();
    }
    // A falsy winnerId (e.g. Quickshoot's no-show draw) just re-shows the same
    // unresolved match so the host can replay it - the bracket doesn't change.
    showScreen('bracket');
    renderBracket(false);
  }

  function exitBracket() {
    bracket = null;
    backToMenu();
  }

  el('bracket-end-top').addEventListener('click', exitBracket);

  // ---------- End game early (from a live match) ----------
  el('end-game-btn').addEventListener('click', () => {
    if (!confirm('End this game and return to the menu?')) return;
    if (bracket) exitBracket();
    else backToMenu();
  });

  // ---------- Socket wiring ----------
  socket.on('connect', () => {
    socket.emit('host:create', {}, (res) => {
      if (!res || !res.ok) {
        toast('Could not start a room. Refresh to try again.');
        return;
      }
      roomCode = res.code;
      el('room-code').textContent = res.code;
      if (res.qrDataUrl) el('qr-img').src = res.qrDataUrl;
    });
  });

  socket.on('lobby:update', ({ players: list }) => {
    players = new Map(list.map((p) => [p.id, p]));
    renderPlayerList();
    if (screens.select.classList.contains('active')) {
      selected = selected.filter((id) => players.has(id));
      renderSelectList();
    }
  });

  socket.on('game:start', ({ game, players: matchPlayers }) => {
    stopActiveGame();
    showScreen('game');
    const root = el('game-root');
    root.innerHTML = '';
    const mod = window.MP_GAMES && window.MP_GAMES[game];
    if (!mod) {
      toast('That game is not available yet.');
      backToMenu();
      return;
    }
    const inBracketMatch = !!(bracket && bracket.game === game && currentMatch());
    activeGameHandle = mod.start(root, {
      socket,
      roomCode,
      players: matchPlayers,
      onExit: inBracketMatch ? exitBracket : backToMenu,
      bracketMode: inBracketMatch,
      onMatchResult: inBracketMatch ? onBracketMatchResult : undefined,
    });
  });

  socket.on('menu:show', () => {
    bracket = null;
    stopActiveGame();
    showScreen('lobby');
  });

  socket.on('match:aborted', ({ reason }) => {
    bracket = null;
    stopActiveGame();
    showScreen('lobby');
    toast(reason === 'playerLeft' ? 'A player disconnected — match ended.' : 'Match ended.');
  });

  socket.on('room:closed', () => {
    toast('Room closed.');
  });

  function stopActiveGame() {
    if (activeGameHandle && typeof activeGameHandle.stop === 'function') {
      activeGameHandle.stop();
    }
    activeGameHandle = null;
    el('game-root').innerHTML = '';
  }

  function backToMenu() {
    bracket = null;
    socket.emit('host:toMenu', { code: roomCode });
  }

  renderPlayerList();
})();
