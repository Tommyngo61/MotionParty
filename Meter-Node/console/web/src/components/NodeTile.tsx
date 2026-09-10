import { Link } from "react-router-dom";
import { StatusDot } from "./StatusDot";
import { Sparkline } from "./Sparkline";
import { cn, num, since } from "@/lib/utils";
import type { NodeSummary } from "@/lib/types";

/**
 * One machine, as a tile.
 *
 * The whole fleet has to fit on one 1440px screen at 200 nodes, read at a
 * glance like an equipment panel. That constraint drives everything here:
 *
 *   - Nearly bare in light theme. Hairline border at step 6, no shadow, no
 *     filled card background. Card-heavy dashboards collapse into visual noise
 *     at this density, and that is the main risk on this screen — so whitespace
 *     and the status dot do the work.
 *   - A healthy tile is almost colourless. The sparkline is the only ink, and
 *     it is muted when the node is not reporting.
 *   - Colour appears only when something is wrong, which is what makes colour
 *     in peripheral vision meaningful.
 */
export type Density = "fit" | "compact" | "comfortable";

export function NodeTile({ node, density = "compact" }: { node: NodeSummary; density?: Density }) {
  const offline = node.health === "unreachable" || node.health === "unenrolled";
  const attention = node.health === "quarantined" || node.health === "degraded";

  // "fit" is the density the brief actually requires: two hundred machines on
  // one 1440px screen without scrolling, read like an equipment panel rather
  // than browsed. The tile drops to a single line plus a sparkline strip —
  // still a status glyph, a name, a trend and a power figure, just with every
  // pixel of padding argued for.
  const fit = density === "fit";

  return (
    <Link
      to={`/nodes/${node.id}`}
      className={cn(
        "group relative flex flex-col rounded-tile border bg-transparent",
        "border-[var(--hairline)]",
        "transition-[background,border-color,box-shadow] duration-150",
        "hover:border-[var(--color-sand-7)] hover:bg-[var(--panel)] hover:shadow-glass",
        "focus-visible:bg-[var(--panel)]",
        attention && "border-[color-mix(in_srgb,var(--tile-accent)_38%,var(--hairline))]",
        fit ? "h-[34px] justify-center gap-0 px-1.5 py-0.5" : "justify-between p-2.5",
        density === "compact" && "h-[88px]",
        density === "comfortable" && "h-[104px]",
      )}
      style={
        {
          "--tile-accent":
            node.health === "quarantined"
              ? "var(--color-status-fault)"
              : "var(--color-status-derated)",
        } as React.CSSProperties
      }
    >
      {fit ? (
        <>
          <div className="flex items-center gap-1">
            <StatusDot health={node.health} size={7} />
            <span className="truncate text-[10px] font-medium leading-none text-[var(--text-hi)]">
              {node.name.replace("mn-", "")}
            </span>
            <span className="tnum ml-auto shrink-0 text-[9px] leading-none text-[var(--text-lo)]">
              {offline ? "—" : num(node.powerW)}
            </span>
          </div>
          <div className="-mb-0.5 mt-0.5">
            <Sparkline data={node.gpuUtil1h} width={96} height={11} muted={offline} />
          </div>
        </>
      ) : (
        <>
          <div className="min-w-0">
            <div className="flex items-center gap-1.5">
              <StatusDot health={node.health} size={9} />
              <span className="truncate text-xs font-medium text-[var(--text-hi)]">
                {node.name}
              </span>
            </div>
            <div className="mt-0.5 pl-[15px] text-2xs text-[var(--text-lo)]">
              {node.siteShortName}
              {offline && <span className="tnum"> · {since(node.lastSeenAt)} ago</span>}
            </div>
          </div>

          <div className="-mx-0.5">
            <Sparkline data={node.gpuUtil1h} width={150} height={24} muted={offline} />
          </div>

          <div className="flex items-baseline justify-between">
            <span className="tnum text-2xs text-[var(--text-mid)]">
              {offline ? "—" : `${num(node.powerW)} W`}
            </span>
            <span className="tnum text-2xs text-[var(--text-lo)]">
              {offline ? "" : `${num(node.gpuUtil1h[node.gpuUtil1h.length - 1])}%`}
            </span>
          </div>
        </>
      )}
    </Link>
  );
}
