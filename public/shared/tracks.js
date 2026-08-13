// Procedurally-defined "Stay on Track" courses, shared by the host (progress overview)
// and the player controller (the actual tilt-maze). Deterministic so every run is fair.
(function (global) {
  // Each wobble term adds amp*sin(2*pi*freq*progress + phase) to the centerline's x offset.
  // Difficulty scales mainly via narrower width (less room for error) plus a modest speed/
  // wobble increase - tuned so the required steering rate never outruns a phone's realistic
  // tilt-response, i.e. even the hardest track is meant to be beatable with patient, accurate
  // tilting, not a game of catching up to a target moving faster than you can react.
  const TRACKS = [
    { name: 'Track 1', width: 0.56, speed: 0.15, wobble: [{ amp: 0.26, freq: 0.8, phase: 0 }] },
    {
      name: 'Track 2', width: 0.44, speed: 0.17,
      wobble: [{ amp: 0.3, freq: 1.1, phase: 0.6 }, { amp: 0.08, freq: 2.4, phase: 0 }],
    },
    {
      name: 'Track 3', width: 0.34, speed: 0.19,
      wobble: [{ amp: 0.34, freq: 1.4, phase: 1.1 }, { amp: 0.1, freq: 3.0, phase: 2.0 }],
    },
    {
      name: 'Track 4', width: 0.3, speed: 0.2,
      wobble: [
        { amp: 0.3, freq: 1.5, phase: 0.3 },
        { amp: 0.1, freq: 3.0, phase: 1.4 },
        { amp: 0.04, freq: 4.5, phase: 0 },
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
