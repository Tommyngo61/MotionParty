import { useParams, Link } from "react-router-dom";
import { ArrowLeft, Terminal, Home, Wifi, Zap, Cpu, type LucideIcon } from "lucide-react";
import {
  Area, AreaChart, CartesianGrid, Line, LineChart, ResponsiveContainer, XAxis, YAxis, ReferenceLine,
} from "recharts";
import { Card, Row, TopBar } from "@/components/Shell";
import { StatusChip } from "@/components/StatusDot";
import { Meter } from "@/components/Meter";
import { nodeDetail } from "@/lib/fixtures";
import { bytes, cn, num, since } from "@/lib/utils";

const SEV_COLOR: Record<string, string> = {
  INFO: "var(--text-lo)",
  WARN: "var(--color-status-derated)",
  ERROR: "var(--color-status-fault)",
  CRITICAL: "var(--color-status-fault)",
};

export function NodeDetailPage() {
  const { id = "" } = useParams();
  const n = nodeDetail(id);
  const gpu = n.gpus[0];
  const offline = n.health === "unreachable" || n.health === "unenrolled";

  return (
    <>
      <TopBar
        title={n.name}
        subtitle={
          <span className="flex flex-wrap items-center gap-2">
            <Link to="/" className="inline-flex items-center gap-1 hover:text-[var(--text-mid)]">
              <ArrowLeft size={12} /> Fleet
            </Link>
            <span className="text-[var(--hairline)]">/</span>
            <span>{n.siteName}</span>
            <span className="text-[var(--hairline)]">·</span>
            <span className="tnum">agent {n.agentVersion}</span>
            <span className="text-[var(--hairline)]">·</span>
            <span className="tnum">last seen {since(n.lastSeenAt)} ago</span>
          </span>
        }
        right={
          <>
            <StatusChip health={n.health} />
            <button
              className="flex h-9 items-center gap-2 rounded-full px-4 text-xs font-medium text-white shadow-glass transition-colors"
              style={{ background: "var(--color-accent-9)" }}
            >
              <Terminal size={14} strokeWidth={2} /> Command console
            </button>
          </>
        }
      />

      {n.reasons.length > 0 && (
        <Card
          className="mb-3 border-l-2"
          style={{
            borderLeftColor:
              n.health === "quarantined"
                ? "var(--color-status-fault)"
                : "var(--color-status-derated)",
          }}
        >
          <div className="text-2xs uppercase tracking-[0.06em] text-[var(--text-lo)]">Why this node is not healthy</div>
          <ul className="mt-2 space-y-1">
            {n.reasons.map((r) => (
              <li key={r} className="text-xs text-[var(--text-hi)]">· {r}</li>
            ))}
          </ul>
        </Card>
      )}

      {/* Live vitals */}
      <div className="mb-3 grid grid-cols-2 gap-3 lg:grid-cols-4">
        <Vital label="GPU utilisation" value={offline ? "—" : `${num(gpu.utilPct)}%`} icon={Cpu}
               foot={`${gpu.name.replace("NVIDIA ", "")}`} />
        <Vital label="Power draw" value={offline ? "—" : `${num(gpu.powerW)} W`} icon={Zap}
               foot={`limit ${num(gpu.powerLimitW)} W`} />
        <Vital label="GPU temperature" value={offline ? "—" : `${num(gpu.tempC)} °C`} icon={Zap}
               foot={gpu.throttleReasons.length ? gpu.throttleReasons.join(", ") : "not throttling"}
               tone={gpu.throttleReasons.length ? "warn" : undefined} />
        <Vital label="Upstream" value={offline ? "—" : `${num(n.series.at(-1)!.txMbps, 1)} Mb/s`} icon={Wifi}
               foot={`plan ${num(n.planUpMbps)} Mb/s up`} />
      </div>

      <div className="grid gap-3 xl:grid-cols-[1fr_320px]">
        <div className="space-y-3">
          {/* Charts. Thin strokes, no fills, gridlines minimal, y labelled at
              min and max only, all sharing one time range. */}
          <Card title="GPU utilisation & temperature — 24 h">
            <ResponsiveContainer width="100%" height={168}>
              <LineChart data={n.series} margin={{ top: 4, right: 4, bottom: 0, left: -6 }}>
                <CartesianGrid stroke="var(--hairline)" strokeDasharray="2 4" vertical={false} />
                <XAxis dataKey="t" tickFormatter={(t) => new Date(t).getHours() + "h"}
                       stroke="var(--text-lo)" fontSize={10} tickLine={false} axisLine={false} minTickGap={44} />
                <YAxis domain={[0, 100]} ticks={[0, 100]} stroke="var(--text-lo)" fontSize={10}
                       tickLine={false} axisLine={false} width={30} />
                <ReferenceLine y={85} stroke="var(--color-sand-8)" strokeDasharray="3 3" strokeWidth={1} />
                <Line type="monotone" dataKey="gpuUtil" stroke="var(--color-series-1)" strokeWidth={1.3}
                      dot={false} isAnimationActive={false} />
                <Line type="monotone" dataKey="gpuTemp" stroke="var(--color-series-3)" strokeWidth={1.3}
                      dot={false} isAnimationActive={false} strokeDasharray="4 3" />
              </LineChart>
            </ResponsiveContainer>
            <Legend items={[["GPU utilisation %", "var(--color-series-1)"], ["Temperature °C", "var(--color-series-3)"]]} />
          </Card>

          <div className="grid gap-3 md:grid-cols-2">
            {/* The three panels with no equivalent in datacenter tooling. */}
            <Card title="Host relationship">
              <div className="mb-3 flex items-center gap-2.5">
                <div className="flex h-9 w-9 items-center justify-center rounded-[10px]"
                     style={{ background: "var(--color-accent-3)", color: "var(--color-accent-11)" }}>
                  <Home size={16} strokeWidth={1.9} />
                </div>
                <div className="min-w-0">
                  <div className="truncate text-xs font-medium text-[var(--text-hi)]">{n.siteName}</div>
                  <div className="truncate text-2xs text-[var(--text-lo)]">{n.address}</div>
                </div>
              </div>
              <Row label="ISP" value={n.isp} mono={false} />
              <Row label="Plan" value={`${num(n.planDownMbps)} ↓ / ${num(n.planUpMbps)} ↑ Mb/s`} />
              <Row label="Local time" value={n.timezone.split("/")[1].replace("_", " ")} mono={false} />
              <Row label="Nodes at site" value="1" />
            </Card>

            <Card title="Bandwidth against cap">
              <div className="tnum mb-1 text-2xl font-semibold text-[var(--text-hi)]">
                {bytes(n.txGbMonth)}
                <span className="ml-1.5 text-xs font-normal text-[var(--text-lo)]">of {bytes(n.capGb)}</span>
              </div>
              <div className="mb-3 text-2xs text-[var(--text-lo)]">
                month to date on the homeowner's plan
              </div>
              <Meter value={n.txGbMonth} max={n.capGb} />
              <div className="mt-4 border-t border-[var(--hairline)] pt-3">
                <Row label="Our control plane" value={`${num(n.controlPlaneMb)} MB`} />
                <Row label="Share of their cap" value={`${num((n.controlPlaneMb / 1024 / n.capGb) * 100, 3)} %`} />
              </div>
            </Card>

            <Card title="Energy accrual">
              <div className="tnum mb-1 text-2xl font-semibold text-[var(--text-hi)]">
                {num(n.energyKwhMonth)}
                <span className="ml-1.5 text-xs font-normal text-[var(--text-lo)]">kWh this month</span>
              </div>
              <div className="mb-3 text-2xs text-[var(--text-lo)]">
                drawn on the homeowner's meter
              </div>
              <ResponsiveContainer width="100%" height={56}>
                <AreaChart data={n.series} margin={{ top: 2, right: 0, bottom: 0, left: 0 }}>
                  <Area type="monotone" dataKey="powerW" stroke="var(--color-series-4)" strokeWidth={1.2}
                        fill="var(--color-series-4)" fillOpacity={0.07} isAnimationActive={false} />
                </AreaChart>
              </ResponsiveContainer>
              <div className="mt-2 border-t border-[var(--hairline)] pt-3">
                <Row label="Owed at $0.14/kWh"
                     value={`$${num(n.energyKwhMonth * n.energyRatePerKwh, 2)}`} />
                <Row label="Mean draw" value={`${num(n.powerW)} W`} />
              </div>
            </Card>

            <Card title="Storage">
              {n.disks.map((d) => (
                <div key={d.mount} className="mb-3 last:mb-0">
                  <Meter value={d.usedGb} max={d.totalGb} label={d.mount}
                         unit="GB" tone="neutral" />
                  <div className="mt-1 text-[10px] text-[var(--text-lo)]">
                    SMART {d.smartOk === null ? "not readable" : d.smartOk ? "ok" : "FAILING"}
                  </div>
                </div>
              ))}
            </Card>
          </div>
        </div>

        {/* Machine record */}
        <div className="space-y-3">
          <Card title="Identity">
            <Row label="Node ID" value={<span title={n.id}>{n.id.slice(0, 13)}…</span>} />
            <Row label="Lifecycle" value={n.lifecycle} mono={false} />
            <Row label="Enrolled" value={new Date(n.enrolledAt).toLocaleDateString()} />
            <Row label="Fingerprint" value={`${n.fingerprintHash.slice(0, 12)}…`} />
            <Row label="Boot ID" value={`${n.bootId.slice(0, 12)}…`} />
            <Row label="Uptime" value={offline ? "—" : since(new Date(Date.now() - n.uptimeS * 1000).toISOString())} />
            <Row label="Clock skew" value={`${n.clockSkewMs > 0 ? "+" : ""}${num(n.clockSkewMs)} ms`} />
          </Card>

          <Card title="Hardware">
            <Row label="Board" value={`${n.boardVendor} ${n.boardProduct}`} mono={false} />
            <Row label="CPU" value={n.cpuModel.replace(" 16-Core", "")} mono={false} />
            <Row label="Memory" value={`${num(n.memTotalMb / 1024)} GB`} />
            <Row label="GPU" value={gpu.name.replace("NVIDIA ", "")} mono={false} />
            <Row label="VRAM" value={`${num(gpu.memTotalMb / 1024)} GB`} />
            <Row label="PCIe link"
                 value={<span style={{ color: gpu.pcieGen < 5 ? "var(--color-status-derated)" : undefined }}>
                   gen{gpu.pcieGen} ×{gpu.pcieWidth}
                 </span>} />
            <Row label="ECC errors"
                 value={<span style={{ color: gpu.eccErrors > 0 ? "var(--color-status-fault)" : undefined }}>
                   {num(gpu.eccErrors)}
                 </span>} />
            <Row label="Driver" value={gpu.driverVersion} />
            <Row label="OS" value={n.os} mono={false} />
          </Card>

          <Card title="Event timeline">
            <div className="space-y-2.5">
              {n.events.map((e) => (
                <div key={e.id} className="flex gap-2.5">
                  <div className="mt-1 h-1.5 w-1.5 shrink-0 rounded-full"
                       style={{ background: SEV_COLOR[e.severity] }} />
                  <div className="min-w-0 flex-1">
                    <div className="flex items-baseline justify-between gap-2">
                      <span className="truncate text-2xs font-medium" style={{ color: SEV_COLOR[e.severity] }}>
                        {e.code}
                      </span>
                      <span className="tnum shrink-0 text-[10px] text-[var(--text-lo)]">
                        {since(e.t)} ago
                      </span>
                    </div>
                    <div className="text-2xs leading-snug text-[var(--text-mid)]">{e.message}</div>
                  </div>
                </div>
              ))}
            </div>
          </Card>
        </div>
      </div>
    </>
  );
}

function Vital({
  label, value, foot, icon: Icon, tone,
}: {
  label: string; value: string; foot?: string;
  icon: LucideIcon;
  tone?: "warn";
}) {
  return (
    <div className="glass rounded-card px-4 py-3.5 shadow-glass">
      <div className="flex items-center gap-1.5 text-[var(--text-lo)]">
        <Icon size={12} strokeWidth={2} />
        <span className="text-2xs">{label}</span>
      </div>
      <div className={cn("tnum mt-1.5 text-[1.6rem] font-semibold leading-none text-[var(--text-hi)]")}>
        {value}
      </div>
      {foot && (
        <div className="mt-1.5 truncate text-[10px]"
             style={{ color: tone === "warn" ? "var(--color-status-derated)" : "var(--text-lo)" }}>
          {foot}
        </div>
      )}
    </div>
  );
}

function Legend({ items }: { items: [string, string][] }) {
  return (
    <div className="mt-2 flex flex-wrap gap-4">
      {items.map(([label, color]) => (
        <span key={label} className="flex items-center gap-1.5 text-[10px] text-[var(--text-lo)]">
          <span className="h-px w-4" style={{ background: color }} />
          {label}
        </span>
      ))}
    </div>
  );
}
