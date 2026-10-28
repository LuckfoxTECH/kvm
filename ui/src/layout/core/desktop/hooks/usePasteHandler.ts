import { useCallback, useEffect, useRef } from "react";

import { useJsonRpc } from "@/hooks/useJsonRpc";
import { useSettingsStore, useHidStore, useUiStore } from "@/hooks/stores";
import { keys, modifiers } from "@/keyboardMappings";
import { chars, resolveKeyboardLayoutKey } from "@/keyboardLayouts";
import notifications from "@/notifications";
import { eventMatchesShortcut } from "@/utils/shortcuts";

export const usePasteHandler = (pasteCaptureRef?: React.RefObject<HTMLTextAreaElement>) => {
  const [send] = useJsonRpc();
  const pasteShortcutEnabled = useSettingsStore(state => state.pasteShortcutEnabled);
  const pasteShortcut = useSettingsStore(state => state.pasteShortcut);
  const keyboardLayout = useSettingsStore(state => state.keyboardLayout);
  const setKeyboardLayout = useSettingsStore(state => state.setKeyboardLayout);
  const debugMode = useSettingsStore(state => state.debugMode);
  const isReinitializingGadget = useHidStore(state => state.isReinitializingGadget);
  const disableVideoFocusTrap = useUiStore(state => state.disableVideoFocusTrap);
  const setDisableVideoFocusTrap = useUiStore(state => state.setDisableVideoFocusTrap);
  const focusTrapPrevRef = useRef<boolean | null>(null);
  const focusTrapRestoreTimerRef = useRef<number | null>(null);

  const log = useCallback((...args: unknown[]) => {
    if (!debugMode) return;
    console.debug("[override-ctrlv]", ...args);
  }, [debugMode]);

  const normalizedKeyboardLayout = resolveKeyboardLayoutKey(keyboardLayout);
  const safeKeyboardLayout = normalizedKeyboardLayout ?? "en_US";

  const restoreFocusTrapNow = useCallback(() => {
    if (focusTrapRestoreTimerRef.current !== null) {
      clearTimeout(focusTrapRestoreTimerRef.current);
      focusTrapRestoreTimerRef.current = null;
    }
    if (focusTrapPrevRef.current === null) return;
    setDisableVideoFocusTrap(focusTrapPrevRef.current);
    focusTrapPrevRef.current = null;
  }, [setDisableVideoFocusTrap]);

  const scheduleFocusTrapRestore = useCallback((delayMs: number) => {
    if (focusTrapRestoreTimerRef.current !== null) {
      clearTimeout(focusTrapRestoreTimerRef.current);
    }
    focusTrapRestoreTimerRef.current = window.setTimeout(() => {
      restoreFocusTrapNow();
    }, delayMs);
  }, [restoreFocusTrapNow]);

  const ensureFocusTrapPaused = useCallback(() => {
    const didChange = !disableVideoFocusTrap;
    if (focusTrapPrevRef.current === null) {
      focusTrapPrevRef.current = disableVideoFocusTrap;
    }
    if (!disableVideoFocusTrap) {
      setDisableVideoFocusTrap(true);
    }
    scheduleFocusTrapRestore(1200);
    return didChange;
  }, [disableVideoFocusTrap, scheduleFocusTrapRestore, setDisableVideoFocusTrap]);

  useEffect(() => {
    return () => restoreFocusTrapNow();
  }, [restoreFocusTrapNow]);

  const getInvalidCharacters = useCallback((txt: string) => {
    return [
      ...new Set(
        // @ts-expect-error Intl.Segmenter is not fully typed in all envs
        [...new Intl.Segmenter().segment(txt)].map(x => x.segment).filter(ch => !chars[safeKeyboardLayout]?.[ch]),
      ),
    ];
  }, [safeKeyboardLayout]);

  const sendTextViaHID = useCallback(async (t: string) => {
    for (const ch of t) {
      const mapping = chars[safeKeyboardLayout][ch];
      if (!mapping || !mapping.key) continue;
      const { key, shift, altRight, deadKey, accentKey } = mapping;
      const keyz = [keys[key]];
      const modz = [(shift ? modifiers["ShiftLeft"] : 0) | (altRight ? modifiers["AltRight"] : 0)];
      if (deadKey) {
        keyz.push(keys["Space"]);
        modz.push(0);
      }
      if (accentKey) {
        keyz.unshift(keys[accentKey.key as keyof typeof keys]);
        modz.unshift(((accentKey.shift ? modifiers["ShiftLeft"] : 0) | (accentKey.altRight ? modifiers["AltRight"] : 0)));
      }
      for (const [index, kei] of keyz.entries()) {
        await new Promise<void>((resolve, reject) => {
          send("keyboardReport", { keys: [kei], modifier: modz[index] }, params => {
            if ("error" in params) return reject(params.error as unknown as Error);
            send("keyboardReport", { keys: [], modifier: 0 }, params => {
              if ("error" in params) return reject(params.error as unknown as Error);
              resolve();
            });
          });
        });
      }
    }
  }, [send, safeKeyboardLayout]);

  const sendTextToRemote = useCallback(async (txt: string) => {
    if (!txt) return;
    const invalid = getInvalidCharacters(txt);
    if (invalid.length > 0) {
      notifications.error(`Invalid characters: ${invalid.join(", ")}`);
      log("invalid chars", invalid, { safeKeyboardLayout, normalizedKeyboardLayout, keyboardLayout });
      return;
    }

    if (isReinitializingGadget) {
      log("blocked: isReinitializingGadget");
      return;
    }

    try {
      await sendTextViaHID(txt);
      notifications.success(`Pasted: "${txt}"`);
      log("sent text", { length: txt.length });
    } catch (err) {
      notifications.error("Failed to paste text");
      log("send failed", err);
    } finally {
      restoreFocusTrapNow();
    }
  }, [getInvalidCharacters, isReinitializingGadget, keyboardLayout, log, normalizedKeyboardLayout, restoreFocusTrapNow, safeKeyboardLayout, sendTextViaHID]);

  useEffect(() => {
    send("getKeyboardLayout", {}, resp => {
      if ("error" in resp) {
        log("getKeyboardLayout error", resp.error);
        return;
      }
      setKeyboardLayout(resp.result as string);
      log("getKeyboardLayout ok", resp.result);
    });
  }, [log, send, setKeyboardLayout]);

  useEffect(() => {
    if (!pasteShortcutEnabled) return;

    const onKeyDownCapture = (e: KeyboardEvent) => {
      if (!pasteShortcutEnabled) return;
      if (!eventMatchesShortcut(e, pasteShortcut)) return;
      if (isReinitializingGadget) return;

      const activeElement = document.activeElement as HTMLElement | null;
      const isEditable =
        !!activeElement
        && (activeElement.tagName === "INPUT"
          || activeElement.tagName === "TEXTAREA"
          || activeElement.isContentEditable);
      if (isEditable) return;

      void (async () => {
        const didChangeTrap = ensureFocusTrapPaused();
        if (didChangeTrap) {
          await new Promise<void>(resolve => setTimeout(resolve, 0));
        }
        if (navigator.clipboard?.readText) {
          try {
            const txt = await navigator.clipboard.readText();
            log("clipboard.readText ok", { length: txt.length });
            if (txt) {
              e.preventDefault();
              await sendTextToRemote(txt);
              return;
            }
          } catch (err) {
            log("clipboard.readText failed", err);
          }
        }

        const el = pasteCaptureRef?.current;
        if (!el) {
          log("pasteCaptureRef missing");
          return;
        }
        el.value = "";
        el.focus();
        const activeAfterFocus = document.activeElement as HTMLElement | null;
        if (activeAfterFocus !== el) {
          setTimeout(() => {
            el.focus();
            log("fallback refocus pasteCaptureRef", { activeTagAfterRefocus: (document.activeElement as HTMLElement | null)?.tagName });
          }, 0);
        }
      })();
    };

    document.addEventListener("keydown", onKeyDownCapture, { capture: true });
    return () => {
      document.removeEventListener("keydown", onKeyDownCapture, { capture: true });
    };
  }, [ensureFocusTrapPaused, isReinitializingGadget, log, pasteShortcutEnabled, pasteShortcut, pasteCaptureRef, safeKeyboardLayout, sendTextToRemote]);

  const handleGlobalPaste = useCallback(async (e: React.ClipboardEvent<HTMLTextAreaElement> | ClipboardEvent) => {
    if (!pasteShortcutEnabled) return;
    e.preventDefault();
    
    const clipboardData = (e as React.ClipboardEvent).clipboardData || (e as ClipboardEvent).clipboardData;
    const txt = clipboardData?.getData("text") || "";
  
    await sendTextToRemote(txt);
  }, [log, pasteShortcutEnabled, safeKeyboardLayout, sendTextToRemote]);

  return {
    handleGlobalPaste,
    pasteShortcutEnabled,
  };
};
