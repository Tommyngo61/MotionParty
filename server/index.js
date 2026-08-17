const path = require('path');
const http = require('http');
const express = require('express');
const { Server } = require('socket.io');
const QRCode = require('qrcode');
const { nanoid } = require('nanoid');
const { RoomStore } = require('./rooms');
const trivia = require('../public/shared/trivia.js');

const app = express();
const server = http.createServer(app);

// Cloudflare Tunnel strips the trailing slash from proxied request paths (e.g.
// "/socket.io/" -> "/socket.io", "/host/" -> "/host"). engine.io's handshake and
// express.static's directory-index resolution both require that exact trailing
// slash, so restore it - for these known directory-style routes only - before any
// request listener (including engine.io's own) sees it.
const originalEmit = server.emit.bind(server);
server.emit = (event, ...args) => {
  if (event === 'request' || event === 'upgrade') {
    const req = args[0];
    if (typeof req.url === 'string') {
      req.url = req.url.replace(/^\/(socket\.io|host|player)(?=$|\?)/, '/$1/');
    }
  }
  return originalEmit(event, ...args);
};

const io = new Server(server);

const PORT = process.env.PORT || 3000;
const PUBLIC_DIR = path.join(__dirname, '..', 'public');

// `redirect: false` stops express.static from 301'ing "/host" -> "/host/" to resolve
// a directory's index.html. Cloudflare Tunnel strips trailing slashes from proxied
// requests, so that redirect's target immediately loses its slash again on the way
// back in - an infinite redirect loop. We always link with the trailing slash already
// (see public/index.html), so the auto-redirect was never actually needed.
app.use(express.static(PUBLIC_DIR, { redirect: false }));

const store = new RoomStore();

// ---- Quickshoot (1-2-3 Shoot) timing constants (ms) ----
const QS_WALK_DURATION = 3600; // must match the walk/turn animation on the host screen
const QS_AIM_FALLBACK = 5000; // force-start past QS_WALK_DURATION if a phone never confirms aim-down (e.g. permission denied)
const QS_COUNTDOWN_MS = 5000; // fixed ready-to-fire countdown, shown on host + phones
const QS_RESULT_TIMEOUT = 3000; // grace period after FIRE before we declare a no-show

// Stay on Track / Tilt Maze both randomize their course from a per-match seed at
// one of these difficulty tiers - see public/shared/tracks.js / mazes.js for what
// each tier actually changes (course size, obstacle/hazard count, etc).
const DIFFICULTIES = ['easy', 'medium', 'hard', 'impossible'];

// ---- Stay on Track timing constants (ms) ----
const SOT_COUNTDOWN_MS = 3000; // must match the 3-2-1-GO countdown shown on host + phones

// ---- Tilt Maze timing constants (ms) ----
const TM_COUNTDOWN_MS = 3000; // must match the 3-2-1-GO countdown shown on host + phones

// ---- Color Match Relay timing/tuning ----
const CM_COUNTDOWN_MS = 3000; // must match the 3-2-1-GO countdown shown on host + phones
const CM_ROUND_MS = 5000; // time to find + photograph a matching object
// Server picks the target color (rather than trusting a client), so it can't be
// spoofed and every screen is guaranteed to show the same one.
const CM_PALETTE = [
  { name: 'Red', hex: '#e53935' },
  { name: 'Orange', hex: '#fb8c00' },
  { name: 'Yellow', hex: '#fdd835' },
  { name: 'Green', hex: '#43a047' },
  { name: 'Blue', hex: '#1e88e5' },
  { name: 'Purple', hex: '#8e24aa' },
  { name: 'Pink', hex: '#ec407a' },
  { name: 'Brown', hex: '#6d4c41' },
];
// "redmean" distance (0..~764) below which a captured color counts as a real match -
// mirrors public/shared/colors.js's colorDistance, kept generous since a phone
// camera's white balance/lighting skews colors a fair amount.
const CM_MATCH_THRESHOLD = 110;

// ---- Trivia Throwdown timing constants (ms) ----
const TR_COUNTDOWN_MS = 3000; // must match the "get ready" beat shown on host + phones
const TR_BUZZ_WINDOW_MS = 12000; // how long the buzz-in stays open before the tile auto-clears unscored
const TR_WAGER_TIMEOUT_MS = 20000; // Daily Double: default to a $0 wager if nobody submits one in time
const TR_BOARD_TO_FACEOFF_DELAY = 3000; // pause after the last tile clears before the Face-Off intro
const TR_FACEOFF_COUNTDOWN_MS = 3000;
const TR_FACEOFF_ANSWER_MS = 15000; // per-question answer window
const TR_FACEOFF_REVEAL_MS = 2600; // pause showing a Face-Off question's result before the next one
const TR_MODES = ['ffa', 'teams'];

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

  socket.on('host:startMatch', ({ code, game, difficulty, mode, teams, deductWrong }) => {
    const room = store.getRoom(code);
    if (!room || room.hostSocketId !== socket.id) return;
    const isFreeForAll = game === 'stayontrack' || game === 'tiltmaze' || game === 'colormatch' || game === 'trivia';
    // Trivia's buzz-race and Face-Off ("top 2 scorers") only make sense with at
    // least 2 in the game, unlike the other free-for-all games which allow solo play.
    const minPlayers = game === 'trivia' ? 2 : (isFreeForAll ? 1 : 2);
    const maxPlayers = isFreeForAll ? Infinity : 2;
    if (!room.matchPlayers || room.matchPlayers.length < minPlayers || room.matchPlayers.length > maxPlayers) return;

    clearGameTimers(room);
    room.currentGame = game;
    // A fresh random seed every match is what makes Stay on Track / Tilt Maze /
    // Trivia Throwdown layouts different each time instead of the same fixed
    // course - the host and every player regenerate the identical layout from
    // this one number.
    const seed = Math.floor(Math.random() * 0xffffffff);
    const safeDifficulty = DIFFICULTIES.includes(difficulty) ? difficulty : 'medium';
    room.gameState = { phase: 'starting', timers: [], resolved: false, seed, difficulty: safeDifficulty };

    const players = room.matchPlayers.map((id) => publicPlayer(room, id));
    io.to(room.code).emit('game:start', { game, players, seed, difficulty: safeDifficulty });

    if (game === 'quickshoot') {
      startQuickshoot(room);
    } else if (game === 'stayontrack') {
      startStayOnTrack(room);
    } else if (game === 'tiltmaze') {
      startTiltMaze(room);
    } else if (game === 'colormatch') {
      startColorMatch(room);
    } else if (game === 'trivia') {
      startTrivia(room, { mode, teams, deductWrong });
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

  // Quickshoot: player has held their phone in the aim-down/holster pose long enough
  socket.on('quickshoot:aimReady', () => {
    const room = store.getRoom(socket.data.roomCode);
    if (!room || room.currentGame !== 'quickshoot' || !room.gameState) return;
    const gs = room.gameState;
    if (gs.phase !== 'walk') return;
    const playerId = socket.data.playerId;
    if (!room.matchPlayers.includes(playerId)) return;
    gs.aimReady.add(playerId);
    io.to(room.code).emit('quickshoot:aimStatus', { readyIds: Array.from(gs.aimReady) });
    tryAdvanceQuickshoot(room);
  });

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

  // Stay on Track is free-for-all - any number of racers, first to clear all 4
  // tracks wins. There's no single "loser": everyone else just didn't finish first.
  socket.on('stayontrack:finish', ({ totalTimeMs }) => {
    const room = store.getRoom(socket.data.roomCode);
    if (!room || room.currentGame !== 'stayontrack' || !room.gameState) return;
    const gs = room.gameState;
    if (gs.resolved || gs.phase !== 'racing') return;
    gs.resolved = true;
    clearGameTimers(room);
    io.to(room.code).emit('stayontrack:result', {
      winnerId: socket.data.playerId,
      winnerTimeMs: Math.round(totalTimeMs),
    });
  });

  // Tilt Maze is also free-for-all - first to solve both mazes wins.
  socket.on('tiltmaze:finish', ({ totalTimeMs }) => {
    const room = store.getRoom(socket.data.roomCode);
    if (!room || room.currentGame !== 'tiltmaze' || !room.gameState) return;
    const gs = room.gameState;
    if (gs.resolved || gs.phase !== 'racing') return;
    gs.resolved = true;
    clearGameTimers(room);
    io.to(room.code).emit('tiltmaze:result', {
      winnerId: socket.data.playerId,
      winnerTimeMs: Math.round(totalTimeMs),
    });
  });

  // Color Match Relay: a player photographed something and we scored how close its
  // sampled color is to the target. First to clear the match threshold wins outright;
  // otherwise everyone's best attempt is kept for the round-timeout fallback.
  socket.on('colormatch:submit', ({ hex, distance }) => {
    const room = store.getRoom(socket.data.roomCode);
    if (!room || room.currentGame !== 'colormatch' || !room.gameState) return;
    const gs = room.gameState;
    if (gs.resolved || gs.phase !== 'racing') return;
    const playerId = socket.data.playerId;
    if (!playerId) return;
    const clamped = Math.max(0, Math.min(1000, Number(distance)));
    if (!Number.isFinite(clamped)) return;
    if (!gs.attempts[playerId] || clamped < gs.attempts[playerId].distance) {
      gs.attempts[playerId] = { hex: String(hex || '').slice(0, 9), distance: clamped };
    }
    if (clamped <= CM_MATCH_THRESHOLD) {
      gs.resolved = true;
      clearGameTimers(room);
      io.to(room.code).emit('colormatch:result', {
        winnerId: playerId,
        winnerDistance: Math.round(clamped),
        attempts: gs.attempts,
      });
    }
  });

  // Trivia Throwdown: active player/team picks a board tile.
  socket.on('trivia:pickTile', ({ catIndex, valIndex }) => {
    const room = store.getRoom(socket.data.roomCode);
    if (!room || room.currentGame !== 'trivia' || !room.gameState) return;
    const gs = room.gameState;
    if (gs.phase !== 'board') return;
    const entity = triviaEntityFor(room, socket.data.playerId);
    if (!entity || entity !== gs.activeEntity) return;
    if (!Number.isInteger(catIndex) || !Number.isInteger(valIndex)) return;
    if (catIndex < 0 || catIndex > 4 || valIndex < 0 || valIndex > 4) return;
    if (gs.cleared[catIndex][valIndex]) return;

    gs.pendingTile = { catIndex, valIndex };
    const isDailyDouble = gs.dailyDouble.cat === catIndex && gs.dailyDouble.val === valIndex;
    if (isDailyDouble) {
      gs.phase = 'wager';
      gs.wagerEntity = entity;
      clearGameTimers(room);
      gs.timers = [];
      io.to(room.code).emit('trivia:dailyDouble', {
        catIndex, valIndex, entity, maxWager: Math.max(0, gs.scores[entity] || 0),
      });
      gs.timers.push(setTimeout(() => {
        if (gs.phase === 'wager') triviaSubmitWager(room, entity, 0);
      }, TR_WAGER_TIMEOUT_MS));
    } else {
      triviaOpenClue(room, catIndex, valIndex, 0);
    }
  });

  // Trivia Throwdown: Daily Double wager, bounded to the selecting entity's own score.
  socket.on('trivia:wager', ({ amount }) => {
    const room = store.getRoom(socket.data.roomCode);
    if (!room || room.currentGame !== 'trivia' || !room.gameState) return;
    const gs = room.gameState;
    if (gs.phase !== 'wager') return;
    const entity = triviaEntityFor(room, socket.data.playerId);
    if (!entity || entity !== gs.wagerEntity) return;
    triviaSubmitWager(room, entity, amount);
  });

  // Trivia Throwdown: buzz-in race. First arrival wins - simplest fair "server
  // timestamp comparison" for a room full of independently-clocked phones, since
  // trusting each phone's own clock would be neither fair nor tamper-proof.
  socket.on('trivia:buzz', () => {
    const room = store.getRoom(socket.data.roomCode);
    if (!room || room.currentGame !== 'trivia' || !room.gameState) return;
    const gs = room.gameState;
    if (gs.phase !== 'clue') return;
    const entity = triviaEntityFor(room, socket.data.playerId);
    if (!entity || gs.wrongEntities.has(entity) || gs.buzzedBy) return;
    gs.buzzedBy = entity;
    gs.phase = 'buzzed';
    clearGameTimers(room);
    gs.timers = [];
    io.to(room.code).emit('trivia:buzzResult', { entity });
  });

  // Trivia Throwdown: host heard the buzzed-in answer out loud and judges it -
  // free-text NLP grading isn't reliable enough for open trivia, so (like the
  // real show) a human makes the call.
  socket.on('trivia:judge', ({ code, correct }) => {
    const room = store.getRoom(code);
    if (!room || room.hostSocketId !== socket.id) return;
    if (room.currentGame !== 'trivia' || !room.gameState) return;
    const gs = room.gameState;
    if (gs.phase !== 'buzzed' || !gs.buzzedBy) return;
    const entity = gs.buzzedBy;
    const clue = gs.currentClue;

    if (correct) {
      const award = clue.isDailyDouble ? clue.wager * 2 : clue.value;
      gs.scores[entity] = (gs.scores[entity] || 0) + award;
      gs.activeEntity = entity;
      resolveTriviaClue(room, { scored: true, entity, correct: true });
      return;
    }

    if (clue.isDailyDouble) {
      // Daily Double is one shot for the selecting entity only - no reopening.
      gs.scores[entity] = (gs.scores[entity] || 0) - clue.wager;
      resolveTriviaClue(room, { scored: true, entity, correct: false });
      return;
    }

    if (gs.deductWrong) gs.scores[entity] = (gs.scores[entity] || 0) - clue.value;
    gs.wrongEntities.add(entity);
    gs.buzzedBy = null;
    const stillEligible = gs.entities.some((e) => !gs.wrongEntities.has(e));
    if (!stillEligible) {
      resolveTriviaClue(room, { scored: false });
      return;
    }
    gs.phase = 'clue';
    io.to(room.code).emit('trivia:reopenBuzz', { wrongEntity: entity, scores: gs.scores });
    gs.timers.push(setTimeout(() => triviaBuzzTimeout(room), TR_BUZZ_WINDOW_MS));
  });

  // Trivia Throwdown Face-Off: a finalist (or anyone on a finalist team) submits
  // a free-text guess, matched server-side against that question's answer list.
  socket.on('trivia:faceoffSubmit', ({ text }) => {
    const room = store.getRoom(socket.data.roomCode);
    if (!room || room.currentGame !== 'trivia' || !room.gameState) return;
    const gs = room.gameState;
    if (gs.phase !== 'faceoff' || !gs.faceoff) return;
    const entity = triviaEntityFor(room, socket.data.playerId);
    if (!entity || !gs.faceoff.finalists.includes(entity)) return;
    if (gs.faceoff.answers[entity] != null) return;
    const q = gs.faceoff.questions[gs.faceoff.qIndex];
    const matched = trivia.MP_matchFaceoffAnswer(q, text);
    gs.faceoff.answers[entity] = { text: String(text || '').slice(0, 60), points: matched ? matched.points : 0 };
    io.to(room.hostSocketId).emit('trivia:faceoffAnswered', { entity });
    if (gs.faceoff.finalists.every((f) => gs.faceoff.answers[f] != null)) {
      resolveTriviaFaceoffQuestion(room);
    }
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
  gs.aimReady = new Set();
  gs.walkMinElapsed = false;

  // The walk/turn animation on the host screen always plays out in full...
  gs.timers.push(
    setTimeout(() => {
      gs.walkMinElapsed = true;
      tryAdvanceQuickshoot(room);
    }, QS_WALK_DURATION)
  );

  // ...but the duel itself won't start its countdown until both phones confirm
  // they're held in the aim-down/holster pose, up to a grace period in case a
  // phone's motion permission was denied or its sensor never reports.
  gs.timers.push(
    setTimeout(() => {
      if (gs.phase === 'walk') advanceQuickshootToCountdown(room);
    }, QS_WALK_DURATION + QS_AIM_FALLBACK)
  );
}

function tryAdvanceQuickshoot(room) {
  const gs = room.gameState;
  if (!gs || gs.phase !== 'walk' || !gs.walkMinElapsed) return;
  const allAimed = room.matchPlayers.every((id) => gs.aimReady.has(id));
  if (allAimed) advanceQuickshootToCountdown(room);
}

function advanceQuickshootToCountdown(room) {
  const gs = room.gameState;
  if (!gs || gs.phase !== 'walk') return;
  gs.phase = 'countdown';
  io.to(room.code).emit('quickshoot:state', { phase: 'countdown', ts: Date.now(), durationMs: QS_COUNTDOWN_MS });

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
    }, QS_COUNTDOWN_MS)
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

function startTiltMaze(room) {
  const gs = room.gameState;
  gs.phase = 'countdown';
  gs.timers = [];
  gs.timers.push(
    setTimeout(() => {
      if (gs.resolved) return;
      gs.phase = 'racing';
      io.to(room.code).emit('tiltmaze:go', { ts: Date.now() });
    }, TM_COUNTDOWN_MS)
  );
}

function startColorMatch(room) {
  const gs = room.gameState;
  gs.phase = 'countdown';
  gs.timers = [];
  gs.attempts = {}; // playerId -> { hex, distance } - each player's closest attempt
  gs.target = CM_PALETTE[Math.floor(Math.random() * CM_PALETTE.length)];
  gs.timers.push(
    setTimeout(() => {
      if (gs.resolved) return;
      gs.phase = 'racing';
      io.to(room.code).emit('colormatch:go', { target: gs.target, roundMs: CM_ROUND_MS, ts: Date.now() });
      gs.timers.push(setTimeout(() => finishColorMatchRound(room), CM_ROUND_MS));
    }, CM_COUNTDOWN_MS)
  );
}

function finishColorMatchRound(room) {
  const gs = room.gameState;
  if (gs.resolved) return;
  gs.resolved = true;
  clearGameTimers(room);
  let winnerId = null;
  let bestDistance = Infinity;
  for (const [playerId, attempt] of Object.entries(gs.attempts)) {
    if (attempt.distance < bestDistance) {
      bestDistance = attempt.distance;
      winnerId = playerId;
    }
  }
  io.to(room.code).emit('colormatch:result', {
    winnerId,
    winnerDistance: winnerId ? Math.round(bestDistance) : null,
    attempts: gs.attempts,
  });
}

function resolveQuickshoot(room, result) {
  const gs = room.gameState;
  if (!gs || gs.resolved) return;
  gs.resolved = true;
  clearGameTimers(room);
  io.to(room.code).emit('quickshoot:result', result);
}

// A "scorable entity" is a playerId in Free-for-all mode, or 'A'/'B' in Teams
// mode - everything from turn order to the scoreboard is keyed on this instead
// of playerId directly, so the same board/buzz/judge logic works for both modes.
function triviaEntityFor(room, playerId) {
  const gs = room.gameState;
  if (!gs || !playerId) return null;
  if (gs.mode === 'teams') return gs.teams[playerId] || null;
  return playerId;
}

function startTrivia(room, opts) {
  const gs = room.gameState;
  const mode = TR_MODES.includes(opts.mode) ? opts.mode : 'ffa';
  gs.mode = mode;
  gs.deductWrong = !!opts.deductWrong;
  gs.phase = 'countdown';
  gs.timers = [];

  if (mode === 'teams') {
    const raw = opts.teams || {};
    const map = {};
    room.matchPlayers.forEach((id) => { map[id] = raw[id] === 'B' ? 'B' : 'A'; });
    gs.teams = map;
    gs.entities = ['A', 'B'];
  } else {
    gs.teams = null;
    gs.entities = room.matchPlayers.slice();
  }
  gs.scores = {};
  gs.entities.forEach((e) => { gs.scores[e] = 0; });

  const board = trivia.MP_generateTriviaBoard(gs.seed);
  gs.categories = board.categories;
  gs.dailyDouble = board.dailyDouble; // never sent to clients directly - see trivia:board below
  gs.cleared = gs.categories.map(() => [false, false, false, false, false]);
  gs.activeEntity = gs.entities[0];
  gs.currentClue = null;
  gs.pendingTile = null;
  gs.wrongEntities = new Set();
  gs.buzzedBy = null;
  gs.wagerEntity = null;
  gs.faceoff = null;

  gs.timers.push(
    setTimeout(() => {
      gs.phase = 'board';
      io.to(room.code).emit('trivia:board', {
        categories: gs.categories.map((c) => ({ name: c.name, values: c.clues.map((cl) => cl.value) })),
        cleared: gs.cleared,
        scores: gs.scores,
        activeEntity: gs.activeEntity,
        mode: gs.mode,
        teams: gs.teams,
      });
    }, TR_COUNTDOWN_MS)
  );
}

function triviaOpenClue(room, catIndex, valIndex, wager) {
  const gs = room.gameState;
  const clueData = gs.categories[catIndex].clues[valIndex];
  const isDailyDouble = gs.dailyDouble.cat === catIndex && gs.dailyDouble.val === valIndex;
  gs.currentClue = { catIndex, valIndex, value: clueData.value, isDailyDouble, wager: isDailyDouble ? wager : 0 };
  gs.wrongEntities = new Set();
  gs.buzzedBy = null;
  gs.phase = 'clue';

  io.to(room.code).emit('trivia:clue', {
    catIndex, valIndex, text: clueData.clue, value: clueData.value,
    isDailyDouble, wager: gs.currentClue.wager, entity: gs.activeEntity,
  });

  if (isDailyDouble) {
    // Real Daily Double rules: only the selecting entity gets to answer, no buzz race.
    gs.buzzedBy = gs.activeEntity;
    gs.phase = 'buzzed';
    io.to(room.code).emit('trivia:buzzResult', { entity: gs.activeEntity });
  } else {
    gs.timers.push(setTimeout(() => triviaBuzzTimeout(room), TR_BUZZ_WINDOW_MS));
  }
}

function triviaSubmitWager(room, entity, amount) {
  const gs = room.gameState;
  if (gs.phase !== 'wager' || gs.wagerEntity !== entity) return;
  clearGameTimers(room);
  gs.timers = [];
  const max = Math.max(0, gs.scores[entity] || 0);
  const wager = Math.max(0, Math.min(max, Math.round(Number(amount)) || 0));
  const { catIndex, valIndex } = gs.pendingTile;
  triviaOpenClue(room, catIndex, valIndex, wager);
}

function triviaBuzzTimeout(room) {
  const gs = room.gameState;
  if (gs.phase !== 'clue') return; // already buzzed or otherwise resolved
  resolveTriviaClue(room, { scored: false });
}

function resolveTriviaClue(room, result) {
  const gs = room.gameState;
  clearGameTimers(room);
  gs.timers = [];
  const { catIndex, valIndex } = gs.pendingTile;
  gs.cleared[catIndex][valIndex] = true;
  const answer = gs.categories[catIndex].clues[valIndex].answer;
  gs.currentClue = null;
  gs.buzzedBy = null;
  gs.wagerEntity = null;
  const boardDone = gs.cleared.every((row) => row.every(Boolean));

  io.to(room.code).emit('trivia:clueResolved', {
    catIndex, valIndex, scored: result.scored, entity: result.entity || null,
    correct: result.correct || false, answer, scores: gs.scores, boardDone,
  });

  if (boardDone) {
    gs.phase = 'boardDone';
    gs.timers.push(setTimeout(() => startTriviaFaceoff(room), TR_BOARD_TO_FACEOFF_DELAY));
  } else {
    gs.phase = 'board';
    io.to(room.code).emit('trivia:turn', { entity: gs.activeEntity });
  }
}

function startTriviaFaceoff(room) {
  const gs = room.gameState;
  const ranked = gs.entities.slice().sort((a, b) => (gs.scores[b] || 0) - (gs.scores[a] || 0));
  const finalists = ranked.slice(0, 2);
  gs.faceoff = { finalists, questions: trivia.MP_generateTriviaFaceoff(gs.seed), qIndex: 0, answers: {} };
  gs.phase = 'faceoff-countdown';
  gs.timers = [];
  io.to(room.code).emit('trivia:faceoffStart', { finalists, scores: gs.scores });
  gs.timers.push(setTimeout(() => openTriviaFaceoffQuestion(room), TR_FACEOFF_COUNTDOWN_MS));
}

function openTriviaFaceoffQuestion(room) {
  const gs = room.gameState;
  const fo = gs.faceoff;
  if (fo.qIndex >= fo.questions.length) {
    finishTrivia(room);
    return;
  }
  const q = fo.questions[fo.qIndex];
  fo.answers = {};
  gs.phase = 'faceoff';
  io.to(room.code).emit('trivia:faceoffQuestion', {
    index: fo.qIndex, total: fo.questions.length, prompt: q.prompt, finalists: fo.finalists,
  });
  gs.timers.push(setTimeout(() => resolveTriviaFaceoffQuestion(room), TR_FACEOFF_ANSWER_MS));
}

function resolveTriviaFaceoffQuestion(room) {
  const gs = room.gameState;
  if (gs.phase !== 'faceoff') return;
  clearGameTimers(room);
  gs.timers = [];
  const fo = gs.faceoff;
  fo.finalists.forEach((e) => {
    const a = fo.answers[e];
    if (a && a.points) gs.scores[e] = (gs.scores[e] || 0) + a.points;
  });
  io.to(room.code).emit('trivia:faceoffResult', { index: fo.qIndex, answers: fo.answers, scores: gs.scores });
  fo.qIndex++;
  gs.timers.push(setTimeout(() => openTriviaFaceoffQuestion(room), TR_FACEOFF_REVEAL_MS));
}

function finishTrivia(room) {
  const gs = room.gameState;
  gs.phase = 'done';
  gs.resolved = true;
  clearGameTimers(room);
  const ranked = gs.entities.slice().sort((a, b) => (gs.scores[b] || 0) - (gs.scores[a] || 0));
  const topScore = gs.scores[ranked[0]] || 0;
  const tie = ranked.length > 1 && (gs.scores[ranked[1]] || 0) === topScore;
  io.to(room.code).emit('trivia:matchResult', { winner: tie ? null : ranked[0], scores: gs.scores, tie });
}

server.listen(PORT, () => {
  console.log(`Motion Party server running on http://localhost:${PORT}`);
  console.log(`  Host screen:  http://localhost:${PORT}/host/`);
  console.log(`  Player join:  http://localhost:${PORT}/player/`);
});
