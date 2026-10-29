import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { useAudioModeStore, useHidStore, useSettingsStore, useUsbEpModeStore } from "@/hooks/stores";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import notifications from "@/notifications";

import { UsbDeviceConfig, defaultUsbDeviceConfig } from "./usbModeOptions";

export interface UsbEpModeData {
  usbEpMode: string;
  loading: boolean;
  usbDeviceConfig: UsbDeviceConfig;
  audioMode: string;
  onUsbEpModeChange: (mode: string) => void;
  onUsbConfigChange: (devices: UsbDeviceConfig) => void;
  onAudioModeChange: (mode: string) => void;
}

// useUsbEpModeData owns the USB endpoint mode state that used to live inside the
// UsbEpModeSelect container. The bottom bar needs it for the USB drawer, and the
// state must be synced even while that drawer is closed, so the caller keeps one
// instance alive for the bar's lifetime instead of inside the popover panel.
export function useUsbEpModeData(): UsbEpModeData {
  const usbEpMode = useUsbEpModeStore(state => state.usbEpMode);
  const setUsbEpMode = useUsbEpModeStore(state => state.setUsbEpMode);
  const audioMode = useAudioModeStore(state => state.audioMode);
  const setAudioMode = useAudioModeStore(state => state.setAudioMode);
  const forceHttp = useSettingsStore(state => state.forceHttp);
  const setUsbKeyboardEnabled = useHidStore(state => state.setUsbKeyboardEnabled);
  const setUsbMouseEnabled = useHidStore(state => state.setUsbMouseEnabled);
  const setUsbEmulationEnabled = useHidStore(state => state.setUsbEmulationEnabled);
  const emulationEnabled = useHidStore(state => state.usbEmulationEnabled);

  // Latest authoritative values, so the input gates can be recomputed from
  // either side (device config reread, or the emulation switch) without
  // depending on the other one's render order.
  const deviceConfigRef = useRef<UsbDeviceConfig>(defaultUsbDeviceConfig);
  const emulationRef = useRef(true);

  // The input handlers read these flags to stop capturing keyboard/mouse events
  // while the matching HID interface is not attached. Without them a disabled
  // interface keeps receiving reports, and a fully stopped gadget swallows every
  // report while the host still sees a pointer it cannot move.
  const applyInputGates = useCallback(() => {
    const config = deviceConfigRef.current;
    const emulation = emulationRef.current;
    setUsbKeyboardEnabled(Boolean(config.keyboard) && emulation);
    setUsbMouseEnabled(Boolean(config.absolute_mouse || config.relative_mouse) && emulation);
  }, [setUsbKeyboardEnabled, setUsbMouseEnabled]);

  // Emulation off means the gadget is detached: there is no HID interface left to
  // send to, so local capture stops as well. One switch, both effects, and it
  // does not matter which component flipped it.
  useEffect(() => {
    emulationRef.current = emulationEnabled;
    applyInputGates();
  }, [emulationEnabled, applyInputGates]);
  const [send] = useJsonRpc();
  const [loading, setLoading] = useState(false);
  const [usbDeviceConfig, setUsbDeviceConfig] = useState<UsbDeviceConfig>(defaultUsbDeviceConfig);

  const syncUsbDeviceConfig = useCallback(() => {
    send("getUsbDevices", {}, resp => {
      if ("error" in resp) {
        console.error("Failed to load USB devices:", resp.error);
        notifications.error(
          `Failed to load USB devices: ${resp.error.data || "Unknown error"}`,
        );
      } else {
        const usbConfigState = resp.result as UsbDeviceConfig;
        setUsbDeviceConfig(usbConfigState);
        deviceConfigRef.current = usbConfigState;
        applyInputGates();
        if (usbConfigState.mtp && !usbConfigState.audio) {
          setUsbEpMode("mtp");
        } else if (usbConfigState.audio && !usbConfigState.mtp) {
          setUsbEpMode("uac");
        } else {
          setUsbEpMode("disabled");
        }
      }
    });
  }, [applyInputGates, send, setUsbEpMode]);

  // The emulation switch detaches the whole gadget, so it gates input capture as
  // well. Read it once on mount: useUsbEpModeData is kept alive for the bar's
  // lifetime, so the flag is authoritative even while the drawer is closed.
  useEffect(() => {
    send("getUsbEmulationState", {}, resp => {
      if ("error" in resp) {
        console.error("Failed to load USB emulation state:", resp.error);
        return;
      }
      setUsbEmulationEnabled(Boolean(resp.result));
    });
  }, [send, setUsbEmulationEnabled]);

  const handleUsbConfigChange = useCallback(
    (devices: UsbDeviceConfig) => {
      setLoading(true);
      // A change that touches the keyboard or the mouse detaches those HID
      // interfaces for a moment. Suspend local capture immediately instead of
      // letting reports race the reconfiguration; the resync below restores the
      // authoritative state once the gadget is back.
      const affectsInput =
        devices.keyboard !== usbDeviceConfig.keyboard ||
        devices.absolute_mouse !== usbDeviceConfig.absolute_mouse ||
        devices.relative_mouse !== usbDeviceConfig.relative_mouse;
      if (affectsInput) {
        setUsbKeyboardEnabled(false);
        setUsbMouseEnabled(false);
      }
      send("setUsbDevices", { devices }, async resp => {
        if ("error" in resp) {
          notifications.error(
            `Failed to set usb devices: ${resp.error.data || "Unknown error"}`,
          );
          setLoading(false);
          return;
        }
        await new Promise(resolve => setTimeout(resolve, 2000));
        setLoading(false);
        syncUsbDeviceConfig();
        notifications.success(`USB Devices updated`);
      });
    },
    [send, syncUsbDeviceConfig, usbDeviceConfig, setUsbKeyboardEnabled, setUsbMouseEnabled],
  );

  const handleAudioModeChange = useCallback(
    (mode: string) => {
      send("setAudioMode", { mode }, resp => {
        if ("error" in resp) {
          notifications.error(
            `Failed to set Audio Mode: ${resp.error.data || "Unknown error"}`,
          );
          return;
        }

        notifications.success(`Audio Mode set to ${mode}. It takes effect after refreshing the page`);
        setAudioMode(mode);
        window.location.reload();
      });
    },
    [send, setAudioMode],
  );

  const handleUsbEpModeChange = useCallback(
    (newMode: string) => {
      setUsbEpMode(newMode);
    },
    [setUsbEpMode],
  );

  useEffect(() => {
    syncUsbDeviceConfig();

    send("getAudioMode", {}, resp => {
      if ("error" in resp) return;
      setAudioMode(String(resp.result));
    });
  }, [syncUsbDeviceConfig, send, setAudioMode]);

  const effectiveUsbEpMode = useMemo(() => {
    if (forceHttp && usbEpMode === "uac") {
      return "disabled";
    }
    return usbEpMode;
  }, [forceHttp, usbEpMode]);

  useEffect(() => {
    if (!forceHttp) return;
    if (usbEpMode !== "uac") return;

    const nextConfig: UsbDeviceConfig = {
      ...usbDeviceConfig,
      audio: false,
      mtp: false,
    };

    handleUsbConfigChange(nextConfig);
    setUsbEpMode("disabled");
    if (audioMode !== "disabled") {
      handleAudioModeChange("disabled");
    }
  }, [forceHttp, usbEpMode, usbDeviceConfig, handleUsbConfigChange, setUsbEpMode, audioMode, handleAudioModeChange]);

  const filteredUsbDeviceConfig = useMemo<UsbDeviceConfig>(() => {
    if (!forceHttp) return usbDeviceConfig;
    return {
      ...usbDeviceConfig,
      audio: false,
    };
  }, [forceHttp, usbDeviceConfig]);

  return {
    usbEpMode: effectiveUsbEpMode,
    loading,
    usbDeviceConfig: filteredUsbDeviceConfig,
    audioMode,
    onUsbEpModeChange: handleUsbEpModeChange,
    onUsbConfigChange: handleUsbConfigChange,
    onAudioModeChange: handleAudioModeChange,
  };
}

export default useUsbEpModeData;
