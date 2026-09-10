# Design inheritance from the RMS portal

> **Status: BLOCKED. Neither Case A nor Case B has been determined, because
> `rms-frontend-portal` could not be read.**
>
> Section 3 of the brief says plainly: *"If it is not present on disk, stop and
> tell me rather than inventing a design system."* This document is that stop.

## What was attempted

`MeterHome/rms-frontend-portal` exists and this account can reach it — it shows
up in the repository listing as private, `can_push: true`, last pushed
2026-04-09. The blocker is the session, not permissions:

```
add_repo: cross-tier adds are not supported in v1:
requested "meterhome/rms-frontend-portal" but session already has repos
from owner(s) [tommyngo61]
```

There is no `gh` CLI in this environment, and GitHub access is mediated by the
MCP server, which is scoped to a single owner per session. So the clone that
Step 1 depends on could not happen here.

## What that does and does not block

**Not blocked — everything delivered so far.** Console M0 and M1 are backend:
migrations, enrollment, credential issuance, hardware fingerprint binding, the
health endpoint, CI, and the compose stack. None of it touches the design
system.

**Blocked — M3 and every screen after it.** The fleet grid, node detail, alerts
view, agent release view, and audit log view all depend on tokens, primitives,
and conventions that must be *read* from RMS, not guessed at.

## What is needed to unblock

Either of:

1. **Start a session whose initial repository is `MeterHome/rms-frontend-portal`**
   (the error message names this as the supported path), or
2. **Clone it into the workspace out of band** so it sits at
   `../../rms-frontend-portal` relative to `console/`, which is where
   Section 3 specifies it should be.

## The extraction that will run once it is present

Recorded here now so the work is unambiguous when the repository appears. Every
item below produces an entry in this document, replacing this status block.

**Step 0 — Case A or Case B.** The first line of the rewritten document states
which:

- **Case A** — a real token layer exists: a populated `@theme` block, semantic
  colour names, consistent spacing and radii used across screens. Inherit
  directly.
- **Case B** — no coherent token layer. The brief anticipates this: the codebase
  was exported from Figma Make and then migrated to hand-written code, so it may
  be screen-by-screen styling with hard-coded values and duplicated class
  strings. Then the job changes to auditing the *rendered* values across RMS's
  main screens, deriving the de facto system (which colours, sizes, and radii it
  really uses, and how often each appears), and proposing a token set that
  reproduces RMS's current appearance faithfully — **not** an improved one. The
  goal is that Console and RMS look like siblings, which means matching what RMS
  looks like today. Case B also produces a short list of the inconsistencies
  found (five near-identical greys, three card radii, and so on) as input to a
  future RMS cleanup.

**Step 1 — files to read and record.**

| Source | What to capture |
| --- | --- |
| `components.json` | shadcn style variant, base colour, CSS-variables flag, icon library, path aliases. Console's must match field for field. |
| `@theme` block (`src/index.css`, `src/globals.css`, or `src/styles/`) | Every custom property: colour tokens, spacing, radii, font families and sizes, shadows, breakpoints. Tailwind v4 keeps the theme in CSS, not JS. |
| `src/components/ui/**` | Full shadcn primitive inventory; which are customised away from stock, and how. |
| `src/lib/utils.ts` | `cn()` and any variant helpers. |
| `tsconfig.json`, `vite.config.ts`, `eslint.config.*`, `.prettierrc` | Copy **verbatim**. Re-deriving them guarantees drift. |
| Font loading | Typefaces, loading mechanism, weights. |
| Icon usage | Library, size, stroke width, wrapper component. |
| Layout shell | How sidebar, top bar, page header, and content container compose; routing convention. |
| Charts | Library, wrapper component, how it consumes theme tokens. |
| Dark theme | Whether one exists; how it is toggled and persisted. |

**Step 2 — mirror conventions, not just tokens.** File layout, folder naming,
component naming, export style, prop naming, and state management follow RMS
even where a different choice would be preferable. Consistency across the two
codebases is worth more than any individual improvement. Anything that looks
actively wrong gets noted in this document and **left alone** pending a decision.

**Step 3 — adopt unchanged.** Colour tokens, type scale, spacing, radii, shadow
scale, icon set, focus rings, buttons, inputs, selects, dialogs, toasts, tabs,
tooltips, table primitives.

**Step 4 — extend, never fork.** Node tile, status dot, sparkline, bandwidth
meter, and the energy accrual panel are new components built strictly from RMS's
existing tokens and primitives. No UI dependency RMS does not already have,
without asking.

**Step 5 — RMS stays read-only.** Console is read-only against it in phase 1.
Anything Console needs that RMS lacks is added locally and logged here as a
candidate for a future shared `qpo-ui` package.

## Three questions that will need a decision, not a guess

These are flagged now because Section 3 says explicitly they are decisions to be
seen rather than made silently. Each becomes a section of this document with a
real answer once RMS can be read.

1. **Does RMS have a dark theme?** If not, Console's is built on Radix Colors'
   12-step scales with RMS's light-theme values as the light scale, written so it
   can be lifted into RMS unchanged later.

2. **Does RMS use colour decoratively** — brand-coloured buttons, coloured chart
   series, coloured section headers? If it does, then Console's rule reserving
   green/amber/red exclusively for node state becomes a real **divergence** from
   RMS rather than an extension. That is a call about whether to change RMS or
   accept the inconsistency, and it is not one to resolve unilaterally.

3. **Which warm neutral scale?** Console's brief calls for a sand or olive
   family rather than slate — home-adjacent rather than datacenter-cold. Whether
   that is compatible with RMS's existing neutrals, or a fourth divergence
   needing sign-off, depends on what RMS actually uses.

## The three sanctioned divergences (unchanged by any of the above)

RMS serves installers and homeowners looking at one system at a time; Console
serves operators watching a whole fleet. That difference justifies exactly
three departures, and a fourth requires asking:

1. **Higher density.** Smaller row heights, smaller base type, more per screen.
   RMS's comfort spacing is wrong for a 200-node grid.
2. **A stricter colour rule.** Green, amber, and red are reserved exclusively
   for node state, behind semantic aliases (`--status-online`,
   `--status-derated`, `--status-offline`, `--status-fault`), with a CI check
   that fails on raw colour literals in `src/features/`. The consequence is the
   goal: a healthy fleet renders as an almost colourless screen, so colour in
   peripheral vision always means something needs attention. Every status colour
   is paired with a distinct glyph or fill pattern, so state survives
   colourblindness, greyscale printing, and screenshots pasted into reports.
3. **Live-updating data**, which RMS mostly does not have. Values crossfade and
   never count up; charts do not animate on mount; skeletons do not shimmer. The
   only motion in the application is data actually changing — with one
   exception, a single brief pulse on a tile that changes state.
