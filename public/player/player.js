(function () {
  const socket = io();
  const el = (id) => document.getElementById(id);
  const screens = {
    join: el('screen-join'),
    waiting: el('screen-waiting'),
    spectate: el('screen-spectate'),
    controller: el('screen-controller'),
    closed: el('screen-closed'),
  };
  function showScreen(name) {
    Object.values(screens).forEach((s) => s.classList.remove('active'));
    screens[name].classList.add('active');
  }

  let roomCode = null;
  let playerId = null;
  let selectedAvatar = window.MP_AVATARS[0].id;
  let activeController = null;

  // ---------- Motion permission (iOS 13+ needs a user gesture) ----------
  window.MP_requestMotionPermission = async function () {
    try {
      const needsMotion = typeof DeviceMotionEvent !== 'undefined' &&
        typeof DeviceMotionEvent.requestPermission === 'function';
      const needsOrientation = typeof DeviceOrientationEvent !== 'undefined' &&
        typeof DeviceOrientationEvent.requestPermission === 'function';
      if (needsMotion) {
        const res = await DeviceMotionEvent.requestPermission();
        if (res !== 'granted') return false;
      }
      if (needsOrientation) {
        const res = await DeviceOrientationEvent.requestPermission();
        if (res !== 'granted') return false;
      }
      return true;
    } catch (e) {
      return false;
    }
  };

  // ---------- Join screen setup ----------
  const params = new URLSearchParams(location.search);
  if (params.get('code')) el('input-code').value = params.get('code').toUpperCase();

  const grid = el('avatar-grid');
  window.MP_AVATARS.forEach((a, i) => {
    const opt = document.createElement('div');
    opt.className = 'avatar-opt' + (i === 0 ? ' selected' : '');
    opt.innerHTML = a.svg;
    opt.title = a.name;
    opt.addEventListener('click', () => {
      grid.querySelectorAll('.avatar-opt').forEach((o) => o.classList.remove('selected'));
      opt.classList.add('selected');
      selectedAvatar = a.id;
    });
    grid.appendChild(opt);
  });

  el('btn-join').addEventListener('click', async () => {
    const code = el('input-code').value.trim().toUpperCase();
    const name = el('input-name').value.trim() || 'Player';
    el('join-error').textContent = '';
    if (code.length !== 4) {
      el('join-error').textContent = 'Enter the 4-letter code from the TV.';
      return;
    }
    el('btn-join').disabled = true;
    await window.MP_requestMotionPermission();

    socket.emit('player:join', { code, name, avatarId: selectedAvatar }, (res) => {
      el('btn-join').disabled = false;
      if (!res || !res.ok) {
        el('join-error').textContent = (res && res.error) || 'Could not join. Try again.';
        return;
      }
      roomCode = res.code;
      playerId = res.playerId;
      const av = window.MP_getAvatar(selectedAvatar);
      el('you-avatar').src = 'data:image/svg+xml;utf8,' + encodeURIComponent(av.svg);
      el('you-name').textContent = name;
      showScreen('waiting');
    });
  });

  // ---------- Game lifecycle ----------
  socket.on('game:start', ({ game, players }) => {
    const me = players.find((p) => p.id === playerId);
    if (!me) {
      const others = players.map((p) => p.name).join(' vs ');
      el('spectate-text').textContent = others || "A game is starting";
      showScreen('spectate');
      return;
    }
    const opponent = players.find((p) => p.id !== playerId);
    const root = el('controller-root');
    root.innerHTML = '';
    const mod = window.MP_CONTROLLERS && window.MP_CONTROLLERS[game];
    if (!mod) return;
    // Show the screen first so game modules that measure the DOM (e.g. sizing a canvas)
    // don't read zero dimensions from a still-`display:none` container.
    showScreen('controller');
    activeController = mod.start(root, { socket, roomCode, playerId, me, opponent });
  });

  socket.on('menu:show', () => {
    stopController();
    showScreen('waiting');
  });

  socket.on('match:aborted', () => {
    stopController();
    showScreen('waiting');
  });

  socket.on('room:closed', () => {
    stopController();
    showScreen('closed');
  });

  function stopController() {
    if (activeController && typeof activeController.stop === 'function') activeController.stop();
    activeController = null;
    el('controller-root').innerHTML = '';
  }
})();
