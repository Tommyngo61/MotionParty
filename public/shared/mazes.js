// Procedurally-*generated* "Tilt Maze" courses, shared by the host (progress
// overview) and the player controller (the actual tilt maze). Both the host and
// every player's phone call window.MP_generateMazes(seed, difficulty) with the
// same server-issued seed, so everyone ends up with an identical layout without
// the server having to ship the whole thing (same pattern as tracks.js).
(function (global) {
  // Recursive-backtracker: carves a spanning tree over the grid, so there's exactly
  // one path between any two cells - every branch that isn't on that path is a dead
  // end, and there are no loops. That's what makes it a proper "tilt maze".
  function generate(cols, rows, seed) {
    const rand = global.MP_mulberry32(seed);
    const cells = [];
    for (let y = 0; y < rows; y++) {
      for (let x = 0; x < cols; x++) {
        cells.push({ x, y, N: true, S: true, E: true, W: true, visited: false });
      }
    }
    const at = (x, y) => cells[y * cols + x];
    const stack = [];
    let current = at(0, 0);
    current.visited = true;
    let visitedCount = 1;
    const total = cols * rows;
    while (visitedCount < total) {
      const { x, y } = current;
      const options = [];
      if (y > 0 && !at(x, y - 1).visited) options.push(['N', 'S', at(x, y - 1)]);
      if (y < rows - 1 && !at(x, y + 1).visited) options.push(['S', 'N', at(x, y + 1)]);
      if (x > 0 && !at(x - 1, y).visited) options.push(['W', 'E', at(x - 1, y)]);
      if (x < cols - 1 && !at(x + 1, y).visited) options.push(['E', 'W', at(x + 1, y)]);
      if (options.length === 0) {
        current = stack.pop();
        continue;
      }
      const [dir, opp, next] = options[Math.floor(rand() * options.length)];
      current[dir] = false;
      next[opp] = false;
      next.visited = true;
      visitedCount++;
      stack.push(current);
      current = next;
    }
    return cells;
  }

  // Wall line segments in cell-unit coordinates, used for both collision and drawing.
  function wallSegments(cols, rows, cells) {
    const at = (x, y) => cells[y * cols + x];
    const segs = [];
    for (let y = 0; y < rows; y++) {
      for (let x = 0; x < cols; x++) {
        const c = at(x, y);
        if (c.N) segs.push([x, y, x + 1, y]);
        if (c.W) segs.push([x, y, x, y + 1]);
        if (y === rows - 1 && c.S) segs.push([x, y + 1, x + 1, y + 1]);
        if (x === cols - 1 && c.E) segs.push([x + 1, y, x + 1, y + 1]);
      }
    }
    return segs;
  }

  // Picks `count` cells to become hazard holes: falling into one restarts the
  // current maze attempt, same as drifting off a Stay on Track course. Kept away
  // from the start and finish cells so nobody can fall in before they've even
  // gotten moving, or lose the race to a hazard sitting right on the goal.
  function placeHazards(cells, count, seed, start, finish) {
    if (count <= 0) return [];
    const rand = global.MP_mulberry32(seed);
    const candidates = cells.filter((c) => {
      const dStart = Math.hypot(c.x + 0.5 - start.x, c.y + 0.5 - start.y);
      const dFinish = Math.hypot(c.x + 0.5 - finish.x, c.y + 0.5 - finish.y);
      return dStart > 1.5 && dFinish > 1.5;
    });
    for (let i = candidates.length - 1; i > 0; i--) {
      const j = Math.floor(rand() * (i + 1));
      [candidates[i], candidates[j]] = [candidates[j], candidates[i]];
    }
    return candidates.slice(0, count).map((c) => ({ x: c.x + 0.5, y: c.y + 0.5 }));
  }

  // Difficulty scales maze size (bigger grid = longer, twistier solve) and hazard
  // count. Both mazes in a race grow together so "Hard Maze" always stays the
  // bigger of the pair, same shape the original fixed Easy/Hard pairing had.
  const TIERS = {
    easy: { sizes: [4, 6], hazards: 0 },
    medium: { sizes: [5, 8], hazards: 1 },
    hard: { sizes: [7, 10], hazards: 2 },
    impossible: { sizes: [9, 13], hazards: 3 },
  };
  const NAMES = ['Easy Maze', 'Hard Maze'];

  function generateMazes(seed, difficulty) {
    const tier = TIERS[difficulty] || TIERS.medium;
    return tier.sizes.map((size, i) => {
      // Distinct but deterministic per-maze seeds derived from the match seed,
      // so the two mazes in a race never accidentally end up identical.
      const mazeSeed = (seed + i * 7919) >>> 0;
      const cols = size, rows = size;
      const cells = generate(cols, rows, mazeSeed);
      const walls = wallSegments(cols, rows, cells);
      const start = { x: 0.5, y: 0.5 };
      const finish = { x: cols - 0.5, y: rows - 0.5 };
      const hazards = placeHazards(cells, tier.hazards, (mazeSeed + 104729) >>> 0, start, finish);
      return { name: NAMES[i] || `Maze ${i + 1}`, cols, rows, seed: mazeSeed, cells, walls, start, finish, hazards };
    });
  }

  global.MP_DIFFICULTIES = global.MP_DIFFICULTIES || Object.keys(TIERS);
  global.MP_generateMazes = generateMazes;
})(window);
