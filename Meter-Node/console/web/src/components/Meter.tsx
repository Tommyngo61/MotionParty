import { cn, num } from "@/lib/utils";

/**
 * A bar showing a value against a ceiling.
 *
 * Used for bandwidth-against-cap and for GPU memory. It deliberately does NOT
 * turn red as it fills: red is reserved for node state, and a node using 90%
 * of the homeowner's data plan is a commercial fact, not a fault. The ceiling
 * is marked with a tick instead, which is what an operator actually looks for.
 */
export function Meter({
  value,
  max,
  label,
  unit,
  markerAt,
  markerLabel,
  className,
  tone = "accent",
}: {
  value: number;
  max: number;
  label?: string;
  unit?: string;
  markerAt?: number;
  markerLabel?: string;
  className?: string;
  tone?: "accent" | "neutral";
}) {
  const pct = Math.max(0, Math.min(100, (value / (max || 1)) * 100));
  const markerPct = markerAt != null ? Math.max(0, Math.min(100, (markerAt / (max || 1)) * 100)) : null;

  return (
    <div className={cn("w-full", className)}>
      {(label || unit) && (
        <div className="mb-1.5 flex items-baseline justify-between">
          {label && <span className="text-2xs text-[var(--text-lo)]">{label}</span>}
          {unit && (
            <span className="tnum text-xs text-[var(--text-mid)]">
              {num(value, value < 10 ? 1 : 0)}
              <span className="text-[var(--text-lo)]"> / {num(max, 0)} {unit}</span>
            </span>
          )}
        </div>
      )}
      <div className="relative h-1.5 w-full overflow-hidden rounded-full bg-[var(--color-sand-4)]">
        <div
          className="h-full rounded-full transition-[width] duration-500 ease-out"
          style={{
            width: `${pct}%`,
            background: tone === "accent" ? "var(--color-accent-9)" : "var(--color-sand-8)",
          }}
        />
        {markerPct != null && (
          <div
            className="absolute top-[-2px] h-[10px] w-px bg-[var(--color-sand-9)]"
            style={{ left: `${markerPct}%` }}
            title={markerLabel}
          />
        )}
      </div>
    </div>
  );
}
