// Tiny seeded PRNG shared by anything that needs a deterministic random sequence
// from a numeric seed - e.g. generating a maze/track/trivia-board layout that the
// server, the host, and every player's phone can independently reproduce
// identically from the same server-issued seed, without the server having to
// ship the whole layout. Works both as a browser <script> (attaches to window)
// and as a plain Node require() (server/index.js uses it for Trivia Throwdown),
// so it can't assume either module or window exists.
(function () {
  function mulberry32(seed) {
    let a = seed >>> 0;
    return function () {
      a |= 0;
      a = (a + 0x6d2b79f5) | 0;
      let t = Math.imul(a ^ (a >>> 15), 1 | a);
      t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
      return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
    };
  }

  if (typeof module !== 'undefined' && module.exports) module.exports = { MP_mulberry32: mulberry32 };
  if (typeof window !== 'undefined') window.MP_mulberry32 = mulberry32;
})();
