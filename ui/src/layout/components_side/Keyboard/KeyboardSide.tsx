import { useCallback, useEffect, useMemo } from "react";
import { Checkbox } from "antd";
import { isMobile } from "react-device-detect";
import { useReactAt } from "i18n-auto-extractor/react";

import { SettingsItem } from "@components/Settings/SettingsView";
import SettingsSelect from "@components/Settings/SettingsSelect";
import { layouts, resolveKeyboardLayoutKey } from "@/keyboardLayouts";
import { KeyboardLedSync, useHidStore, useSettingsStore } from "@/hooks/stores";
import {
  keyboardFormFactorOptions,
  isKeyboardFormFactor,
  type KeyboardFormFactor,
} from "@/keyboardFormFactor";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import notifications from "@/notifications";
import KeyboardLedStatusRow from "@/layout/components_bottom/keyboard/KeyboardLedStatusRow";

const ledSyncDescriptions: Record<string, string> = {
  host: "Mirrors the LEDs the target reports.",
  browser: "Mirrors this browser's lock keys.",
};

export default function KeyboardSide() {
  const { $at } = useReactAt();
  const [send] = useJsonRpc();

  const keyboardLayout = useSettingsStore(state => state.keyboardLayout);
  const setKeyboardLayout = useSettingsStore(state => state.setKeyboardLayout);
  const keyboardFormFactor = useSettingsStore(state => state.keyboardFormFactor);
  const setKeyboardFormFactor = useSettingsStore(state => state.setKeyboardFormFactor);
  const showPressedKeys = useSettingsStore(state => state.showPressedKeys);
  const setShowPressedKeys = useSettingsStore(state => state.setShowPressedKeys);
  const swapCommandControl = useSettingsStore(state => state.swapCommandControl);
  const setSwapCommandControl = useSettingsStore(state => state.setSwapCommandControl);
  const isolateBrowserShortcuts = useSettingsStore(state => state.isolateBrowserShortcuts);
  const setIsolateBrowserShortcuts = useSettingsStore(
    state => state.setIsolateBrowserShortcuts,
  );
  const keyboardLedSync = useSettingsStore(state => state.keyboardLedSync);
  const setKeyboardLedSync = useSettingsStore(state => state.setKeyboardLedSync);
  const resetKeyboardLedTruth = useHidStore(state => state.resetKeyboardLedTruth);

  // The stored value may be spelled with a dash or an underscore, so resolve it
  // first; otherwise the dropdown would silently show the first option.
  const safeKeyboardLayout = useMemo(
    () => resolveKeyboardLayoutKey(keyboardLayout) ?? "en_US",
    [keyboardLayout],
  );

  const layoutOptions = useMemo(
    () =>
      Object.entries(layouts).map(([code, language]) => ({
        value: code,
        label: `${language} (${code})`,
      })),
    [],
  );

  const formFactorOptions = useMemo(
    () => keyboardFormFactorOptions.map(o => ({ value: o.value, label: o.label })),
    [],
  );

  const ledSyncOptions = useMemo(
    () => [
      { value: "host", label: $at("Host Only") },
      { value: "browser", label: $at("Browser Only") },
    ],
    [$at],
  );

  useEffect(() => {
    send("getKeyboardLayout", {}, resp => {
      if ("error" in resp) return;
      setKeyboardLayout(resp.result as string);
    });
    // Layouts choose the characters, the form factor chooses which keys exist.
    send("getKeyboardFormFactor", {}, resp => {
      if ("error" in resp) return;
      if (isKeyboardFormFactor(resp.result)) setKeyboardFormFactor(resp.result);
    });
  }, []);

  const onKeyboardLayoutChange = useCallback(
    (layout: string) => {
      send("setKeyboardLayout", { layout }, resp => {
        if ("error" in resp) {
          notifications.error(
            `Failed to set keyboard layout: ${resp.error.data || "Unknown error"}`,
          );
          return;
        }
        notifications.success("Keyboard layout set successfully");
        setKeyboardLayout(layout);
      });
    },
    [send, setKeyboardLayout],
  );

  const onKeyboardFormFactorChange = useCallback(
    (value: string) => {
      const formFactor = value as KeyboardFormFactor;
      send("setKeyboardFormFactor", { formFactor }, resp => {
        if ("error" in resp) {
          notifications.error(
            `Failed to set keyboard form factor: ${resp.error.data || "Unknown error"}`,
          );
          return;
        }
        setKeyboardFormFactor(formFactor);
      });
    },
    [send, setKeyboardFormFactor],
  );

  const handleLedChange = (next: KeyboardLedSync) => {
    if (next === useSettingsStore.getState().keyboardLedSync) return;
    setKeyboardLedSync(next);
    // Whatever the previous source believed is not an answer for the new one: the
    // browser path stamps reported=true, so its CapsLock would survive the switch
    // and be drawn as host truth until the host pushed its first LED report.
    resetKeyboardLedTruth();
  };

  // The description is looked up by the current value, so a settings file written
  // by another build can carry a value this list does not know. Falling back to an
  // empty string keeps the row readable instead of handing $at a key that is not
  // a translation at all.
  const ledSyncDescription = ledSyncDescriptions[keyboardLedSync];

  return (
    <div
      className="space-y-3"
      onKeyUp={e => e.stopPropagation()}
      onKeyDown={e => e.stopPropagation()}
    >
      {/* The desktop bottom bar already shows Num/Caps/Scroll; only the phone bar
          leaves them out, so the row stays for mobile alone. */}
      {isMobile && <KeyboardLedStatusRow />}

      <div className="space-y-4">
        <SettingsItem
          title={$at("LED State Synchronization")}
          description={ledSyncDescription ? $at(ledSyncDescription) : ""}
        >
          <SettingsSelect
            value={keyboardLedSync}
            options={ledSyncOptions}
            onChange={value => handleLedChange(value as KeyboardLedSync)}
          />
        </SettingsItem>

        <SettingsItem title={$at("Keyboard Layout")} description={$at("Character set to type with")}>
          <SettingsSelect
            value={safeKeyboardLayout}
            options={layoutOptions}
            showSearch
            optionFilterProp="label"
            onChange={value => onKeyboardLayoutChange(value)}
          />
        </SettingsItem>

        <SettingsItem title={$at("Keyboard Form Factor")} description={$at("Key arrangement the target expects")}>
          <SettingsSelect
            value={keyboardFormFactor}
            options={formFactorOptions}
            onChange={value => onKeyboardFormFactorChange(value)}
          />
        </SettingsItem>

        <SettingsItem
          title={$at("Show Pressed Keys")}
          description={$at("Draw the keys held down")}
          noCol
          className="flex-row items-center"
        >
          <Checkbox
            checked={showPressedKeys}
            onChange={e => setShowPressedKeys(e.target.checked)}
          />
        </SettingsItem>

        <SettingsItem
          title={$at("Swap Command and Control")}
          description={$at("Send Command as Control")}
          noCol
          className="flex-row items-center"
        >
          <Checkbox
            checked={swapCommandControl}
            onChange={e => setSwapCommandControl(e.target.checked)}
          />
        </SettingsItem>

        <SettingsItem
          title={$at("Isolate Browser Shortcuts")}
          description={$at(
            "Send Ctrl, Alt and function keys to the host only; Win, Cmd and Alt+Tab still reach this system",
          )}
          noCol
          className="flex-row items-center"
        >
          <Checkbox
            checked={isolateBrowserShortcuts}
            onChange={e => setIsolateBrowserShortcuts(e.target.checked)}
          />
        </SettingsItem>
      </div>
    </div>
  );
}
