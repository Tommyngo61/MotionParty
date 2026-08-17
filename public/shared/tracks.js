// Procedurally-*generated* "Stay on Track" courses, shared by the host (progress
// overview) and the player controller (the actual ball physics/rendering).
// window.MP_generateTracks(seed, difficulty) is called independently by the host
// and every player's phone with the same server-issued seed, so everyone ends up
// with an identical layout without the server having to ship the whole thing.
(function (global) {
  const TRACK_COUNT = 4;

  // Each tier is a starting point for track 1; every subsequent track in the race
  // gets narrower / faster / wobblier per the *Step values, same "ramps up within
  // a run" shape the original fixed tracks had - the tier just moves the whole
  // curve harder or easier. Obstacles are new: `obstacleCount` dodgeable blockers
  // get placed on every track from Medium up, `impossible` adds one extra on the
  // race's last (loop) track same as the width/speed curve already singles it out.
  const TIERS = {
    easy: { width: 0.50, widthStep: 0.030, speed: 0.40, speedStep: 0.025, amp: 0.16, ampStep: 0.03, obstacleCount: 0 },
    medium: { width: 0.40, widthStep: 0.045, speed: 0.48, speedStep: 0.035, amp: 0.22, ampStep: 0.045, obstacleCount: 1 },
    hard: { width: 0.32, widthStep: 0.045, speed: 0.55, speedStep: 0.045, amp: 0.28, ampStep: 0.055, obstacleCount: 2 },
    impossible: { width: 0.25, widthStep: 0.040, speed: 0.62, speedStep: 0.05, amp: 0.32, ampStep: 0.06, obstacleCount: 2 },
  };
  // However narrow the track gets, an obstacle only ever eats up to this fraction
  // of it - the rest always stays open as a passable gap next to it.
  const OBSTACLE_MAX_FRAC = 0.42;
  const DIFFICULTIES = Object.keys(TIERS);

  // Width is *narrowed* by widthStep going into each later track, so higher-index
  // tracks are harder - clamp keeps the last (narrowest) track always passable.
  function widthFor(tier, i) { return Math.max(0.16, tier.width - tier.widthStep * i); }

  function generateTracks(seed, difficulty) {
    const tier = TIERS[difficulty] || TIERS.medium;
    const rand = global.MP_mulberry32(seed >>> 0);
    const tracks = [];
    for (let i = 0; i < TRACK_COUNT; i++) {
      const isLast = i === TRACK_COUNT - 1;
      const width = widthFor(tier, i);
      const speed = tier.speed + tier.speedStep * i;
      const amp = tier.amp + tier.ampStep * i;
      const wobble = [
        { amp, freq: 0.7 + rand() * 0.9, phase: rand() * Math.PI * 2 },
        { amp: amp * 0.32, freq: 2.1 + rand() * 1.4, phase: rand() * Math.PI * 2 },
      ];
      const obstacleCount = tier.obstacleCount + (isLast && tier.obstacleCount > 0 ? 1 : 0);
      const obstacles = [];
      for (let o = 0; o < obstacleCount; o++) {
        // Spread obstacles across the middle of the track (never right at the
        // start or the finish/lap-close), each hugging one side so a gap of at
        // least (1 - OBSTACLE_MAX_FRAC) of the track width always remains open
        // on the other side, however narrow the track itself has gotten.
        const at = (0.18 + (o + rand() * 0.7) / obstacleCount * 0.7) % 1;
        const obsWidth = Math.min(width * OBSTACLE_MAX_FRAC, 0.05 + rand() * 0.04);
        const side = rand() < 0.5 ? -1 : 1;
        const x = side * (width / 2 - obsWidth / 2 - 0.015);
        obstacles.push({ at, span: 0.05 + rand() * 0.025, x, width: obsWidth });
      }
      tracks.push({
        name: `Track ${i + 1}`,
        type: isLast ? 'loop' : 'linear',
        laps: isLast ? 2 : undefined,
        width, speed, wobble, obstacles,
      });
    }
    return tracks;
  }

  function centerlineX(track, progress) {
    let x = 0;
    for (const w of track.wobble) x += w.amp * Math.sin(2 * Math.PI * w.freq * progress + w.phase);
    return Math.max(-0.85, Math.min(0.85, x));
  }

  global.MP_DIFFICULTIES = global.MP_DIFFICULTIES || DIFFICULTIES;
  global.MP_generateTracks = generateTracks;
  global.MP_trackCenterlineX = centerlineX;
})(window);
