const path = require('path');
const http = require('http');
const express = require('express');
const { Server } = require('socket.io');
const QRCode = require('qrcode');
const { nanoid } = require('nanoid');
const { RoomStore } = require('./rooms');

const app = express();
const server = http.createServer(app);

// Some reverse proxies (Cloudflare Tunnel included) strip the trailing slash
// from "/socket.io/" when a query string follows, e.g. "/socket.io?EIO=...".
// engine.io only matches the exact "/socket.io/" prefix, so restore the slash
// before any request listener (including engine.io's own) sees it.
const originalEmit = server.emit.bind(server);
server.emit = (event, ...args) => {
  if (event === 'request' || event === 'upgrade') {
    const req = args[0];
    if (typeof req.url === 'string') {
      req.url = req.url.replace(/^\/socket\.io(?=$|\?)/, '/socket.io/');
    }
  }
  return originalEmit(event, ...args);
};

const io = new Server(server);

const PORT = process.env.PORT || 3000;
const PUBLIC_DIR = path.join(__dirname, '..', 'public');

app.use(express.static(PUBLIC_DIR));

const store = new RoomStore();

// ---- Quickshoot (1-2-3 Shoot) timing constants (ms) ----
const QS_WALK_DURATION = 3600; // must match the walk/turn animation on the host screen
const QS_READY_DELAY = [1000, 2200];
const QS_SET_DELAY = [900, 2100];
const QS_RESULT_TIMEOUT = 3000; // grace period after FIRE before we declare a no-show

// ---- Stay on Track timing constants (ms) ----
const SOT_COUNTDOWN_MS = 3000; // must match the 3-2-1-GO countdown shown on host + phones

function rand(min, max) {
  return Math.floor(min + Math.random() * (max - min));
}

function clearGameTimers(room) {
  if (room.gameState && room.gameState.timers) {
    room.gameState.timers.forEach((t) => clearTimeout(t));
  }
}

function joinUrlFor(socket) {
  const headers = socket.handshake.headers;
  const proto = headers['x-forwarded-proto'] || (socket.handshake.secure ? 'https' : 'http');
  const host = headers['x-forwarded-host'] || headers.host;
  return `${proto}://${host}/player/`;
}

io.on('connection', (socket) => {
  // ---------------- HOST ----------------
  socket.on('host:create', async (_data, cb) => {
    const room = store.createRoom(socket.id);
    socket.join(room.code);
    const base = joinUrlFor(socket);
    const joinUrl = `${base}?code=${room.code}`;
    let qrDataUrl = null;
    try {
      qrDataUrl = await QRCode.toDataURL(joinUrl, { margin: 1, width: 320 });
    } catch (e) {
      qrDataUrl = null;
    }
    cb && cb({ ok: true, code: room.code, joinUrl, qrDataUrl });
  });

  socket.on('host:selectGame', ({ code, game }) => {
    const room = store.getRoom(code);
    if (!room || room.hostSocketId !== socket.id) return;
    room.currentGame = game;
    io.to(room.code).emit('menu:gameSelected', { game });
  });

  socket.on('host:setMatchPlayers', ({ code, playerIds }) => {
    const room = store.getRoom(code);
    if (!room || room.hostSocketId !== socket.id) return;
    const valid = (playerIds || []).filter((id) => room.players.has(id));
    room.matchPlayers = valid;
    const players = valid.map((id) => publicPlayer(room, id));
    io.to(room.code).emit('match:playersSet', { players });
  });

  socket.on('host:startMatch', ({ code, game }) => {
    const room = store.getRoom(code);
    if (!room || room.hostSocketId !== socket.id) return;
    const minPlayers = game === 'stayontrack' ? 1 : 2;
    if (!room.matchPlayers || room.matchPlayers.length < minPlayers || room.matchPlayers.length > 2) return;

    clearGameTimers(room);
    room.currentGame = game;
    room.gameState = { phase: 'starting', timers: [], resolved: false };

    const players = room.matchPlayers.map((id) => publicPlayer(room, id));
    io.to(room.code).emit('game:start', { game, players });

    if (game === 'quickshoot') {
      startQuickshoot(room);
    } else if (game === 'stayontrack') {
      startStayOnTrack(room);
    }
  });

  socket.on('host:toMenu', ({ code }) => {
    const room = store.getRoom(code);
    if (!room || room.hostSocketId !== socket.id) return;
    clearGameTimers(room);
    room.currentGame = null;
    room.gameState = null;
    room.matchPlayers = [];
    io.to(room.code).emit('menu:show');
  });

  // Generic relay: host -> one player or all players (used by tennis for cue/vibrate messages)
  socket.on('host:broadcast', ({ code, event, payload, targetPlayerId }) => {
    const room = store.getRoom(code);
    if (!room || room.hostSocketId !== socket.id) return;
    if (targetPlayerId) {
      const p = room.players.get(targetPlayerId);
      if (p) io.to(p.socketId).emit('host:event', { event, payload });
    } else {
      for (const p of room.players.values()) {
        io.to(p.socketId).emit('host:event', { event, payload });
      }
    }
  });

  socket.on('host:reportResult', ({ code }) => {
    const room = store.getRoom(code);
    if (!room || room.hostSocketId !== socket.id) return;
    clearGameTimers(room);
  });

  // ---------------- PLAYER ----------------
  socket.on('player:join', ({ code, name, avatarId }, cb) => {
    const room = store.getRoom(code);
    if (!room) {
      cb && cb({ ok: false, error: 'Room not found. Check the code and try again.' });
      return;
    }
    const cleanName = String(name || 'Player').trim().slice(0, 16) || 'Player';
    const id = nanoid(8);
    store.addPlayer(room.code, { id, name: cleanName, avatarId, socketId: socket.id });
    socket.join(room.code);
    socket.data.playerId = id;
    socket.data.roomCode = room.code;

    io.to(room.hostSocketId).emit('lobby:update', { players: store.publicPlayers(room) });
    cb && cb({ ok: true, playerId: id, code: room.code });
  });

  socket.on('player:updateProfile', ({ name, avatarId }) => {
    const room = store.getRoom(socket.data.roomCode);
    if (!room) return;
    const p = room.players.get(socket.data.playerId);
    if (!p) return;
    if (name) p.name = String(name).trim().slice(0, 16) || p.name;
    if (avatarId) p.avatarId = avatarId;
    io.to(room.hostSocketId).emit('lobby:update', { players: store.publicPlayers(room) });
  });

  // Player raw controller input -> relay to host only
  socket.on('player:input', ({ gameEvent, payload }) => {
    const room = store.getRoom(socket.data.roomCode);
    if (!room) return;
    io.to(room.hostSocketId).emit('input:relay', {
      playerId: socket.data.playerId,
      gameEvent,
      payload,
    });
  });

  // Quickshoot: player moved too early
  socket.on('quickshoot:falseStart', () => {
    const room = store.getRoom(socket.data.roomCode);
    if (!room || room.currentGame !== 'quickshoot' || !room.gameState) return;
    resolveQuickshoot(room, {
      reason: 'falseStart',
      loserId: socket.data.playerId,
    });
  });

  // Quickshoot: player drew after FIRE
  socket.on('quickshoot:draw', ({ reactionTime }) => {
    const room = store.getRoom(socket.data.roomCode);
    if (!room || room.currentGame !== 'quickshoot' || !room.gameState) return;
    const gs = room.gameState;
    if (gs.resolved || gs.phase !== 'fire') return;
    gs.results = gs.results || {};
    if (gs.results[socket.data.playerId] != null) return;
    gs.results[socket.data.playerId] = Math.max(0, Math.round(reactionTime));

    const ids = room.matchPlayers;
    const haveBoth = ids.every((id) => gs.results[id] != null);
    if (haveBoth) {
      const [a, b] = ids;
      const winnerId = gs.results[a] <= gs.results[b] ? a : b;
      const loserId = winnerId === a ? b : a;
      resolveQuickshoot(room, { reason: 'normal', winnerId, loserId, times: gs.results });
    }
  });

  // Stay on Track: a player reached the end of the 4th track
  socket.on('stayontrack:finish', ({ totalTimeMs }) => {
    const room = store.getRoom(socket.data.roomCode);
    if (!room || room.currentGame !== 'stayontrack' || !room.gameState) return;
    const gs = room.gameState;
    if (gs.resolved || gs.phase !== 'racing') return;
    gs.resolved = true;
    clearGameTimers(room);
    const winnerId = socket.data.playerId;
    const loserId = (room.matchPlayers || []).find((id) => id !== winnerId) || null;
    io.to(room.code).emit('stayontrack:result', {
      winnerId,
      loserId,
      winnerTimeMs: Math.round(totalTimeMs),
    });
  });

  // ---------------- DISCONNECT ----------------
  socket.on('disconnect', () => {
    const result = store.removeBySocket(socket.id);
    if (!result) return;
    const { room, hostLeft, playerId } = result;
    if (hostLeft) {
      io.to(room.code).emit('room:closed');
      return;
    }
    if (playerId) {
      io.to(room.hostSocketId).emit('lobby:update', { players: store.publicPlayers(room) });
      if (room.matchPlayers && room.matchPlayers.includes(playerId) && room.currentGame) {
        clearGameTimers(room);
        io.to(room.code).emit('match:aborted', { reason: 'playerLeft', playerId });
        room.currentGame = null;
        room.gameState = null;
      }
    }
  });
});

function publicPlayer(room, id) {
  const p = room.players.get(id);
  if (!p) return null;
  return { id: p.id, name: p.name, avatarId: p.avatarId };
}

function startQuickshoot(room) {
  const gs = room.gameState;
  gs.phase = 'walk';
  gs.timers = [];

  gs.timers.push(
    setTimeout(() => {
      gs.phase = 'ready';
      io.to(room.code).emit('quickshoot:state', { phase: 'ready', ts: Date.now() });

      gs.timers.push(
        setTimeout(() => {
          gs.phase = 'set';
          io.to(room.code).emit('quickshoot:state', { phase: 'set', ts: Date.now() });

          gs.timers.push(
            setTimeout(() => {
              gs.phase = 'fire';
              gs.results = {};
              io.to(room.code).emit('quickshoot:state', { phase: 'fire', ts: Date.now() });

              gs.timers.push(
                setTimeout(() => {
                  if (gs.resolved) return;
                  const ids = room.matchPlayers;
                  const results = gs.results || {};
                  const [a, b] = ids;
                  const aTime = results[a];
                  const bTime = results[b];
                  let winnerId = null;
                  let loserId = null;
                  if (aTime == null && bTime == null) {
                    resolveQuickshoot(room, { reason: 'noShow' });
                    return;
                  } else if (aTime == null) {
                    winnerId = b;
                    loserId = a;
                  } else if (bTime == null) {
                    winnerId = a;
                    loserId = b;
                  } else {
                    winnerId = aTime <= bTime ? a : b;
                    loserId = winnerId === a ? b : a;
                  }
                  resolveQuickshoot(room, { reason: 'timeout', winnerId, loserId, times: results });
                }, QS_RESULT_TIMEOUT)
              );
            }, rand(QS_SET_DELAY[0], QS_SET_DELAY[1]))
          );
        }, rand(QS_READY_DELAY[0], QS_READY_DELAY[1]))
      );
    }, QS_WALK_DURATION)
  );
}

function startStayOnTrack(room) {
  const gs = room.gameState;
  gs.phase = 'countdown';
  gs.timers = [];
  gs.timers.push(
    setTimeout(() => {
      if (gs.resolved) return;
      gs.phase = 'racing';
      io.to(room.code).emit('stayontrack:go', { ts: Date.now() });
    }, SOT_COUNTDOWN_MS)
  );
}

function resolveQuickshoot(room, result) {
  const gs = room.gameState;
  if (!gs || gs.resolved) return;
  gs.resolved = true;
  clearGameTimers(room);
  io.to(room.code).emit('quickshoot:result', result);
}

server.listen(PORT, () => {
  console.log(`Motion Party server running on http://localhost:${PORT}`);
  console.log(`  Host screen:  http://localhost:${PORT}/host/`);
  console.log(`  Player join:  http://localhost:${PORT}/player/`);
});
