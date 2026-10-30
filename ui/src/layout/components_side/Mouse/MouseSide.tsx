import { useEffect, useMemo, useState } from "react";
import { Checkbox, Slider } from "antd";
import { useReactAt } from "i18n-auto-extractor/react";

import { SettingsItem, SettingsItemNew } from "@components/Settings/SettingsView";
import SettingsSelect from "@components/Settings/SettingsSelect";
import { useSettingsStore } from "@/hooks/stores";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import notifications from "@/notifications";

const inputModeDescriptions: Record<string, string> = {
  absolute: "Maps the pointer to a position on the target screen",
  relative: "Sends movement deltas, the target adds its own acceleration",
  "digitizer-stylus": "Reports a pen: hover and a single contact",
  "touch-screen": "Reports a touch screen with up to ten contacts",
};

export default function MouseSide() {
  const { $at } = useReactAt();
  const [send] = useJsonRpc();

  // Labels go through $at so both the trigger and the popup follow the UI
  // language; the values are the wire names the backend stores.
  // Ordered absolute first: every mode below is a variant of "where is the
  // pointer", and the two relative modes only exist because some targets ignore it.
  const inputModeOptions = useMemo(
    () => [
      { label: $at("Absolute Mouse"), value: "absolute" },
      { label: $at("Relative Mouse"), value: "relative" },
      { label: $at("Touch Pen"), value: "digitizer-stylus" },
      { label: $at("Touch Screen"), value: "touch-screen" },
    ],
    [$at],
  );

  const scrollThrottlingOptions = useMemo(
    () => [
      { label: $at("Off"), value: "0" },
      { label: $at("Low"), value: "10" },
      { label: $at("Medium"), value: "25" },
      { label: $at("High"), value: "50" },
      { label: $at("Very High"), value: "100" },
    ],
    [$at],
  );

  const mouseMode = useSettingsStore(state => state.mouseMode);
  const setMouseMode = useSettingsStore(state => state.setMouseMode);
  const mouseSensitivity = useSettingsStore(state => state.mouseSensitivity);
  const setMouseSensitivity = useSettingsStore(state => state.setMouseSensitivity);
  const scrollThrottling = useSettingsStore(state => state.scrollThrottling);
  const setScrollThrottling = useSettingsStore(state => state.setScrollThrottling);
  const isCursorHidden = useSettingsStore(state => state.isCursorHidden);
  const setCursorVisibility = useSettingsStore(state => state.setCursorVisibility);
  const invertScroll = useSettingsStore(state => state.invertScroll);
  const setInvertScroll = useSettingsStore(state => state.setInvertScroll);

  // The jiggler lives on the device, not in the browser settings, so its state
  // has to be read back before the checkbox can show the truth.
  const [jiggler, setJiggler] = useState(false);
  useEffect(() => {
    send("getJigglerState", {}, resp => {
      if ("error" in resp) return;
      setJiggler(Boolean(resp.result));
    });
  }, [send]);

  const handleJigglerChange = (enabled: boolean) => {
    send("setJigglerState", { enabled }, resp => {
      if ("error" in resp) {
        notifications.error(
          `Failed to set jiggler state: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      setJiggler(enabled);
    });
  };

  // Same lookup guard as the keyboard drawer: an unknown value from an older
  // settings file must not be passed to $at as a translation key.
  const inputModeDescription = inputModeDescriptions[mouseMode];

  return (
    <div
      className="space-y-3"
      onKeyUp={e => e.stopPropagation()}
      onKeyDown={e => e.stopPropagation()}
    >
      <div className="space-y-4">
        <SettingsItem
          title={$at("Input Modes")}
          description={inputModeDescription ? $at(inputModeDescription) : ""}
        >
          <SettingsSelect
            value={mouseMode}
            options={inputModeOptions}
            onChange={value => setMouseMode(value)}
          />
        </SettingsItem>

        {/* Sensitivity only affects relative movement: the absolute modes derive
            their position from the video frame instead. */}
        {mouseMode === "relative" && (
          <SettingsItemNew
            title={$at("Speed Sensitivity")}
            description={`${mouseSensitivity.toFixed(1)}x`}
            className="flex-col w-full h-[40px]"
          >
            {/* The row is a centered flex column, so the slider needs an explicit
                full width or it collapses to its handle width and shows no rail. */}
            <Slider
              className="w-full"
              min={0.1}
              max={3}
              step={0.1}
              value={mouseSensitivity}
              onChange={value =>
                setMouseSensitivity(typeof value === "number" ? value : value[0])
              }
              styles={{
                rail: {
                  borderRadius: "4px",
                },
                track: {
                  borderRadius: "4px",
                },
              }}
            />
          </SettingsItemNew>
        )}

        <SettingsItem title={$at("Scroll Throttling")} description={$at("Caps wheel events per second")}>
          <SettingsSelect
            value={String(scrollThrottling)}
            options={scrollThrottlingOptions}
            onChange={value => setScrollThrottling(Number(value))}
          />
        </SettingsItem>

        <SettingsItem
          title={$at("Hide Cursor")}
          description={$at("Hide the local cursor")}
          noCol
          className="flex-row items-center"
        >
          <Checkbox
            checked={isCursorHidden}
            onChange={e => setCursorVisibility(e.target.checked)}
          />
        </SettingsItem>

        <SettingsItem
          title={$at("Jiggler")}
          description={$at("Nudge the pointer to keep the target awake")}
          noCol
          className="flex-row items-center"
        >
          <Checkbox checked={jiggler} onChange={e => handleJigglerChange(e.target.checked)} />
        </SettingsItem>

        <SettingsItem
          title={$at("Invert Scroll")}
          description={$at("Reverse vertical scrolling only; horizontal scrolling keeps its direction")}
          noCol
          className="flex-row items-center"
        >
          <Checkbox checked={invertScroll} onChange={e => setInvertScroll(e.target.checked)} />
        </SettingsItem>
      </div>
    </div>
  );
}
