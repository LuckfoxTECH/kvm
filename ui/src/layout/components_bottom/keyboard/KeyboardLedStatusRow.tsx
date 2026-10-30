import React from "react";
import { useReactAt } from "i18n-auto-extractor/react";

import { useHidStore, useSettingsStore } from "@/hooks/stores";
import { useThemeSettings } from "@routes/login_page/useLocalAuth";

interface LedDotProps {
  on: boolean | undefined;
  label: string;
}

function LedDot({ on, label }: LedDotProps) {
  const unknown = on === undefined;
  return (
    <span className="flex items-center gap-2" style={{ fontSize: 14 }}>
      <span
        style={{
          width: 12,
          height: 12,
          borderRadius: "50%",
          backgroundColor: unknown
            ? "rgba(170, 170, 170, 0.55)"
            : on
              ? "rgba(0, 205, 27, 1)"
              : "rgba(205, 205, 205, 1)",
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          // Unknown reads as a grey question mark and nothing else. Keeping the
          // signalling in the dot means no mode ever needs a text state label.
          color: unknown ? "rgba(70, 70, 70, 0.95)" : "rgba(255, 255, 255, 0.95)",
          fontSize: 9,
          lineHeight: 1,
        }}
      >
        {unknown ? "?" : null}
      </span>
      {label}
    </span>
  );
}

// KeyboardLedStatusRow is the strip that used to sit under the phone bottom bar.
// The desktop bar renders the same three LEDs itself, so this row is drawn on
// mobile only, where the bar has no room for them. Padding is left to the drawer
// content wrapper so it lines up with the settings items below it.
export default function KeyboardLedStatusRow() {
  const { $at } = useReactAt();
  const { isDark } = useThemeSettings();
  const keyboardLedState = useHidStore(state => state.keyboardLedState);
  const keyboardLayout = useSettingsStore(state => state.keyboardLayout);
  // Host mode stays unknown until the host actually reports. Painting a guess
  // here would show the operator's own lock state as if it were the remote's.
  const ledsKnown = keyboardLedState?.reported === true;

  return (
    <div
      className="pt-2 pb-1 flex items-center gap-4"
      style={{ color: isDark ? "rgba(255,255,255,0.85)" : "rgba(0,0,0,0.85)" }}
    >
      <LedDot on={ledsKnown ? keyboardLedState?.num_lock : undefined} label={$at("Num")} />
      <LedDot on={ledsKnown ? keyboardLedState?.caps_lock : undefined} label={$at("Caps")} />
      <LedDot on={ledsKnown ? keyboardLedState?.scroll_lock : undefined} label={$at("Scroll")} />
      <span className="ml-auto opacity-70" style={{ fontSize: 14 }}>{keyboardLayout || "en_US"}</span>
    </div>
  );
}
