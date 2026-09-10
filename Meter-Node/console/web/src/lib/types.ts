/**
 * The shapes the UI renders.
 *
 * These mirror meter-node-proto rather than restating it: `NodeHealth` is
 * DERIVED by the controller from telemetry, never asserted by an agent, and
 * `lifecycle` is the separate administrative state an operator sets. Keeping
 * the two apart in the type system is what stops a screen from quietly
 * treating "an operator quarantined this" and "this node is reporting ECC
 * errors" as the same fact.
 */

export type NodeHealth =
  | "healthy"
  | "degraded"
  | "unreachable"
  | "unenrolled"
  | "quarantined";

export type Lifecycle = "unenrolled" | "enrolled" | "quarantined" | "retired";

export interface GpuSample {
  index: number;
  uuid: string;
  name: string;
  utilPct: number;
  memUsedMb: number;
  memTotalMb: number;
  tempC: number;
  powerW: number;
  powerLimitW: number;
  fanPct: number;
  pcieGen: number;
  pcieWidth: number;
  eccErrors: number;
  throttleReasons: string[];
  driverVersion: string;
}

export interface NodeSummary {
  id: string;
  name: string;
  siteId: string;
  siteShortName: string;
  health: NodeHealth;
  lifecycle: Lifecycle;
  agentVersion: string;
  lastSeenAt: string;
  /** One hour of GPU utilisation, oldest first. Drawn as the tile sparkline. */
  gpuUtil1h: number[];
  powerW: number;
  gpuTempC: number;
  /** Month-to-date upstream against the site's cap. */
  txGbMonth: number;
  capGb: number;
  upMbps: number;
  /** Control-plane bytes the agent has spent this month. */
  controlPlaneMb: number;
  /** Why the node is not healthy. Empty when it is. */
  reasons: string[];
}

export interface NodeDetail extends NodeSummary {
  siteName: string;
  address: string;
  timezone: string;
  isp: string;
  planDownMbps: number;
  planUpMbps: number;
  hostname: string;
  cpuModel: string;
  cpuCores: number;
  memTotalMb: number;
  kernel: string;
  os: string;
  boardVendor: string;
  boardProduct: string;
  fingerprintHash: string;
  enrolledAt: string;
  bootId: string;
  uptimeS: number;
  clockSkewMs: number;
  cpuUtilPct: number;
  cpuTempC: number;
  memUsedMb: number;
  gpus: GpuSample[];
  disks: { mount: string; usedGb: number; totalGb: number; smartOk: boolean | null }[];
  containers: { id: string; image: string; state: string; cpuPct: number; memMb: number; restarts: number }[];
  /** Energy accrued on the homeowner's meter this month, and what it is worth. */
  energyKwhMonth: number;
  energyRatePerKwh: number;
  series: { t: number; gpuUtil: number; gpuTemp: number; powerW: number; txMbps: number }[];
  events: NodeEvent[];
  connections: { at: string; event: "connected" | "disconnected"; detail: string }[];
}

export interface NodeEvent {
  id: string;
  t: string;
  severity: "INFO" | "WARN" | "ERROR" | "CRITICAL";
  code: string;
  message: string;
}

export interface Alert {
  id: string;
  rule: string;
  nodeId: string | null;
  nodeName: string | null;
  siteShortName: string;
  severity: "INFO" | "WARN" | "ERROR" | "CRITICAL";
  state: "pending" | "firing" | "resolved" | "suppressed";
  openedAt: string;
  lastSeenAt: string;
  value: number | null;
  summary: string;
}

export interface Site {
  id: string;
  shortName: string;
  name: string;
  nodeCount: number;
}
