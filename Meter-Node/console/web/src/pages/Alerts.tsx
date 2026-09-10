import { Card, TopBar } from "@/components/Shell";
import { ALERTS } from "@/lib/fixtures";
import { num, since } from "@/lib/utils";
import { Link } from "react-router-dom";

const SEV: Record<string, { c: string; bg: string }> = {
  CRITICAL: { c: "var(--color-status-fault)", bg: "var(--color-status-fault-bg)" },
  ERROR: { c: "var(--color-status-fault)", bg: "var(--color-status-fault-bg)" },
  WARN: { c: "var(--color-status-derated)", bg: "var(--color-status-derated-bg)" },
  INFO: { c: "var(--text-lo)", bg: "var(--color-status-offline-bg)" },
};

/**
 * Alerts, grouped by site.
 *
 * Grouping is by site rather than by node because a whole-house outage is ONE
 * problem: an ISP failure at a site with four nodes should read as a single
 * row to investigate, not four separate pages.
 */
export function Alerts() {
  const bySite = ALERTS.reduce<Record<string, typeof ALERTS>>((acc, a) => {
    (acc[a.siteShortName] ||= []).push(a);
    return acc;
  }, {});

  return (
    <>
      <TopBar
        title="Alerts"
        subtitle={<span className="tnum">{num(ALERTS.length)} firing · grouped by site</span>}
      />
      <div className="space-y-3">
        {Object.entries(bySite).map(([site, alerts]) => (
          <Card key={site} bare>
            <div className="flex items-baseline gap-2 px-4 pt-4">
              <h3 className="text-xs font-semibold uppercase tracking-[0.06em] text-[var(--text-mid)]">{site}</h3>
              <span className="tnum text-2xs text-[var(--text-lo)]">{alerts.length}</span>
            </div>
            <div className="mt-2">
              {alerts.map((a) => (
                <Link
                  key={a.id}
                  to={`/nodes/${a.nodeId}`}
                  className="flex items-center gap-3 border-t border-[var(--hairline)] px-4 py-2.5 transition-colors hover:bg-[var(--color-sand-3)]"
                >
                  <span
                    className="rounded-full px-2 py-0.5 text-[10px] font-semibold"
                    style={{ background: SEV[a.severity].bg, color: SEV[a.severity].c }}
                  >
                    {a.severity}
                  </span>
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-xs font-medium text-[var(--text-hi)]">{a.rule}</div>
                    <div className="truncate text-2xs text-[var(--text-lo)]">{a.summary}</div>
                  </div>
                  <div className="tnum shrink-0 text-right">
                    <div className="text-2xs text-[var(--text-mid)]">{a.nodeName}</div>
                    <div className="text-[10px] text-[var(--text-lo)]">firing {since(a.openedAt)}</div>
                  </div>
                </Link>
              ))}
            </div>
          </Card>
        ))}
      </div>
    </>
  );
}
