import React from "react";
import { useReactAt } from "i18n-auto-extractor/react";
import Hdml2SVG from "@assets/second/hdml2.svg?react";
import HdmlSVG from "@assets/second/hdml.svg?react";

import { useVideoStore } from "@/hooks/stores";
import type { VideoState } from "@/hooks/stores";
import { StatusPanel, StatusPanelRow } from "@/layout/core/bar_bottom/StatusPanel";
import type { StatusTone } from "@/layout/core/bar_bottom/StatusPanel";

export interface HdmiStatusPanelProps {
  hdmiState: VideoState["hdmiState"];
  inputWidth: number;
  inputHeight: number;
  inputFps: number;
  /** Live WebRTC stats value; more current than the device event when present. */
  liveFps?: number;
}

const UNKNOWN = "---";

const STATE_LABEL: Record<VideoState["hdmiState"], string> = {
  ready: "Connected",
  connecting: "Connecting",
  no_signal: "No Signal",
  no_lock: "No Lock",
  out_of_range: "Out of Range",
};

const STATE_TONE: Record<VideoState["hdmiState"], StatusTone> = {
  ready: "ok",
  connecting: "warn",
  no_signal: "bad",
  no_lock: "bad",
  out_of_range: "bad",
};

// HdmiStatusPanel is the drawer behind the HDMI button. Resolution and frame
// rate come from the device's video input timings, so they are meaningless
// while the input is not ready - those rows fall back to "---" instead of
// showing the stale numbers of the last connected source.
export default function HdmiStatusPanel({
  hdmiState,
  inputWidth,
  inputHeight,
  inputFps,
  liveFps,
}: HdmiStatusPanelProps) {
  const { $at } = useReactAt();

  const connected = hdmiState === "ready";
  const fps = (liveFps && liveFps > 0 ? liveFps : inputFps) || 0;

  const resolution = connected && inputWidth > 0 && inputHeight > 0
    ? `${inputWidth} × ${inputHeight}`
    : UNKNOWN;
  const framerate = connected && fps > 0 ? `${fps.toFixed(1)} fps` : UNKNOWN;

  return (
    <StatusPanel
      icon={connected ? <Hdml2SVG fontSize={14} /> : <HdmlSVG fontSize={14} />}
      title={$at("HDMI")}
      badge={{ text: $at(STATE_LABEL[hdmiState]), tone: STATE_TONE[hdmiState] }}
      minWidth={240}
    >
      <StatusPanelRow label={$at("Resolution")} value={resolution} />
      <StatusPanelRow label={$at("Framerate")} value={framerate} />
    </StatusPanel>
  );
}

// HdmiStatusPanelConnected is the store-bound variant used by the mobile
// drawer, where there is no bottom bar button to pass the states down.
export function HdmiStatusPanelConnected() {
  const hdmiState = useVideoStore(state => state.hdmiState);
  const inputWidth = useVideoStore(state => state.inputWidth);
  const inputHeight = useVideoStore(state => state.inputHeight);
  const inputFps = useVideoStore(state => state.inputFps);

  return (
    <HdmiStatusPanel
      hdmiState={hdmiState}
      inputWidth={inputWidth}
      inputHeight={inputHeight}
      inputFps={inputFps}
    />
  );
}
