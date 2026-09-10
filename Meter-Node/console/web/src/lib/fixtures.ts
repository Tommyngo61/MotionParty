/**
 * Fixture data.
 *
 * The controller does not serve fleet or telemetry endpoints yet — that is M2
 * (ingest) and M4 (health scoring). This module stands in for them so the
 * screens can be built and reviewed now, and it is deliberately the ONLY place
 * that fabricates anything: every component takes data as props, so replacing
 * this with real fetches touches one file.
 *
 * The distributions are chosen to make the screens honest rather than
 * flattering. A real residential fleet is mostly fine and occasionally not:
 * roughly 8% of nodes are unreachable at any moment because homeowners unplug
 * things, a handful are thermally derated in a closed cabinet, and one or two
 * are genuinely faulted. A demo where everything is green would hide the exact
 * failure the fleet grid exists to surface.
 */

import type { Alert, NodeDetail, NodeEvent, NodeSummary, Site } from "./types";

/** Deterministic PRNG, so the fleet looks the same on every reload. */
function rng(seed: number) {
  let s = seed >>> 0;
  return () => {
    s = (s * 1664525 + 1013904223) >>> 0;
    return s / 0xffffffff;
  };
}

const SITE_NAMES = [
  ["RDG", "Ridgefield"], ["ASH", "Ashgrove"], ["MPL", "Maple Court"],
  ["CDR", "Cedar Hollow"], ["BRK", "Brookline"], ["ELM", "Elmwood"],
  ["HTH", "Heathrow Lane"], ["WLW", "Willow Bend"], ["ORC", "Orchard Hill"],
  ["PNE", "Pine Ridge"], ["QRY", "Quarry Road"], ["STN", "Stonebridge"],
];

const ISPS = ["Comcast Xfinity", "Spectrum", "Cox", "AT&T Fiber", "Frontier", "Optimum"];

function utilSeries(rand: () => number, base: number, points = 60): number[] {
  const out: number[] = [];
  let v = base;
  for (let i = 0; i < points; i++) {
    v += (rand() - 0.5) * 14;
    v = Math.max(0, Math.min(100, v));
    out.push(Math.round(v));
  }
  return out;
}

export const FLEET_SIZE = 200;

function buildFleet(): NodeSummary[] {
  const rand = rng(20260910);
  const nodes: NodeSummary[] = [];

  for (let i = 0; i < FLEET_SIZE; i++) {
    const [short, full] = SITE_NAMES[i % SITE_NAMES.length];
    const roll = rand();

    // The shape of a real fleet, not a demo fleet.
    let health: NodeSummary["health"] = "healthy";
    const reasons: string[] = [];
    if (roll > 0.985) {
      health = "quarantined";
      reasons.push("hardware fingerprint changed: gpu_uuids");
    } else if (roll > 0.965) {
      health = "unenrolled";
      reasons.push("awaiting enrollment");
    } else if (roll > 0.945) {
      health = "degraded";
      reasons.push("ECC errors on GPU 0", "PCIe link at gen3 x8");
    } else if (roll > 0.895) {
      health = "degraded";
      reasons.push("thermal throttling for 22m");
    } else if (roll > 0.815) {
      health = "unreachable";
      reasons.push("no heartbeat");
    }

    const offline = health === "unreachable" || health === "unenrolled";
    const derated = health === "degraded";
    const baseUtil = offline ? 0 : derated ? 30 + rand() * 25 : 55 + rand() * 42;
    const capGb = [1024, 1229, 1229, 2048][i % 4];

    nodes.push({
      id: `1a2b3c4d-0000-4000-8000-${String(i).padStart(12, "0")}`,
      name: `mn-${short.toLowerCase()}-${String((i % 17) + 1).padStart(2, "0")}`,
      siteId: `site-${short}`,
      siteShortName: short,
      health,
      lifecycle: health === "quarantined" ? "quarantined" : health === "unenrolled" ? "unenrolled" : "enrolled",
      agentVersion: rand() > 0.12 ? "1.0.0" : "0.9.4",
      lastSeenAt: new Date(
        Date.now() - (offline ? 60_000 + rand() * 40 * 3600_000 : rand() * 25_000),
      ).toISOString(),
      gpuUtil1h: offline ? new Array(60).fill(0) : utilSeries(rand, baseUtil),
      powerW: offline ? 0 : Math.round((derated ? 240 : 380) + rand() * 210),
      gpuTempC: offline ? 0 : Math.round((derated ? 82 : 62) + rand() * 9),
      txGbMonth: Math.round(rand() * 0.55 * capGb),
      capGb,
      upMbps: [35, 20, 12, 5, 25][i % 5],
      controlPlaneMb: Math.round(62 + rand() * 46),
      reasons,
      // full name kept for the detail page's header
      ...( { siteName: full } as object ),
    } as NodeSummary);
  }
  return nodes;
}

export const FLEET: NodeSummary[] = buildFleet();

export const SITES: Site[] = SITE_NAMES.map(([short, name]) => ({
  id: `site-${short}`,
  shortName: short,
  name,
  nodeCount: FLEET.filter((n) => n.siteShortName === short).length,
}));

const EVENT_POOL: Omit<NodeEvent, "id" | "t">[] = [
  { severity: "INFO", code: "agent.started", message: "agent started" },
  { severity: "INFO", code: "net.reconnected", message: "reconnected after 4m offline" },
  { severity: "WARN", code: "gpu.throttled", message: "sw_thermal_slowdown active for 6m" },
  { severity: "WARN", code: "net.cap_approaching", message: "control-plane budget at 78% with 9 days left" },
  { severity: "ERROR", code: "agent.collector_failed", message: "collector gpu timed out after 3s" },
  { severity: "CRITICAL", code: "gpu.ecc_error", message: "2 uncorrected ECC errors on GPU 0" },
  { severity: "INFO", code: "host.booted", message: "host rebooted — boot_id changed" },
  { severity: "WARN", code: "gpu.pcie_downtrained", message: "link dropped to gen3 x8" },
];

export function nodeDetail(id: string): NodeDetail {
  const summary = FLEET.find((n) => n.id === id) ?? FLEET[0];
  const rand = rng(parseInt(summary.id.slice(-6), 10) || 7);
  const site = SITE_NAMES.find(([s]) => s === summary.siteShortName)!;
  const offline = summary.health === "unreachable" || summary.health === "unenrolled";

  const series = Array.from({ length: 96 }, (_, i) => ({
    t: Date.now() - (95 - i) * 15 * 60_000,
    gpuUtil: offline ? 0 : Math.max(0, Math.min(100, summary.gpuUtil1h[i % 60] + (rand() - 0.5) * 10)),
    gpuTemp: offline ? 0 : summary.gpuTempC + (rand() - 0.5) * 7,
    powerW: offline ? 0 : summary.powerW + (rand() - 0.5) * 90,
    txMbps: offline ? 0 : 2 + rand() * (summary.upMbps * 0.5),
  }));

  const events: NodeEvent[] = Array.from({ length: 9 }, (_, i) => {
    const base = EVENT_POOL[Math.floor(rand() * EVENT_POOL.length)];
    return {
      ...base,
      id: `ev-${i}`,
      t: new Date(Date.now() - i * (1200_000 + rand() * 5400_000)).toISOString(),
    };
  });

  return {
    ...summary,
    siteName: site[1],
    address: `${100 + Math.floor(rand() * 800)} ${site[1]}, OR`,
    timezone: "America/Los_Angeles",
    isp: ISPS[Math.floor(rand() * ISPS.length)],
    planDownMbps: [300, 500, 200, 940][Math.floor(rand() * 4)],
    planUpMbps: summary.upMbps,
    hostname: summary.name,
    cpuModel: "AMD Ryzen 9 7950X 16-Core",
    cpuCores: 16,
    memTotalMb: 65536,
    kernel: "6.8.0-45-generic",
    os: "Ubuntu 24.04.1 LTS",
    boardVendor: "ASUS",
    boardProduct: "ProArt X670E-CREATOR",
    fingerprintHash: "6c7480f7675c00934b1ec9f2a0d3e8815f2c7a9b4e6d1c8f3a2b5e7d9c0f1a2b",
    enrolledAt: new Date(Date.now() - 47 * 86400_000).toISOString(),
    bootId: "c1a2b3c4-d5e6-4778-89ab-cdef01234567",
    uptimeS: offline ? 0 : 86400 * 9 + 4210,
    clockSkewMs: Math.round((rand() - 0.5) * 90),
    cpuUtilPct: offline ? 0 : 18 + rand() * 34,
    cpuTempC: offline ? 0 : 48 + rand() * 16,
    memUsedMb: offline ? 0 : Math.round(19000 + rand() * 12000),
    gpus: [
      {
        index: 0,
        uuid: "GPU-1a2b3c4d-5e6f-7081-92a3-b4c5d6e7f809",
        name: "NVIDIA RTX PRO 6000 Blackwell",
        utilPct: offline ? 0 : summary.gpuUtil1h[59],
        memUsedMb: offline ? 0 : Math.round(22000 + rand() * 40000),
        memTotalMb: 98304,
        tempC: summary.gpuTempC,
        powerW: summary.powerW,
        powerLimitW: 600,
        fanPct: offline ? 0 : Math.round(46 + rand() * 38),
        pcieGen: summary.reasons.some((r) => r.includes("PCIe")) ? 3 : 5,
        pcieWidth: summary.reasons.some((r) => r.includes("PCIe")) ? 8 : 16,
        eccErrors: summary.reasons.some((r) => r.includes("ECC")) ? 2 : 0,
        throttleReasons: summary.reasons.some((r) => r.includes("thermal")) ? ["sw_thermal_slowdown"] : [],
        driverVersion: "565.57.01",
      },
    ],
    disks: [
      { mount: "/", usedGb: 212, totalGb: 1863, smartOk: true },
      { mount: "/var/lib/docker", usedGb: 640 + Math.round(rand() * 900), totalGb: 3726, smartOk: null },
    ],
    containers: [],
    energyKwhMonth: Math.round((summary.powerW / 1000) * 24 * 18 * (0.7 + rand() * 0.3)),
    energyRatePerKwh: 0.14,
    series,
    events,
    connections: Array.from({ length: 6 }, (_, i) => ({
      at: new Date(Date.now() - i * 9 * 3600_000).toISOString(),
      event: i % 2 === 0 ? ("connected" as const) : ("disconnected" as const),
      detail: i % 2 === 0 ? "websocket established" : "no heartbeat for 45s",
    })),
  };
}

export const ALERTS: Alert[] = FLEET.filter((n) => n.reasons.length > 0)
  .slice(0, 14)
  .map((n, i) => ({
    id: `al-${i}`,
    rule: n.reasons[0].includes("thermal")
      ? "GPU thermal throttling sustained"
      : n.reasons[0].includes("ECC")
        ? "GPU ECC errors detected"
        : n.reasons[0].includes("heartbeat")
          ? "Node unreachable"
          : "Hardware re-attestation required",
    nodeId: n.id,
    nodeName: n.name,
    siteShortName: n.siteShortName,
    severity: n.health === "quarantined" ? "CRITICAL" : n.health === "degraded" ? "WARN" : "ERROR",
    state: "firing",
    openedAt: new Date(Date.now() - (i + 1) * 2700_000).toISOString(),
    lastSeenAt: new Date(Date.now() - 45_000).toISOString(),
    value: n.gpuTempC || null,
    summary: n.reasons[0],
  }));
