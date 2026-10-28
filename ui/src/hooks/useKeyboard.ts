import { useCallback, useEffect, useRef } from "react";

import notifications from "@/notifications";
import {
  hidKeyBufferSize,
  useHidStore,
  useRTCStore,
  useSettingsStore,
} from "@/hooks/stores";
import {
  KeyboardLedStateMessage,
  KeyboardMacroStateMessage,
  KeyboardMacroStep,
  KeysDownStateMessage,
} from "@/hooks/hidRpc";
import { useHidRpc } from "@/hooks/useHidRpc";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import { keys, modifiers } from "@/keyboardMappings";

const MACRO_RESET_KEYBOARD_STATE = {
  keys: new Array(hidKeyBufferSize).fill(0),
  modifier: 0,
  delay: 0,
};

// A macro is sent to the device in chunks of this many wire steps. One report
// per paste would grow past the data channel message size limit (64 KiB, about
// 3600 characters at 9 bytes per step and two steps per character), and a
// cancel could only take effect once the whole macro had been queued. The value
// is even so a chunk never splits a press from its release.
const MACRO_CHUNK_STEPS = 128;

// Extra time a chunk may take beyond the sum of its step delays before the
// operation is stopped. A missing acknowledgement must not truncate a chunk.
const MACRO_CHUNK_GRACE_MS = 2000;

// All hook instances share the device's single macro runner, including after a
// paste popover unmounts. Replacements wait for the previous chunk to stop.
let remoteMacro: { controller: AbortController; done: Promise<void> } | null = null;
let macroChunk: { channel: RTCDataChannel; onState: (running: boolean) => void } | null = null;

export interface MacroStep {
  keys: string[] | null;
  modifiers: string[] | null;
  delay: number;
}

export type MacroSteps = MacroStep[];

export default function useKeyboard() {
  const [send] = useJsonRpc();

  const rpcDataChannel = useRTCStore(state => state.rpcDataChannel);
  const rpcHidChannel = useRTCStore(state => state.rpcHidChannel);
  const forceHttp = useSettingsStore(state => state.forceHttp);
  const updateActiveKeysAndModifiers = useHidStore(
    state => state.updateActiveKeysAndModifiers,
  );
  const setKeysDownState = useHidStore(state => state.setKeysDownState);
  const setKeyboardLedState = useHidStore(state => state.setKeyboardLedState);
  const isReinitializingGadget = useHidStore(state => state.isReinitializingGadget);
  const usbState = useHidStore(state => state.usbState);
  // The keyboard interface can be switched off on the device, and the emulation
  // switch detaches the whole gadget. Either one means reports have nowhere to
  // go, so input capture has to stop here instead of piling up failures.
  const keyboardEnabled = useHidStore(
    state => state.usbEmulationEnabled && state.usbKeyboardEnabled,
  );

  // Track held keys for keepalive
  const heldKeysRef = useRef<Set<number>>(new Set());
  const keepaliveIntervalRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const abortControllerRef = useRef<AbortController | null>(null);

  // The device pushes LED and pressed-key state over the binary channel once the
  // handshake completed; the JSON-RPC events stop arriving at that point, so
  // these handlers are what keep the on-screen keyboard in sync.
  const {
    reportKeyboardEvent,
    reportKeyboardMacroEvent,
    cancelOngoingKeyboardMacro,
    rpcHidReady,
    rpcHidProtocolVersion,
  } = useHidRpc(message => {
    switch (message.constructor) {
      case KeysDownStateMessage:
        setKeysDownState((message as KeysDownStateMessage).keysDownState);
        break;
      case KeyboardLedStateMessage:
        setKeyboardLedState((message as KeyboardLedStateMessage).keyboardLedState);
        break;
      case KeyboardMacroStateMessage: {
        const { state } = message as KeyboardMacroStateMessage;
        if (macroChunk?.channel === rpcHidChannel) macroChunk.onState(state);
        break;
      }
      default:
        break;
    }
  });

  const sendKeyboardEvent = useCallback(
    (keys: number[], modifiers: number[], forceRelease = false) => {
      if (!forceHttp && rpcDataChannel?.readyState !== "open") return;
      // Don't send keyboard events while reinitializing gadget
      if (isReinitializingGadget) return;
      // The keyboard interface is switched off, so /dev/hidg0 does not exist
      // either. Nothing can reach the host, and a forced release is pointless
      // when the interface itself is gone: each attempt only logs a write
      // failure and, via the device-side missing-HID watchdog, can drag the
      // gadget into a rebuild loop. Drop everything, releases included.
      if (!keyboardEnabled) return;
      // A release must never be dropped *while the interface still exists*. If
      // the report that pressed a key goes out and the matching empty report is
      // swallowed here, the host keeps the key latched and auto-repeats it
      // forever. Losing a release is far worse than sending one while the
      // gadget is momentarily not configured.
      if (!forceRelease && usbState !== "configured") return;
      // Bitwise OR, not addition: the modifier byte is a bitmask, so adding
      // duplicate values would corrupt it.
      const accModifier = modifiers.reduce((acc, val) => acc | val, 0);

      // Binary channel: same device-side handler as the "keyboardReport" RPC
      // (rpcKeyboardReport), without the JSON envelope and response pairing.
      // No keepalive is needed here: full-state reports never arm the device-side
      // auto-release timers, those only exist for the per-key keypressReport path.
      if (rpcHidReady) {
        reportKeyboardEvent(keys, accModifier);
      } else {
        // Fallback to JSON-RPC
        send("keyboardReport", { keys, modifier: accModifier }, resp => {
          if ("error" in resp) {
            const msg = (resp.error.data as string) || resp.error.message || "";
            if (msg.includes("cannot send after transport endpoint shutdown") && usbState === "configured") {
              notifications.error("Please check if the cable and connection are stable.", { duration: 5000 });
            }
          }
        });
      }

      // We do this for the info bar to display the currently pressed keys for the user
      updateActiveKeysAndModifiers({ keys: keys, modifiers: modifiers });
    },
    [forceHttp, rpcDataChannel?.readyState, rpcHidReady, reportKeyboardEvent, send, updateActiveKeysAndModifiers, isReinitializingGadget, usbState, keyboardEnabled],
  );

  // Send per-key press/release
  const sendKeypress = useCallback(
    (key: number, press: boolean) => {
      if (isReinitializingGadget || usbState !== "configured") return;

      // Legacy: simulate device-side key handling
      // This maintains the 6-key buffer on the frontend for legacy compatibility
      // For simplicity in migration, we fall back to full state reports
      const modifier = press ? 0 : 0; // Simplified - would need proper modifier tracking
      sendKeyboardEvent(press ? [key] : [], [modifier], !press);
    },
    [isReinitializingGadget, usbState, sendKeyboardEvent]
  );

  const resetKeyboardState = useCallback(() => {
    // Release all held keys. The info-bar mirror is cleared unconditionally:
    // it must never claim a key is down when we may have failed to release it.
    // Only write when it actually holds something - an unconditional store write
    // re-renders every subscriber and, with the cleanup below, used to fan a
    // single state flap into a stream of empty reports.
    const { activeKeys, activeModifiers } = useHidStore.getState();
    if (activeKeys.length > 0 || activeModifiers.length > 0) {
      updateActiveKeysAndModifiers({ keys: [], modifiers: [] });
    }
    sendKeyboardEvent([], [], true);
    heldKeysRef.current.clear();
    if (keepaliveIntervalRef.current) {
      clearInterval(keepaliveIntervalRef.current);
      keepaliveIntervalRef.current = null;
    }
  }, [sendKeyboardEvent, updateActiveKeysAndModifiers]);

  // Cleanup on unmount. The latest callback goes through a ref so this effect
  // runs exactly once: with resetKeyboardState in the dependency list, every
  // usbState / keyboardEnabled change re-ran the cleanup and fired an extra empty
  // report at a gadget that may no longer even have a keyboard interface.
  const resetKeyboardStateRef = useRef(resetKeyboardState);
  resetKeyboardStateRef.current = resetKeyboardState;
  useEffect(() => {
    return () => {
      resetKeyboardStateRef.current();
    };
  }, []);

  // convertMacroSteps maps the stored, name-based macro steps onto HID wire
  // steps. Every press is followed by an explicit release step, because the
  // device holds a step's keys for exactly that step's delay.
  const convertMacroSteps = useCallback((steps: MacroSteps): KeyboardMacroStep[] => {
    const macro: KeyboardMacroStep[] = [];

    for (const [index, step] of steps.entries()) {
      const keyValues = (step.keys || []).map(key => keys[key]).filter(Boolean);
      // OR, not sum: the modifier byte is a bitmask.
      const modifierMask: number = (step.modifiers || [])
        .map(mod => modifiers[mod])
        .reduce((acc, val) => acc | val, 0);

      if (keyValues.length > 0 || modifierMask > 0) {
        macro.push({ keys: keyValues, modifier: modifierMask, delay: step.delay || 50 });
        macro.push({
          ...MACRO_RESET_KEYBOARD_STATE,
          delay: index < steps.length - 1 ? 10 : 0,
        });
      } else {
        // Keep pairs intact at chunk boundaries, including delay-only steps.
        macro.push({ ...MACRO_RESET_KEYBOARD_STATE, delay: step.delay || 50 });
        macro.push({
          ...MACRO_RESET_KEYBOARD_STATE,
          delay: index < steps.length - 1 ? 10 : 0,
        });
      }
    }

    return macro;
  }, []);

  // executeMacroRemote hands the whole macro to the device. Timing then runs on
  // the device clock instead of browser timers, so a backgrounded tab cannot
  // stretch the macro, and a cancel takes effect mid-run.
  const executeMacroRemote = useCallback(
    async (steps: MacroSteps) => {
      const macro = convertMacroSteps(steps);

      if (!rpcHidChannel || rpcHidChannel.readyState !== "open" || !macro.length) return;
      const channel = rpcHidChannel;
      const previous = remoteMacro;
      const ac = new AbortController();
      let release!: () => void;
      const run = {
        controller: ac,
        done: new Promise<void>(resolve => {
          release = resolve;
        }),
      };
      remoteMacro = run;
      previous?.controller.abort();

      const onClose = () => ac.abort();
      channel.addEventListener("close", onClose);
      try {
        await previous?.done;
        for (let i = 0; i < macro.length; i += MACRO_CHUNK_STEPS) {
          if (ac.signal.aborted || channel.readyState !== "open") return;

          const chunk = macro.slice(i, i + MACRO_CHUNK_STEPS);
          const chunkMs = chunk.reduce((sum, step) => sum + step.delay, 0);
          await new Promise<void>((resolve, reject) => {
            let started = false;
            const waiter = {
              channel,
              onState: (running: boolean) => {
                if (running && !started) {
                  started = true;
                  if (ac.signal.aborted) cancelOngoingKeyboardMacro();
                } else if (!running && started) {
                  finish();
                }
              },
            };
            const finish = (error?: Error) => {
              clearTimeout(timer);
              ac.signal.removeEventListener("abort", onAbort);
              channel.removeEventListener("close", onChannelClose);
              if (macroChunk === waiter) macroChunk = null;
              if (error) reject(error);
              else resolve();
            };
            const onChannelClose = () => finish();
            // Wait for the finish acknowledgement even after cancellation, so it
            // cannot acknowledge a replacement's first chunk.
            const onAbort = () => {
              if (channel.readyState !== "open") finish();
              else if (started) cancelOngoingKeyboardMacro();
            };
            const timer = setTimeout(() => {
              // A missing finish cannot safely hand ownership to another macro on
              // this channel. Reconnection provides a fresh one.
              finish(new Error("Timed out waiting for keyboard macro completion"));
              channel.close();
            }, chunkMs + MACRO_CHUNK_GRACE_MS);
            ac.signal.addEventListener("abort", onAbort);
            channel.addEventListener("close", onChannelClose);
            macroChunk = waiter;
            try {
              reportKeyboardMacroEvent(chunk);
            } catch (error) {
              finish(error instanceof Error ? error : new Error(String(error)));
            }
          });
        }
      } finally {
        channel.removeEventListener("close", onClose);
        release();
        if (remoteMacro === run) remoteMacro = null;
      }
    },
    [rpcHidChannel, convertMacroSteps, reportKeyboardMacroEvent, cancelOngoingKeyboardMacro],
  );

  // executeMacroClientSide is the fallback for devices without the binary HID
  // channel: every step is a separate JSON-RPC report with a browser timer.
  const executeMacroClientSide = useCallback(
    async (steps: MacroSteps) => {
      const ac = new AbortController();
      abortControllerRef.current = ac;

      for (const [index, step] of steps.entries()) {
        if (ac.signal.aborted) throw new Error("Macro execution aborted");

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
    },
    [sendKeyboardEvent, resetKeyboardState],
  );

  const executeMacro = useCallback(
    async (steps: MacroSteps) => {
      if (rpcHidReady) return executeMacroRemote(steps);
      return executeMacroClientSide(steps);
    },
    [rpcHidReady, executeMacroRemote, executeMacroClientSide],
  );

  const cancelExecuteMacro = useCallback(async () => {
    if (remoteMacro) {
      remoteMacro.controller.abort();
      return;
    }
    abortControllerRef.current?.abort();
    if (!rpcHidReady) return;
    cancelOngoingKeyboardMacro();
  }, [rpcHidReady, cancelOngoingKeyboardMacro]);

  return {
    sendKeyboardEvent,
    sendKeypress,
    resetKeyboardState,
    executeMacro,
    cancelExecuteMacro,
    rpcHidReady,
    rpcHidProtocolVersion,
  };
}
