import React, { useCallback, useEffect, useMemo, useState } from "react";
import { Divider, Button } from "antd";
import LeftSVG from "@assets/second/left.svg?react";
import { isDesktop, isMobile } from "react-device-detect";
import { CloseOutlined } from '@ant-design/icons';
import { useReactAt } from "i18n-auto-extractor/react";

import ScrollThrottlingSelect, { Option } from "@components/ScrollThrottlingSelect";
import { layouts, keyboards, resolveKeyboardLayoutKey } from "@/keyboardLayouts";
import { KeyboardLedSync, useHidStore, useSettingsStore } from "@/hooks/stores";
import {
  keyboardFormFactorOptions,
  isKeyboardFormFactor,
  type KeyboardFormFactor,
} from "@/keyboardFormFactor";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import notifications from "@/notifications";
import KeyboardLedStatusRow from "@/layout/components_bottom/keyboard/KeyboardLedStatusRow";
import KeyboardLayoutModal, { KeyboardLayoutContent } from "@/layout/components_bottom/keyboard/KeyboardLayoutModal";
import { dark_bg2_style, dark_font_style, dark_bd_style, dark_line_style } from "@/layout/theme_color";
import { useThemeSettings } from "@routes/login_page/useLocalAuth";


const KeyboardPanel: React.FC = () => { 
  const { $at } = useReactAt();
  const { isDark } = useThemeSettings();
  const [showMore, setShowMore] = useState(false);
  const keyboardLayout = useSettingsStore(state => state.keyboardLayout);
  const setKeyboardLayout = useSettingsStore(state => state.setKeyboardLayout);
  const keyboardFormFactor = useSettingsStore(state => state.keyboardFormFactor);
  const setKeyboardFormFactor = useSettingsStore(state => state.setKeyboardFormFactor);

  const [layoutOptions, setLayoutOptions] = useState<Option[]>();
  const [maxShowCount, setMaxShowCount] = useState(3);

  // The select lists underscored keys ("ja_JP-mac") while the stored value may
  // be in either spelling; resolve it so the dropdown preselects the active
  // layout instead of silently showing the first option. Declared before the
  // effect that depends on it.
  const safeKeyboardLayout = useMemo(() => {
    return resolveKeyboardLayoutKey(keyboardLayout) ?? "en_US";
  }, [keyboardLayout]);

  const layoutAbbrevMap = useMemo(() => {
    const map: Record<string, string> = {};
    keyboards.forEach(kb => {
      const oldCode = kb.isoCode.replace("-", "_");
      map[oldCode] = oldCode;
    });
    return map;
  }, []);

  useEffect(() => {
    const curLayoutOptions = (() => {
      const options = Object.entries(layouts).map(([code, language]) => ({
        value: code,
        label: `${language} (${layoutAbbrevMap[code] || code})`,
      }));

      const currentIndex = options.findIndex(option => option.value === safeKeyboardLayout);
      if (currentIndex === -1 || currentIndex < 3) {
        setMaxShowCount(3);
        return options;
      }
      setMaxShowCount(4);
      const [movedItem] = options.splice(currentIndex, 1);
      options.splice(3, 0, movedItem);
      return options;
    })();
    setLayoutOptions(curLayoutOptions);
  }, [layouts, safeKeyboardLayout, layoutAbbrevMap]);

  const [send] = useJsonRpc();
  const resetKeyboardLedTruth = useHidStore(state => state.resetKeyboardLedTruth);

  useEffect(() => {
    send("getKeyboardLayout", {}, resp => {
      if ("error" in resp) return;
      setKeyboardLayout(resp.result as string);
    });
    // The form factor is a separate setting: layouts choose the characters, the
    // form factor chooses which keys exist.
    send("getKeyboardFormFactor", {}, resp => {
      if ("error" in resp) return;
      if (isKeyboardFormFactor(resp.result)) setKeyboardFormFactor(resp.result);
    });
  }, []);

  const onKeyboardLayoutChange = useCallback(
    (layout: string[] | string) => {
      send("setKeyboardLayout", { layout }, resp => {
        if ("error" in resp) {
          notifications.error(
            `Failed to set keyboard layout: ${resp.error.data || "Unknown error"}`,
          );
        }
        notifications.success("Keyboard layout set successfully");
        setKeyboardLayout(layout as string);
      });
    },
    [send, setKeyboardLayout],
  );

  const onKeyboardFormFactorChange = useCallback(
    (value: string[] | string) => {
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

  const formFactorOptions: Option[] = useMemo(
    () => keyboardFormFactorOptions.map(o => ({ value: o.value, label: o.label })),
    [],
  );

  const keysOptionsList: Option[] = [
    { label: "Show Pressed Keys", value: "show-pressed-keys" },
    { label: "Swap Command and Control", value: "swap-command-control" },
  ];

  const showPressedKeys = useSettingsStore(state => state.showPressedKeys);
  const setShowPressedKeys = useSettingsStore(state => state.setShowPressedKeys);
  const swapCommandControl = useSettingsStore(state => state.swapCommandControl);
  const setSwapCommandControl = useSettingsStore(state => state.setSwapCommandControl);
  const [keysOptions, setKeysOptions] = useState<string[]>([]);

  // Both toggles are rendered as one multi-select, so the option list has to mirror
  // the stored settings rather than hold its own state.
  useEffect(() => {
    const selected: string[] = [];
    if (showPressedKeys) selected.push("show-pressed-keys");
    if (swapCommandControl) selected.push("swap-command-control");
    setKeysOptions(selected);
  }, [showPressedKeys, swapCommandControl]);

  const handleShowPressedChange = (data: string[] | string) => {
    const selected = Array.isArray(data) ? data : [data];
    setShowPressedKeys(selected.includes("show-pressed-keys"));
    setSwapCommandControl(selected.includes("swap-command-control"));
  };

  const ledSyncOptions: Option[] = [
    { value: "host", label: "Host Only" },
    { value: "browser", label: "Browser Only" },
  ];

  const keyboardLedSync = useSettingsStore(state => state.keyboardLedSync);
  const setKeyboardLedSync = useSettingsStore(state => state.setKeyboardLedSync);
  const [ledSync, setLedSync] = useState<string>(keyboardLedSync);

  useEffect(() => {
    setLedSync(keyboardLedSync);
  }, [keyboardLedSync]);

  const handleLedChange = (data: string[] | string) => {
    const next = data as KeyboardLedSync;
    if (next === useSettingsStore.getState().keyboardLedSync) return;

    setKeyboardLedSync(next);
    // Whatever the previous source believed is not an answer for the new one. In
    // practice this is what made host mode look like it had already reported:
    // the browser path stamps reported=true, so its CapsLock survived the switch
    // and was drawn as host truth until the host pushed its first LED report.
    resetKeyboardLedTruth();

    // The layout's query effect reruns on source changes and rejects stale
    // responses. Browser state stays unknown until its next local sample.
  };

  const DividerLine = ({isMobile = false}: {isMobile?: boolean}) => {
    if (isMobile) {
      return (
        <div className="px-[20px] w-full">
            <Divider size={"small"} className="my-0" />
        </div>
      );
    }
    return <div className={`w-full h-px my-2 ${dark_line_style}`} />
  };

  //custom-keyboard-layout
  if (showMore && isMobile) {
    return (
      <div className={`${dark_bg2_style} px-[20px] w-full h-full`}>
        <div className={`flex justify-between items-center text-lg font-bold py-4 ${dark_font_style}`}>
          Keyboard Layout
          <Button
            type="text"
            icon={<CloseOutlined />}
            onClick={() => setShowMore(false)}
            className="text-gray-400"
          />
        </div>
        <KeyboardLayoutContent 
            value={safeKeyboardLayout}
            onChange={onKeyboardLayoutChange}
            layoutOptions={layoutOptions}
        />
      </div>
    );
  }

  if (isMobile && !showMore) {
    return (
      <div className={`w-full h-full flex flex-col ${dark_bg2_style}`}>
        <div className={`
          flex flex-col w-full mx-auto
          ${isDark ? 'text-white' : 'text-black'}
        `}>
          {/* Keyboard LEDs + active layout head the drawer. */}
          <KeyboardLedStatusRow />

          {/* LED State Synchronization */}
          <div className="px-[20px] pt-4">
            <ScrollThrottlingSelect
              mode="single"
              title={$at("LED State Synchronization")}
              options={ledSyncOptions}
              value={ledSync}
              onChange={handleLedChange}
            />
          </div>

          <DividerLine isMobile={true} />

          {/* Keyboard Layout */}
          <div className="px-[20px]">
            <ScrollThrottlingSelect
              mode="single"
              title={$at("Keyboard Layout")}
              options={layoutOptions}
              value={safeKeyboardLayout}
              onChange={onKeyboardLayoutChange}
              maxShowCount={maxShowCount}
              specialOptionText="More"
              specialOptionIcon={<LeftSVG />}
              onSpecialOptionClick={() => setShowMore(true)}
            />
          </div>

          <DividerLine isMobile={true} />

          {/* Keyboard Form Factor (ANSI / ISO / JIS) */}
          <div className="px-[20px]">
            <ScrollThrottlingSelect
              mode="single"
              title={$at("Keyboard Form Factor")}
              options={formFactorOptions}
              value={keyboardFormFactor}
              onChange={onKeyboardFormFactorChange}
            />
          </div>

          <DividerLine isMobile={true} />

          {/* Keys */}
          <div className="px-[20px]">
            <ScrollThrottlingSelect
              mode="multiple"
              title={$at("Keys")}
              options={keysOptionsList}
              value={keysOptions}
              onChange={handleShowPressedChange}
            />
          </div>
        </div>
      </div>
    );
  }

  return (
    <div style={{boxShadow: '0 4px 12px rgba(0, 0, 0, 0.15)'}} className={` ${isMobile ? 'px-[20px] pt-5 h-full w-full' : 'p-4 w-[240px] rounded'}  font-sans ${dark_bg2_style} border ${dark_bd_style}`}>
      <div className={`w-full h-full   flex flex-col justify-between`}>
        <ScrollThrottlingSelect
          mode="single"
          title={$at("LED State Synchronization")}
          options={ledSyncOptions}
          value={ledSync}
          onChange={handleLedChange}
        />

        <DividerLine />
        <ScrollThrottlingSelect
          mode="single"
          title={$at("Keyboard Layout")}
          options={layoutOptions}
          value={safeKeyboardLayout}
          onChange={onKeyboardLayoutChange}
          maxShowCount={maxShowCount}
          specialOptionText="More"
          specialOptionIcon={<LeftSVG />}
          onSpecialOptionClick={() => setShowMore(true)}
        />

        <DividerLine />
        <ScrollThrottlingSelect
          mode="single"
          title={$at("Keyboard Form Factor")}
          options={formFactorOptions}
          value={keyboardFormFactor}
          onChange={onKeyboardFormFactorChange}
        />

        <DividerLine />
        <ScrollThrottlingSelect
          mode="multiple"
          title={$at("Keys")}
          options={keysOptionsList}
          value={keysOptions}
          onChange={handleShowPressedChange}
        />
      </div>

      <KeyboardLayoutModal
        visible={showMore && isDesktop}
        onCancel={() => setShowMore(false)}
        value={safeKeyboardLayout}
        onChange={onKeyboardLayoutChange}
        layoutOptions={layoutOptions}
      />
    </div>
  );
};

export default KeyboardPanel;
