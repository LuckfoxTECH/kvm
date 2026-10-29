import React from "react";

import { dark_bd_style, dark_bg2_style } from "@/layout/theme_color";
import { useThemeSettings } from "@routes/login_page/useLocalAuth";

export type StatusTone = "ok" | "warn" | "bad" | "idle";

export interface StatusPanelProps {
  icon?: React.ReactNode;
  title: string;
  badge?: { text: string; tone: StatusTone };
  minWidth?: number;
  children: React.ReactNode;
}

// StatusPanel is the shared shell of the three bottom bar drawers (HDMI, USB,
// Remote). Having one shell is what makes them read as the same family: header
// strip with icon, title and a state badge, then full-width rows underneath.
export function StatusPanel({ icon, title, badge, minWidth = 240, children }: StatusPanelProps) {
  const { isDark } = useThemeSettings();

  const toneStyle = (tone: StatusTone): React.CSSProperties => {
    switch (tone) {
      case "ok":
        return { background: "rgba(0,205,27,0.16)", color: isDark ? "#6ef07c" : "#1c8a2b" };
      case "warn":
        return { background: "rgba(250,173,20,0.16)", color: isDark ? "#ffc957" : "#946200" };
      case "bad":
        return { background: "rgba(255,77,79,0.16)", color: isDark ? "#ff8a8c" : "#c02022" };
      default:
        return {
          background: isDark ? "rgba(255,255,255,0.08)" : "rgba(0,0,0,0.06)",
          color: isDark ? "rgba(255,255,255,0.55)" : "rgba(0,0,0,0.55)",
        };
    }
  };

  return (
    <div
      style={{ borderRadius: 5, boxShadow: "0 6px 18px rgba(0,0,0,0.28)", minWidth }}
      className={`${dark_bg2_style} border ${dark_bd_style} overflow-hidden`}
    >
      <div
        className={`flex items-center gap-2 px-3 py-[7px] border-b ${dark_bd_style}`}
        style={{
          background: isDark ? "rgba(255,255,255,0.05)" : "rgba(0,0,0,0.03)",
          color: isDark ? "rgba(255,255,255,0.9)" : "rgba(0,0,0,0.85)",
        }}
      >
        {icon && <span className="flex items-center">{icon}</span>}
        <span style={{ fontSize: 12, fontWeight: 600 }}>{title}</span>
        <span className="flex-1" />
        {badge && (
          <span
            style={{
              fontSize: 10.5,
              lineHeight: "15px",
              padding: "0 7px",
              borderRadius: 9,
              whiteSpace: "nowrap",
              ...toneStyle(badge.tone),
            }}
          >
            {badge.text}
          </span>
        )}
      </div>
      {children}
    </div>
  );
}

export interface StatusPanelRowProps {
  label: React.ReactNode;
  value?: React.ReactNode;
  onClick?: () => void;
  /** Render a small dot in front of the label. */
  dotColor?: string;
  dim?: boolean;
}

export function StatusPanelRow({ label, value, onClick, dotColor, dim }: StatusPanelRowProps) {
  const { isDark } = useThemeSettings();
  const [hover, setHover] = React.useState(false);

  return (
    <div
      className={`flex items-center justify-between px-3 py-[7px] border-b last:border-b-0 ${dark_bd_style}`}
      style={{
        cursor: onClick ? "pointer" : "default",
        background: onClick && hover
          ? (isDark ? "rgba(255,255,255,0.08)" : "rgba(0,0,0,0.04)")
          : "transparent",
        color: isDark ? "rgba(255,255,255,0.85)" : "rgba(0,0,0,0.85)",
        fontSize: 12,
      }}
      onClick={onClick}
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
    >
      <span className="flex items-center" style={{ opacity: dim ? 0.55 : 1 }}>
        {dotColor && (
          <span
            style={{
              width: 7,
              height: 7,
              borderRadius: "50%",
              backgroundColor: dotColor,
              marginRight: 8,
              display: "inline-block",
              flex: "none",
            }}
          />
        )}
        {label}
      </span>
      {value !== undefined && (
        <span style={{ fontVariantNumeric: "tabular-nums", opacity: dim ? 0.55 : 1 }}>{value}</span>
      )}
    </div>
  );
}

// StatusPanelSection is the small uppercase sub-heading used to group rows
// (for example "MODE" above the USB mode choices).
export function StatusPanelSection({ label }: { label: string }) {
  const { isDark } = useThemeSettings();
  return (
    <div
      className={`px-3 pt-2 pb-1 border-t ${dark_bd_style}`}
      style={{
        fontSize: 10,
        letterSpacing: "0.09em",
        textTransform: "uppercase",
        color: isDark ? "rgba(255,255,255,0.4)" : "rgba(0,0,0,0.45)",
      }}
    >
      {label}
    </div>
  );
}
