(function () {
  const socket = io();
  let roomCode = null;
  let players = new Map(); // id -> {id,name,avatarId}
  let pendingGame = null;
  let selected = [];
  let activeGameHandle = null;

  const el = (id) => document.getElementById(id);
  const screens = {
    lobby: el('screen-lobby'),
    select: el('screen-select'),
    game: el('screen-game'),
  };

  function showScreen(name) {
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
  function minPlayersFor(game) {
    return game === 'stayontrack' ? 1 : 2;
  }

  function openSelectScreen(game) {
    pendingGame = game;
    selected = [];
    const titles = {
      tennis: 'Choose 2 Players for Motion Tennis',
      quickshoot: 'Choose 2 Players for 1-2-3 Shoot!',
      stayontrack: 'Choose 1-2 Players for Stay on Track',
    };
    el('select-title').textContent = titles[game] || 'Choose Players';
    renderSelectList();
    showScreen('select');
  }

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
    el('select-start').disabled = selected.length < minPlayersFor(pendingGame) || selected.length > 2;
  }

  function toggleSelect(id) {
    if (selected.includes(id)) {
      selected = selected.filter((x) => x !== id);
    } else {
      if (selected.length >= 2) {
        toast('Only 2 players for this game — deselect one first.');
        return;
      }
      selected.push(id);
    }
    renderSelectList();
  }

  el('select-back').addEventListener('click', () => showScreen('lobby'));
  el('select-start').addEventListener('click', () => {
    if (selected.length < minPlayersFor(pendingGame) || selected.length > 2) return;
    socket.emit('host:setMatchPlayers', { code: roomCode, playerIds: selected });
    socket.emit('host:startMatch', { code: roomCode, game: pendingGame });
  });

  // ---------- Game tiles ----------
  document.querySelectorAll('.game-tile[data-game]').forEach((tile) => {
    tile.addEventListener('click', () => openSelectScreen(tile.dataset.game));
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
    activeGameHandle = mod.start(root, {
      socket,
      roomCode,
      players: matchPlayers,
      onExit: backToMenu,
    });
  });

  socket.on('menu:show', () => {
    stopActiveGame();
    showScreen('lobby');
  });

  socket.on('match:aborted', ({ reason }) => {
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
    socket.emit('host:toMenu', { code: roomCode });
  }

  renderPlayerList();
})();
