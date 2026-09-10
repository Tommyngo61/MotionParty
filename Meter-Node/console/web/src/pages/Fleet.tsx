import { useMemo, useState } from "react";
import { ChevronDown } from "lucide-react";
import { Card, SearchBox, TopBar } from "@/components/Shell";
import { NodeTile, type Density } from "@/components/NodeTile";
import { StatusDot, STATUS_STYLE } from "@/components/StatusDot";
import { Sparkline } from "@/components/Sparkline";
import { FLEET } from "@/lib/fixtures";
import { cn, num } from "@/lib/utils";
import type { NodeHealth, NodeSummary } from "@/lib/types";

type GroupBy = "site" | "health" | "none";
type SortBy = "health" | "utilisation" | "bandwidth" | "lastSeen" | "name";

const HEALTH_ORDER: NodeHealth[] = ["quarantined", "degraded", "unreachable", "unenrolled", "healthy"];

export function Fleet() {
  const [query, setQuery] = useState("");
  const [groupBy, setGroupBy] = useState<GroupBy>("site");
  const [sortBy, setSortBy] = useState<SortBy>("health");
  const [filter, setFilter] = useState<NodeHealth | "all">("all");
  // "fit" is the default because the brief's requirement is a hard one: the
  // whole fleet on one 1440px screen at 200 nodes, without scrolling. Grouping
  // adds headers and ragged final rows, so it is turned off in that mode.
  const [density, setDensity] = useState<Density>("fit");
  const effectiveGroupBy: GroupBy = density === "fit" ? "none" : groupBy;

  const counts = useMemo(() => {
    const c = { healthy: 0, degraded: 0, unreachable: 0, quarantined: 0, unenrolled: 0 } as Record<NodeHealth, number>;
    FLEET.forEach((n) => c[n.health]++);
    return c;
  }, []);

  const fleetPower = useMemo(() => FLEET.reduce((s, n) => s + n.powerW, 0), []);
  const controlPlane = useMemo(
    () => FLEET.reduce((s, n) => s + n.controlPlaneMb, 0) / FLEET.length,
    [],
  );

  const visible = useMemo(() => {
    let out = FLEET.filter(
      (n) =>
        (filter === "all" || n.health === filter) &&
        (query === "" ||
          n.name.toLowerCase().includes(query.toLowerCase()) ||
          n.siteShortName.toLowerCase().includes(query.toLowerCase())),
    );
    const last = (n: NodeSummary) => n.gpuUtil1h[n.gpuUtil1h.length - 1];
    out = [...out].sort((a, b) => {
      switch (sortBy) {
        case "health":
          return HEALTH_ORDER.indexOf(a.health) - HEALTH_ORDER.indexOf(b.health) || a.name.localeCompare(b.name);
        case "utilisation":
          return last(b) - last(a);
        case "bandwidth":
          return b.txGbMonth / b.capGb - a.txGbMonth / a.capGb;
        case "lastSeen":
          return +new Date(a.lastSeenAt) - +new Date(b.lastSeenAt);
        default:
          return a.name.localeCompare(b.name);
      }
    });
    return out;
  }, [query, sortBy, filter]);

  const groups = useMemo(() => {
    if (effectiveGroupBy === "none") return [{ key: "", nodes: visible }];
    const map = new Map<string, NodeSummary[]>();
    visible.forEach((n) => {
      const k = effectiveGroupBy === "site" ? n.siteShortName : n.health;
      if (!map.has(k)) map.set(k, []);
      map.get(k)!.push(n);
    });
    return [...map.entries()]
      .map(([key, nodes]) => ({ key, nodes }))
      .sort((a, b) =>
        effectiveGroupBy === "health"
          ? HEALTH_ORDER.indexOf(a.key as NodeHealth) - HEALTH_ORDER.indexOf(b.key as NodeHealth)
          : a.key.localeCompare(b.key),
      );
  }, [visible, effectiveGroupBy]);

  const needsAttention = counts.quarantined + counts.degraded + counts.unreachable;

  return (
    <>
      <TopBar
        title="Fleet"
        subtitle={
          <span className="tnum">
            {num(FLEET.length)} nodes · {num(SITES_COUNT)} sites ·{" "}
            {needsAttention === 0 ? (
              "all healthy"
            ) : (
              <span style={{ color: "var(--color-status-derated)" }}>
                {num(needsAttention)} need attention
              </span>
            )}
          </span>
        }
        right={
          <>
            <SearchBox value={query} onChange={setQuery} />
            <Select value={density} onChange={(v) => setDensity(v as Density)} options={[
              ["fit", "Density: fit all"], ["compact", "Density: compact"], ["comfortable", "Density: comfortable"],
            ]} />
            {/* Grouping adds headers and ragged final rows, which is exactly
                what breaks the fit-on-one-screen guarantee — so it is disabled
                rather than silently ignored in that density. */}
            <Select
              value={groupBy}
              onChange={(v) => setGroupBy(v as GroupBy)}
              disabled={density === "fit"}
              title={density === "fit" ? "Grouping is unavailable at fit density" : undefined}
              options={[
                ["site", "Group: site"], ["health", "Group: health"], ["none", "No grouping"],
              ]}
            />
            <Select value={sortBy} onChange={(v) => setSortBy(v as SortBy)} options={[
              ["health", "Sort: health"], ["utilisation", "Sort: utilisation"],
              ["bandwidth", "Sort: bandwidth"], ["lastSeen", "Sort: last seen"], ["name", "Sort: name"],
            ]} />
          </>
        }
      />

      {/* Summary strip. Numbers first, in the reference's stat-card idiom. */}
      <div className={cn("grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-6", density === "fit" ? "mb-2" : "mb-4")}>
        {HEALTH_ORDER.map((h) => (
          <button
            key={h}
            onClick={() => setFilter(filter === h ? "all" : h)}
            className={cn(
              "glass rounded-card px-3 text-left shadow-glass transition-all",
              density === "fit" ? "py-1.5" : "py-3",
              "hover:shadow-lift",
              filter === h && "ring-2 ring-[var(--color-accent-9)]",
            )}
          >
            <div className="flex items-center gap-1.5">
              <StatusDot health={h} size={9} />
              <span className="text-2xs text-[var(--text-lo)]">{STATUS_STYLE[h].label}</span>
            </div>
            <div
              className={cn("tnum mt-0.5 font-semibold leading-none", density === "fit" ? "text-lg" : "text-2xl")}
              style={{ color: h === "healthy" ? "var(--text-hi)" : STATUS_STYLE[h].color }}
            >
              {num(counts[h])}
            </div>
          </button>
        ))}

        <div className={cn("glass rounded-card px-3 shadow-glass", density === "fit" ? "py-1.5" : "py-3")}>
          <div className="text-2xs text-[var(--text-lo)]">Fleet draw</div>
          <div className={cn("tnum mt-0.5 font-semibold leading-none text-[var(--text-hi)]", density === "fit" ? "text-lg" : "text-2xl")}>
            {num(fleetPower / 1000, 1)}
            <span className="ml-1 text-sm font-normal text-[var(--text-lo)]">kW</span>
          </div>
        </div>
      </div>

      {/* Control-plane budget — the metric the whole residential premise rests on. */}
      <Card className={cn(density === "fit" ? "mb-2 !py-2" : "mb-4")}>
        <div className="flex flex-wrap items-center justify-between gap-4">
          <div>
            <div className="text-2xs text-[var(--text-lo)]">Control-plane traffic, mean per node</div>
            <div className="tnum mt-0.5 text-lg font-semibold text-[var(--text-hi)]">
              {num(controlPlane, 0)} <span className="text-xs font-normal text-[var(--text-lo)]">MB / month</span>
            </div>
          </div>
          <div className="min-w-56 flex-1">
            <div className="relative h-1.5 w-full overflow-hidden rounded-full bg-[var(--color-sand-4)]">
              <div
                className="h-full rounded-full"
                style={{ width: `${(controlPlane / 500) * 100}%`, background: "var(--color-accent-9)" }}
              />
              <div className="absolute top-[-3px] h-[12px] w-px bg-[var(--color-sand-9)]" style={{ left: "30%" }} />
            </div>
            <div className="tnum mt-1 flex justify-between text-[10px] text-[var(--text-lo)]">
              <span>0</span><span>150 MB target</span><span>500 MB ceiling</span>
            </div>
          </div>
        </div>
      </Card>

      {/* The grid itself. */}
      <div className={density === "fit" ? "space-y-3" : "space-y-5"}>
        {groups.map(({ key, nodes }) => (
          <div key={key}>
            {key && (
              <div className="mb-2 flex items-baseline gap-2">
                <h3 className="text-xs font-semibold uppercase tracking-[0.06em] text-[var(--text-mid)]">
                  {key}
                </h3>
                <span className="tnum text-2xs text-[var(--text-lo)]">{num(nodes.length)}</span>
                <div className="ml-1 h-px flex-1 bg-[var(--hairline)]" />
                <Sparkline
                  data={nodes.map((n) => n.gpuUtil1h[n.gpuUtil1h.length - 1])}
                  width={72}
                  height={14}
                />
              </div>
            )}
            <div
              className={cn(
                "grid",
                density === "fit"
                  ? "grid-cols-[repeat(auto-fill,minmax(104px,1fr))] gap-1"
                  : "grid-cols-[repeat(auto-fill,minmax(162px,1fr))] gap-2",
              )}
            >
              {nodes.map((n) => (
                <NodeTile key={n.id} node={n} density={density} />
              ))}
            </div>
          </div>
        ))}
        {visible.length === 0 && (
          <Card className="py-12 text-center text-xs text-[var(--text-lo)]">
            No nodes match that filter.
          </Card>
        )}
      </div>
    </>
  );
}

const SITES_COUNT = new Set(FLEET.map((n) => n.siteShortName)).size;

function Select({
  value,
  onChange,
  options,
  disabled,
  title,
}: {
  value: string;
  onChange: (v: string) => void;
  options: [string, string][];
  disabled?: boolean;
  title?: string;
}) {
  return (
    <div
      className={cn("glass relative flex h-9 items-center rounded-full pl-3 pr-7", disabled && "opacity-45")}
      title={title}
    >
      <select
        value={value}
        onChange={(e) => onChange(e.target.value)}
        disabled={disabled}
        className="cursor-pointer appearance-none bg-transparent text-xs text-[var(--text-mid)] outline-none disabled:cursor-not-allowed"
      >
        {options.map(([v, label]) => (
          <option key={v} value={v}>{label}</option>
        ))}
      </select>
      <ChevronDown size={13} className="pointer-events-none absolute right-2.5 text-[var(--text-lo)]" />
    </div>
  );
}
