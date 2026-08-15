# Motion Party 🎮

A Mario-Party-style party game where the TV is the "big screen" host and everyone's
phone is a motion controller. No app to install — players scan a QR code, pick a
name and an animal avatar, and play by swinging/aiming their phone.

## Games

Motion Tennis and 1-2-3 Shoot! are 1v1 games - play a **Single Play** match between
2 people, or run a **Tournament Bracket** (single-elimination, 3+ players). Stay on
Track, Tilt Maze, and Color Match Relay are **Free-for-all** - any number of players
compete at once, no bracket needed. See [Modes](#modes) below for how that choice
works on the host screen.

- **Motion Tennis** — Wii-Sports-style angled court. Your character auto-moves
  into position; you just swing your phone like a racket when the ball is in range.
  Tilt your phone left/right at the moment of the swing to steer the return.
- **1-2-3 Shoot!** — Quick-draw duel. Point your phone straight down (like a
  holstered gun) - once every player's locked that stance, both avatars walk three
  paces apart, turn to face off, and a fixed 5-second countdown runs before the
  light goes **green**. Draw (flick your phone up) after green — draw early and
  you lose instantly. Both reaction times are shown, and the loser goes down in a
  cartoon "BANG!".
- **Stay on Track** — Free-for-all balance race across 4 increasingly-tight courses,
  each rendered on your own screen (not the TV). Hold your phone flat when the race
  starts - the race won't begin until it's level, so nobody gets a head start. You
  move forward on your own; **tilt left/right is the only control**, steering a ball
  along a winding path that rolls with its own momentum rather than snapping to a
  stop. The whole track is shown at once, so the ball visibly drives from the
  bottom of the screen up to the top as you make progress, instead of the track
  scrolling underneath a ball that stays put. Drift off the edge and the ball
  visibly falls off the track, then restarts *that* track from the beginning — no
  penalty beyond lost time. The first 3 tracks end when the ball reaches the top of
  the screen, then the next track picks up right away; **Track 4 is a loop** shown
  whole on screen, and you have to steer around it for 2 full laps to clear it.
  First to clear all 4 tracks wins; the host screen shows every racer's live
  progress bar.
- **Tilt Maze** — Free-for-all race to roll an iron ball through a maze to the hole,
  on your own screen (not the TV), across an Easy maze then a Hard one. Hold your
  phone flat to start, then tilt it like a tray — the ball rolls with real momentum
  and slides along walls rather than clipping through them, and it can't fall off
  since the maze itself is the boundary. Mazes are procedurally generated (a
  spanning-tree "perfect maze" — exactly one path from start to the hole, so every
  branch off it is a genuine dead end) but seeded, so every racer gets an identical
  layout. First to solve both mazes wins; the host screen shows every racer's live
  progress bar.
- **Color Match Relay** — Free-for-all. The TV flashes a target color; everyone
  grabs their phone, runs off to find something in the room that color, and
  photographs it within 5 seconds. Each photo is scored by averaging the pixels in
  a center sample square against the target (using a perceptually-weighted color
  distance, not naive RGB subtraction) and shown as a match percentage — first
  player to clear the match threshold wins the round outright; if nobody does, the
  closest attempt when time runs out wins instead. Needs camera access (prompted
  with a tap, the same as the motion-permission prompt — see below); if it's never
  granted, the round still runs and just times out for that player instead of
  hanging. The result screen shows everyone's captured swatch side by side, best
  match first, so you can see just how far off that "definitely maroon" sock really
  was.
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
- **Free-for-all** (Stay on Track / Tilt Maze / Color Match Relay only) — no mode
  choice; pick any number of joined players (no upper limit) and everyone
  competes at once.

**End Game**, visible in the top corner during any live match (and as *End
Tournament* on the bracket screen), immediately aborts the current game or
tournament and returns everyone to the menu - with a confirmation prompt, since
it's the one action here that throws away an in-progress match.

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
- On the host, tap a game tile, choose a mode if there's one to choose (see
  [Modes](#modes)), pick joined players, and hit Start.

### ⚠️ Motion sensors (and the camera) need HTTPS

Phone browsers only expose `DeviceMotionEvent`/`DeviceOrientationEvent` - and
`getUserMedia`, which Color Match Relay uses for the camera - on a **secure
context**: `https://` or `localhost`. Plain `http://192.168.x.x:3000` will load the
pages fine, but the gyro/accelerometer/camera permission prompts will silently fail
on real phones. For LAN playtesting, put the server behind a quick HTTPS tunnel.

**Option A — Cloudflare Tunnel** (no account needed):

```
winget install --id Cloudflare.cloudflared -e
cloudflared tunnel --url http://localhost:3000
```

This prints a random `https://<words>.trycloudflare.com` URL — use it on the
phones for `/player/` and on the TV for `/host/`. The URL changes every time you
restart the tunnel, and Cloudflare gives no uptime guarantee for these "quick"
tunnels, so it's meant for playtesting, not a standing address.

**Option B — ngrok** (needs a free account + authtoken configured first):

```
npx ngrok http 3000
```

Either way, use the printed `https://...` URL on the phones (the host screen can
still use plain HTTP on your LAN if you prefer). For a real deployment, put this
behind any HTTPS reverse proxy.

On iOS, the motion-permission prompt only appears in response to a tap — that's why
the Join button itself requests permission (`DeviceMotionEvent.requestPermission()`/
`DeviceOrientationEvent.requestPermission()`, see `MP_requestMotionPermission` in
`public/player/player.js`). Color Match Relay's camera access follows the same
rule and gets its own tap-triggered "Enable Camera" button for the same reason -
just calling `getUserMedia` on mount, without a preceding tap, doesn't reliably
prompt either.

## Project layout

```
server/
  index.js     Express static hosting + Socket.IO: rooms, lobby, relay, and the
               server-authoritative 1-2-3 Shoot / Stay on Track / Tilt Maze /
               Color Match Relay timing state machines.
  rooms.js     In-memory room/player store.

public/
  index.html         Landing page (links to /host/ and /player/).
  shared/avatars.js  Hand-coded SVG animal avatars, shared by both apps.
  shared/tracks.js   Procedural course definitions for Stay on Track, shared by both apps.
  shared/mazes.js    Seeded procedural maze generation for Tilt Maze, shared by both apps.
  shared/colors.js   Color-distance math for Color Match Relay, shared by both apps.

  host/              The big-screen app: lobby, QR code, game menu, player-select,
    host.js          bracket orchestration, and game rendering (Canvas 2D for
                     Tennis, DOM/CSS for the rest).
    games/tennis.js
    games/quickshoot.js
    games/stayontrack.js
    games/tiltmaze.js
    games/colormatch.js

  player/            The phone app: join flow, avatar picker, and per-game
    player.js        controllers that read devicemotion/deviceorientation/camera
    controllers/tennis.js      and send input to the host/server over the socket.
    controllers/quickshoot.js
    controllers/stayontrack.js
    controllers/tiltmaze.js
    controllers/colormatch.js
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
- **1-2-3 Shoot**: the *server* is authoritative for timing fairness — each phone
  detects its own "pointed straight down" ground-lock via `devicemotion` and reports
  it; once every player in the match has, the server runs a
  groundcheck → walk → countdown → fire sequence (the countdown is a fixed
  `QS_COUNTDOWN_MS`, not random) and broadcasts each phase to the host and every
  phone at the same instant. Phones compute their own reaction time locally and
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
- **Color Match Relay**: the *server* picks the target color (so it can't be
  spoofed) and is authoritative for winning - each phone samples its own captured
  photo and computes its own match distance locally (so there's no latency between
  taking the photo and seeing your own result), but only the server's
  `colormatch:submit` handler decides whether that distance actually clears the
  match threshold and, if so, declares that player the winner. Non-winning attempts
  are still relayed to the host over the generic `player:input` channel so it can
  show everyone's live "trying…" swatch, and are kept server-side as each player's
  best attempt in case the round times out with nobody clearing the threshold -
  the closest overall attempt wins instead.

Stay on Track, Tilt Maze, and Color Match Relay are free-for-all: any number of
racers can be in `room.matchPlayers`, "winner" is just whoever's server-arbitrated
win condition triggers first (or closest, on a Color Match Relay timeout), and
there's no `loserId` in any of their result events - it wouldn't mean anything with
more than one non-winner.

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
on your hardware. `quickshoot.js`'s `isGroundAligned` (the "pointed straight down"
ready check) reads the device's Y axis as the gravity-dominant one; if that reads
backwards on some phones, widen its tolerance rather than trying to flip a sign -
"vertical, pointing down" reads the same regardless of which way the phone is
rotated around that axis, so there's no sign to flip. `QS_COUNTDOWN_MS` (fixed
ready-to-fire countdown, replacing what used to be a random delay) lives in
`server/index.js`, since the server owns match timing.

Stay on Track's canvas rendering (in `public/player/controllers/stayontrack.js`)
uses a racetrack look - striped grass background, gray asphalt, red/white curb
stripes (`drawGrass`/`strokeCurb`) - the ball itself is unchanged. Its difficulty
lives in `public/shared/tracks.js` as `width` (how much drift is forgiven), `speed`
(the *max* forward rate a full forward tilt can reach —
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

Forward movement is automatic, driven entirely by each track's `speed` - tilt
left/right is the only input. `START_FLAT_DEG` gates the *start* of the race (both
axes must be level before the countdown-triggered `GO` actually starts the ball
moving), so nobody gets a head start from already being tilted when it ends; it's
unrelated to steering itself, which only reads gamma (left/right).

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

Color Match Relay's timing (`CM_COUNTDOWN_MS`, `CM_ROUND_MS`) and its target
palette (`CM_PALETTE`) live in `server/index.js`, since the server is what picks
the target color. `CM_MATCH_THRESHOLD` there is the "redmean" color-distance
(0..~764, see `public/shared/colors.js`) a captured photo must beat to count as a
match - **it's duplicated** as `MATCH_THRESHOLD` in
`public/player/controllers/colormatch.js` purely so the phone can show instant
local "match!"/"try again" feedback without waiting on a round-trip; the server's
copy is the one that's actually authoritative, so keep the two in sync if you
retune it. Raise the threshold if real phone cameras/lighting are making close
matches read as misses; lower it if matches feel too easy. `SAMPLE_FRAC` in the
same file controls how big a square (as a fraction of the shorter video
dimension) gets averaged into the sampled color - matches the `.cmc-reticle`
outline shown on the viewfinder, so change them together.

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
