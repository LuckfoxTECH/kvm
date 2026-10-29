import React, { CSSProperties } from 'react';
import USCSvg from "@assets/second/UAC.svg?react";

import BottomPopoverButton from "@components/PopoverButton";
import { dark_bd_style, dark_bg2_style } from "@/layout/theme_color";
import { USB_MODE_OPTIONS, UsbDeviceConfig } from './usbModeOptions';
import { UsbModeOptionsPanel } from './UsbModeOptionsPanel';

interface UsbEpModeSelectPCProps {
  usbEpMode: string;
  loading: boolean;
  usbDeviceConfig: UsbDeviceConfig;
  audioMode: string;
  onUsbEpModeChange: (newMode: string) => void;
  onUsbConfigChange: (devices: UsbDeviceConfig) => void;
  onAudioModeChange: (mode: string) => void;
}

// The standalone USB mode button. The bottom bar no longer mounts it: it moved
// into the USB drawer, which renders UsbModeOptionsPanel directly.
export const UsbEpModeSelectPC: React.FC<UsbEpModeSelectPCProps> = ({
  usbEpMode,
  usbDeviceConfig,
  audioMode,
  onUsbEpModeChange,
  onUsbConfigChange,
  onAudioModeChange,
}) => {
  const currentOption = USB_MODE_OPTIONS.find(opt => opt.value === usbEpMode);
  const buttonText = usbEpMode === "disabled" ? "" : (currentOption?.displayLabel || currentOption?.label || usbEpMode);
  const optionsContainerStyle: CSSProperties = {
    borderRadius: 4,
    boxShadow: '0 4px 12px rgba(0, 0, 0, 0.15)',
    minWidth: 220
  };

  return (
    <BottomPopoverButton
      buttonText={buttonText}
      buttonIconNode={<USCSvg fontSize={16} />}
      panelContent={(
        <div style={optionsContainerStyle} className={` ${dark_bg2_style} border ${dark_bd_style}`}>
          <UsbModeOptionsPanel
            usbEpMode={usbEpMode}
            usbDeviceConfig={usbDeviceConfig}
            audioMode={audioMode}
            onUsbEpModeChange={onUsbEpModeChange}
            onUsbConfigChange={onUsbConfigChange}
            onAudioModeChange={onAudioModeChange}
          />
        </div>
      )}
      align="left"
    />
  );
};

export default UsbEpModeSelectPC;
