// Tiny seeded PRNG shared by anything that needs a deterministic random sequence
// from a numeric seed - e.g. generating a maze/track layout that the host and
// every player's phone can independently reproduce identically from the same
// server-issued seed, without the server having to ship the whole layout.
(function (global) {
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

  global.MP_mulberry32 = mulberry32;
})(window);
