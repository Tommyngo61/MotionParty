# Motion Party 🎮

A Mario-Party-style party game where the TV is the "big screen" host and everyone's
phone is a motion controller. No app to install — players scan a QR code, pick a
name and an animal avatar, and play by swinging/aiming their phone.

## Games

Motion Tennis and 1-2-3 Shoot! are 1v1 games - play a **Single Play** match between
2 people, or run a **Tournament Bracket** (single-elimination, 3+ players). Stay on
Track and Tilt Maze are **Free-for-all** races - any number of players compete at
once, no bracket needed. See [Modes](#modes) below for how that choice works on the
host screen.

- **Motion Tennis** — Wii-Sports-style angled court. Your character auto-moves
  into position; you just swing your phone like a racket when the ball is in range.
  Tilt your phone left/right at the moment of the swing to steer the return.
- **1-2-3 Shoot!** — Quick-draw duel. Both avatars walk three paces apart, turn to
  face off, then a light goes red → orange → **green** at a random moment. Draw
  (flick your phone up) after green — draw early and you lose instantly. Both
  reaction times are shown, and the loser goes down in a cartoon "BANG!".
- **Stay on Track** — Free-for-all balance race across 4 increasingly-tight courses,
  each rendered on your own screen (not the TV). Hold your phone flat when the race
  starts - the race won't begin until it's level, so nobody gets a head start.
  Tilt the phone forward/back to speed up or reverse, and left/right to steer a ball
  along a winding path, which rolls with its own momentum rather than snapping to a
  stop — full manual control, so you can slow down or backtrack to line up a tricky
  turn. Drift off the edge and the ball visibly falls off the track, then restarts
  *that* track from the beginning — no penalty beyond lost time. The first 3 tracks
  scroll forward to a finish line; **Track 4 is a loop** shown whole on screen, and
  you have to steer around it for 2 full laps to clear it. First to clear all 4
  tracks wins; the host screen shows every racer's live progress bar.
- **Tilt Maze** — Free-for-all race to roll an iron ball through a maze to the hole,
  on your own screen (not the TV), across an Easy maze then a Hard one. Hold your
  phone flat to start, then tilt it like a tray — the ball rolls with real momentum
  and slides along walls rather than clipping through them, and it can't fall off
  since the maze itself is the boundary. Mazes are procedurally generated (a
  spanning-tree "perfect maze" — exactly one path from start to the hole, so every
  branch off it is a genuine dead end) but seeded, so every racer gets an identical
  layout. First to solve both mazes wins; the host screen shows every racer's live
  progress bar.
- **Fruit Ninja** — on the menu as "coming soon", intentionally skipped for now.

All character art is hand-drawn as plain SVG shapes in `public/shared/avatars.js` —
no AI-generated images.

## Modes

The host's player-select screen shows a short **how-to-play** description for
whichever game is selected, so the room can read the controls off the TV before
anyone starts.

- **Single Play** (Tennis / 1-2-3 Shoot! only) — the classic 1v1 match. Pick exactly
  2 players.
- **Tournament Bracket** (Tennis / 1-2-3 Shoot! only) — pick 3+ players and hit
  *Start Tournament*. Seeding is randomized (with a brief shuffle-reveal animation
  showing names dropping into the bracket), and if the player count isn't a power of
  2 the extra slot(s) get a single, evenly-distributed bye each so nobody ever faces
  an empty match. The host then plays out one match at a time - each one is a
  completely normal 1v1 match, just with "Advancing the bracket…" instead of "Play
  Again" when it ends - until a single champion remains. A drawn/no-show 1-2-3
  Shoot! match (nobody drew) just replays instead of advancing, since a bracket
  match can't end in a tie.
- **Free-for-all** (Stay on Track / Tilt Maze only) — no mode choice; pick any
  number of joined players (no upper limit) and everyone races at once.

**End Game**, visible in the top corner during any live match (and as *End
Tournament* on the bracket screen), immediately aborts the current game or
tournament and returns everyone to the menu - with a confirmation prompt, since
it's the one action here that throws away an in-progress match.

## Running it

**Quick start** — two terminals, no accounts, no LAN IP hunting:

```
npm install
npm start
```

```
npm run tunnel
```

`npm run tunnel` runs a Cloudflare quick Tunnel via the `cloudflared` dev dependency
— nothing extra to install, no login. It prints one
`https://<random-name>.trycloudflare.com` URL; use that same URL for everything:

- **`<url>/host/`** on the TV / big screen — creates a room, shows a QR code and
  4-letter join code, and is the game menu (the host itself is never a player).
- **`<url>/player/`** on phones (normally by scanning the QR code) — enter a name,
  pick an animal, tap Join.
- On the host, tap a game tile, choose a mode if there's one to choose (see
  [Modes](#modes)), pick joined players, and hit Start.

This matters because phone browsers only expose `DeviceMotionEvent`/
`DeviceOrientationEvent` — the tilt/swing controls — on a **secure context**
(`https://` or `localhost`). Plain `http://192.168.x.x:3000` loads the pages fine,
but the motion-permission prompt silently fails on real phones. The tunnel URL is
HTTPS by default, so it sidesteps that entirely and this is the fastest path from
clone to playable on real hardware.

Two things worth knowing:
- The tunnel URL changes every time you restart it, and Cloudflare gives no uptime
  guarantee for these "quick" tunnels — fine for playtesting, not a standing address.
- The server itself always runs on port 3000 regardless of the tunnel (override with
  `PORT=...`); the tunnel just forwards HTTPS traffic to it, so the host screen can
  still use plain `http://<lan-ip>:3000/host/` on your LAN if you'd rather skip the
  tunnel for the TV and only use it for phones.

**Alternative — ngrok** (needs a free account + authtoken configured first):

```
npx ngrok http 3000
```

Use the printed `https://...ngrok...` URL the same way. For a real deployment, put
the server behind any HTTPS reverse proxy instead of a quick tunnel.

On iOS, the motion-permission prompt only appears in response to a tap — that's why
the Join button itself requests permission.

## Project layout

```
server/
  index.js     Express static hosting + Socket.IO: rooms, lobby, relay, and the
               server-authoritative 1-2-3 Shoot / Stay on Track / Tilt Maze timing
               state machines.
  rooms.js     In-memory room/player store.

public/
  index.html         Landing page (links to /host/ and /player/).
  shared/avatars.js  Hand-coded SVG animal avatars, shared by both apps.
  shared/tracks.js   Procedural course definitions for Stay on Track, shared by both apps.
  shared/mazes.js    Seeded procedural maze generation for Tilt Maze, shared by both apps.

  host/              The big-screen app: lobby, QR code, game menu, player-select,
    host.js          and game rendering (Canvas 2D for Tennis, DOM/CSS for the rest).
    games/tennis.js
    games/quickshoot.js
    games/stayontrack.js
    games/tiltmaze.js

  player/            The phone app: join flow, avatar picker, and per-game
    player.js        controllers that read devicemotion/deviceorientation and
    controllers/tennis.js      send input to the host/server over the socket.
    controllers/quickshoot.js
    controllers/stayontrack.js
    controllers/tiltmaze.js
```

### How input flows

Phones never run the *scoring* logic — but Stay on Track is the one game where the
phone does run its own real-time physics (see below), since routing raw tilt through
the server to a host-rendered ball would add enough latency to make balance
control feel unresponsive.

- **Tennis**: the host browser is authoritative — it runs the whole rally
  simulation (ball flight, auto-positioning, scoring) in a `requestAnimationFrame`
  loop. Phones detect a swing (an accelerometer peak) and send a `player:input`
  event; the server relays it to the host only, and the host decides whether the
  swing landed in the hit window.
- **1-2-3 Shoot**: the *server* is authoritative for timing fairness — it runs the
  walk → ready → set → fire sequence and broadcasts each phase to the host and both
  phones at the same instant. Phones compute their own reaction time locally and
  report it back; the server picks the winner and every screen renders the result.
- **Stay on Track**: each phone runs its own local tilt-maze simulation and canvas
  render (reading `deviceorientation` continuously, not just single gestures) so
  steering feels instant. The server only broadcasts the synchronized `GO` signal
  after the walk-in countdown and arbitrates the win (whichever phone's
  `stayontrack:finish` arrives first, once all 4 tracks are cleared). Phones relay
  their live track/progress/attempt count over the existing generic `player:input`
  channel purely so the host can render a live progress bar — the host never
  simulates this game.
- **Tilt Maze**: same shape as Stay on Track - each phone runs its own local ball
  physics and wall-collision simulation, the server just broadcasts `GO` after the
  countdown and arbitrates the win off the first `tiltmaze:finish`, and phones relay
  live maze-index/progress over the generic `player:input` channel for the host's
  progress bar.

Stay on Track and Tilt Maze are free-for-all: any number of racers can be in
`room.matchPlayers`, "winner" is just whoever's `*:finish` the server sees first,
and there's no `loserId` in the result event - it wouldn't mean anything with more
than one non-winner.

**Tournament brackets need zero server changes.** The server only ever runs one
plain 2-player match at a time (`host:setMatchPlayers` + `host:startMatch`), exactly
like Single Play - it has no concept of a "tournament". All bracket state (rounds,
byes, whose turn is next) lives entirely in `public/host/host.js`, in the browser
tab. It plays a bracket by calling the normal match-start flow once per pairing and
listening for that specific match to end, so `tennis.js`/`quickshoot.js`'s actual
gameplay simulation is completely unmodified for bracket play - only their
result-screen code branches on a `bracketMode` flag (see below).

## Tuning

Motion thresholds (swing sensitivity, draw sensitivity) are simple constants at the
top of `public/player/controllers/tennis.js` and `quickshoot.js` — different phones
report different accelerometer scales, so nudge `SWING_THRESHOLD` /
`READY_THRESHOLD` / `DRAW_THRESHOLD` if a game feels too twitchy or too unresponsive
on your hardware. 1-2-3 Shoot also won't advance past the walk-apart phase until both
phones report holding the aim-down/holster pose (`POINT_DOWN_Y`, checked against
`accelerationIncludingGravity.y` — around `-9.8` when a phone is held vertically with
its top pointed at the ground, `+9.8` held upright) for `POINT_DOWN_HOLD_MS`; if a
phone never confirms (e.g. motion permission denied) the round force-starts after
`QS_AIM_FALLBACK` in `server/index.js` so the game can't hang forever.

Stay on Track's difficulty lives in `public/shared/tracks.js` as `width` (how much
drift is forgiven), `speed` (the *max* forward rate a full forward tilt can reach —
players control their own pace, so this is a ceiling, not an autopilot), and
`wobble` (sine terms that build the winding centerline the player has to follow).
`type: 'loop'` plus a `laps` count turns a track into a closed circuit shown whole
on screen instead of one that scrolls to a finish line; the same `wobble` formula
just gets applied per-lap (see `centerlineX` in the same file). It's tuned so each
track's *worst-case* required steering speed stays comfortably under what the ball's
`STEER_ACCEL` constant (in `public/player/controllers/stayontrack.js`) can actually
deliver — i.e. every track should be beatable with patient, accurate tilting, just
progressively less forgiving of mistakes. If you widen the gap between the two
you risk a track that's steering-rate-impossible rather than merely hard; if you
tune it, sanity-check the new peak wobble slope against `STEER_ACCEL` the same way.

Steering also has `STEER_DAMPING` (kept low on purpose) and `MAX_STEER_VEL` — low
damping plus a velocity cap is what makes the ball carry its own momentum like it
has real weight, instead of stopping the instant the phone goes level; raise
`STEER_DAMPING` if it feels too slippery, but keep it well under `STEER_ACCEL`'s
useful range or it goes back to feeling snappy instead of rolling.

Forward/back speed control has its own constants alongside those: `MAX_PITCH_DEG`
(how far from level counts as full speed), `PITCH_DEADZONE_DEG` (how much
accidental pitch near level is ignored), and `PITCH_SIGN` (flip this to `1` if
forward/back tilt feels inverted on your device). `START_FLAT_DEG` is separate -
it's how level (on both axes) a phone must be before the race is allowed to start,
so nobody gets a head start from already being tilted when the countdown ends.

Tilt Maze's difficulty lives in `public/shared/mazes.js` as `cols`/`rows` (grid
size - bigger means more turns and longer dead ends) and `seed` (which layout the
recursive-backtracker generator produces for that grid size; same seed always
gives the same maze). Its ball physics (`ACCEL`, `DAMPING`, `MAX_VEL`) and the
flat-start gate (`START_FLAT_DEG`) live in `public/player/controllers/tiltmaze.js`
and follow the exact same "rolling ball" tuning philosophy as Stay on Track's
steering, above. `BALL_R` and `WALL_HALF` (both in maze cell-units) size the
collision circle and wall thickness for the slide-off-walls physics - if you
shrink the maze's corridor width relative to those, tight turns can pinch the ball
to a stop instead of letting it slide through.

## Adding the next game

Each game is a self-contained pair of modules with the same shape:

- `public/host/games/<name>.js` → registers `window.MP_GAMES.<name> = { start(root, {socket, roomCode, players, onExit, bracketMode, onMatchResult}) }`, returns `{ stop() }`.
- `public/player/controllers/<name>.js` → registers `window.MP_CONTROLLERS.<name> = { start(root, {socket, roomCode, playerId, me, opponent, opponents}) }`, returns `{ stop() }`.

`opponent` is the first other player (what 1v1 games use); `opponents` is *every*
other player, for free-for-all games that need to message about more than one
rival. `bracketMode`/`onMatchResult` only matter for a 1v1 game that wants
tournament support (see [Modes](#modes)): when `bracketMode` is true, show a plain
"so-and-so wins!" result instead of your normal Back to Menu/Play Again buttons,
and call `onMatchResult(winnerId)` after a couple of seconds - or
`onMatchResult(null)` if the match ended in a draw/no result, which just replays
the same pairing instead of advancing. Add `GAME_INFO[<name>]` in
`public/host/host.js` for the tile's how-to-play blurb, plus either
`supportsBracket: true` (1v1 games) or `freeForAll: true` there.

Then add a tile to the menu grid in `public/host/index.html` and wire up any
server-side relay/timing it needs in `server/index.js` (most games can just use the
existing generic `player:input` → `input:relay` relay and won't need server changes
at all - and free-for-all/bracket games need *no* server changes beyond the normal
match-result event, since brackets are host-side only, as above).
