import React from "react";
import { CheckOutlined } from "@ant-design/icons";

import { useThemeSettings } from "@routes/login_page/useLocalAuth";
import { dark_bd_style } from "@/layout/theme_color";
import { useSettingsStore } from "@/hooks/stores";
import { USB_MODE_OPTIONS, UsbDeviceConfig } from "./usbModeOptions";

export interface UsbModeOptionsPanelProps {
  usbEpMode: string;
  usbDeviceConfig: UsbDeviceConfig;
  audioMode: string;
  onUsbEpModeChange: (newMode: string) => void;
  onUsbConfigChange: (devices: UsbDeviceConfig) => void;
  onAudioModeChange: (mode: string) => void;
  style?: React.CSSProperties;
}

// UsbModeOptionsPanel is just the option rows: the bar renders it inside the USB
// drawer, while the standalone UsbEpModeSelect button wraps it in its own
// popover. Keeping the rows here means both paths share one behaviour.
export const UsbModeOptionsPanel: React.FC<UsbModeOptionsPanelProps> = ({
  usbEpMode,
  usbDeviceConfig,
  audioMode,
  onUsbEpModeChange,
  onUsbConfigChange,
  onAudioModeChange,
  style,
}) => {
  const { isDark } = useThemeSettings();
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

  const optionItemStyle: React.CSSProperties = {
    padding: "7px 12px",
    cursor: "pointer",
    backgroundColor: "transparent",
    color: isDark ? "rgba(255, 255, 255, 0.85)" : "rgba(0, 0, 0, 0.85)",
    display: "flex",
    justifyContent: "space-between",
    alignItems: "center",
    fontSize: "12px",
  };

  const hoverColor = isDark ? "rgba(255, 255, 255, 0.08)" : "rgba(0, 0, 0, 0.04)";

  return (
    <div style={style}>
      {USB_MODE_OPTIONS.filter(option => !forceHttp || option.value !== "uac").map(option => {
        const isSelected = option.value === usbEpMode;
        return (
          <div
            key={option.value}
            style={optionItemStyle}
            className={`border-b last:border-b-0 ${dark_bd_style}`}
            onClick={() => handleUsbEpModeChange(option.value)}
            onMouseEnter={e => {
              e.currentTarget.style.backgroundColor = hoverColor;
            }}
            onMouseLeave={e => {
              e.currentTarget.style.backgroundColor = optionItemStyle.backgroundColor!;
            }}
          >
            <span style={{ fontSize: "12px" }}>{option.label}</span>
            {isSelected && (
              <CheckOutlined style={{ fontSize: "12px", color: isDark ? "#fff" : "#999" }} />
            )}
          </div>
        );
      })}
    </div>
  );
};

export default UsbModeOptionsPanel;
