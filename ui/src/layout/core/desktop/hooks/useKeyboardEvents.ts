import { useCallback } from "react";

import  useKeyboard  from "@/hooks/useKeyboard";
import { useHidStore, useSettingsStore, useUiStore } from "@/hooks/stores";
import { keys, modifiers } from "@/keyboardMappings";
import { eventMatchesShortcut } from "@/utils/shortcuts";

// getAdjustedKeyCode normalises the browser's physical key code before it is
// looked up in the usage table. The browser reports ISO/JIS positions under
// their own names (IntlYen, IntlRo, Convert, NonConvert) while keyboardMappings
// keys them as Yen, KeyRO, Henkan, Muhenkan. Without this step those keys
// resolve to undefined and the filter(Boolean) below silently drops the whole
// report, so a real JIS keyboard produces nothing for yen, ro, convert or
// non-convert.
//
// appleImeKeys switches the two IME positions between the Apple and the Windows
// spellings: an Apple JIS keyboard labels them 英数 / かな and sends LANG2 (0x91)
// / LANG1 (0x90), while a Windows JIS keyboard labels the same positions 無変換 /
// ひらがな and sends Muhenkan (0x8b) / KatakanaHiragana (0x88). Both report the
// same browser codes, so the target usage has to follow the selected layout.
function getAdjustedKeyCode(e: KeyboardEvent, appleImeKeys: boolean) {
  const key = e.key;
  let code = e.code;

  // the same physical position is reported under two codes depending on the
  // layout: ISO/UK says IntlBackslash where US says Backquote
  if (code == "IntlBackslash" && ["`", "~"].includes(key)) {
    code = "Backquote";
  } else if (code == "Backquote" && ["§", "±"].includes(key)) {
    code = "IntlBackslash";
  }
  // Japanese 106/109
  else if (code === "IntlYen") {
    code = "Yen";
  } else if (code === "IntlRo") {
    code = "KeyRO";
  } else if (code === "Convert") {
    code = "Henkan";
  } else if (code === "NonConvert") {
    code = appleImeKeys ? "Lang2" : "Muhenkan";
  }
  // The kana position. UI Events calls it KanaMode and macOS-derived browsers
  // report Kana; neither exists in the usage table, so without this branch
  // keys[code] is undefined and filter(Boolean) drops the whole report -- which
  // is why the kana key looked dead and never showed up in the info bar.
  else if (code === "KanaMode" || code === "Kana") {
    code = appleImeKeys ? "Lang1" : "KatakanaHiragana";
  }

  return code;
}

export const useKeyboardEvents = (
  pasteCaptureRef?: React.RefObject<HTMLTextAreaElement>,
  isReinitializingGadget?: boolean
) => {
  const { sendKeyboardEvent, resetKeyboardState } = useKeyboard();
  const { setIsNumLockActive, setIsCapsLockActive, setIsScrollLockActive } = useHidStore();

  const keyboardLedSync = useSettingsStore(state => state.keyboardLedSync);
  const isKeyboardLedManagedByHost = keyboardLedSync === "host";
  const pasteShortcutEnabled = useSettingsStore(state => state.pasteShortcutEnabled);
  const pasteShortcut = useSettingsStore(state => state.pasteShortcut);
  const isOcrMode = useUiStore(state => state.isOcrMode);
  const keyboardLayout = useSettingsStore(state => state.keyboardLayout);
  const appleImeKeys =
    String(keyboardLayout ?? "").replace(/_/g, "-").toLowerCase() === "ja-jp-mac";

  const handleModifierKeys = useCallback((e: KeyboardEvent, activeModifiers: number[]) => {
    const { shiftKey, ctrlKey, altKey, metaKey } = e;
    const filteredModifiers = activeModifiers.filter(Boolean);

    return filteredModifiers
      .filter(modifier => shiftKey || (modifier !== modifiers["ShiftLeft"] && modifier !== modifiers["ShiftRight"]))
      .filter(modifier => ctrlKey || (modifier !== modifiers["ControlLeft"] && modifier !== modifiers["ControlRight"]))
      .filter(modifier => altKey || (modifier !== modifiers["AltLeft"] && modifier !== modifiers["AltRight"]))
      .filter(modifier => metaKey || (modifier !== modifiers["MetaLeft"] && modifier !== modifiers["MetaRight"]));
  }, []);

  const keyDownHandler = useCallback(async (e: KeyboardEvent) => {
    if (isOcrMode) return;
    if (pasteShortcutEnabled && eventMatchesShortcut(e, pasteShortcut)) {
      if (isReinitializingGadget) return;
      if (pasteCaptureRef && pasteCaptureRef.current) {
        pasteCaptureRef.current.value = "";
        pasteCaptureRef.current.focus();
      }
      return;
    }
    if (isReinitializingGadget) return;

    if (e.repeat) {
      e.preventDefault();
      return;
    }

    e.preventDefault();
    const prev = useHidStore.getState();
    const code = getAdjustedKeyCode(e, appleImeKeys);

    if (!isKeyboardLedManagedByHost) {
      setIsNumLockActive(e.getModifierState("NumLock"));
      setIsCapsLockActive(e.getModifierState("CapsLock"));
      setIsScrollLockActive(e.getModifierState("ScrollLock"));
    }

    const newKeys = [...prev.activeKeys, keys[code]].filter(Boolean);
    // A key whose browser code has no usage-table entry is dropped by
    // filter(Boolean) above and the whole report silently disappears, which is
    // indistinguishable from "the device is broken" in the UI. Surface it.
    if (keys[code] === undefined) {
      console.warn(
        `[keyboard] unknown key code "${code}" (key="${e.key}") - report dropped`,
      );
    }
    const newModifiers = handleModifierKeys(e, [...prev.activeModifiers, modifiers[code]]);

    if (e.metaKey) {
      setTimeout(() => {
        const prev = useHidStore.getState();
        sendKeyboardEvent([], newModifiers || prev.activeModifiers, true);
      }, 10);
    }

    sendKeyboardEvent([...new Set(newKeys)], [...new Set(newModifiers)]);
  }, [handleModifierKeys, sendKeyboardEvent, isKeyboardLedManagedByHost, setIsNumLockActive, setIsCapsLockActive, setIsScrollLockActive, pasteShortcutEnabled, pasteShortcut, pasteCaptureRef, isReinitializingGadget, isOcrMode, appleImeKeys]);

  const keyUpHandler = useCallback((e: KeyboardEvent) => {
    if (isOcrMode) return;
    if (isReinitializingGadget) return;
    e.preventDefault();
    const prev = useHidStore.getState();
    // the same normalisation as keydown, otherwise a key pressed under its raw
    // code is looked up and released under its normalised one
    const code = getAdjustedKeyCode(e, appleImeKeys);

    if (!isKeyboardLedManagedByHost) {
      setIsNumLockActive(e.getModifierState("NumLock"));
      setIsCapsLockActive(e.getModifierState("CapsLock"));
      setIsScrollLockActive(e.getModifierState("ScrollLock"));
    }

    const newKeys = prev.activeKeys.filter(k => k !== keys[code]).filter(Boolean);
    const newModifiers = handleModifierKeys(
      e,
      prev.activeModifiers.filter(k => k !== modifiers[code]),
    );

    // Always forced: a release that gets swallowed leaves the key latched on
    // the host, which then auto-repeats it.
    sendKeyboardEvent([...new Set(newKeys)], [...new Set(newModifiers)], true);
  }, [handleModifierKeys, sendKeyboardEvent, isKeyboardLedManagedByHost, setIsNumLockActive, setIsCapsLockActive, setIsScrollLockActive, isOcrMode, isReinitializingGadget, appleImeKeys]);

  const setupKeyboardEvents = useCallback(() => {
    const abortController = new AbortController();
    const signal = abortController.signal;

    document.addEventListener("keydown", keyDownHandler, { signal });
    document.addEventListener("keyup", keyUpHandler, { signal });
    window.addEventListener("blur", resetKeyboardState, { signal });
    document.addEventListener("visibilitychange", resetKeyboardState, { signal });

    return () => abortController.abort();
  }, [keyDownHandler, keyUpHandler, resetKeyboardState]);

  return {
    setupKeyboardEvents,
  };
};
