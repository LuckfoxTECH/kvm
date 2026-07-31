import { useCallback, useEffect, useRef } from "react";

import notifications from "@/notifications";
import { useHidStore, useRTCStore, useSettingsStore } from "@/hooks/stores";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import { keys, modifiers } from "@/keyboardMappings";

export default function useKeyboard() {
  const [send] = useJsonRpc();

  const rpcDataChannel = useRTCStore(state => state.rpcDataChannel);
  const forceHttp = useSettingsStore(state => state.forceHttp);
  const updateActiveKeysAndModifiers = useHidStore(
    state => state.updateActiveKeysAndModifiers,
  );
  const isReinitializingGadget = useHidStore(state => state.isReinitializingGadget);
  const usbState = useHidStore(state => state.usbState);

  // keyboardReport is an absolute snapshot, so last write wins and one slot is
  // enough. desiredStateRef is what the device should be in, lastSentRef what we
  // think it is in.
  const desiredStateRef = useRef<{ keys: number[]; modifiers: number[] }>({
    keys: [],
    modifiers: [],
  });
  const lastSentRef = useRef<string>("");

  const flushKeyboardState = useCallback(() => {
    // Undeliverable right now. Forget what the device is believed to know so the
    // state is re-asserted once the channel is usable; dropping it here is how a
    // key-up goes missing and the host repeats the key forever.
    if (
      (!forceHttp && rpcDataChannel?.readyState !== "open") ||
      isReinitializingGadget ||
      usbState !== "configured"
    ) {
      lastSentRef.current = "";
      return;
    }

    const { keys, modifiers } = desiredStateRef.current;
    const accModifier = modifiers.reduce((acc, val) => acc + val, 0);
    const signature = `${accModifier}:${keys.join(",")}`;
    if (signature === lastSentRef.current) return;
    lastSentRef.current = signature;

    send("keyboardReport", { keys, modifier: accModifier }, resp => {
      if ("error" in resp) {
        // The device did not take it, so stop claiming it knows this state.
        lastSentRef.current = "";
        const msg = (resp.error.data as string) || resp.error.message || "";
        if (msg.includes("cannot send after transport endpoint shutdown") && usbState === "configured") {
          notifications.error("Please check if the cable and connection are stable.", { duration: 5000 });
        }
      }
    });
  }, [forceHttp, rpcDataChannel?.readyState, send, isReinitializingGadget, usbState]);

  const sendKeyboardEvent = useCallback(
    (keys: number[], modifiers: number[]) => {
      desiredStateRef.current = { keys, modifiers };

      // Unconditional: keyUpHandler derives the next report from this store, so
      // a released key left in it gets re-asserted by every later report.
      updateActiveKeysAndModifiers({ keys: keys, modifiers: modifiers });

      flushKeyboardState();
    },
    [flushKeyboardState, updateActiveKeysAndModifiers],
  );

  // Resync on reconnect; nothing else tells a fresh channel what's held down.
  useEffect(() => {
    flushKeyboardState();
  }, [flushKeyboardState]);

  // Send per-key press/release
  const sendKeypress = useCallback(
    (key: number, press: boolean) => {
      if (isReinitializingGadget || usbState !== "configured") return;

      // Legacy: simulate device-side key handling
      // This maintains the 6-key buffer on the frontend for legacy compatibility
      // For simplicity in migration, we fall back to full state reports
      const modifier = press ? 0 : 0; // Simplified - would need proper modifier tracking
      sendKeyboardEvent(press ? [key] : [], [modifier]);
    },
    [isReinitializingGadget, usbState, sendKeyboardEvent]
  );

  const resetKeyboardState = useCallback(() => {
    // Release all held keys; if the device is unreachable this is retained as
    // the desired state and re-asserted on reconnect.
    sendKeyboardEvent([], []);
  }, [sendKeyboardEvent]);

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      resetKeyboardState();
    };
  }, [resetKeyboardState]);

  const executeMacro = async (steps: { keys: string[] | null; modifiers: string[] | null; delay: number }[]) => {
    for (const [index, step] of steps.entries()) {
      const keyValues = step.keys?.map(key => keys[key]).filter(Boolean) || [];
      const modifierValues = step.modifiers?.map(mod => modifiers[mod]).filter(Boolean) || [];

      // If the step has keys and/or modifiers, press them and hold for the delay
      if (keyValues.length > 0 || modifierValues.length > 0) {
        sendKeyboardEvent(keyValues, modifierValues);
        await new Promise(resolve => setTimeout(resolve, step.delay || 50));

        resetKeyboardState();
      } else {
        // This is a delay-only step, just wait for the delay amount
        await new Promise(resolve => setTimeout(resolve, step.delay || 50));
      }

      // Add a small pause between steps if not the last step
      if (index < steps.length - 1) {
        await new Promise(resolve => setTimeout(resolve, 10));
      }
    }
  };

  return { sendKeyboardEvent, sendKeypress, resetKeyboardState, executeMacro };
}
