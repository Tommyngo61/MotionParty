import { NavLink, useLocation } from "react-router-dom";
import {
  LayoutGrid, Bell, Server, ScrollText, Package, Moon, Sun, Search,
} from "lucide-react";
import { useEffect, useState } from "react";
import { cn } from "@/lib/utils";

/**
 * The application shell.
 *
 * Composition follows RMS — a narrow left icon rail, a top bar, a content
 * container — so an installer who moves between the two products is oriented
 * immediately. The surface treatment follows the reference design: the rail
 * and bar float as translucent panels over a warm ground rather than sitting
 * as opaque chrome.
 */

const NAV = [
  { to: "/", icon: LayoutGrid, label: "Fleet", exact: true },
  { to: "/alerts", icon: Bell, label: "Alerts", badge: 14 },
  { to: "/releases", icon: Package, label: "Agent releases" },
  { to: "/audit", icon: ScrollText, label: "Audit log" },
];

function useTheme() {
  const [theme, setTheme] = useState<"light" | "dark">(() => {
    const saved = localStorage.getItem("meternode.theme");
    if (saved === "light" || saved === "dark") return saved;
    // prefers-color-scheme is honoured on the FIRST visit only; after that the
    // operator's explicit choice wins and persists.
    return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
  });

  useEffect(() => {
    document.documentElement.dataset.theme = theme;
    localStorage.setItem("meternode.theme", theme);
  }, [theme]);

  return [theme, () => setTheme((t) => (t === "light" ? "dark" : "light"))] as const;
}

export function Shell({ children }: { children: React.ReactNode }) {
  const [theme, toggleTheme] = useTheme();
  const { pathname } = useLocation();

  return (
    <div className="flex min-h-screen gap-3 p-3">
      {/* Icon rail */}
      <nav className="glass sticky top-3 flex h-[calc(100vh-1.5rem)] w-14 shrink-0 flex-col items-center rounded-shell py-4 shadow-glass">
        <div
          className="mb-6 flex h-8 w-8 items-center justify-center rounded-[10px] text-white"
          style={{ background: "var(--color-accent-9)" }}
          title="MeterNode"
        >
          <Server size={16} strokeWidth={2.2} />
        </div>

        <div className="flex flex-1 flex-col gap-1">
          {NAV.map(({ to, icon: Icon, label, badge, exact }) => {
            const active = exact ? pathname === to : pathname.startsWith(to);
            return (
              <NavLink
                key={to}
                to={to}
                title={label}
                aria-label={label}
                className={cn(
                  "relative flex h-9 w-9 items-center justify-center rounded-[10px] transition-colors",
                  active
                    ? "bg-[var(--color-accent-3)] text-[var(--color-accent-11)]"
                    : "text-[var(--text-lo)] hover:bg-[var(--color-sand-3)] hover:text-[var(--text-mid)]",
                )}
              >
                <Icon size={17} strokeWidth={1.9} />
                {badge ? (
                  <span
                    className="tnum absolute -right-0.5 -top-0.5 flex h-4 min-w-4 items-center justify-center rounded-full px-1 text-[9px] font-semibold text-white"
                    style={{ background: "var(--color-status-derated)" }}
                  >
                    {badge}
                  </span>
                ) : null}
              </NavLink>
            );
          })}
        </div>

        <button
          onClick={toggleTheme}
          aria-label={theme === "light" ? "Switch to dark theme" : "Switch to light theme"}
          className="flex h-9 w-9 items-center justify-center rounded-[10px] text-[var(--text-lo)] transition-colors hover:bg-[var(--color-sand-3)] hover:text-[var(--text-mid)]"
        >
          {theme === "light" ? <Moon size={16} strokeWidth={1.9} /> : <Sun size={16} strokeWidth={1.9} />}
        </button>
      </nav>

      <main className="min-w-0 flex-1 pb-3">{children}</main>
    </div>
  );
}

export function TopBar({
  title,
  subtitle,
  right,
}: {
  title: string;
  subtitle?: React.ReactNode;
  right?: React.ReactNode;
}) {
  return (
    <header className="mb-4 flex flex-wrap items-end justify-between gap-3">
      <div>
        <h1 className="text-[1.35rem] font-semibold leading-tight tracking-[-0.01em] text-[var(--text-hi)]">
          {title}
        </h1>
        {subtitle && <div className="mt-1 text-xs text-[var(--text-lo)]">{subtitle}</div>}
      </div>
      <div className="flex items-center gap-2">{right}</div>
    </header>
  );
}

export function SearchBox({
  value,
  onChange,
  placeholder = "Search nodes…",
}: {
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
}) {
  return (
    <div className="glass flex h-9 items-center gap-2 rounded-full px-3">
      <Search size={14} className="text-[var(--text-lo)]" strokeWidth={2} />
      <input
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        className="w-44 bg-transparent text-xs text-[var(--text-hi)] outline-none placeholder:text-[var(--text-lo)]"
      />
    </div>
  );
}

export function Card({
  title,
  action,
  children,
  className,
  bare,
  style,
}: {
  title?: string;
  action?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
  bare?: boolean;
  style?: React.CSSProperties;
}) {
  return (
    <section
      className={cn("glass rounded-card shadow-glass", bare ? "" : "p-4", className)}
      style={style}
    >
      {title && (
        <div className={cn("mb-3 flex items-center justify-between gap-2", bare && "px-4 pt-4")}>
          <h2 className="text-xs font-semibold uppercase tracking-[0.06em] text-[var(--text-lo)]">
            {title}
          </h2>
          {action}
        </div>
      )}
      {children}
    </section>
  );
}

/** Grey label, dark value, hairline between rows — straight from RMS. */
export function Row({ label, value, mono = true }: { label: string; value: React.ReactNode; mono?: boolean }) {
  return (
    <div className="flex items-baseline justify-between gap-3 border-b border-[var(--hairline)] py-2 last:border-0">
      <span className="shrink-0 text-2xs text-[var(--text-lo)]">{label}</span>
      <span className={cn("truncate text-right text-xs text-[var(--text-hi)]", mono && "tnum")}>
        {value}
      </span>
    </div>
  );
}
