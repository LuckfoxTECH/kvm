import React, { useCallback, useEffect, useState } from "react";
import { Button as AntdButton, Typography } from "antd";
import { useReactAt } from "i18n-auto-extractor/react";
import KeyboardSVG from "@assets/second/keyboard.svg?react";
import Keyboard2SVG from "@assets/second/keyboard2.svg?react";
import MouseSVG from "@assets/second/mouse.svg?react";
import VideoSVG from "@assets/second/vedio.svg?react";
import Video2SVG from "@assets/second/vedio2.svg?react";
import MediaSVG from "@assets/second/media.svg?react";
import HdmlSVG from "@assets/second/hdml.svg?react";
import Hdml2SVG from "@assets/second/hdml2.svg?react";
import UsbSVG from "@assets/second/usb.svg?react";
import Usb2SVG from "@assets/second/usb2.svg?react";
import NetworkSVG from "@assets/second/network.svg?react";
import { useInterval } from "usehooks-ts";

import {
  AvailableSidebarViews,
  NetworkSettings,
  useHidStore,
  useRTCStore,
  useSettingsStore,
  useUiStore,
  useVpnStore,
} from "@/hooks/stores";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import { remoteIconColor } from "@/layout/core/bar_bottom/RemoteStatusPanel";
import { keyboards } from "@/keyboardLayouts";
import {
  button_primary_color,
  dark_bd_style,
 dark_bg_style_fun,
  dark_font_style,

  text_primary_color,
} from "@/layout/theme_color";
import { useTheme } from "@/layout/contexts/ThemeContext";

const { Text } = Typography;
const views = [
  "KeyboardPanel",
  "MousePanel",
  "VirtualMedia",
  "SettingsVideo",
];
export default function BottomBarMobile() {
  const { $at } = useReactAt();
  const { isDark } = useTheme();
  const setDisableFocusTrap = useUiStore(state => state.setDisableVideoFocusTrap);
  const toggleSidebarView = useUiStore(state => state.toggleSidebarView);
  const forceHttp = useSettingsStore(state => state.forceHttp);

  const peerConnectionState = useRTCStore(state => state.peerConnectionState);
  const tailScaleConnectionState = useVpnStore(state => state.tailScaleConnectionState);
  const zeroTierConnectionState = useVpnStore(state => state.zeroTierConnectionState);
  const tunnelEntries = useVpnStore(state => state.tunnelEntries);
  const usbState = useHidStore(state => state.usbState);
  const usbEmulationEnabled = useHidStore(state => state.usbEmulationEnabled);
  const usbActive = usbEmulationEnabled && usbState === "configured";

  const [hostname, setHostname] = useState("");
  const [activeTab, setActiveTab] = useState<number>(-1);
  const [send] = useJsonRpc();
  const peerConnection = useRTCStore(state => state.peerConnection);
  const mediaStream = useRTCStore(state => state.mediaStream);
  const [fps, setFps] = useState(0);
  const isVirtualKeyboardEnabled = useHidStore(state => state.isVirtualKeyboardEnabled);
  useInterval(function collectWebRTCStats() {
    (async () => {
      if (forceHttp) return;
      if (!mediaStream) return;
      const videoTrack = mediaStream.getVideoTracks()[0];
      if (!videoTrack) return;
      const stats = await peerConnection?.getStats();


      stats?.forEach(report => {
        if (report.type === "inbound-rtp") {
          setFps(report.framesPerSecond ?? 0);
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


  const handleTabClick = useCallback((index: number) => {

    if (index < views.length) {
      setDisableFocusTrap(true);
      toggleSidebarView(views[index] as AvailableSidebarViews);
    }

    setActiveTab(activeTab === index ? -1 : index);
  }, [activeTab, setDisableFocusTrap, toggleSidebarView]);

  const activeColor = "#2563eb";
  const inactiveColor = "#000";

  const tabs = [
    { icon: isDark ? Keyboard2SVG : KeyboardSVG, label: $at("keyboard") },
    { icon: MouseSVG, label: $at("mouse") },
    { icon: MediaSVG, label: $at("media") },
    { icon: isDark ? Video2SVG : VideoSVG, label: $at("video") },
  ];
  if(isVirtualKeyboardEnabled){
    return <></>
  }
  return (
    <div className={`w-[100vw] flex flex-col ${dark_bg_style_fun(isDark)}`}>
      <div className={`h-[38px] w-full ${dark_bg_style_fun(isDark)} flex flex-row items-center
                      justify-between bg-white border-t border-gray-200 ${dark_bd_style}`}>
        <div className="flex-1 h-full flex flex-row items-center">
          {tabs.map((tab, index) => (
            <TabButton
              key={index}
              icon={tab.icon}
              index={index}
              activeTab={activeTab}
              activeColor={activeColor}
              inactiveColor={inactiveColor}
              tabCount={tabs.length}
              onClick={handleTabClick}
            />
          ))}
        </div>
      </div>

      <div className={`h-[38px] w-full flex flex-row flex-wrap items-center justify-start gap-1 px-2 bg-white border-t border-gray-200 ${dark_bg_style_fun(isDark)} border-b ${dark_bd_style}`}>
        <Text style={{ fontSize: 12 }}>{hostname}:</Text>
        <div onClick={() => { setDisableFocusTrap(true); toggleSidebarView("HdmiStatus"); }}>
          <ConnectionStatusButton
            icon={peerConnectionState ? <Hdml2SVG /> : <HdmlSVG />}
            text={$at("HDMI")}
            isActive={!!peerConnectionState}
          />
        </div>
        <div onClick={() => toggleSidebarView("UsbEpModeSelect")}>
          <ConnectionStatusButton
            icon={usbActive ? <Usb2SVG /> : <UsbSVG />}
            text={$at("USB")}
            isActive={usbActive}
            color={!usbEmulationEnabled ? "rgba(250, 173, 20, 1)" : undefined}
          />
        </div>
        <div onClick={() => { setDisableFocusTrap(true); toggleSidebarView("RemoteStatus"); }}>
          <ConnectionStatusButton
            icon={<NetworkSVG style={{ color: remoteIconColor(tailScaleConnectionState, zeroTierConnectionState, tunnelEntries) }} />}
            text={$at("Remote")}
            isActive={false}
          />
        </div>
      </div>
    </div>
  );
}

interface TabButtonProps {
  icon: React.ComponentType<any>;
  index: number;
  activeTab: number;
  activeColor: string;
  inactiveColor: string;
  onClick: (index: number) => void;
  disabled?: boolean;
  tabCount: number;
}

function TabButton({ icon: Icon, index, onClick, disabled, tabCount }: TabButtonProps) {
  const sidebarView = useUiStore(state => state.sidebarView);
  const isActive = !disabled && sidebarView === views[index];

  return (
    <div
      style={{ width: `${100 / tabCount}%` }}
      className={`
        h-full flex justify-center items-center ${disabled ? "cursor-default" : "cursor-pointer"} relative
        transition-all duration-200 ease-in-out
        ${isActive ? "text-[rgba(22,152,217,1)]" : `text-gray-600 ${dark_font_style}`}
      `}
      onClick={() => {
        if (!disabled) {
          onClick(index);
        }
      }}
    >
      <Icon
        style={{
          width: 18,
          height: 18,
        }}
        className={isActive?text_primary_color:dark_font_style}
      />
      <div
        className={`
          absolute bottom-0 left-0 w-full h-0.5 ${button_primary_color} transition-all duration-200
          ${isActive ? "opacity-100" : "opacity-0"}
        `}
      />
    </div>
  );
}

interface ConnectionStatusButtonProps {
  icon: React.ReactNode;
  text: string;
  isActive: boolean;
  /** Overrides the derived active/inactive colour, used for the stopped state. */
  color?: string;
}

function ConnectionStatusButton({ icon, text, isActive, color }: ConnectionStatusButtonProps) {
  return (
    <AntdButton
      icon={icon}
      type="text"
      size="small"
      style={{
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        color: color ?? (isActive ? "rgba(0, 205, 27, 1)" : "rgba(205, 205, 205, 1)"),
        fontSize: 12,
      }}
    >
      {text}
    </AntdButton>
  );
}

