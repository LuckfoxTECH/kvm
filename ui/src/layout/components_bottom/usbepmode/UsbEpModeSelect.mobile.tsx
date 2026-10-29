import React from "react";
import { useReactAt } from "i18n-auto-extractor/react";
import UsbSVG from "@assets/second/usb.svg?react";
import Usb2SVG from "@assets/second/usb2.svg?react";

import { useHidStore, useSettingsStore } from "@/hooks/stores";
import SharedFolders from "@/layout/components_side/SharedFolders";
import { StatusPanel, StatusPanelRow } from "@/layout/core/bar_bottom/StatusPanel";
import VolumeControl from "@components/VolumeControl";

import UsbFunctionsPanel from "./UsbFunctionsPanel";
import { USB_MODE_OPTIONS, UsbDeviceConfig } from "./usbModeOptions";

interface UsbEpModeSelectMobileProps {
  usbEpMode: string;
  loading: boolean;
  usbDeviceConfig: UsbDeviceConfig;
  audioMode: string;
  onUsbEpModeChange: (newMode: string) => void;
  onUsbConfigChange: (devices: UsbDeviceConfig) => void;
  onAudioModeChange: (mode: string) => void;
}

// UsbEpModeSelectMobile is the phone sized USB drawer. It mirrors the desktop
// drawer: one card with the gadget state in the header, the shared USB body
// (reinitialize, emulation switches, extended functions) and finally the
// settings that belong to the selected extended mode.
export const UsbEpModeSelectMobile: React.FC<UsbEpModeSelectMobileProps> = ({
  usbEpMode,
  loading,
  usbDeviceConfig,
  audioMode,
  onUsbEpModeChange,
  onUsbConfigChange,
  onAudioModeChange,
}) => {
  const { $at } = useReactAt();
  const usbState = useHidStore(state => state.usbState);
  const emulationEnabled = useHidStore(state => state.usbEmulationEnabled);
  const forceHttp = useSettingsStore(state => state.forceHttp);

  const handleUsbEpModeChange = async (newMode: string) => {
    onUsbEpModeChange(newMode);

    if (newMode === "uac") {
      onUsbConfigChange({
        ...usbDeviceConfig,
        audio: true,
        mtp: false,
      });
      onAudioModeChange("usb");
    } else if (newMode === "mtp") {
      onUsbConfigChange({
        ...usbDeviceConfig,
        audio: false,
        mtp: true,
      });
      if (audioMode !== "disabled") {
        onAudioModeChange("disabled");
      }
    } else {
      onUsbConfigChange({
        ...usbDeviceConfig,
        audio: false,
        mtp: false,
      });
      if (audioMode !== "disabled") {
        onAudioModeChange("disabled");
      }
    }
  };

  const configured = usbState === "configured";
  const modeOptions = USB_MODE_OPTIONS.filter(option => !forceHttp || option.value !== "uac");

  return (
    <div className="w-full flex flex-col">
      <StatusPanel
        icon={
          !emulationEnabled ? (
            <UsbSVG style={{ color: "rgba(250, 173, 20, 1)" }} />
          ) : configured ? (
            <Usb2SVG />
          ) : (
            <UsbSVG />
          )
        }
        title={$at("USB")}
        badge={
          !emulationEnabled
            ? { text: $at("Not started"), tone: "warn" }
            : {
                text: configured ? $at("Connected") : $at("Not Attached"),
                tone: configured ? "ok" : "idle",
              }
        }
        minWidth={0}
      >
        <UsbFunctionsPanel
          extended={
            <>
              {modeOptions.map(option => (
                <StatusPanelRow
                  key={option.value}
                  label={option.label}
                  value={usbEpMode === option.value ? "✓" : undefined}
                  dim={loading}
                  onClick={() => !loading && handleUsbEpModeChange(option.value)}
                />
              ))}
            </>
          }
        />
      </StatusPanel>

      {/* Settings of the selected mode live outside the card, so they keep the
          same page-like layout as the other phone drawers (Virtual Media). */}
      {usbEpMode === "uac" && (
        <div className="w-full pt-4">
          <VolumeControl size="XL" theme="light" fullWidth={true} className="w-full" />
        </div>
      )}

      {usbEpMode === "mtp" && (
        <div className="w-full pt-4">
          <SharedFolders />
        </div>
      )}
    </div>
  );
};

export default UsbEpModeSelectMobile;
