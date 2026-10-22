import { useEffect, useCallback, useRef, useState } from "react";
import { Select as SelectMenuBasic , Button as AntdButton, Slider } from "antd";
import {useReactAt} from 'i18n-auto-extractor/react'
import { isMobile } from "react-device-detect";

import { SettingsPageHeader } from "@components/Settings/SettingsPageheader";
import { SettingsItem } from "@components/Settings/SettingsView";
import { BacklightSettings, useSettingsStore } from "@/hooks/stores";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import { InputField } from "@components/InputField";
import notifications from "@/notifications";


export default function SettingsHardware() {
  const { $at }= useReactAt();
  const [send] = useJsonRpc();
  const settings = useSettingsStore();

  const setDisplayRotation = useSettingsStore(state => state.setDisplayRotation);

  const handleDisplayRotationChange = (rotation: string) => {
    setDisplayRotation(rotation);
    handleDisplayRotationSave();
  };

  const handleDisplayRotationSave = () => {
    send("setDisplayRotation", { params: { rotation: settings.displayRotation } }, resp => {
      if ("error" in resp) {
        notifications.error(
          `Failed to set display orientation: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      notifications.success("Display orientation updated successfully");
    });
  };

  const setBacklightSettings = useSettingsStore(state => state.setBacklightSettings);

  // The slider is dragged continuously, so keep a local value for smooth feedback
  // and only persist it after the user stops moving for a moment.
  const [brightness, setBrightness] = useState(settings.backlightSettings.max_brightness);
  const brightnessSaveTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Follow changes made on the touchscreen or by another browser.
  useEffect(() => {
    setBrightness(settings.backlightSettings.max_brightness);
  }, [settings.backlightSettings.max_brightness]);

  useEffect(() => {
    return () => {
      if (brightnessSaveTimer.current) clearTimeout(brightnessSaveTimer.current);
    };
  }, []);

  const sendBacklightSettings = useCallback((bl: BacklightSettings) => {
    send("setBacklightSettings", { params: bl }, resp => {
      if ("error" in resp) {
        notifications.error(
          `Failed to set backlight settings: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
    });
  }, [send]);

  const handleBrightnessChange = (value: number) => {
    setBrightness(value);

    const next = { ...useSettingsStore.getState().backlightSettings, max_brightness: value };
    setBacklightSettings(next);

    if (brightnessSaveTimer.current) clearTimeout(brightnessSaveTimer.current);
    brightnessSaveTimer.current = setTimeout(() => {
      sendBacklightSettings(next);
    }, 300);
  };

  const handleBacklightSettingsChange = (settings: BacklightSettings) => {
    // If the user has set the display to dim after it turns off, set the dim_after
    // value to never.
    if (settings.dim_after > settings.off_after && settings.off_after != 0) {
      settings.dim_after = 0;
    }

    setBacklightSettings(settings);
    handleBacklightSettingsSave();
  };

  const handleBacklightSettingsSave = () => {
    send("setBacklightSettings", { params: settings.backlightSettings }, resp => {
      if ("error" in resp) {
        notifications.error(
          `Failed to set backlight settings: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      notifications.success("Backlight settings updated successfully");
    });
  };

  useEffect(() => {
    send("getBacklightSettings", {}, resp => {
      if ("error" in resp) {
        return notifications.error(
          `Failed to get backlight settings: ${resp.error.data || "Unknown error"}`,
        );
      }
      const result = resp.result as BacklightSettings;
      setBacklightSettings(result);
    });

    send("getDisplayState", {}, resp => {
      if ("error" in resp) return;
      const result = resp.result as { on: boolean };
      useSettingsStore.getState().setDisplayOn(result.on);
    });
  }, [send, setBacklightSettings]);

  // One click display off / on, mirrored by the displayState event.
  const handleToggleDisplay = () => {
    send("toggleDisplay", {}, resp => {
      if ("error" in resp) {
        notifications.error(
          `Failed to switch display: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      const result = resp.result as { on: boolean };
      useSettingsStore.getState().setDisplayOn(result.on);
    });
  };

  const setTimeZone = useSettingsStore(state => state.setTimeZone);

  const handleTimeZoneSave = () => {
    send("setTimeZone", { timeZone: settings.timeZone }, resp => {
      if ("error" in resp) {
        notifications.error(
          `Failed to set time zone: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      notifications.success("Time zone updated successfully");
    });
  };
  
  const handleTimeZoneChange = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    const value = e.target.value.trim();
    setTimeZone(value);
  }, []);

  useEffect(() => {
    send("getTimeZone", {}, resp => {
      if ("error" in resp) {
        return notifications.error(
          `Failed to get time zone: ${resp.error.data || "Unknown error"}`,
        );
      }
      console.log("Time zone:", resp.result);
      const result = resp.result as string;
      setTimeZone(result);
    });
  }, [send, setTimeZone]);
  
  const setLedGreenMode = useSettingsStore(state => state.setLedGreenMode);
  const setLedYellowMode = useSettingsStore(state => state.setLedYellowMode);

  const handleLedGreenModeChange = (mode: string) => {
    setLedGreenMode(mode);
    send("setLedGreenMode", { mode }, resp => {
      if ("error" in resp) {
        notifications.error(
          `Failed to set LED-Green mode: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      notifications.success("LED-Green mode updated successfully");
    });
  };

  const handleLedYellowModeChange = (mode: string) => {
    setLedYellowMode(mode);
    send("setLedYellowMode", { mode }, resp => {
      if ("error" in resp) {
        notifications.error(
          `Failed to set LED-Yellow mode: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      notifications.success("LED-Yellow mode updated successfully");
    });
  };
  
  useEffect(() => {
    send("getLedGreenMode", {}, resp => {
      if ("error" in resp) {
        return notifications.error(
          `Failed to get LED-Green mode: ${resp.error.data || "Unknown error"}`,
        );
      }
      const result = resp.result as string;
      setLedGreenMode(result);
    });    

    send("getLedYellowMode", {}, resp => {
      if ("error" in resp) {
        return notifications.error(
          `Failed to get LED-Yellow mode: ${resp.error.data || "Unknown error"}`,
        );
      }
      const result = resp.result as string;
      setLedYellowMode(result);
    });    
  }, [send, setLedGreenMode, setLedYellowMode]);
  
  return (
    <div className="space-y-4">
      <SettingsPageHeader
        title={$at("Hardware")}
        description={$at("Configure display settings and hardware options for your KVM device")}
      />
      <div className="space-y-4">
        <SettingsItem
          title={$at("Display Orientation")}
          description={$at("Set the orientation of the display")}
        >
          <SelectMenuBasic
            value={settings.displayRotation.toString()}
            className={`${isMobile?"w-full":"h-[36px] w-[22%]"}`}
            options={[
              { value: "180", label: "Normal" },
              { value: "90", label: "90" },
              { value: "0", label: "180" },
              { value: "270", label: "270" },
            ]}
            onChange={e => {
              settings.displayRotation = e;
              handleDisplayRotationChange(settings.displayRotation);
            }}
          />
        </SettingsItem>
        <div className="select-none">
          <SettingsItem
            title={$at("Display Brightness")}
            description={$at("Set the brightness of the display")}
            noCol
          >
            <AntdButton className="shrink-0" onClick={handleToggleDisplay}>
              {settings.displayOn ? $at("Screen Off") : $at("Screen On")}
            </AntdButton>
          </SettingsItem>
          <div className="flex items-center gap-3 px-2 pb-1">
            <Slider
              className="flex-1"
              min={10}
              max={100}
              step={1}
              value={brightness}
              tooltip={{ formatter: value => `${value}%` }}
              onChange={handleBrightnessChange}
            />
            <span className="w-[38px] shrink-0 text-right">{brightness}%</span>
          </div>
        </div>
        {/* <SettingsItem
          title="Enable Ctrl+Alt+Del Action Bar"
          description="Enable or disable the action bar action for sending a Ctrl+Alt+Del to the host"
        >
          <Checkbox
            checked={actionBarConfig.ctrlAltDel}
            onChange={onActionBarItemChange("ctrlAltDel")}
          />
        </SettingsItem> */}
        {settings.backlightSettings.max_brightness != 0 && (
          <>
            <SettingsItem
              title={$at("Dim Display After")}
              description={$at("Set how long to wait before dimming the display")}
            >
              <SelectMenuBasic
                value={settings.backlightSettings.dim_after.toString()}
                className={`${isMobile?"w-full":"h-[36px] w-[22%]"}`}
                options={[
                  { value: "0", label: "Never" },
                  { value: "60", label: "1 Minute" },
                  { value: "300", label: "5 Minutes" },
                  { value: "600", label: "10 Minutes" },
                  { value: "1800", label: "30 Minutes" },
                  { value: "3600", label: "1 Hour" },
                ]}
                onChange={e => {
                  settings.backlightSettings.dim_after = parseInt(e);
                  handleBacklightSettingsChange(settings.backlightSettings);
                }}
              />
            </SettingsItem>
            <SettingsItem
              title={$at("Turn off Display After")}
              description={$at("Period of inactivity before display automatically turns off")}
            >
              <SelectMenuBasic
                value={settings.backlightSettings.off_after.toString()}
                className={`${isMobile?"w-full":"h-[36px] w-[22%]"}`}
                options={[
                  { value: "0", label: "Never" },
                  { value: "300", label: "5 Minutes" },
                  { value: "600", label: "10 Minutes" },
                  { value: "1800", label: "30 Minutes" },
                  { value: "3600", label: "1 Hour" },
                ]}
                onChange={e => {
                  settings.backlightSettings.off_after = parseInt(e);
                  handleBacklightSettingsChange(settings.backlightSettings);
                }}
              />
            </SettingsItem>
          
            <p className="text-xs text-slate-600 dark:text-[#ffffff]">
              {$at("The display will wake up when the connection state changes, or when touched.")}
            </p>

          </>
        )}

        <SettingsItem
          title={$at("Time Zone")}
          description={$at("Set the time zone for the clock")}
        >
        </SettingsItem>
        <div className="space-y-4">  
          <div className="flex items-end gap-x-2">
            <InputField
              size="SM"
              value={settings.timeZone.toString()}
              className={`${isMobile?"w-full":""}`}
              onChange={handleTimeZoneChange}
              placeholder="Enter Time Zone"
            /> 
            <AntdButton
              type="primary"
              className="!h-[38px]"
              onClick={handleTimeZoneSave}
            >{$at("Set")}</AntdButton>
          </div>
        </div>

        <SettingsItem
          title={$at("LED-Green Type")}
          description={$at("Set the type of system status indicated by the LED-Green")}
        >
          <SelectMenuBasic
            value={settings.ledGreenMode.toString()}
            className={`${isMobile?"w-full":"h-[36px] w-[22%]"}`}
            options={[
              { value: "disabled", label: $at("Disabled") },
              { value: "network-link", label: $at("network-link") },
              { value: "network-tx", label: $at("network-tx") },
              { value: "network-rx", label: $at("network-rx") },
              { value: "kernel-activity", label: $at("kernel-activity") },
            ]}
            onChange={e => {
              settings.ledGreenMode = e;
              handleLedGreenModeChange(settings.ledGreenMode);
            }}
          />
        </SettingsItem>

        <SettingsItem
          title={$at("LED-Yellow Type")}
          description={$at("Set the type of system status indicated by the LED-Yellow")}
        >

          <SelectMenuBasic
            value={settings.ledYellowMode.toString()}
            className={`${isMobile?"w-full":"h-[36px] w-[22%]"}`}
            options={[
              { value: "disabled", label: $at("Disabled") },
              { value: "network-link", label: $at("network-link") },
              { value: "network-tx", label: $at("network-tx") },
              { value: "network-rx", label: $at("network-rx") },
              { value: "kernel-activity", label: $at("kernel-activity") },
            ]}
            onChange={e => {
              settings.ledYellowMode = e;
              handleLedYellowModeChange(settings.ledYellowMode);
            }}
          />
        </SettingsItem>

      </div>

      {/*<UsbEpModeSetting />*/}
      {/*<UsbDeviceSetting /> */}
      {/*<UsbInfoSetting /> */}
    </div>
  );
}
