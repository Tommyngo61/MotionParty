# Design inheritance from the RMS portal

> **Status: UNBLOCKED BY DIRECTION, NOT BY EXTRACTION.**
>
> `rms-frontend-portal` still could not be read (see below). The UI was built
> anyway, because the owner supplied the direction directly: a screenshot of
> the live RMS device page, a reference design for the intended look
> (glassmorphism, warm ground, soft radii), and an explicit instruction to
> build on that rather than wait.
>
> That is a legitimate unblock, and it is a *different* one from what this
> document originally called for. What follows records both what was inherited
> from the screenshot and what is still owed if true parity with RMS matters.

## What was inherited, and from where

**From the RMS screenshot** — patterns an installer already recognises:

| Pattern | Where it landed |
| --- | --- |
| White cards, hairline border, minimal shadow | `.glass` panels and `Card` |
| Pill status chips, light tint + saturated text | `StatusChip` |
| Grey-label / dark-value rows with hairline dividers | `Row` |
| Solid blue primary button with a leading icon | Command console button |
| Narrow left icon rail | `Shell` |
| Section headers in small caps | `Card` titles |
| Green reserved for "OKAY" / "No Errors" | Extended into the status-colour rule |

**From the reference design** — surface treatment:
warm cream ground rather than cold grey, generous radii (12/16/24px),
translucent panels with backdrop blur, floating rail, soft layered shadows,
stat cards with a small label above a large number.

**From the product brief** — the rules that survived both, and matter most:
green/amber/red reserved exclusively for node state; every number monospace
with tabular figures; density sufficient for 200 nodes on one 1440px screen;
motion only when data actually changes.

## What is still owed

This was built from a *picture* of RMS, not its source. If genuine parity
matters, these are the things a screenshot cannot supply:

- `components.json` — the shadcn style variant, base colour, and icon library.
  Console's should match it field for field; right now it has no shadcn config
  at all.
- The real `@theme` block. Console's sand scale was derived to *look* right
  next to the screenshot, not sampled from RMS's tokens. Exact hex parity is
  unverified.
- `tsconfig.json`, `vite.config.ts`, `eslint.config.*`, `.prettierrc` — the
  brief says copy these verbatim. Console's were written from scratch, so
  they will drift.
- The `src/components/ui/**` primitive inventory, and which primitives have
  been customised away from stock shadcn.
- Font loading. Console loads Inter + JetBrains Mono from Google Fonts; RMS's
  actual typeface and weights are unconfirmed.
- Whether RMS uses colour decoratively. If it does, Console's reserved-colour
  rule is a real **divergence** to decide on rather than an extension.

None of that blocks the UI shipping. All of it is cheap to reconcile once the
repository is readable — the token layer is one file, `src/index.css`.

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

**Not blocked — the backend.** Console M0 and M1 never touched the design
system.

**No longer blocked — the UI.** The fleet grid, node detail and alerts views
are built (`console/web/`), on tokens derived from the supplied references
rather than extracted from RMS. The agent release and audit log views are
placeholders because their backing services are M5, not because of design.

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
