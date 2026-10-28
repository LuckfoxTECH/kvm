import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ExclamationCircleIcon } from "@heroicons/react/16/solid";
import { useClose } from "@headlessui/react";
import { Checkbox, Button, Input } from "antd";
import { useReactAt } from "i18n-auto-extractor/react";
import { isMobile } from "react-device-detect";

import { TextAreaWithLabel } from "@components/TextArea";
import { SettingsItem } from "@components/Settings/SettingsView";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import { useHidStore, useRTCStore, useUiStore, useSettingsStore, useVideoStore } from "@/hooks/stores";
import { keys, modifiers } from "@/keyboardMappings";
import { layouts, chars, resolveKeyboardLayoutKey } from "@/keyboardLayouts";
import notifications from "@/notifications";
import { eventMatchesShortcut, shortcutFromKeyboardEvent } from "@/utils/shortcuts";

const hidKeyboardPayload = (keys: number[], modifier: number) => {
  return { keys, modifier };
};

const modifierCode = (shift?: boolean, altRight?: boolean) => {
  return (shift ? modifiers["ShiftLeft"] : 0)
    | (altRight ? modifiers["AltRight"] : 0);
};
const noModifier = 0;

export default function Clipboard() {
  const TextAreaRef = useRef<HTMLTextAreaElement>(null);
  const setPasteMode = useHidStore(state => state.setPasteModeEnabled);
  const setDisableVideoFocusTrap = useUiStore(state => state.setDisableVideoFocusTrap);
  const setSidebarView = useUiStore(state => state.setSidebarView);
  const toggleTopBarView = useUiStore(state => state.toggleTopBarView);
  const isOcrMode = useUiStore(state => state.isOcrMode);
  const setOcrMode = useUiStore(state => state.setOcrMode);
  const isReinitializingGadget = useHidStore(state => state.isReinitializingGadget);
  const videoWidth = useVideoStore(state => state.width);
  const videoHeight = useVideoStore(state => state.height);
  const [send] = useJsonRpc();
  const rpcDataChannel = useRTCStore(state => state.rpcDataChannel);

  const [invalidChars, setInvalidChars] = useState<string[]>([]);
  const close = useClose();
  const pasteShortcutEnabled = useSettingsStore(state => state.pasteShortcutEnabled);
  const setPasteShortcutEnabled = useSettingsStore(state => state.setPasteShortcutEnabled);
  const pasteShortcut = useSettingsStore(state => state.pasteShortcut);
  const setPasteShortcut = useSettingsStore(state => state.setPasteShortcut);
  const ocrShortcutEnabled = useSettingsStore(state => state.ocrShortcutEnabled);
  const setOcrShortcutEnabled = useSettingsStore(state => state.setOcrShortcutEnabled);
  const ocrShortcut = useSettingsStore(state => state.ocrShortcut);
  const setOcrShortcut = useSettingsStore(state => state.setOcrShortcut);
  const [readyToRender, setReadyToRender] = useState(false);

  useEffect(() => {
    const timer = setTimeout(() => {
      setReadyToRender(true);
    }, 250);
    return () => clearTimeout(timer);
  }, []);

  const keyboardLayout = useSettingsStore(state => state.keyboardLayout);
  const setKeyboardLayout = useSettingsStore(
    state => state.setKeyboardLayout,
  );
  const { $at } = useReactAt();

  // this ensures we always get the original en_US if it hasn't been set yet
  const safeKeyboardLayout = useMemo(() => {
    return resolveKeyboardLayoutKey(keyboardLayout) ?? "en_US";
  }, [keyboardLayout]);

  useEffect(() => {
    send("getKeyboardLayout", {}, resp => {
      if ("error" in resp) return;
      setKeyboardLayout(resp.result as string);
    });
  }, [send, setKeyboardLayout]);

  const onCancelPasteMode = useCallback(() => {
    setPasteMode(false);
    setDisableVideoFocusTrap(false);
    setInvalidChars([]);
  }, [setDisableVideoFocusTrap, setPasteMode]);

  const onConfirmPaste = useCallback(async () => {
    setPasteMode(false);
    setDisableVideoFocusTrap(false);
    if (rpcDataChannel?.readyState !== "open" || !TextAreaRef.current) return;
    // Don't send keyboard events while reinitializing gadget
    if (isReinitializingGadget) {
      notifications.error("USB gadget is reinitializing, please wait...");
      return;
    }
    if (!safeKeyboardLayout) return;
    if (!chars[safeKeyboardLayout]) return;
    const text = TextAreaRef.current.value;

    try {
      for (const char of text) {
        const mapping = chars[safeKeyboardLayout][char];
        if (!mapping || !mapping.key) continue;
        const { key, shift, altRight, deadKey, accentKey } = mapping;
        if (!key) continue;

        const keyz = [keys[key]];
        const modz = [modifierCode(shift, altRight)];

        if (deadKey) {
          keyz.push(keys["Space"]);
          modz.push(noModifier);
        }
        if (accentKey) {
          keyz.unshift(keys[accentKey.key]);
          modz.unshift(modifierCode(accentKey.shift, accentKey.altRight));
        }

        for (const [index, kei] of keyz.entries()) {
          await new Promise<void>((resolve, reject) => {
            send(
              "keyboardReport",
              hidKeyboardPayload([kei], modz[index]),
              params => {
                if ("error" in params) return reject(params.error);
                send("keyboardReport", hidKeyboardPayload([], 0), params => {
                  if ("error" in params) return reject(params.error);
                  resolve();
                });
              },
            );
          });
        }
      }
    } catch (error) {
      console.error(error);
      notifications.error("tt");
    }
  }, [rpcDataChannel?.readyState, send, setDisableVideoFocusTrap, setPasteMode, safeKeyboardLayout]);

  const handleTextSend = useCallback(async (text: string) => {
    const segInvalid = [
      ...new Set(
        // @ts-expect-error TS doesn't recognize Intl.Segmenter in some environments
        [...new Intl.Segmenter().segment(text)]
          .map(x => x.segment)
          .filter(char => !chars[safeKeyboardLayout][char]),
      ),
    ];
    setInvalidChars(segInvalid);
    if (segInvalid.length === 0) {
      if (rpcDataChannel?.readyState !== "open" || isReinitializingGadget) return;
      try {
        for (const char of text) {
          const mapping = chars[safeKeyboardLayout][char];
          if (!mapping || !mapping.key) continue;
          const { key, shift, altRight, deadKey, accentKey } = mapping;

          const keyz = [keys[key]];
          const modz = [modifierCode(shift, altRight)];

          if (deadKey) {
            keyz.push(keys["Space"]);
            modz.push(noModifier);
          }
          if (accentKey) {
            keyz.unshift(keys[accentKey.key]);
            modz.unshift(modifierCode(accentKey.shift, accentKey.altRight));
          }

          for (const [index, kei] of keyz.entries()) {
            await new Promise<void>((resolve, reject) => {
              send(
                "keyboardReport",
                hidKeyboardPayload([kei], modz[index]),
                params => {
                  if ("error" in params) return reject(params.error);
                  send("keyboardReport", hidKeyboardPayload([], 0), params => {
                    if ("error" in params) return reject(params.error);
                    resolve();
                  });
                },
              );
            });
          }
        }
        notifications.success(`Pasted: "${text}"`);
      } catch (error) {
        notifications.error("Failed to paste text");
      }
    } else {
      notifications.error(`Invalid characters: ${segInvalid.join(", ")}`);
    }
  }, [safeKeyboardLayout, rpcDataChannel?.readyState, isReinitializingGadget, send]);

  const readClipboardToBufferAndSend = useCallback(async () => {
    try {
      const text = await navigator.clipboard.readText();
      await handleTextSend(text);
    } catch {
      void 0;
    }
  }, [handleTextSend]);


  const handleShortcutInput = useCallback(
    (setter: (shortcut: string) => void) => (e: React.KeyboardEvent<HTMLInputElement>) => {
      e.preventDefault();
      e.stopPropagation();
      const shortcut = shortcutFromKeyboardEvent(e.nativeEvent);
      if (!shortcut) return;
      setter(shortcut);
    },
    [],
  );

  const handleOpenOcr = useCallback(() => {
    if (videoWidth === 0 || videoHeight === 0) {
      notifications.error($at("No video signal"));
      return;
    }
    setOcrMode(!isOcrMode);
    close();
    if (isMobile) {
      toggleTopBarView("ClipboardMobile");
    } else {
      setSidebarView(null);
    }
  }, [videoWidth, videoHeight, $at, setOcrMode, isOcrMode, close, toggleTopBarView, setSidebarView]);

  return (
    <div className="space-y-4  py-3" >
    <div className="grid h-full grid-rows-(--grid-headerBody)">
        <div className="h-full space-y-4">
          <div className="space-y-4">

              <div className="grid grid-cols-[minmax(0,1fr)_140px] items-center gap-2 sm:grid-cols-[minmax(0,1fr)_180px]">
                <Checkbox
                  className="min-w-0"
                  checked={pasteShortcutEnabled}
                  onChange={e => setPasteShortcutEnabled(e.target.checked)}
                >
                  <span className="whitespace-normal break-words">
                    {$at("Enable paste shortcut")}
                  </span>
                </Checkbox>
                <Input
                  size="small"
                  value={pasteShortcut}
                  onKeyDown={handleShortcutInput(setPasteShortcut)}
                  onChange={() => void 0}
                  className="w-full"
                />
              </div>

              <div className="w-full px-1 outline-none"
                   tabIndex={pasteShortcutEnabled ? 0 : -1}
                   onKeyUp={e => e.stopPropagation()}
                   onKeyDown={e => {
                     e.stopPropagation();
                     if (pasteShortcutEnabled && eventMatchesShortcut(e.nativeEvent, pasteShortcut)) {
                       e.preventDefault();
                       readClipboardToBufferAndSend();
                     }
                   }}
                   onPaste={e => {
                     if (pasteShortcutEnabled) {
                       e.preventDefault();
                       const txt = e.clipboardData?.getData("text") || "";
                       if (txt) {
                         handleTextSend(txt);
                       } else {
                         readClipboardToBufferAndSend();
                       }
                     }
                   }}>
                {!pasteShortcutEnabled && readyToRender && <TextAreaWithLabel
                  ref={TextAreaRef}
                  label={$at("Copy text from your client to the remote host")}
                  rows={4}
                  onClick={() => {setDisableVideoFocusTrap(true);
                    if (TextAreaRef.current) {
                      TextAreaRef.current.focus();
                    }
                  }}
                  onKeyUp={e => e.stopPropagation()}
                  onKeyDown={e => {
                    e.stopPropagation();
                    if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
                      e.preventDefault();
                      onConfirmPaste();
                    } else if (e.key === "Escape") {
                      e.preventDefault();
                      onCancelPasteMode();
                    }
                  }}
                  onChange={e => {
                    const value = e.target.value;
                    const invalidChars = [
                      ...new Set(
                        // @ts-expect-error TS doesn't recognize Intl.Segmenter in some environments
                        [...new Intl.Segmenter().segment(value)]
                          .map(x => x.segment)
                          .filter(char => !chars[safeKeyboardLayout][char]),
                      ),
                    ];

                    setInvalidChars(invalidChars);
                  }}
                />}

                {!pasteShortcutEnabled && invalidChars.length > 0 && (
                  <div className="mt-2 flex items-center gap-x-2">
                    <ExclamationCircleIcon className="h-4 w-4 text-red-500 dark:text-red-400" />
                    <span className="text-xs text-red-500 dark:text-red-400">
                          {$at("The following characters will not be pasted:")} {invalidChars.join(", ")}
                        </span>
                  </div>
                )}
              </div>

            <div className="space-y-4">
              <p className="text-xs text-slate-600 dark:text-[#ffffff]">
                {$at("Sending text using keyboard layout:")} {layouts[safeKeyboardLayout]}
              </p>
            </div>
          </div>
        </div>

      </div>
      <div
        className="flex animate-fadeIn opacity-0 flex-col gap-y-2"
        style={{
          animationDuration: "0.7s",
          animationDelay: "0.2s",
        }}
      >
        {!pasteShortcutEnabled && <Button
          type="primary"
          className="w-full"
          onClick={onConfirmPaste}
        >
          {$at("Confirm paste")}</Button>}
        <div className="grid grid-cols-[minmax(0,1fr)_140px] items-center gap-2 sm:grid-cols-[minmax(0,1fr)_180px]">
          <Checkbox
            className="min-w-0"
            checked={ocrShortcutEnabled}
            onChange={e => setOcrShortcutEnabled(e.target.checked)}
          >
            <span className="whitespace-normal break-words">
              {$at("Enable OCR shortcut")}
            </span>
          </Checkbox>
          <Input
            size="small"
            value={ocrShortcut}
            onKeyDown={handleShortcutInput(setOcrShortcut)}
            onChange={() => void 0}
            className="w-full"
          />
        </div>

        <SettingsItem
          title={$at("OCR")}
          description={$at("Open OCR selection mode on the video area")}
        >
          <Button
            type="primary"
            className={`${isMobile ? "w-full" : ""}`}
            onClick={handleOpenOcr}
          >
            {$at("Open")}
          </Button>
        </SettingsItem>
      </div>
    </div>

  );
}
