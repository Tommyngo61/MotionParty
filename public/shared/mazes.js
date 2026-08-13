// Procedurally-defined "Tilt Maze" courses, shared by the host (progress overview)
// and the player controller (the actual tilt maze). Deterministic (seeded) so both
// players in a race see the exact same layout.
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

  // Recursive-backtracker: carves a spanning tree over the grid, so there's exactly
  // one path between any two cells - every branch that isn't on that path is a dead
  // end, and there are no loops. That's what makes it a proper "tilt maze".
  function generate(cols, rows, seed) {
    const rand = mulberry32(seed);
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

  const MAZES = [
    { name: 'Easy Maze', cols: 5, rows: 5, seed: 7 },
    { name: 'Hard Maze', cols: 8, rows: 8, seed: 13 },
  ];

  MAZES.forEach((m) => {
    m.cells = generate(m.cols, m.rows, m.seed);
    m.walls = wallSegments(m.cols, m.rows, m.cells);
    m.start = { x: 0.5, y: 0.5 };
    m.finish = { x: m.cols - 0.5, y: m.rows - 0.5 };
  });

  global.MP_MAZES = MAZES;
})(window);
