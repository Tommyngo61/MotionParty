// Color matching helpers for Color Match Relay, shared by the host and player apps.
(function (global) {
  function hexToRgb(hex) {
    const clean = String(hex || '').replace('#', '');
    const full = clean.length === 3 ? clean.split('').map((c) => c + c).join('') : clean;
    const n = parseInt(full, 16) || 0;
    return { r: (n >> 16) & 255, g: (n >> 8) & 255, b: n & 255 };
  }

  function rgbToHex({ r, g, b }) {
    const clamp = (v) => Math.max(0, Math.min(255, Math.round(v)));
    return '#' + [r, g, b].map((v) => clamp(v).toString(16).padStart(2, '0')).join('');
  }

  // "redmean" weighted RGB distance - cheap (no color-space conversion needed) but a
  // noticeably closer match to human color perception than plain Euclidean RGB
  // distance, which is what makes it worth using over a straight sqrt(dr^2+dg^2+db^2).
  function colorDistance(c1, c2) {
    const rmean = (c1.r + c2.r) / 2;
    const dr = c1.r - c2.r, dg = c1.g - c2.g, db = c1.b - c2.b;
    return Math.sqrt((2 + rmean / 256) * dr * dr + 4 * dg * dg + (2 + (255 - rmean) / 256) * db * db);
  }

  const MAX_DISTANCE = colorDistance({ r: 0, g: 0, b: 0 }, { r: 255, g: 255, b: 255 });

  function similarityPct(distance) {
    return Math.max(0, Math.round(100 - (distance / MAX_DISTANCE) * 100));
  }

  global.MP_COLORS = { hexToRgb, rgbToHex, colorDistance, MAX_DISTANCE, similarityPct };
})(window);
