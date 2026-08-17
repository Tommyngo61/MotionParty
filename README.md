# Motion Party 🎮

A Mario-Party-style party game where the TV is the "big screen" host and everyone's
phone is a motion controller. No app to install — players scan a QR code, pick a
name and an animal avatar, and play by swinging/aiming their phone.

## Games

Motion Tennis and 1-2-3 Shoot! are 1v1 games - play a **Single Play** match between
2 people, or run a **Tournament Bracket** (single-elimination, 3+ players). Stay on
Track, Tilt Maze, and Color Match Relay are **Free-for-all** - any number of players
compete at once, no bracket needed. Trivia Throwdown is free-for-all too, but can
also be split into **Teams** instead. See [Modes](#modes) below for how these
choices work on the host screen.

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
  each rendered on your own screen (not the TV). Pick a difficulty on the select
  screen (**Easy/Medium/Hard/Impossible**) - it scales how narrow the courses get,
  how fast they move, and how many caution-striped **obstacles** get dropped onto
  them; every race also **rerolls the actual layout** (wobble shape and obstacle
  placement) from a fresh random seed, so it's never the same course twice, even at
  the same difficulty. Hold your phone flat when the race starts - the race won't
  begin until it's level, so nobody gets a head start. You move forward on your
  own; **tilt left/right is the only control**, steering a ball along a winding
  path that rolls with its own momentum rather than snapping to a stop, dodging
  obstacles as they come up. The whole track is shown at once, so the ball visibly
  drives from the bottom of the screen up to the top as you make progress, instead
  of the track scrolling underneath a ball that stays put. Drift off the edge (or
  clip an obstacle) and the ball visibly falls off the track, then restarts *that*
  track from the beginning — no penalty beyond lost time. The first 3 tracks end
  when the ball reaches the top of the screen, then the next track picks up right
  away; **Track 4 is a loop** shown whole on screen, and you have to steer around
  it for 2 full laps to clear it. First to clear all 4 tracks wins; the host screen
  shows every racer's live progress bar.
- **Tilt Maze** — Free-for-all race to roll an iron ball through a maze to the hole,
  on your own screen (not the TV), across an Easy maze then a Hard one. Pick a
  difficulty on the select screen - it scales how big both mazes are (a 4×4 warm-up
  at Easy up to a 9×9 monster at Impossible) and how many **hazard holes** get
  scattered through them; every race generates a **brand-new maze layout** from a
  fresh random seed, so it's never the same maze twice. Hold your phone flat to
  start, then tilt it like a tray — the ball rolls with real momentum and slides
  along walls rather than clipping through them, and it can't fall off the outer
  edge since the maze itself is the boundary — but drifting into a hazard hole
  (shown as a dashed red pit, distinct from the dark finish hole) sends the ball
  back to that maze's start and counts as another attempt, same idea as falling
  off in Stay on Track. Mazes are procedurally generated (a spanning-tree "perfect
  maze" — exactly one path from start to the hole, so every branch off it is a
  genuine dead end) but seeded, so every racer in the same race gets an identical
  layout. First to solve both mazes wins; the host screen shows every racer's live
  progress bar and attempt count.
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
- **Trivia Throwdown** — Free-for-all (or Teams), the one game that isn't about
  motion at all - phones are a touchscreen game-show buzzer/controller instead.
  Choose **Free-for-all** or **Teams** (exactly two teams, drag players onto
  Team A/B on the select screen) before starting. The TV shows a 5×5 board - 5
  categories × $100-$500, difficulty scaling with value - and whoever's turn it
  is picks a category/value on their *own* phone; the TV reveals the clue and
  every phone shows a big **BUZZ** button. First tap wins the buzz (server
  arrival order - simplest fair "timestamp comparison" for a room full of
  independently-clocked phones); everyone answers **out loud**, and the host
  taps ✅/❌ on the TV to judge it, the same way an actual game-show host would
  - free-text grading isn't reliable enough for open trivia. Correct awards the
  tile's value and clears it; wrong can optionally deduct that value (a toggle
  at setup) and reopens the buzz to whoever hasn't already missed this clue. One
  random tile every board is a hidden **Daily Double** - the picking player/team
  wagers any amount up to their current score *before* the clue shows, then
  answers alone (no buzz race); correct doubles the wager and adds it, wrong
  subtracts it. Once the board is cleared, the top 2 scorers move on to a
  **Face-Off**: five survey-style questions (Family-Feud style - "Name
  something you'd find in a kitchen") where both finalists type a free-text
  guess, matched against a pre-set weighted answer list. Whoever has the higher
  *overall* score once the Face-Off ends wins the game.
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
- **Free-for-all** (Stay on Track / Tilt Maze / Color Match Relay / Trivia
  Throwdown only) — no mode choice; pick any number of joined players (no upper
  limit) and everyone competes at once.
- **Difficulty** (Stay on Track / Tilt Maze only) — Easy/Medium/Hard/Impossible
  buttons appear on the select screen; the host's choice is sent to the server
  along with Start Game, which scales the course/maze size and obstacle/hazard
  count and rolls a fresh random seed. See [Randomization &
  difficulty](#randomization--difficulty) below for what each tier actually does.
- **Teams** (Trivia Throwdown only) — a Free-for-all/Teams toggle appears
  alongside a "Deduct points for wrong answers" checkbox. Switching to Teams
  gives every already-selected player card a Team A/B chip (click to flip it);
  Start Game is disabled until both teams have at least one member. Exactly two
  teams, never more - Trivia's Face-Off round needs "the top 2 scorers" to mean
  something concrete, and two teams maps onto that directly.

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

### ⚠️ Motion sensors (and the camera) need HTTPS

This matters because phone browsers only expose `DeviceMotionEvent`/
`DeviceOrientationEvent` (the tilt/swing controls) and `getUserMedia` (Color Match
Relay's camera) on a **secure context** (`https://` or `localhost`). Plain
`http://192.168.x.x:3000` loads the pages fine, but the motion/camera permission
prompts silently fail on real phones. The tunnel URL is HTTPS by default, so it
sidesteps that entirely and this is the fastest path from clone to playable on real
hardware.

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
               Color Match Relay / Trivia Throwdown timing + game-logic state
               machines.
  rooms.js     In-memory room/player store.

public/
  index.html         Landing page (links to /host/ and /player/).
  shared/avatars.js  Hand-coded SVG animal avatars, shared by both apps.
  shared/prng.js     Tiny seeded PRNG (mulberry32) - a plain browser <script> AND
                     a Node require() (server/index.js needs it too, for Trivia
                     Throwdown - see below), so it can't assume window or module.
  shared/tracks.js   Seeded, difficulty-scaled Stay on Track course generator, shared by both apps.
  shared/mazes.js    Seeded, difficulty-scaled Tilt Maze generator, shared by both apps.
  shared/colors.js   Color-distance math for Color Match Relay, shared by both apps.
  shared/trivia.js   Trivia Throwdown's category/clue/Face-Off content pool and
                     board generator - same dual browser-script-or-Node-require
                     trick as prng.js, but only actually require()'d by the
                     server; see Trivia Throwdown below for why.
  shared/feedback.js Synthesized (Web Audio) sound effects + a vibrate() wrapper,
                     shared by both apps - see Sound, vibration & tutorials below.
  shared/tutorial.js The inline autoplaying select-screen demo + window.MP_TUTORIALS
                     registry, shown on the host only.

  host/              The big-screen app: lobby, QR code, game menu, player-select,
    host.js          bracket orchestration, and game rendering (Canvas 2D for
                     Tennis, DOM/CSS for the rest).
    games/tennis.js
    games/quickshoot.js
    games/stayontrack.js
    games/tiltmaze.js
    games/colormatch.js
    games/trivia.js
    tutorials/       Captured gameplay GIFs used by MP_TUTORIALS entries that have
                     real footage (see Sound, vibration & tutorials below).

  player/            The phone app: join flow, avatar picker, and per-game
    player.js        controllers that read devicemotion/deviceorientation/camera
    controllers/tennis.js      and send input to the host/server over the socket.
    controllers/quickshoot.js
    controllers/stayontrack.js
    controllers/tiltmaze.js
    controllers/colormatch.js
    controllers/trivia.js
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
- **1-2-3 Shoot**: the *server* is authoritative for timing fairness. The walk/turn
  animation plays out on its own timer while, in parallel, each phone watches for
  the aim-down/holster pose (`accelerationIncludingGravity`, held for a beat) and
  reports it; once both the animation has finished *and* every player in the match
  has aimed (or a grace-period fallback expires, in case a phone's motion
  permission was denied), the server starts a fixed `QS_COUNTDOWN_MS` countdown -
  not random - and broadcasts each phase to the host and every phone at the same
  instant. Phones compute their own reaction time locally and report it back; the
  server picks the winner and every screen renders the result.
- **Stay on Track**: each phone runs its own local tilt-maze simulation and canvas
  render (reading `deviceorientation` continuously, not just single gestures) so
  steering feels instant. The server picks the difficulty-scaled random `seed` at
  `host:startMatch` and includes it in the `game:start` broadcast; the host and
  every phone then independently call `MP_generateTracks(seed, difficulty)` and get
  the identical course layout without the server ever shipping the course data
  itself. The server only broadcasts the synchronized `GO` signal after the walk-in
  countdown and arbitrates the win (whichever phone's `stayontrack:finish` arrives
  first, once all 4 tracks are cleared). Phones relay their live
  track/progress/attempt count over the existing generic `player:input` channel
  purely so the host can render a live progress bar — the host never simulates
  this game.
- **Tilt Maze**: same shape as Stay on Track - each phone runs its own local ball
  physics and wall-collision simulation, `game:start`'s `seed`/`difficulty` drive
  the same independent-but-identical `MP_generateMazes` call on host and phones,
  the server just broadcasts `GO` after the countdown and arbitrates the win off
  the first `tiltmaze:finish`, and phones relay live maze-index/progress/attempt
  count over the generic `player:input` channel for the host's progress bar.
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
- **Trivia Throwdown**: the *server* is authoritative for essentially everything
  - board content, whose turn it is, the buzz race, and Face-Off answer
  grading - since unlike the other games there's no physics to run locally, just
  turn-taking and judgment calls a room full of independent phones can't be
  trusted to agree on by themselves. It's also the one game with its own
  dedicated `trivia:*` socket events instead of the generic `player:input`
  relay, because there's real per-message validation to do (is it actually your
  turn to pick, did you already whiff this clue, is your Face-Off answer even
  from a finalist) that a blind relay can't provide. See [Trivia
  Throwdown](#trivia-throwdown) below for the full picture.

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

## Randomization & difficulty

Stay on Track and Tilt Maze don't ship a fixed set of courses - `public/shared/
tracks.js` and `mazes.js` each export a `generate(seed, difficulty)` function
instead of a static array, and the host and every phone independently call it
with the same server-issued `seed`/`difficulty` (from the `game:start` broadcast)
to arrive at an identical layout without the server shipping the layout itself.
A fresh `seed` is rolled on every `host:startMatch` (including Play Again), so
races are never the same course twice - only the difficulty tier is sticky across
Play Again, since it comes from the host's own `pendingDifficulty` state, not the
server.

Both generators are keyed on the same four tiers - `easy` / `medium` / `hard` /
`impossible` - validated server-side against `DIFFICULTIES` in `server/index.js`
(an unrecognized or missing value falls back to `medium`):

- **Stay on Track** scales `width` (narrower = less room for error), `speed`, and
  wobble `amp`litude per tier, still ramping harder across the 4 tracks within one
  race the way the old fixed set did. From Medium up, each track also gets
  `obstacleCount` caution-striped **obstacles** - a `{ at, span, x, width }` region
  (progress-range × lateral-range) that acts exactly like the track edge: drift
  into one and you fall off. They're generated hugging one side of the track so a
  passable gap always remains on the other side; `public/player/controllers/
  stayontrack.js`'s `drawObstacles` renders them and the same at/x/width math
  gates the collision check in `update()`, so what you see is what you'll hit.
- **Tilt Maze** scales both mazes' `cols`/`rows` together per tier (a 4×4 warm-up
  up to a 9×9 monster at Impossible) and places `hazards` hole-shaped hazards per
  maze from Medium up, kept at least 1.5 cells from both the start and finish so
  nobody can fall in immediately or lose the race to a hazard sitting on the goal.
  Falling into one (`HAZARD_R` capture radius in `public/player/controllers/
  tiltmaze.js`) pauses briefly, respawns the ball at that maze's start, and
  increments the same `attempts` counter Stay on Track uses - relayed to the host
  the same way, so its progress bar can show "Attempt 2" etc for either game.

## Sound, vibration & tutorials

Every game gets three layers of feedback beyond its visuals:

- **Sound** — `public/shared/feedback.js` exposes `window.MP_Feedback`, a small
  Web Audio wrapper (`tone`/`noise`/`sequence`) plus a set of named presets
  (`tick`, `go`, `ready`, `fire`, `swing`, `hit`, `point`, `success`, `fall`,
  `finish`, `win`, `lose`, `error`) that game code calls as `MP_Feedback.play('win')`
  at the moment something happens. Everything is synthesized in-browser -
  there are no audio files to download, license, or ship, the same "nothing
  pre-made" approach the project already takes with hand-drawn SVG avatars
  instead of generated art. Because browsers block audio until a real user
  gesture, `MP_Feedback.unlock()` is called once at the first tap on each app -
  the player's Join button (`public/player/player.js`) and the host's first
  game-tile click (`public/host/host.js`).
- **Vibration** — a plain `navigator.vibrate(...)` wrapper on the *player* side
  only (the host is a TV, it can't vibrate). Existing per-game vibration calls
  are unchanged; sound was layered in alongside them at the same moments rather
  than routed through a shared helper, so each game's vibration patterns stay
  easy to find next to the gameplay code that triggers them.
- **Tutorials** — `public/shared/tutorial.js` exposes `window.MP_renderTutorial`
  and a `window.MP_TUTORIALS` registry (same shape as `MP_GAMES`/`MP_CONTROLLERS`).
  The host's player-select screen renders whichever game is selected into
  `#select-stage` immediately and autoplays it - no button, no modal - via
  `openSelectScreen` in `host.js`, which also stops the previous one whenever
  navigation leaves that screen.
  - **Preferred: real captured footage.** `public/host/tutorials/<name>.gif` is an
    actual screen-recording of a real round (host-screen view) played back with a
    plain `<img>` - GIFs autoplay/loop with no JS. `quickshoot.js`'s `render()` is
    the reference example. These were captured by scripting an actual match end to
    end (two simulated "phones" via synthetic `devicemotion`/`deviceorientation`
    events) and recording the host tab. The one real trap: the recorder only
    captures a frame on an actual `computer` click/type/key/drag action, *not* on
    screenshots or JS execution, and the game's own timers keep running in real
    time regardless of how long each recording step takes - so a slow step can let
    the round finish before the next scripted action lands. Polling actual page
    state (e.g. the caption text) before each action, instead of guessing fixed
    delays, is what makes this reliable; widening a game's result-grace timing
    constant temporarily (revert it before committing!) buys extra margin on
    tightly-timed games like Quick Shoot's post-FIRE window.
  - **Fallback: a small looping CSS animation** for any game without captured
    footage yet - a `.mpt-phone` glyph other games reuse, rotated/translated to
    mime the actual motion. `tennis.js`/`stayontrack.js`/`tiltmaze.js`/
    `colormatch.js`'s `render()` functions are the reference examples for this
    style.

## Trivia Throwdown

The odd one out - a touchscreen game-show buzzer/board instead of a motion game -
so its architecture works differently enough from the other five to be worth
spelling out on its own.

**Content lives in `public/shared/trivia.js`** as a pool of 8 categories (5 clues
each, `$100`-`$500`) and 6 Family-Feud-style Face-Off prompts, all original
(nothing copied from any actual quiz show). `MP_generateTriviaBoard(seed)` picks 5
of the 8 categories and one hidden Daily Double tile; `MP_generateTriviaFaceoff(seed)`
picks 5 of the 6 Face-Off prompts. Unlike tracks.js/mazes.js, **only the server
calls these** - trivia.js is written to work as both a browser `<script>` and a
plain Node `require()` (see the `module.exports`/`window` branches at the bottom of
both it and prng.js) specifically so `server/index.js` can `require('../public/
shared/trivia.js')` directly and stay the single source of truth for what's on the
board. The host and phones never generate their own copy; the server pushes the
actual category names, clue text, and Face-Off prompts to them over `trivia:board`/
`trivia:clue`/`trivia:faceoffQuestion` as each one becomes relevant - closer to how
Color Match Relay's server-picked target color works than to Stay on Track/Tilt
Maze's "everyone regenerates the same thing locally" pattern, because the *A*
Daily Double's position needs to stay hidden from clients until it's picked, the
same way the target color needs to stay hidden until reveal.

**Turn order, buzzing, and judging** all live in `room.gameState` on the server
(`server/index.js`'s `startTrivia`/`triviaOpenClue`/etc). A "scorable entity" is a
playerId in Free-for-all mode or `'A'`/`'B'` in Teams mode (`triviaEntityFor()`
maps one to the other), so the exact same board/buzz/judge code path handles both
modes without a fork. Buzzing in is a plain race for server arrival order across
however many phones call `trivia:buzz` while the buzz window is open - simpler and
more tamper-resistant than trusting each phone's own clock, and "the same pattern
as a reaction-time buzzer" the spec asked for really just means *some* form of
server-side timestamp comparison, which arrival order already is. The host's ✅/❌
buttons (`trivia:judge`, host-only - checked against `room.hostSocketId`) are the
one place a human makes a correctness call instead of code, because grading
free-form spoken trivia answers isn't something free-text matching can do
reliably - unlike the Face-Off round below, where the answer space per question is
small and predictable enough that automatic matching actually works.

**Daily Double** is a single random tile (`gs.dailyDouble`, chosen once per board,
never sent to clients until the holder picks it) that swaps the normal buzz race
for a private wager: `trivia:wager` is bounded server-side to `0..thatEntity's
current score`, then the same `triviaOpenClue` path runs but auto-assigns the buzz
to the wagering entity instead of opening it up, and a wrong answer isn't
reopened to anyone else - one shot, like the real thing. Correct doubles the
wagered amount and adds it (`score += wager * 2`); wrong subtracts the wager
(`score -= wager`) - that's a deliberately more generous swing than a real Daily
Double's "just add/subtract the wager once," a per-this-project design choice, not
a bug.

**Face-Off** kicks off once every tile is cleared (`gs.cleared` all `true`): the
top 2 scorers by `entities.sort()` become the finalists, and `public/shared/
trivia.js`'s `matchAnswer()` normalizes each finalist's free-text guess
(lowercase, strip punctuation) and checks it against every answer slot's `accept`
list for either string containing the other - so "fridge" / "a fridge" /
"refrigerator" all land on the same slot. Both finalists (or anyone on a finalist
team) can submit once per question; matched points add to that finalist's score
independently, so both can score on the same question. The game's actual winner is
whoever has the higher *combined* score (board round + Face-Off) once all 5
questions resolve - not a separate Face-Off-only tally - since the Face-Off is
framed as the decider for the whole match, not a fresh mini-game.

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
`QS_AIM_FALLBACK` in `server/index.js` so the game can't hang forever. Once it does
advance, `QS_COUNTDOWN_MS` (also in `server/index.js`) is the fixed ready-to-fire
countdown - a deliberate replacement for what used to be a random delay, so the
suspense is "will you jump the gun" rather than "did you guess the timing right".

Stay on Track's canvas rendering (in `public/player/controllers/stayontrack.js`)
uses a racetrack look - striped grass background, gray asphalt, red/white curb
stripes (`drawGrass`/`strokeCurb`) - the ball itself is unchanged. Its difficulty
lives in `public/shared/tracks.js`'s `TIERS` table: `width` (how much drift is
forgiven - narrower per track within a race via `widthStep`, and per tier overall),
`speed` (the *max* forward rate a full forward tilt can reach - players control
their own pace, so this is a ceiling, not an autopilot, ramped by `speedStep`
across the 4 tracks), `amp`/`ampStep` (wobble amplitude, same ramp shape), and
`obstacleCount` (see [Randomization & difficulty](#randomization--difficulty)
above). `type: 'loop'` plus a `laps` count (always the last of the 4 generated
tracks) turns a track into a closed circuit shown whole on screen instead of one
that scrolls to a finish line; the same `wobble` formula just gets applied per-lap
(see `centerlineX` in the same file). It's tuned so each tier's *worst-case*
required steering speed stays comfortably under what the ball's `STEER_ACCEL`
constant (in `public/player/controllers/stayontrack.js`) can actually deliver —
i.e. every track should be beatable with patient, accurate tilting, just
progressively less forgiving of mistakes. If you widen the gap between a tier's
`amp`/`speed` and `STEER_ACCEL` you risk a track that's steering-rate-impossible
rather than merely hard; if you tune it, sanity-check the new peak wobble slope
against `STEER_ACCEL` the same way, across all four tiers.

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

Tilt Maze's difficulty lives in `public/shared/mazes.js`'s `TIERS` table as
`sizes` (the `[cols, rows]` pair for the Easy-named and Hard-named maze at that
tier - bigger means more turns and longer dead ends) and `hazards` (hole count,
see [Randomization & difficulty](#randomization--difficulty) above). The
recursive-backtracker generator itself is deterministic per seed (same seed
always gives the same maze) - `generateMazes` derives two distinct per-maze seeds
from the match seed so the pair is never accidentally identical. Ball physics
(`ACCEL`, `DAMPING`, `MAX_VEL`) and the flat-start gate (`START_FLAT_DEG`) live in
`public/player/controllers/tiltmaze.js` and follow the exact same "rolling ball"
tuning philosophy as Stay on Track's steering, above. `BALL_R` and `WALL_HALF`
(both in maze cell-units) size the collision circle and wall thickness for the
slide-off-walls physics - if you shrink the maze's corridor width relative to
those, tight turns can pinch the ball to a stop instead of letting it slide
through. `HAZARD_R` is the capture radius around a hazard hole (kept close to
`FINISH_R` so both feel equally "easy to fall into" from the same careless drift).

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

Trivia Throwdown's content (categories/clues/Face-Off prompts) lives in the
`CATEGORY_POOL`/`FACEOFF_POOL` arrays at the top of `public/shared/trivia.js` -
add entries there (each category needs exactly 5 clues, ordered easiest to
hardest) and they're automatically in the random-5-of-N rotation, no other code
changes needed. Its timing (`TR_COUNTDOWN_MS`, `TR_BUZZ_WINDOW_MS`,
`TR_WAGER_TIMEOUT_MS`, `TR_FACEOFF_ANSWER_MS`, etc) lives in `server/index.js`
next to the other games' constants. `TR_BUZZ_WINDOW_MS` (12s) is the one worth
tuning to the room - too short and slower typers/talkers never get a shot at
buzzing; too long and a stalled clue drags the pace down. Face-Off answer
matching (`matchAnswer()` in trivia.js) is intentionally forgiving (substring
match either direction) rather than exact - add more `accept` synonyms per
answer slot if real playtesting shows people phrasing a correct-in-spirit answer
in a way that isn't matching.

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

**Every new game is expected to also ship sound, vibration, and a tutorial** - see
[Sound, vibration & tutorials](#sound-vibration--tutorials) above:

- Call `window.MP_Feedback.play('<preset>')` (adding a new preset in
  `public/shared/feedback.js` if none of the existing ones fit) at the moments a
  player would expect a cue - start/go, success, failure, and match end at minimum.
  Add `navigator.vibrate(...)` alongside it on the player side wherever a phone
  should buzz; the host never vibrates.
- Register `window.MP_TUTORIALS.<name> = { emoji, title, render(stageEl) }` in the
  game's own `public/host/games/<name>.js` (next to its `MP_GAMES.<name>`
  registration) - `render` fills `stageEl` and returns a cleanup function, called
  automatically the moment the select screen opens for that game. Start with the
  CSS-demo style (see the existing games without footage) since it needs no
  assets; swap in real captured footage later the same way `quickshoot.js` does,
  following the capture process described above. A game with no `MP_TUTORIALS`
  entry just shows nothing in that spot, so it's easy to tell if one's missing.
