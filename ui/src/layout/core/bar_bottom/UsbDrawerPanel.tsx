import React from "react";
import { useReactAt } from "i18n-auto-extractor/react";
import UsbSVG from "@assets/second/usb.svg?react";
import Usb2SVG from "@assets/second/usb2.svg?react";

import { StatusPanel } from "@/layout/core/bar_bottom/StatusPanel";
import { useHidStore } from "@/hooks/stores";
import UsbFunctionsPanel from "@/layout/components_bottom/usbepmode/UsbFunctionsPanel";
import UsbModeOptionsPanel from "@/layout/components_bottom/usbepmode/UsbModeOptionsPanel";
import type { UsbDeviceConfig } from "@/layout/components_bottom/usbepmode/usbModeOptions";

export interface UsbDrawerPanelProps {
  usbState: string;
  usbEpMode: string;
  usbDeviceConfig: UsbDeviceConfig;
  audioMode: string;
  onUsbEpModeChange: (mode: string) => void;
  onUsbConfigChange: (devices: UsbDeviceConfig) => void;
  onAudioModeChange: (mode: string) => void;
}

function stateLabel(usbState: string) {
  switch (usbState) {
    case "configured":
      return "Connected";
    case "attached":
      return "Attached";
    case "addressed":
      return "Addressed";
    case "suspended":
      return "Suspended";
    case "default":
      return "Default";
    default:
      return "Not Attached";
  }
}

// UsbDrawerPanel is the drawer behind the USB button: gadget state on top, then
// the shared USB body (reinitialize, emulation switches) and finally the
// extended choices.
export default function UsbDrawerPanel({
  usbState,
  usbEpMode,
  usbDeviceConfig,
  audioMode,
  onUsbEpModeChange,
  onUsbConfigChange,
  onAudioModeChange,
}: UsbDrawerPanelProps) {
  const { $at } = useReactAt();
  const emulationEnabled = useHidStore(state => state.usbEmulationEnabled);

  const configured = usbState === "configured";

  return (
    <StatusPanel
      icon={
        !emulationEnabled ? (
          <UsbSVG fontSize={14} style={{ color: "rgba(250, 173, 20, 1)" }} />
        ) : configured ? (
          <Usb2SVG fontSize={14} />
        ) : (
          <UsbSVG fontSize={14} />
        )
      }
      title={$at("USB")}
      badge={
        !emulationEnabled
          ? { text: $at("Not started"), tone: "warn" }
          : { text: $at(stateLabel(usbState)), tone: configured ? "ok" : "idle" }
      }
      minWidth={240}
    >
      <UsbFunctionsPanel
        extended={
          <UsbModeOptionsPanel
            usbEpMode={usbEpMode}
            usbDeviceConfig={usbDeviceConfig}
            audioMode={audioMode}
            onUsbEpModeChange={onUsbEpModeChange}
            onUsbConfigChange={onUsbConfigChange}
            onAudioModeChange={onAudioModeChange}
          />
        }
      />
    </StatusPanel>
  );
}
