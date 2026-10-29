import React, { useCallback, useEffect, useState } from "react";
import { Checkbox } from "antd";
import { useReactAt } from "i18n-auto-extractor/react";
import RefreshSvg from "@assets/second/refresh.svg?react";

import { useHidStore } from "@/hooks/stores";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import { bumpUsbEmulationEpoch, currentUsbEmulationEpoch } from "@/hooks/usbStateEpoch";
import notifications from "@/notifications";
import { StatusPanelRow, StatusPanelSection } from "@/layout/core/bar_bottom/StatusPanel";

export interface UsbFunctionsPanelProps {
  /** Extra rows rendered below the emulation switches (UAC / MTP / Disabled). */
  extended?: React.ReactNode;
  /** Dim the whole body, used when the gadget is detached. */
  dim?: boolean;
}

// UsbFunctionsPanel is the shared body of the USB drawer: gadget actions and the
// emulation switches. The per-interface function switches (keyboard, mouse, mass
// storage) are intentionally not exposed here: letting them change at runtime
// renumbers the hidg nodes and the host sees interfaces appear and vanish under
// it. The gadget composition stays fixed; only whole-gadget emulation is
// switchable. The extended choices (UAC / MTP / Disabled) are passed in by the
// caller because they differ between the two surfaces.
export default function UsbFunctionsPanel({ extended, dim = false }: UsbFunctionsPanelProps) {
  const { $at } = useReactAt();
  const [send] = useJsonRpc();

  const isReinitializingGadget = useHidStore(state => state.isReinitializingGadget);
  const setIsReinitializingGadget = useHidStore(state => state.setIsReinitializingGadget);
  const setUsbEmulationEnabled = useHidStore(state => state.setUsbEmulationEnabled);
  const [emulationEnabled, setEmulationEnabled] = useState(false);
  const [enhancedDetection, setEnhancedDetection] = useState(false);

  useEffect(() => {
    const issuedAt = currentUsbEmulationEpoch();
    send("getUsbEmulationState", {}, resp => {
      if ("error" in resp) {
        console.warn("Failed to load USB emulation state:", resp.error);
        return;
      }
      // The switch may have been flipped while this query was in flight. Its
      // answer then describes the state before that flip; applying it would
      // revert the switch and close the input gates.
      if (issuedAt !== currentUsbEmulationEpoch()) return;
      setEmulationEnabled(resp.result as boolean);
      setUsbEmulationEnabled(Boolean(resp.result));
    });
    send("getUsbEnhancedDetection", {}, resp => {
      if ("error" in resp) {
        console.warn("Failed to load USB enhanced detection:", resp.error);
        return;
      }
      setEnhancedDetection(resp.result as boolean);
    });
  }, [send, setUsbEmulationEnabled]);

  const handleReinitialize = useCallback(() => {
    if (isReinitializingGadget) return;
    setIsReinitializingGadget(true);
    send("reinitializeUsbGadget", {}, resp => {
      setIsReinitializingGadget(false);
      if ("error" in resp) {
        notifications.error(
          `Failed to reinitialize USB gadget: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      notifications.success("USB Gadget reinitialized successfully");
    });
  }, [isReinitializingGadget, send, setIsReinitializingGadget]);

  const handleEmulationToggle = useCallback(
    (enabled: boolean) => {
      // Bump before the request so any emulation query still in flight is
      // stale by definition: it cannot describe the state this switch sets.
      bumpUsbEmulationEpoch();
      send("setUsbEmulationState", { enabled }, resp => {
        if ("error" in resp) {
          notifications.error(
            `Failed to ${enabled ? "enable" : "disable"} USB emulation: ${resp.error.data || "Unknown error"}`,
          );
          return;
        }
        setEmulationEnabled(enabled);
        setUsbEmulationEnabled(enabled);
      });
    },
    [send, setUsbEmulationEnabled],
  );

  const handleEnhancedDetectionToggle = useCallback(
    (enabled: boolean) => {
      send("setUsbEnhancedDetection", { enabled }, resp => {
        if ("error" in resp) {
          notifications.error(
            `Failed to ${enabled ? "enable" : "disable"} USB enhanced detection: ${resp.error.data || "Unknown error"}`,
          );
          return;
        }
        setEnhancedDetection(enabled);
      });
    },
    [send],
  );

  const checkbox = (checked: boolean, onChange: (next: boolean) => void) => (
    <Checkbox
      checked={checked}
      disabled={dim}
      onClick={e => e.stopPropagation()}
      onChange={e => onChange(e.target.checked)}
    />
  );

  return (
    <>
      <StatusPanelRow
        label={$at("Reinitialize USB Gadget")}
        value={<RefreshSvg fontSize={13} />}
        dim={dim || isReinitializingGadget}
        onClick={handleReinitialize}
      />

      <StatusPanelRow
        label={$at("USB detection enhancement")}
        value={checkbox(enhancedDetection, handleEnhancedDetectionToggle)}
        dim={dim}
        onClick={() => handleEnhancedDetectionToggle(!enhancedDetection)}
      />
      <StatusPanelRow
        label={$at("USB Emulation")}
        value={checkbox(emulationEnabled, handleEmulationToggle)}
        dim={dim}
        onClick={() => handleEmulationToggle(!emulationEnabled)}
      />

      {emulationEnabled && extended && (
        <>
          <StatusPanelSection label={$at("Extended functions")} />
          {extended}
        </>
      )}
    </>
  );
}
