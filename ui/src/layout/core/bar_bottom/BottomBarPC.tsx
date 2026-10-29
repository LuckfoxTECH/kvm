import React, { useEffect, useMemo, useState } from "react";
import KeyboardSVG from "@assets/second/keyboard.svg?react";
import Keyboard2SVG from "@assets/second/keyboard2.svg?react";
import MouseSVG from "@assets/second/mouse.svg?react";
import VideoSVG from "@assets/second/vedio.svg?react";
import MediaSVG from "@assets/second/media.svg?react";
import HdmlSVG from "@assets/second/hdml.svg?react";
import Hdml2SVG from "@assets/second/hdml2.svg?react";
import UsbSVG from "@assets/second/usb.svg?react";
import Usb2SVG from "@assets/second/usb2.svg?react";
import SwichDirSvg from "@assets/second/swich_dri1.svg?react";
import SwichDirSvg2 from "@assets/second/swich_dir2.svg?react";
import { useReactAt } from "i18n-auto-extractor/react";
import { Button as AntdButton, Typography } from "antd";
import { useInterval } from "usehooks-ts";
import StateSvg from "@assets/second/state.svg?react";

import {
  NetworkSettings,
  useAudioModeStore,
  useHidStore,
  useRTCStore,
  useSettingsStore,
  useUiStore,
  useUsbEpModeStore,
  useVideoStore,
  useVpnStore,
} from "@/hooks/stores";
import { keys, modifiers } from "@/keyboardMappings";
import { keyboards } from "@/keyboardLayouts";
import BottomPopoverButton from "@components/PopoverButton";
import MousePanel from "@components/MousePanel";
import KeyboardPanel from "@/layout/components_bottom/keyboard/KeyboardPanel";
import useUsbEpModeData from "@/layout/components_bottom/usbepmode/useUsbEpModeData";
import BarIconButton from "@/layout/core/bar_bottom/BarIconButton";
import RemoteStatusPanel, { remoteIconColor } from "@/layout/core/bar_bottom/RemoteStatusPanel";
import UsbDrawerPanel from "@/layout/core/bar_bottom/UsbDrawerPanel";
import NetworkSVG from "@assets/second/network.svg?react";
import HdmiStatusPanel from "@/layout/core/bar_bottom/HdmiStatusPanel";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import { dark_bg2_style, selected_bt_bg } from "@/layout/theme_color";
import { useThemeSettings } from "@routes/login_page/useLocalAuth";
import VolumeControl from "@components/VolumeControl";
const { Text } = Typography;

export default function BottomBarPC() {
  const { $at } = useReactAt();
  const {  isDark } = useThemeSettings();
  const activeKeys = useHidStore(state => state.activeKeys);
  const activeModifiers = useHidStore(state => state.activeModifiers);
  const audioMode = useAudioModeStore(state => state.audioMode);
  const usbEpMode = useUsbEpModeStore(state => state.usbEpMode);
  const setDisableFocusTrap = useUiStore(state => state.setDisableVideoFocusTrap);
  const toggleSidebarView = useUiStore(state => state.toggleSidebarView);
  const showPressedKeys = useSettingsStore(state => state.showPressedKeys);
  const forceHttp = useSettingsStore(state => state.forceHttp);
  const sidebarView = useUiStore(state => state.sidebarView);
  const tailScaleConnectionState = useVpnStore(state => state.tailScaleConnectionState);
  const zeroTierConnectionState = useVpnStore(state => state.zeroTierConnectionState);
  const tunnelEntries = useVpnStore(state => state.tunnelEntries);
  const usbState = useHidStore(state => state.usbState);
  // Emulation off means the gadget is deliberately stopped: the bar shows it in
  // the warning colour rather than as a healthy green link.
  const usbEmulationEnabled = useHidStore(state => state.usbEmulationEnabled);
  const usbActive = usbEmulationEnabled && usbState === "configured";
  const hdmiState = useVideoStore(state => state.hdmiState);
  const inputWidth = useVideoStore(state => state.inputWidth);
  const inputHeight = useVideoStore(state => state.inputHeight);
  const inputFps = useVideoStore(state => state.inputFps);
  const usbEpData = useUsbEpModeData();


  const keyboardLedState = useHidStore(state => state.keyboardLedState);
  // Host mode stays unknown until the host actually reports (see
  // KeyboardLedStatusRow for the reasoning).
  const ledsKnown = keyboardLedState?.reported === true;
  const keyboardLayout = useSettingsStore(state => state.keyboardLayout);
  const isTurnServerInUse = useRTCStore(state => state.isTurnServerInUse);

  const layoutAbbrev = useMemo(() => {
    if (!keyboardLayout) return "en_US";
    return keyboardLayout;
  }, [keyboardLayout]);

  const [hostname, setHostname] = useState("");
  const [send] = useJsonRpc();
  const peerConnection = useRTCStore(state => state.peerConnection);
  const mediaStream = useRTCStore(state => state.mediaStream);
  const [fps, setFps] = useState(0);
  useInterval(function collectWebRTCStats() {
    (async () => {
      if (forceHttp) return;
      if (!mediaStream) return;
      const videoTrack = mediaStream.getVideoTracks()[0];
      if (!videoTrack) return;
      const stats = await peerConnection?.getStats();


      stats?.forEach(report => {
        if (report.type === "inbound-rtp") {
          setFps(report.framesPerSecond);
        }
      });
    })();
  }, 500);

  useEffect(() => {
    send("getNetworkSettings", {}, resp => {
      if ("error" in resp) return;
      const data = resp.result as NetworkSettings;
      setHostname(data.hostname);
    });
  }, [send]);

  return (
    <div className={`${dark_bg2_style} border-t border-t-slate-800/30 text-slate-800 dark:border-t-slate-300/20 dark:text-white`}>
      <div className="flex flex-wrap items-stretch justify-between gap-1 h-[24px] ">
        <div className="flex items-center">
          <div className="flex flex-wrap items-center pl-2 gap-x-4">
            <Text style={{ fontSize: 12 }}>{hostname}:</Text>

            <BottomPopoverButton
              buttonIconNode={hdmiState === "ready" ? <Hdml2SVG fontSize={16} /> : <HdmlSVG fontSize={16} />}
              buttonText={$at("HDMI")}
              style={{ color: hdmiState === "ready" ? "rgba(0, 205, 27, 1)" : "inherit" }}
              align="left"
              panelContent={(
                <HdmiStatusPanel
                  hdmiState={hdmiState}
                  inputWidth={inputWidth}
                  inputHeight={inputHeight}
                  inputFps={inputFps}
                  liveFps={fps}
                />
              )}
            />

            <BottomPopoverButton
              buttonIconNode={usbActive ? <Usb2SVG fontSize={16} /> : <UsbSVG fontSize={16} />}
              buttonText={$at("USB")}
              style={{
                color: !usbEmulationEnabled
                  ? "rgba(250, 173, 20, 1)"
                  : usbActive
                    ? "rgba(0, 205, 27, 1)"
                    : "inherit",
              }}
              align="left"
              panelContent={(
                <UsbDrawerPanel
                  usbState={usbState}
                  usbEpMode={usbEpData.usbEpMode}
                  usbDeviceConfig={usbEpData.usbDeviceConfig}
                  audioMode={usbEpData.audioMode}
                  onUsbEpModeChange={usbEpData.onUsbEpModeChange}
                  onUsbConfigChange={usbEpData.onUsbConfigChange}
                  onAudioModeChange={usbEpData.onAudioModeChange}
                />
              )}
            />

            <BottomPopoverButton
              buttonIconNode={(
                <NetworkSVG
                  fontSize={16}
                  style={{ color: remoteIconColor(tailScaleConnectionState, zeroTierConnectionState, tunnelEntries) }}
                />
              )}
              buttonText={$at("Remote")}
              align="left"
              panelContent={(
                <RemoteStatusPanel
                  tailScaleState={tailScaleConnectionState}
                  zeroTierState={zeroTierConnectionState}
                />
              )}
            />

            {showPressedKeys && (
              <PressedKeysDisplay
                activeKeys={activeKeys}
                activeModifiers={activeModifiers}
                $at={$at}
              />
            )}
          </div>
        </div>
        {/**/}
        <div className="flex items-center h-full">

          {isTurnServerInUse && (
            <div className="shrink-0 p-1 px-1.5 text-xs text-black dark:text-white">
              Relayed by Cloudflare
            </div>
          )}
          <div style={{ width: "1px", height: "100%" }}
               className={"bg-[rgba(229,229,229,1)] dark:bg-[rgba(56,56,56,1)]"} />
          <BottomPopoverButton
            buttonText={$at("Keyboard")}
            iconOnly
            buttonIconNode={
              // icon and LED cluster live in one node so hovering anywhere on
              // the group reveals the "Keyboard" label
              <div className="flex items-center">
                <span className="pl-1 pr-1">
                  {isDark ? <Keyboard2SVG fontSize={16} /> : <KeyboardSVG fontSize={16} />}
                </span>
                <LedStatusButton
                  ledState={ledsKnown ? keyboardLedState?.num_lock : undefined}
                  text={$at("Num")}
                />
                <LedStatusButton
                  ledState={ledsKnown ? keyboardLedState?.caps_lock : undefined}
                  text={$at("Caps")}
                />
                <LedStatusButton
                  ledState={ledsKnown ? keyboardLedState?.scroll_lock : undefined}
                  text={$at("Scroll")}
                />
                <span className="pl-1 text-xs opacity-70" style={{ fontSize: 12 }}>{layoutAbbrev}</span>
              </div>
            }
            align="left"
            panelContent={<KeyboardPanel />}
          />
          <div style={{ width: "1px", height: "100%" }}
               className={"bg-[rgba(229,229,229,1)] dark:bg-[rgba(56,56,56,1)]"} />
                 
            <BottomPopoverButton
              buttonText={$at("Mouse")}
              buttonIconNode={<MouseSVG fontSize={16} />}
              align="left"
              iconOnly
              panelContent={<MousePanel />}
            />
          <div style={{ width: "1px", height: "100%" }}
               className={"bg-[rgba(229,229,229,1)] dark:bg-[rgba(56,56,56,1)]"} />
                 
          {(audioMode !== "disabled") && (
            <div className="hidden lg:block px-2 h-full">
              <VolumeControl size="XS" theme="light" />
              <div style={{ width: "1px", height: "100%" }}
                   className={"bg-[rgba(229,229,229,1)] dark:bg-[rgba(56,56,56,1)]"} />
            </div>
          )}

          {(usbEpMode == "mtp") && (
            <AntdButton
              type={"text"}
              size={"small"}
              icon={isDark ? <SwichDirSvg2 fontSize={16} /> : <SwichDirSvg fontSize={16} />}
              onClick={() => {
                setDisableFocusTrap(true);
                toggleSidebarView("SharedFolders");
              }}
              style={{height:"24px",borderRadius:0, fontSize: "12px", color: "inherit"}}
              className={`group ${sidebarView === "SharedFolders" ? selected_bt_bg : ""}`}
            >
              <span
                className="inline-block overflow-hidden whitespace-nowrap max-w-0 opacity-0 transition-all duration-200 ease-out group-hover:opacity-100 group-hover:max-w-[160px]"
                style={sidebarView === "SharedFolders" ? { maxWidth: 160, opacity: 1 } : undefined}
              >
                {$at("Shared Folders")}
              </span>
            </AntdButton>

          )}
          
          <div style={{ width: "1px", height: "100%" }}
               className={"bg-[rgba(229,229,229,1)] dark:bg-[rgba(56,56,56,1)]"} />

          <BarIconButton
            icon={<MediaSVG fontSize={16} />}
            label={$at("Virtual Media")}
            active={sidebarView === "VirtualMedia"}
            onClick={() => {
              setDisableFocusTrap(true);
              toggleSidebarView("VirtualMedia");
            }}
          />
          <div style={{ width: "1px", height: "100%" }}
               className={"bg-[rgba(229,229,229,1)] dark:bg-[rgba(56,56,56,1)]"} />
          <BarIconButton
            icon={<VideoSVG fontSize={16} />}
            label={$at("Video")}
            active={sidebarView === "SettingsVideo"}
            onClick={() => {
              setDisableFocusTrap(true);
              toggleSidebarView("SettingsVideo");
            }}
          />
          <div style={{ width: "1px", height: "100%" }}
               className={"bg-[rgba(229,229,229,1)] dark:bg-[rgba(56,56,56,1)]"} />
          {!forceHttp && (
          <div className="hidden md:block">

            <BarIconButton
              icon={<StateSvg fontSize={16} />}
              label={$at("Stats")}
              active={sidebarView === "connection-stats"}
              onClick={() => {
                toggleSidebarView("connection-stats");
              }}
            />
          </div>
          )}

          {keyboardLedState?.compose && (
            <div className="shrink-0 p-1 px-1.5 text-xs">{$at("Compose")}</div>
          )}
          {keyboardLedState?.kana && (
            <div className="shrink-0 p-1 px-1.5 text-xs">{$at("Kana")}</div>
          )}
        </div>
      </div>
    </div>
  );
}

interface LedStatusButtonProps {
  ledState: boolean | undefined;
  text: string;
}

function LedStatusButton({ ledState, text }: LedStatusButtonProps) {
  const unknown = ledState === undefined;
  return (
    <div className="flex items-center justify-center px-2" style={{ color: "inherit" }}>
      <div style={{
        width: "9px",
        height: "9px",
        borderRadius: "50%",
        backgroundColor: unknown
          ? "rgba(170, 170, 170, 0.55)"
          : ledState
            ? "rgba(0, 205, 27, 1)"
            : "rgba(205, 205, 205, 1)",
        marginRight: "6px",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        // Grey question mark for "unknown"; no text label on any mode.
        color: unknown ? "rgba(70, 70, 70, 0.95)" : "rgba(255, 255, 255, 0.95)",
        fontSize: "8px",
        lineHeight: 1,
      }}>{unknown ? "?" : null}</div>
      <span style={{ fontSize: 12, position: "relative", top: "1px" }}>{text}</span>
    </div>
  );
}

interface PressedKeysDisplayProps {
  activeKeys: any[];
  activeModifiers: any[];
  $at: any;
}

// Object.entries(...).find() instead of filter()[0]: a usage that is not in the
// table makes the lookup return undefined, and the previous filter(...)[0][0]
// threw "Cannot read properties of undefined (reading '0')" on any raw usage
// the caller sent that has no name. Fall back to the hex value so an unnamed
// usage is at least visible in the bar.
const usageLabel = (usage: number, table: Record<string, number>) =>
  Object.entries(table).find(([, value]) => value === usage)?.[0] ??
  `0x${usage.toString(16).padStart(2, "0")}`;

function PressedKeysDisplay({ activeKeys, activeModifiers, $at }: PressedKeysDisplayProps) {
  return (
    <div className="flex items-center gap-x-1" style={{ position: "relative", top: "1px", fontSize: 12}}>
      <span className="font-semibold">{$at("Keys")}:</span>
      <h2>
        {[
          ...activeKeys.map(usage => usageLabel(usage, keys)),
          ...activeModifiers.map(usage => usageLabel(usage, modifiers)),
        ].join(", ")}
      </h2>
    </div>
  );
}
