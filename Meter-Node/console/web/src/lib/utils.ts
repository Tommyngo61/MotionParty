import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

/** Formats a number for display. Always paired with the `tnum` class. */
export function num(v: number, digits = 0): string {
  return v.toLocaleString("en-US", {
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  });
}

/**
 * Renders a duration the way an operator reads it — "4m", "3h 12m", "6d" —
 * rather than as seconds. "Last seen 412800 seconds ago" is not a sentence
 * anyone parses at a glance.
 */
export function since(iso: string, now = Date.now()): string {
  const s = Math.max(0, Math.floor((now - new Date(iso).getTime()) / 1000));
  if (s < 60) return `${s}s`;
  if (s < 3600) return `${Math.floor(s / 60)}m`;
  if (s < 86400) {
    const h = Math.floor(s / 3600);
    const m = Math.floor((s % 3600) / 60);
    return m ? `${h}h ${m}m` : `${h}h`;
  }
  const d = Math.floor(s / 86400);
  const h = Math.floor((s % 86400) / 3600);
  return h ? `${d}d ${h}h` : `${d}d`;
}

export function bytes(gb: number): string {
  if (gb >= 1000) return `${num(gb / 1000, 2)} TB`;
  if (gb < 1) return `${num(gb * 1000, 0)} MB`;
  return `${num(gb, 1)} GB`;
}
