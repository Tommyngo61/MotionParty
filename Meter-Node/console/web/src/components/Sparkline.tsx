import { useId } from "react";

/**
 * Hand-rolled rather than drawn with a chart library, for two reasons.
 *
 * Two hundred of these render at once on the fleet grid, and two hundred
 * Recharts instances is a slideshow. And the brief's chart rules — thin
 * strokes, no fills, no gradients, minimal gridlines — are easier to honour in
 * fifteen lines of SVG than by overriding a library's defaults one prop at a
 * time.
 */
export function Sparkline({
  data,
  width = 148,
  height = 30,
  stroke = "var(--color-series-1)",
  muted = false,
}: {
  data: number[];
  width?: number;
  height?: number;
  stroke?: string;
  muted?: boolean;
}) {
  const id = useId();
  if (data.length < 2) return <svg width={width} height={height} aria-hidden />;

  const max = Math.max(...data, 1);
  const min = Math.min(...data, 0);
  const span = max - min || 1;
  const pad = 2;
  const h = height - pad * 2;

  const pts = data.map((v, i) => {
    const x = (i / (data.length - 1)) * width;
    const y = pad + h - ((v - min) / span) * h;
    return [x, y] as const;
  });

  const d = pts.map(([x, y], i) => `${i === 0 ? "M" : "L"}${x.toFixed(1)},${y.toFixed(1)}`).join(" ");

  return (
    <svg
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      preserveAspectRatio="none"
      aria-hidden
      className="overflow-visible"
    >
      <path
        d={d}
        fill="none"
        stroke={muted ? "var(--color-sand-7)" : stroke}
        strokeWidth="1.25"
        strokeLinejoin="round"
        strokeLinecap="round"
        vectorEffect="non-scaling-stroke"
        id={id}
      />
    </svg>
  );
}
