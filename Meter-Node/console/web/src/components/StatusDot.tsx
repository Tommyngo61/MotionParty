import { cn } from "@/lib/utils";
import type { NodeHealth } from "@/lib/types";

/**
 * The most important pixel in the product.
 *
 * Every status colour is paired with a DISTINCT SHAPE, not just a hue. State
 * has to survive colourblindness, greyscale printing, and screenshots pasted
 * into a report — all three of which happen constantly and none of which
 * preserve a red/green distinction.
 *
 *   healthy      filled disc
 *   degraded     ring with a centre dot
 *   unreachable  hollow ring, dashed
 *   quarantined  filled disc with a slash
 *   unenrolled   dotted outline
 */

const STYLE: Record<NodeHealth, { color: string; label: string }> = {
  healthy: { color: "var(--color-status-online)", label: "Healthy" },
  degraded: { color: "var(--color-status-derated)", label: "Degraded" },
  unreachable: { color: "var(--color-status-offline)", label: "Unreachable" },
  quarantined: { color: "var(--color-status-fault)", label: "Quarantined" },
  unenrolled: { color: "var(--color-status-offline)", label: "Unenrolled" },
};

export function StatusDot({
  health,
  size = 10,
  className,
}: {
  health: NodeHealth;
  size?: number;
  className?: string;
}) {
  const { color, label } = STYLE[health];

  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 12 12"
      className={cn("shrink-0", className)}
      role="img"
      aria-label={label}
    >
      <title>{label}</title>
      {health === "healthy" && <circle cx="6" cy="6" r="5" fill={color} />}

      {health === "degraded" && (
        <>
          <circle cx="6" cy="6" r="4.6" fill="none" stroke={color} strokeWidth="1.8" />
          <circle cx="6" cy="6" r="1.6" fill={color} />
        </>
      )}

      {health === "unreachable" && (
        <circle
          cx="6" cy="6" r="4.4" fill="none" stroke={color}
          strokeWidth="1.6" strokeDasharray="2.4 2"
        />
      )}

      {health === "quarantined" && (
        <>
          <circle cx="6" cy="6" r="5" fill={color} />
          <path d="M2.8 9.2 L9.2 2.8" stroke="var(--panel-solid)" strokeWidth="1.8" strokeLinecap="round" />
        </>
      )}

      {health === "unenrolled" && (
        <circle
          cx="6" cy="6" r="4.4" fill="none" stroke={color}
          strokeWidth="1.6" strokeDasharray="0.1 2.6" strokeLinecap="round"
        />
      )}
    </svg>
  );
}

export function StatusChip({ health }: { health: NodeHealth }) {
  const { label } = STYLE[health];
  const bg: Record<NodeHealth, string> = {
    healthy: "var(--color-status-online-bg)",
    degraded: "var(--color-status-derated-bg)",
    unreachable: "var(--color-status-offline-bg)",
    quarantined: "var(--color-status-fault-bg)",
    unenrolled: "var(--color-status-offline-bg)",
  };
  return (
    <span
      className="inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-2xs font-medium"
      style={{ background: bg[health], color: STYLE[health].color }}
    >
      <StatusDot health={health} size={9} />
      {label}
    </span>
  );
}

export { STYLE as STATUS_STYLE };
