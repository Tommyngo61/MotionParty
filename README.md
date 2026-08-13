# Motion Party 🎮

A Mario-Party-style party game where the TV is the "big screen" host and everyone's
phone is a motion controller. No app to install — players scan a QR code, pick a
name and an animal avatar, and play by swinging/aiming their phone.

## Games

- **Motion Tennis** — 1v1, Wii-Sports-style angled court. Your character auto-moves
  into position; you just swing your phone like a racket when the ball is in range.
  Tilt your phone left/right at the moment of the swing to steer the return.
- **1-2-3 Shoot!** — 1v1 quick-draw duel. Both avatars walk three paces apart, turn to
  face off, then a light goes red → orange → **green** at a random moment. Draw
  (flick your phone up) after green — draw early and you lose instantly. Both
  reaction times are shown, and the loser goes down in a cartoon "BANG!".
- **Fruit Ninja** — on the menu as "coming soon", intentionally skipped for now.

All character art is hand-drawn as plain SVG shapes in `public/shared/avatars.js` —
no AI-generated images.

## Running it

```
npm install
npm start
```

This starts one Express + Socket.IO server on port 3000 (override with `PORT=...`).

- Put **`http://<server-ip>:3000/host/`** on the TV / big screen. It creates a room,
  shows a QR code and 4-letter code, and is the game menu — the host itself is never
  a player.
- Players open **`http://<server-ip>:3000/player/`** (normally by scanning the QR
  code), enter a name, pick an animal, and tap Join.
- On the host, tap a game tile, pick exactly 2 joined players, and hit Start.

### ⚠️ Motion sensors need HTTPS

Phone browsers only expose `DeviceMotionEvent`/`DeviceOrientationEvent` on a
**secure context** — `https://` or `localhost`. Plain `http://192.168.x.x:3000` will
load the pages fine, but the gyro/accelerometer permission prompt will silently fail
on real phones. For LAN playtesting, put the server behind a quick HTTPS tunnel, e.g.:

```
npx ngrok http 3000
```

then use the printed `https://...ngrok...` URL on the phones (the host screen can
still use plain HTTP on your LAN if you prefer). For a real deployment, put this
behind any HTTPS reverse proxy.

On iOS, the motion-permission prompt only appears in response to a tap — that's why
the Join button itself requests permission.

## Project layout

```
server/
  index.js     Express static hosting + Socket.IO: rooms, lobby, relay, and the
               server-authoritative 1-2-3 Shoot timing state machine.
  rooms.js     In-memory room/player store.

public/
  index.html         Landing page (links to /host/ and /player/).
  shared/avatars.js  Hand-coded SVG animal avatars, shared by both apps.

  host/              The big-screen app: lobby, QR code, game menu, player-select,
    host.js          and game rendering (Canvas 2D for Tennis, DOM/CSS for Shoot).
    games/tennis.js
    games/quickshoot.js

  player/            The phone app: join flow, avatar picker, and per-game
    player.js        controllers that read devicemotion/deviceorientation and
    controllers/tennis.js      send discrete input events over the socket.
    controllers/quickshoot.js
```

### How input flows

Phones never run any game logic — they just detect a gesture (a swing peak, a
quick-draw flick, a too-early move) and emit a small event over Socket.IO.

- **Tennis**: the host browser is authoritative — it runs the whole rally
  simulation (ball flight, auto-positioning, scoring) in a `requestAnimationFrame`
  loop. Phones send `player:input` swing events, the server relays them to the host
  only, and the host decides whether the swing landed in the hit window.
- **1-2-3 Shoot**: the *server* is authoritative for timing fairness — it runs the
  walk → ready → set → fire sequence and broadcasts each phase to the host and both
  phones at the same instant. Phones compute their own reaction time locally and
  report it back; the server picks the winner and every screen renders the result.

## Tuning

Motion thresholds (swing sensitivity, draw sensitivity) are simple constants at the
top of `public/player/controllers/tennis.js` and `quickshoot.js` — different phones
report different accelerometer scales, so nudge `SWING_THRESHOLD` /
`READY_THRESHOLD` / `DRAW_THRESHOLD` if a game feels too twitchy or too unresponsive
on your hardware.

## Adding the next game

Each game is a self-contained pair of modules with the same shape:

- `public/host/games/<name>.js` → registers `window.MP_GAMES.<name> = { start(root, {socket, roomCode, players, onExit}) }`, returns `{ stop() }`.
- `public/player/controllers/<name>.js` → registers `window.MP_CONTROLLERS.<name> = { start(root, {socket, roomCode, playerId, me, opponent}) }`, returns `{ stop() }`.

Then add a tile to the menu grid in `public/host/index.html` and wire up any
server-side relay/timing it needs in `server/index.js` (most games can just use the
existing generic `player:input` → `input:relay` relay and won't need server changes
at all).
