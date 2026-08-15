// Procedurally-defined "Stay on Track" courses, shared by the host (progress overview)
// and the player controller (the actual tilt-maze). Deterministic so every run is fair.
(function (global) {
  // Each wobble term adds amp*sin(2*pi*freq*progress + phase) to the centerline's x offset.
  // Difficulty scales mainly via narrower width (less room for error) plus a modest speed/
  // wobble increase - tuned so the required steering rate never outruns a phone's realistic
  // tilt-response, i.e. even the hardest track is meant to be beatable with patient, accurate
  // tilting, not a game of catching up to a target moving faster than you can react.
  // `speed` is the max forward rate (in track-fractions/sec) at full forward tilt -
  // it's a ceiling the player drives up to, not an autopilot. For `type: 'loop'`,
  // progress is counted in laps (0..laps) instead of 0..1, and the same wobble
  // formula is applied per-lap (progress passed to centerlineX wraps at 1 per lap).
  const TRACKS = [
    { name: 'Track 1', type: 'linear', width: 0.42, speed: 0.5, wobble: [{ amp: 0.26, freq: 0.8, phase: 0 }] },
    {
      name: 'Track 2', type: 'linear', width: 0.34, speed: 0.55,
      wobble: [{ amp: 0.3, freq: 1.1, phase: 0.6 }, { amp: 0.08, freq: 2.4, phase: 0 }],
    },
    {
      name: 'Track 3', type: 'linear', width: 0.26, speed: 0.6,
      wobble: [{ amp: 0.34, freq: 1.4, phase: 1.1 }, { amp: 0.1, freq: 3.0, phase: 2.0 }],
    },
    {
      name: 'Track 4', type: 'loop', width: 0.26, speed: 0.35, laps: 2,
      wobble: [
        { amp: 0.18, freq: 1, phase: 0.3 },
        { amp: 0.06, freq: 2, phase: 1.4 },
      ],
    },
  ];

  function centerlineX(trackIndex, progress) {
    const track = TRACKS[trackIndex];
    let x = 0;
    for (const w of track.wobble) x += w.amp * Math.sin(2 * Math.PI * w.freq * progress + w.phase);
    return Math.max(-0.85, Math.min(0.85, x));
  }

  global.MP_TRACKS = TRACKS;
  global.MP_trackCenterlineX = centerlineX;
})(window);
