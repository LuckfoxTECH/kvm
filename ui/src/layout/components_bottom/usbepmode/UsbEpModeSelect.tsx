import React from 'react';
import { isMobile } from "react-device-detect";

import { UsbEpModeSelectMobile } from './UsbEpModeSelect.mobile';
import { UsbEpModeSelectPC } from './UsbEpModeSelect.pc';
import { useUsbEpModeData } from './useUsbEpModeData';

// Kept for the mobile bar. The PC bottom bar composes the USB drawer from
// useUsbEpModeData + UsbModeOptionsPanel instead.
const UsbEpModeSelect: React.FC = () => {
  const {
    usbEpMode,
    loading,
    usbDeviceConfig,
    audioMode,
    onUsbEpModeChange,
    onUsbConfigChange,
    onAudioModeChange,
  } = useUsbEpModeData();

  const Component = isMobile ? UsbEpModeSelectMobile : UsbEpModeSelectPC;

  return (
    <Component
      usbEpMode={usbEpMode}
      loading={loading}
      usbDeviceConfig={usbDeviceConfig}
      audioMode={audioMode}
      onUsbEpModeChange={onUsbEpModeChange}
      onUsbConfigChange={onUsbConfigChange}
      onAudioModeChange={onAudioModeChange}
    />
  );
};

export default UsbEpModeSelect;
