# Operator UI

The fleet console. Vite 6 + React 18 + TypeScript + Tailwind v4, matching the
stack of the MeterHome RMS portal so components and tokens can move between the
two codebases.

```bash
make -C console web-install   # once
make -C console web           # localhost:5173, proxying /v1 to the controller
```

## Where the design came from

Two references, reconciled: the live RMS device page supplies the patterns an
installer already recognises — white cards with hairline borders, pill status
chips, grey-label/dark-value rows, a solid blue primary button, a narrow icon
rail. A supplied reference design supplies the surface treatment: a warm cream
ground rather than a cold grey one, generous radii, translucent panels over a
soft gradient.

`console/DESIGN-INHERITANCE.md` records exactly what was inherited from which,
and what is still owed if genuine parity with RMS matters — the tokens here
were derived to *look* right beside RMS, not sampled from its source.

## The three rules that are not preferences

**Green, amber and red are reserved for node state.** Nothing else in the
product may use those hues — not a chart series, not a brand accent, not an
icon. The consequence is the goal: a healthy fleet renders almost colourless,
so colour anywhere in the periphery reliably means something needs attention.
Feature code uses the `--status-*` aliases and never a raw literal; CI fails
the build on one.

**Every number is monospace with tabular figures** (the `tnum` class).
Proportional digits jitter as live values update and make the whole app feel
unstable.

**Every status colour is paired with a distinct glyph.** `StatusDot` draws a
filled disc, a ring with a centre dot, a dashed ring, a slashed disc, a dotted
outline — so state survives colourblindness, greyscale printing, and
screenshots pasted into reports.

## Density

The fleet grid defaults to **fit**: 200 nodes on one 1440×900 screen with no
scrolling, read like an equipment panel rather than browsed. Compact and
comfortable densities trade that for a larger sparkline. Grouping is disabled
at fit density because headers and ragged final rows are exactly what breaks
the guarantee.

## Data

`src/lib/fixtures.ts` is the only module that fabricates anything. The
controller does not serve fleet or telemetry endpoints yet — that is M2
(ingest) and M4 (health scoring) — so every component takes data as props and
swapping fixtures for real fetches touches one file. TanStack Query is already
wired for that.

The fixture distribution is deliberately unflattering: ~8% of nodes
unreachable, a handful thermally derated, one or two faulted. A demo where
everything is green would hide the exact failure the grid exists to surface.

## Layout

```
src/components/   StatusDot, Sparkline, Meter, NodeTile, Shell
src/pages/        Fleet, NodeDetail, Alerts, Placeholder
src/lib/          types, fixtures, utils
src/index.css     the token layer — the only place colours are defined
```
