import React from "react";

import { selected_bt_bg } from "@/layout/theme_color";

interface BarIconButtonProps {
  icon: React.ReactNode;
  label: string;
  active?: boolean;
  onClick: () => void;
  style?: React.CSSProperties;
}

/**
 * BarIconButton is the icon-only, click-to-act sibling of BottomPopoverButton:
 * same 24px footprint, same hover wash, and the name slides out on hover so the
 * bar stays compact without hiding what each icon does.
 */
export default function BarIconButton({ icon, label, active, onClick, style }: BarIconButtonProps) {
  return (
    <div
      onClick={onClick}
      style={{ display: "flex", justifyContent: "center", alignItems: "center", ...style }}
    >
      <div
        className={`group flex items-center justify-center text-xs h-[24px] cursor-pointer hover:bg-[rgba(0,0,0,0.06)] dark:hover:bg-[rgba(255,255,255,0.06)] transition-colors ${active ? selected_bt_bg : ""}`}
      >
        <div className="flex items-center justify-center pl-1.5 pr-1.5">{icon}</div>
        <span
          className="overflow-hidden whitespace-nowrap max-w-0 opacity-0 transition-all duration-200 ease-out group-hover:opacity-100 group-hover:max-w-[160px] group-hover:pr-2"
          style={{ position: "relative", top: "1px" }}
        >
          {label}
        </span>
      </div>
    </div>
  );
}
