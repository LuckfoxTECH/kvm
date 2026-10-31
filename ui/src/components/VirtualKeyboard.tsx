import { Switch , Button as AntdButton } from "antd";
import { LockClosedIcon } from "@heroicons/react/16/solid";
import { useShallow } from "zustand/react/shallow";
import { AnimatePresence, motion } from "framer-motion";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import Keyboard from "react-simple-keyboard";
import { useReactAt } from "i18n-auto-extractor/react";
import {  isMobile } from "react-device-detect";

import Card from "@components/Card";
import "react-simple-keyboard/build/css/index.css";

import DetachIconRaw from "@/assets/detach-icon.svg";
import { cx } from "@/cva.config";
import { useHidStore, useSettingsStore, useUiStore } from "@/hooks/stores";
import useKeyboard from "@/hooks/useKeyboard";
import useKeyboardLayout from "@/hooks/useKeyboardLayout";
import { keyDisplayMap2, keys, modifiers, sKeyDisplayMap, latchingKeys } from "@/keyboardMappings";
import { dark_bg2_style} from "@/layout/theme_color";

import GoBottomSvg from "@/assets/second/gobottom.svg?react";
import { applyFormFactor } from "@/keyboardFormFactor";

export const DetachIcon = ({ className }: { className?: string }) => {
  return <img src={DetachIconRaw} alt="Detach Icon" className={className} />;
};

function KeyboardWrapper() {
  const { $at } = useReactAt();
  const [layoutName, setLayoutName] = useState("default");

  const keyboardRef = useRef<HTMLDivElement>(null);
  const showAttachedVirtualKeyboard = useUiStore(
    state => state.isAttachedVirtualKeyboardVisible,
  );

  const { sendKeyboardEvent, resetKeyboardState } = useKeyboard();
  const { selectedKeyboard } = useKeyboardLayout();
  const keyboardFormFactor = useSettingsStore(state => state.keyboardFormFactor);

  // The mobile keyboard is a fixed four-row layout, so it never runs the full
  // applyFormFactor row rewrite. It only needs the extra keys each form factor
  // brings: ISO adds IntlBackslash, JIS adds Yen/KeyRO and the IME toggles.
  const formFactorIsJIS = keyboardFormFactor === "JIS";
  const formFactorIsISO = keyboardFormFactor === "ISO";

  const keyDisplayMap = useMemo(() => {
    // The layout's own map wins; the base map only fills in the keys a layout
    // does not label itself (the ISO/JIS extra keys added below).
    return { ...keyDisplayMap2, ...selectedKeyboard.keyDisplayMap };
  }, [selectedKeyboard]);

  const virtualKeyboardLayout = useMemo(() => {
    const { main } = selectedKeyboard.virtualKeyboard;
    return {
      ...selectedKeyboard.virtualKeyboard,
      main: {
        ...main,
        default: applyFormFactor(main.default, keyboardFormFactor, "default"),
        // shift is optional on KeyboardLayoutObject: leave it absent when the
        // layout has no shift row at all.
        shift: main.shift
          ? applyFormFactor(main.shift, keyboardFormFactor, "shift")
          : main.shift,
      },
    };
  }, [selectedKeyboard, keyboardFormFactor]);

  const mobileImeRow = useMemo(() => {
    const imeKeys = ["Henkan", "Muhenkan", "KatakanaHiragana", "Lang1", "Lang2"];
    return virtualKeyboardLayout.main.default
      .flatMap(row => row.split(" "))
      .filter(key => imeKeys.includes(key))
      .join(" ");
  }, [virtualKeyboardLayout]);

  const [isDragging, setIsDragging] = useState(false);
  const [position, setPosition] = useState({ x: 0, y: 0 });
  const [newPosition, setNewPosition] = useState({ x: 0, y: 0 });
  
  // State for locked modifier keys
  const [lockedModifiers, setLockedModifiers] = useState({
    ctrl: false,
    alt: false,
    meta: false,
    shift: false,
  });

  // Toggle for modifier key behavior: true = lock mode, false = direct trigger mode
  const [modifierLockMode, setModifierLockMode] = useState(isMobile);

  // Clear locked modifiers when switching to direct mode
  useEffect(() => {
    if (!modifierLockMode) {
      setLockedModifiers({
        ctrl: false,
        alt: false,
        meta: false,
        shift: false,
      });
      setLayoutName("default");
    }
  }, [modifierLockMode]);

  // Force sticky mode on mobile
  useEffect(() => {
    if (isMobile) {
      setModifierLockMode(true);
    }
  }, [isMobile]);

  const [useNum, setUseNum] = useState(false);
  const [useFn, setUseFn] = useState(false);
  const [stickyModifiers, setStickyModifiers] = useState<string[]>([]);
  // Preserve the physical side for both reports and locked-button highlighting.
  const [lockedModifierKeys, setLockedModifierKeys] = useState({
    ctrl: "ControlLeft",
    alt: "AltLeft",
    meta: "MetaLeft",
    shift: "ShiftLeft",
  });

  // Device truth for modifier state. Highlighting is derived from what the host
  // reports rather than from what this client believes it sent.
  const keysDownState = useHidStore(state => state.keysDownState);
  const hostModifierButtons = useMemo(() => {
    const modifier = keysDownState?.modifier ?? 0;
    const pressed: string[] = [];
    if (modifier & modifiers["ShiftLeft"]) pressed.push("ShiftLeft");
    if (modifier & modifiers["ShiftRight"]) pressed.push("ShiftRight");
    return pressed.join(" ");
  }, [keysDownState?.modifier]);
  const modifierButtonTheme = useMemo(
    () => [{ class: "hg-mod-active", buttons: stickyModifiers.join(" ") }],
    [stickyModifiers],
  );
  const isCapsLockActive = useHidStore(useShallow(state => state.keyboardLedState?.caps_lock));

  // HID related states
  const keyboardLedSync = useSettingsStore(state => state.keyboardLedSync);
  const isKeyboardLedManagedByHost = useMemo(
    () => keyboardLedSync === "host",
    [keyboardLedSync],
  );

  const setIsCapsLockActive = useHidStore(state => state.setIsCapsLockActive);

  const startDrag = useCallback((e: MouseEvent | TouchEvent) => {
    if (!keyboardRef.current) return;
    if (e instanceof TouchEvent && e.touches.length > 1) return;
    setIsDragging(true);

    const clientX = e instanceof TouchEvent ? e.touches[0].clientX : e.clientX;
    const clientY = e instanceof TouchEvent ? e.touches[0].clientY : e.clientY;

    const rect = keyboardRef.current.getBoundingClientRect();
    setPosition({
      x: clientX - rect.left,
      y: clientY - rect.top,
    });
  }, []);

  const onDrag = useCallback(
    (e: MouseEvent | TouchEvent) => {
      if (!keyboardRef.current) return;
      if (isDragging) {
        const clientX = e instanceof TouchEvent ? e.touches[0].clientX : e.clientX;
        const clientY = e instanceof TouchEvent ? e.touches[0].clientY : e.clientY;

        const newX = clientX - position.x;
        const newY = clientY - position.y;

        const rect = keyboardRef.current.getBoundingClientRect();
        const maxX = window.innerWidth - rect.width;
        const maxY = window.innerHeight - rect.height;

        setNewPosition({
          x: Math.min(maxX, Math.max(0, newX)),
          y: Math.min(maxY, Math.max(0, newY)),
        });
      }
    },
    [isDragging, position.x, position.y],
  );

  const endDrag = useCallback(() => {
    setIsDragging(false);
  }, []);

  useEffect(() => {
    const handle = keyboardRef.current;
    if (handle) {
      handle.addEventListener("touchstart", startDrag);
      handle.addEventListener("mousedown", startDrag);
    }

    document.addEventListener("mouseup", endDrag);
    document.addEventListener("touchend", endDrag);

    document.addEventListener("mousemove", onDrag);
    document.addEventListener("touchmove", onDrag);

    return () => {
      if (handle) {
        handle.removeEventListener("touchstart", startDrag);
        handle.removeEventListener("mousedown", startDrag);
      }

      document.removeEventListener("mouseup", endDrag);
      document.removeEventListener("touchend", endDrag);

      document.removeEventListener("mousemove", onDrag);
      document.removeEventListener("touchmove", onDrag);
    };
  }, [endDrag, onDrag, startDrag]);

  const onKeyDown = useCallback(
    (key: string, e?: MouseEvent) => {
      if (e) {
        e.preventDefault();
      }
      const isKeyShift = key === "{shift}" || key === "ShiftLeft" || key === "ShiftRight";
      const isKeyCaps = key === "CapsLock";
      const cleanKey = key.replace(/[()]/g, "");
      const keyHasShiftModifier = key.includes("(");

      // Check if this is a modifier key press
      // Both sides: the left-only check used to drop ControlRight into the generic
      // key path, where it has no usage in the keys table and sent nothing.
      const isModifierKey = key === "ControlLeft" || key === "ControlRight" ||
                           key === "AltLeft" || key === "AltRight" ||
                           key === "MetaLeft" || key === "MetaRight" || isKeyShift;

      // Handle toggle of layout for shift or caps lock
      const toggleLayout = () => {
        setLayoutName(prevLayout => (prevLayout === "default" ? "shift" : "default"));
      };

      // Handle modifier key press
      if (key === "ControlLeft" || key === "ControlRight") {
        if (modifierLockMode) {
          // Lock mode: toggle lock state
          setLockedModifierKeys(prev => ({ ...prev, ctrl: key }));
          setLockedModifiers(prev => ({ ...prev, ctrl: !prev.ctrl }));
        } else {
          // Direct trigger mode: send the side that was pressed
          sendKeyboardEvent([], [modifiers[key]]);
          setTimeout(resetKeyboardState, 100);
        }
        return;
      }
      if (key === "AltLeft" || key === "AltRight") {
        if (modifierLockMode) {
          setLockedModifierKeys(prev => ({ ...prev, alt: key }));
          setLockedModifiers(prev => ({ ...prev, alt: !prev.alt }));
        } else {
          sendKeyboardEvent([], [modifiers[key]]);
          setTimeout(resetKeyboardState, 100);
        }
        return;
      }
      if (key === "MetaLeft" || key === "MetaRight") {
        if (modifierLockMode) {
          setLockedModifierKeys(prev => ({ ...prev, meta: key }));
          setLockedModifiers(prev => ({ ...prev, meta: !prev.meta }));
        } else {
          sendKeyboardEvent([], [modifiers[key]]);
          setTimeout(resetKeyboardState, 100);
        }
        return;
      }
      if (isKeyShift) {
        if (modifierLockMode) {
          setLockedModifierKeys(prev => ({
            ...prev,
            shift: key === "ShiftRight" ? "ShiftRight" : "ShiftLeft",
          }));
          setLockedModifiers(prev => ({ ...prev, shift: !prev.shift }));
          if (lockedModifiers.shift) {
            // If unlocking shift, return to default layout
            setLayoutName("default");
          } else {
            // If locking shift, switch to shift layout
            toggleLayout();
          }
        } else {
          // Send the side that was actually pressed; {shift} falls back to left.
          sendKeyboardEvent([], [modifiers[key] ?? modifiers["ShiftLeft"]]);
          setTimeout(resetKeyboardState, 100);
        }
        return;
      }

      if (key === "CtrlAltDelete") {
        sendKeyboardEvent(
          [keys["Delete"]],
          [modifiers["ControlLeft"], modifiers["AltLeft"]],
        );
        setTimeout(resetKeyboardState, 100);
        return;
      }

      if (key === "AltMetaEscape") {
        sendKeyboardEvent(
          [keys["Escape"]],
          [modifiers["MetaLeft"], modifiers["AltLeft"]],
        );

        setTimeout(resetKeyboardState, 100);
        return;
      }

      if (key === "CtrlAltBackspace") {
        sendKeyboardEvent(
          [keys["Backspace"]],
          [modifiers["ControlLeft"], modifiers["AltLeft"]],
        );

        setTimeout(resetKeyboardState, 100);
        return;
      }

      if (isKeyCaps) {
        toggleLayout();

        if (isCapsLockActive) {
          if (!isKeyboardLedManagedByHost) {
            setIsCapsLockActive(false);
          }
          sendKeyboardEvent([keys["CapsLock"]], []);
          setTimeout(resetKeyboardState, 100);
          return;
        }
      }

      // Handle caps lock state change
      if (isKeyCaps && !isKeyboardLedManagedByHost) {
        setIsCapsLockActive(!isCapsLockActive);
      }

      // Collect new active keys and modifiers
      const newKeys = keys[cleanKey] ? [keys[cleanKey]] : [];
      const newModifiers: number[] = [];

      // Add locked modifiers
      if (lockedModifiers.ctrl) {
        newModifiers.push(modifiers[lockedModifierKeys.ctrl]);
      }
      if (lockedModifiers.alt) {
        newModifiers.push(modifiers[lockedModifierKeys.alt]);
      }
      if (lockedModifiers.meta) {
        newModifiers.push(modifiers[lockedModifierKeys.meta]);
      }
      if (lockedModifiers.shift && !isCapsLockActive) {
        newModifiers.push(modifiers[lockedModifierKeys.shift]);
      }

      // Add shift modifier for keys with parentheses (if not caps lock and shift not locked)
      if (keyHasShiftModifier && !isCapsLockActive && !lockedModifiers.shift) {
        newModifiers.push(modifiers["ShiftLeft"]);
      }

      // Update current keys and modifiers
      sendKeyboardEvent(newKeys, newModifiers);

      // If shift was used as a modifier and caps lock is not active and shift is not locked, revert to default layout
      if (keyHasShiftModifier && !isCapsLockActive && !lockedModifiers.shift) {
        setLayoutName("default");
      }

      // Auto-unlock modifiers after regular key press (not for combination keys)
      if (!isModifierKey && newKeys.length > 0) {
        setLockedModifiers({
          ctrl: false,
          alt: false,
          meta: false,
          shift: false,
        });
        setLayoutName("default");
      }
      setTimeout(resetKeyboardState, 100);
    },
    [isCapsLockActive, isKeyboardLedManagedByHost, sendKeyboardEvent, resetKeyboardState, setIsCapsLockActive, lockedModifiers, lockedModifierKeys, modifierLockMode],
  );

  const virtualKeyboard = useHidStore(state => state.isVirtualKeyboardEnabled);
  const setVirtualKeyboard = useHidStore(state => state.setVirtualKeyboardEnabled);

  const modifierLockButtons = [
    lockedModifiers.ctrl ? lockedModifierKeys.ctrl : "",
    lockedModifiers.alt ? lockedModifierKeys.alt : "",
    lockedModifiers.meta ? lockedModifierKeys.meta : "",
    lockedModifiers.shift ? lockedModifierKeys.shift : "",
  ].filter(Boolean).join(" ").trim();
  
  return (
    <div
      className="transition-all duration-200 ease-in-out"
      style={{
        marginBottom: virtualKeyboard ? "0px" : `-${500}px`,
      }}
    >
      <AnimatePresence>
        {virtualKeyboard && (
          <motion.div
            initial={{ opacity: 0, y: "100%" }}
            animate={{ opacity: 1, y: "0%" }}
            exit={{ opacity: 0, y: "100%" }}
            transition={{
              duration: 0.2,
              ease: "easeInOut",
            }}
          >
            <div
              className={cx(
                !showAttachedVirtualKeyboard
                  ? "fixed left-0 top-0 z-50 select-none"
                  : "relative",
              )}
              ref={keyboardRef}
              style={{
                ...(!showAttachedVirtualKeyboard
                  ? { transform: `translate(${newPosition.x}px, ${newPosition.y}px)` }
                  : {}),
              }}
            >
              <Card
                className={cx("overflow-hidden", {
                  "rounded-none": showAttachedVirtualKeyboard,
                })}
              >
                {isMobile ?
                  <>
                    <div
                      className={`flex items-center justify-center w-full border-b border-b-slate-800/30  px-1 py-0 dark:border-b-slate-300/20 ${dark_bg2_style}`}>

                      <style>
                        {`
                          .simple-keyboard-topcontrol .hg-button.hg-mod-active {
                            background-color: rgba(22,152,217,0.15) !important;
                            border-color: rgba(22,152,217,0.6) !important;
                          }
                          .simple-keyboard-main .hg-button.modifier-locked,
                          .simple-keyboard-topcontrol .hg-button.modifier-locked,
                          .simple-keyboard-main2 .hg-button.modifier-locked {
                            background-color: rgba(22,152,217,0.3) !important;
                            border-color: rgba(22,152,217,0.8) !important;
                            color: rgba(22,152,217,1) !important;
                          }
                          html.dark .simple-keyboard-main .hg-button.modifier-locked,
                          html.dark .simple-keyboard-topcontrol .hg-button.modifier-locked,
                          html.dark .simple-keyboard-main2 .hg-button.modifier-locked {
                            background-color: rgba(22,152,217,0.5) !important;
                            color: white !important;
                          }
                          html.dark .simple-keyboard-topcontrol,
                          html.dark .simple-keyboard-main2 {
                            background-color: transparent !important;
                          }
                          html.dark .simple-keyboard-topcontrol .hg-button,
                          html.dark .simple-keyboard-main2 .hg-button {
                            background-color: rgba(26,26,26,1);
                            color: white;
                          }
                          /* Add click animation */
                          .hg-button:active {
                            background-color: rgba(0,0,0,0.2) !important;
                          }
                          html.dark .hg-button:active {
                            background-color: rgba(255,255,255,0.2) !important;
                          }
                          html.dark .simple-keyboard-topcontrol .hg-button:active,
                          html.dark .simple-keyboard-main2 .hg-button:active {
                            background-color: rgba(255,255,255,0.2) !important;
                          }
                          .hg-button {
                            transition: background-color 0.02s ease-in-out;
                          }
                          /* The Back key is the hide button on touch layouts, so it
                             has to stand apart from the letter keys it sits next to. */
                          .simple-keyboard-topcontrol .hg-button.hg-hide-key {
                            border-color: rgba(59, 130, 246, 0.45) !important;
                            background-color: rgba(59, 130, 246, 0.1) !important;
                            color: rgba(37, 99, 235, 1) !important;
                          }
                          html.dark .simple-keyboard-topcontrol .hg-button.hg-hide-key {
                            border-color: rgba(96, 165, 250, 0.55) !important;
                            background-color: rgba(59, 130, 246, 0.16) !important;
                            color: rgba(219, 234, 254, 1) !important;
                          }
                        `}
                      </style>

                      <Keyboard
                        baseClass="simple-keyboard-topcontrol"
                        layoutName={layoutName}
                        buttonTheme={[
                          ...(stickyModifiers.length ? [{ class: "hg-mod-active", buttons: stickyModifiers.join(" ") }] : []),
                          ...(hostModifierButtons ? [{ class: "hg-mod-active", buttons: hostModifierButtons }] : []),
                          ...(modifierLockMode && modifierLockButtons ? [{ class: "modifier-locked", buttons: modifierLockButtons }] : []),
                          { class: "hg-hide-key", buttons: "Back" }
                        ]}
                        onKeyPress={(key: string) => {
                          if (key === "Back") {
                            setVirtualKeyboard(false);
                            return;
                          }
                          if (key === "ShiftLeft" || key === "ControlLeft" || key === "AltLeft" || key === "MetaLeft") {
                            onKeyDown(key);
                            return;
                          }
                          onKeyDown(key);
                        }}
                        display={sKeyDisplayMap}
                        layout={{
                          default: [
                            "Escape Tab MetaLeft PageUp ArrowUp PageDown Delete",
                            "ShiftLeft ControlLeft AltLeft ArrowLeft ArrowDown ArrowRight Back",
                          ],
                          shift: [
                            "Escape Tab MetaLeft PageUp ArrowUp PageDown Delete",
                            "ShiftLeft ControlLeft AltLeft ArrowLeft ArrowDown ArrowRight Back",
                          ],
                        }}
                        disableButtonHold={true}
                        syncInstanceInputs={true}
                        debug={false}
                      />
                    </div>
                    <div
                      className={`flex items-center justify-center flex-col border-b border-b-slate-800/30  px-1 py-0 dark:border-b-slate-300/20 ${dark_bg2_style}`}>
                      {
                        useNum?
                          <>
                            <div className={"flex flex-row w-full justify-between items-center h-1/4"}>
                              <Keyboard
                                baseClass="simple-keyboard-main2"
                                layoutName={layoutName}
                                onKeyPress={onKeyDown}
                                display={keyDisplayMap2}
                                buttonTheme={
                                  modifierLockMode && modifierLockButtons
                                    ? [{ class: "modifier-locked", buttons: modifierLockButtons }]
                                    : []
                                }
                                layout={{
                                  default: [
                                    useFn
                                      ? "F1 F2 F3 F4 F5 F6 F7 F8 F9 F10"
                                      : "Digit1 Digit2 Digit3 Digit4 Digit5 Digit6 Digit7 Digit8 Digit9 Digit0",
                                  ],
                                  shift: [
                                    useFn
                                      ? "F1 F2 F3 F4 F5 F6 F7 F8 F9 F10"
                                      : "(Digit1) (Digit2) (Digit3) (Digit4) (Digit5) (Digit6) (Digit7) (Digit8) (Digit9) (Digit0)",
                                  ],
                                }}

                                disableButtonHold={true}
                                syncInstanceInputs={true}
                                debug={false}
                              />
                            </div>
                            <div className={"flex flex-row w-full justify-between items-center h-1/4 px-3"}>
                              <Keyboard
                                baseClass="simple-keyboard-main2"
                                layoutName={layoutName}
                                onKeyPress={(key: string) => {
                                  if (key === "Fn") {
                                    setUseFn(prev => !prev);
                                    return;
                                  }
                                  onKeyDown(key);
                                }}
                                display={keyDisplayMap2}
                                buttonTheme={
                                  modifierLockMode && modifierLockButtons
                                    ? [{ class: "modifier-locked", buttons: modifierLockButtons }]
                                    : []
                                }
                                layout={{
                                  default: [
                                    useFn
                                      ? `Fn Backquote F11 F12 BracketLeft BracketRight Backslash${formFactorIsJIS ? " Yen" : ""}`
                                      : `Fn Backquote Minus Equal BracketLeft BracketRight Backslash${formFactorIsJIS ? " Yen" : ""}`,
                                  ],
                                  shift: [
                                    useFn
                                      ? `Fn Backquote F11 F12 BracketLeft BracketRight Backslash${formFactorIsJIS ? " Yen" : ""}`
                                      : `Fn (Backquote) (Minus) (Equal) (BracketLeft) (BracketRight) (Backslash)${formFactorIsJIS ? " (Yen)" : ""}`,
                                  ],
                                }}

                                disableButtonHold={true}
                                syncInstanceInputs={true}
                                debug={false}
                              />
                            </div>
                            <div className={"flex flex-row w-full justify-between items-center h-1/4"}>
                              <Keyboard
                                baseClass="simple-keyboard-main2"
                                layoutName={layoutName}
                                onKeyPress={onKeyDown}
                                display={keyDisplayMap2}
                                buttonTheme={
                                  modifierLockMode && modifierLockButtons
                                    ? [{ class: "modifier-locked", buttons: modifierLockButtons }]
                                    : []
                                }
                                layout={{
                                  default: [

                                    "ShiftLeft Semicolon  Quote Period Slash Backspace",
                                  ],
                                  shift: [

                                    "ShiftLeft (Semicolon) (Quote) (Period) (Slash) Backspace",
                                  ],
                                }}

                                disableButtonHold={true}
                                syncInstanceInputs={true}
                                debug={false}
                              />
                            </div>
                            <div className={"flex flex-row w-full justify-between items-center h-1/4"}>
                              <div className={"w-1/5"}>
                                <Keyboard
                                  baseClass="simple-keyboard-main2"
                                  layoutName={layoutName}
                                  onKeyPress={() => {
                                    setUseNum(false);
                                  }}
                                  display={keyDisplayMap2}
                                  layout={{
                                    default: [
                                      "abc",
                                    ],
                                    shift: [
                                      "abc",
                                    ],
                                  }} />
                              </div>
                              <div className={"w-3/5"}>
                                <Keyboard
                                  baseClass="simple-keyboard-main2"
                                  layoutName={layoutName}
                                  onKeyPress={onKeyDown}
                                  display={keyDisplayMap2}
                                  layout={{
                                    default: [
                                      "Space",
                                    ],
                                    shift: [
                                      "Space",
                                    ],
                                  }}

                                  disableButtonHold={true}
                                  syncInstanceInputs={true}
                                  debug={false}
                                />
                              </div>
                              <div className={"w-1/5"}>
                                <Keyboard
                                  baseClass="simple-keyboard-main2"
                                  layoutName={layoutName}
                                  onKeyPress={onKeyDown}
                                  display={keyDisplayMap2}
                                  layout={{
                                    default: [
                                      "Enter",
                                    ],
                                    shift: [
                                      "Enter",
                                    ],
                                  }} />
                              </div>

                            </div>
                          </>
                          :
                          <>
                            <div className={"flex flex-row w-full justify-between items-center h-1/4"}>
                              <Keyboard
                                baseClass="simple-keyboard-main2"
                                layoutName={layoutName}
                                onKeyPress={onKeyDown}
                                display={keyDisplayMap2}
                                layout={{
                                  default: [
                                    "KeyQ KeyW KeyE KeyR KeyT KeyY KeyU KeyI KeyO KeyP",
                                  ],
                                  shift: [
                                    "(KeyQ) (KeyW) (KeyE) (KeyR) (KeyT) (KeyY) (KeyU) (KeyI) (KeyO) (KeyP)",
                                  ],
                                }}

                                disableButtonHold={true}
                                syncInstanceInputs={true}
                                debug={false}
                              />
                            </div>
                            <div className={"flex flex-row w-full justify-between items-center h-1/4 px-3"}>
                              <Keyboard
                                baseClass="simple-keyboard-main2"
                                layoutName={layoutName}
                                onKeyPress={onKeyDown}
                                display={keyDisplayMap2}
                                layout={{
                                  default: [

                                    "KeyA KeyS KeyD KeyF KeyG KeyH KeyJ KeyK KeyL",

                                  ],
                                  shift: [

                                    "(KeyA) (KeyS) (KeyD) (KeyF) (KeyG) (KeyH) (KeyJ) (KeyK) (KeyL)",

                                  ],
                                }}

                                disableButtonHold={true}
                                syncInstanceInputs={true}
                                debug={false}
                              />
                            </div>
                            <div className={"flex flex-row w-full justify-between items-center h-1/4"}>
                              <Keyboard
                                baseClass="simple-keyboard-main2"
                                layoutName={layoutName}
                                onKeyPress={onKeyDown}
                                display={keyDisplayMap2}
                                layout={{
                                  default: [

                                    `ShiftLeft${formFactorIsISO ? " IntlBackslash" : ""} KeyZ KeyX KeyC KeyV KeyB KeyN KeyM Backspace${formFactorIsJIS ? " KeyRO" : ""}`,
                                  ],
                                  shift: [

                                    `ShiftLeft${formFactorIsISO ? " (IntlBackslash)" : ""} (KeyZ) (KeyX) (KeyC) (KeyV) (KeyB) (KeyN) (KeyM) Backspace${formFactorIsJIS ? " (KeyRO)" : ""}`,
                                  ],
                                }}

                                disableButtonHold={true}
                                syncInstanceInputs={true}
                                debug={false}
                              />
                            </div>
                            <div className={"flex flex-row w-full justify-between items-center h-1/4"}>
                              <div className={"w-1/5"}>
                                <Keyboard
                                  baseClass="simple-keyboard-main2"
                                  layoutName={layoutName}
                                  onKeyPress={() => {
                                    setUseNum(true);
                                  }}
                                  display={keyDisplayMap2}
                                  layout={{
                                    default: [
                                      "123",
                                    ],
                                    shift: [
                                      "123",
                                    ],
                                  }} />
                              </div>
                              {formFactorIsJIS && (
                                <div className={"w-1/4"}>
                                  <Keyboard
                                    baseClass="simple-keyboard-main2"
                                    layoutName={layoutName}
                                    onKeyPress={onKeyDown}
                                    display={keyDisplayMap}
                                    layout={{
                                      default: [
                                        mobileImeRow,
                                      ],
                                      shift: [
                                        mobileImeRow,
                                      ],
                                    }}
                                    disableButtonHold={true}
                                    syncInstanceInputs={true}
                                    debug={false}
                                  />
                                </div>
                              )}
                              <div className={formFactorIsJIS ? "flex-1" : "w-3/5"}>
                                <Keyboard
                                  baseClass="simple-keyboard-main2"
                                  layoutName={layoutName}
                                  onKeyPress={onKeyDown}
                                  display={keyDisplayMap2}
                                  layout={{
                                    default: [
                                      "Space",
                                    ],
                                    shift: [
                                      "Space",
                                    ],
                                  }}

                                  disableButtonHold={true}
                                  syncInstanceInputs={true}
                                  debug={false}
                                />
                              </div>
                              <div className={"w-1/5"}>
                                <Keyboard
                                  baseClass="simple-keyboard-main2"
                                  layoutName={layoutName}
                                  onKeyPress={onKeyDown}
                                  display={keyDisplayMap2}
                                  layout={{
                                    default: [
                                      "Enter",
                                    ],
                                    shift: [
                                      "Enter",
                                    ],
                                  }}
                                  
                                  disableButtonHold={true}
                                  syncInstanceInputs={true}
                                  debug={false}
                                />
                              </div>

                            </div>
                          </>
                      }

                    </div>
                  </>
                  :
                  <>
                    <div
                      className={`w-full h-[36px] flex items-center justify-between border-b border-b-slate-800/30 px-2  dark:border-b-slate-300/20 ${dark_bg2_style}`}>
                      <div className=" left-2 flex items-center gap-x-2">
                        {!isMobile && (
                          <div className="flex items-center gap-x-2 ml-2">
                            <Switch
                              size="small"
                              checked={modifierLockMode}
                              onChange={setModifierLockMode}
                              checkedChildren={<LockClosedIcon className="w-3 h-3 mt-0.5" />}
                            />
                            <span className="text-[10px] text-gray-500">
                              {$at("Sticky Keys")}
                            </span>
                          </div>
                        )}
                      </div>
                      <h2 className="select-none self-center font-sans text-[12px] text-[rgba(102,102,102,1)]">
                        {$at("Virtual Keyboard")}
                      </h2>
                      <div className="h-full flex items-center">
                        <div style={{ width: "1px", height: "100%" }}
                             className={"bg-[rgba(229,229,229,1)] dark:bg-[rgba(56,56,56,1)]"}></div>
                        <AntdButton
                          type={"text"}
                          icon={<GoBottomSvg />}
                          onClick={() => setVirtualKeyboard(false)}
                        >
                          {$at("Hide")}
                        </AntdButton>
                      </div>
                    </div>
                    <div>
                      <div className={`flex flex-col ${dark_bg2_style} md:flex-col `}>
                      <style>
                        {`
                          html.dark .simple-keyboard-main,
                          html.dark .simple-keyboard-control,
                          html.dark .simple-keyboard-arrows {
                            background-color: transparent !important;
                          }
                          html.dark .simple-keyboard-main .hg-button,
                          html.dark .simple-keyboard-control .hg-button,
                          html.dark .simple-keyboard-arrows .hg-button {
                            background-color: rgba(44, 44, 46, 1) !important;
                            color: white !important;
                          }
                          /* Add click animation */
                          .hg-button:active {
                            background-color: rgba(0,0,0,0.2) !important;
                          }
                          html.dark .hg-button:active {
                            background-color: rgba(255,255,255,0.2) !important;
                          }
                          /* Specific overrides for dark mode PC layout sections */
                          html.dark .simple-keyboard-main .hg-button:active,
                          html.dark .simple-keyboard-control .hg-button:active,
                          html.dark .simple-keyboard-arrows .hg-button:active {
                            background-color: rgba(255,255,255,0.2) !important;
                          }
                          .hg-button {
                            transition: background-color 0.05s ease-in-out;
                          }
                        `}
                      </style>
                        <div style={{ width: "40%" }} className={"flex items-start justify-center flex-col"}>
                          <div className={"h-[10px]"}></div>
                          <Keyboard
                            baseClass="simple-keyboard-main"
                            layoutName={layoutName}
                            onKeyPress={onKeyDown}
                            buttonTheme={
                              modifierLockMode && modifierLockButtons
                                ? [{ class: "modifier-locked", buttons: modifierLockButtons }]
                                : []
                            }
                            display={keyDisplayMap}
                            layout={{
                              default: [
                                "CtrlAltDelete AltMetaEscape CtrlAltBackspace",

                              ],
                              shift: [
                                "CtrlAltDelete AltMetaEscape CtrlAltBackspace",

                              ],
                            }}
                            disableButtonHold={true}
                            syncInstanceInputs={true}
                            debug={false}
                          />
                        </div>


                        <div className={`flex flex-col  md:flex-row ${dark_bg2_style}`}>
                          <Keyboard
                            baseClass="simple-keyboard-main"
                            layoutName={layoutName}
                            onKeyPress={onKeyDown}
                            display={keyDisplayMap}
                            buttonTheme={
                              modifierLockMode && modifierLockButtons
                                ? [{ class: "modifier-locked", buttons: modifierLockButtons }]
                                : []
                            }
                            layout={virtualKeyboardLayout.main}
                            disableButtonHold={true}
                            syncInstanceInputs={true}
                            debug={false}
                          />

                          {/*<div className="controlArrows">*/}
                          <div style={{
                            display: "flex",
                            flex: 1,
                            alignItems: "center",
                            flexDirection: "column",
                            justifyContent: "space-between",
                          }}>
                            <Keyboard
                              baseClass="simple-keyboard-control"
                              theme="simple-keyboard hg-theme-default hg-layout-default"
                              layoutName={layoutName}
                              onKeyPress={onKeyDown}
                              display={keyDisplayMap}
                              layout={virtualKeyboardLayout.control}
                              syncInstanceInputs={true}
                              debug={false}
                            />
                            <div style={{
                              display: "flex",
                              width: "100%",
                              alignItems: "center",
                              flexDirection: "column",
                              justifyContent: "space-between",
                            }}>
                              <div style={{
                                display: "flex",
                                width: "34%",

                                alignItems: "center",
                                flexDirection: "column",
                                justifyContent: "space-between",
                              }}>
                                <Keyboard
                                  baseClass="simple-keyboard-arrows"
                                  theme="simple-keyboard hg-theme-default hg-layout-default"
                                  onKeyPress={onKeyDown}
                                  display={keyDisplayMap}
                                  layout={{
                                    default: [virtualKeyboardLayout.arrows?.default?.[0] || "ArrowUp"],
                                  }}
                                  syncInstanceInputs={true}
                                  debug={false}

                                />
                              </div>

                              <Keyboard
                                baseClass="simple-keyboard-arrows"
                                theme="simple-keyboard hg-theme-default hg-layout-default"
                                onKeyPress={onKeyDown}
                                display={keyDisplayMap}
                                layout={{
                                  default: [virtualKeyboardLayout.arrows?.default?.[1] || "ArrowLeft ArrowDown ArrowRight"],
                                }}
                                syncInstanceInputs={true}
                                debug={false}
                              />
                            </div>
                          </div>
                        </div>
                      </div>
                    </div>
                  </>}

              </Card>
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}

export default KeyboardWrapper;
