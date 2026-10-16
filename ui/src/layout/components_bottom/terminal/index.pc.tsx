import React, { useEffect, useRef, useState } from "react";
import { Tabs, Empty, Alert } from "antd";
import type { TabsProps } from "antd";

import { isMobile } from "react-device-detect";
import { clsx } from "clsx";
import { AnimatePresence,motion } from "framer-motion";
import { useReactAt } from "i18n-auto-extractor/react";

import TerminalKVM from "@/layout/components_bottom/terminal/TerminalKVM";
import TerminalSerial from "@/layout/components_bottom/terminal/TerminalSerial";
import { useRTCStore, useSerialStore, useUiStore } from "@/hooks/stores";
import {
  button_primary_color,

  dark_bg2_style,

  dark_font_style,
} from "@/layout/theme_color";
import { useThemeSettings } from "@routes/login_page/useLocalAuth";

const IndexPc: React.FC = () => {
  const { $at }= useReactAt(); 
  const setDisableKeyboardFocusTrap = useUiStore(state => state.setDisableVideoFocusTrap);
  const terminalType = useUiStore(state => state.terminalType);
  const { isDark } = useThemeSettings();
  const setTerminalType = useUiStore(state => state.setTerminalType);
  const selfRef = useRef<HTMLDivElement>(null);

  const kvmTerminal = useRTCStore(state => state.kvmTerminal);
  const serialConsole = useRTCStore(state => state.serialConsole);
  const checkPater = (e: MouseEvent) => {
    if (selfRef.current && selfRef.current.contains(e.target as Node)) {
      return true;
    }
    return false;
  };
  
  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      if (selfRef.current && !selfRef.current.contains(e.target as Node)) {
        setTerminalType("none");
      }
    };
    document.addEventListener("mousedown", handleClickOutside);
    return () => {
      document.removeEventListener("mousedown", handleClickOutside);
    };
  }, [setTerminalType, setDisableKeyboardFocusTrap]);

  const [activeTab, setActiveTab] = useState<string>("1");
  const [shouTabs, setShouTabs] = useState(false);
  useEffect(() => {
    if (terminalType === "kvm" || terminalType === "serial") {
      setShouTabs(true);
      if(terminalType == "kvm"){
        setActiveTab("1");
      }else{
        setActiveTab("2");
      }
    } else {
      setShouTabs(false);
    }
  }, [terminalType]);

  const isKVMEnabled = kvmTerminal !== null;
  const isSerialEnabled = serialConsole !== null;

  // Indicator light: green only when the serial channel is up AND the backend
  // port is really open (the port state is owned by the server).
  const serialConnected = useSerialStore(state => state.isConnected);
  const serialChannelOpen = serialConsole?.readyState === "open";
  const isSerialLive = isSerialEnabled && serialChannelOpen && serialConnected;
  const serialDotClass = clsx(
    "inline-block w-2.5 h-2.5 rounded-full shrink-0",
    isSerialLive
      ? "bg-emerald-500 shadow-[0_0_8px_2px_rgba(16,185,129,0.7)]"
      : "bg-gray-400 ring-1 ring-gray-500/50",
  );
  const serialDotTitle = isSerialLive
    ? $at("Serial port connected")
    : $at("Serial port disconnected");

  const renderCustomTabBar: TabsProps["renderTabBar"] = () => {
    return (
      <div className={`w-full flex justify-between p-[1px_8px] ${dark_bg2_style}`}>
        <div className="h-9 flex">
          <div
            className={clsx(`
              p-1 px-4  flex items-center gap-1 font-medium rounded-l   border  
              ${isKVMEnabled ? "cursor-pointer" : "cursor-not-allowed"}
              relative -mr-[2px]
            `,
              activeTab === "1" ? `${button_primary_color} !text-white` : `bg-[transparent] ${dark_font_style}`,
              `${isDark ? "border-[rgba(56,56,56,1)]" : "border-[rgba(229,229,229,1)]"}`,
            )
            }
            onClick={() => {
              if (isKVMEnabled) {
                setActiveTab("1");
                setDisableKeyboardFocusTrap(true);
              }
            }}
          >
            {$at("KVM Terminal")}
            {!isKVMEnabled && (
              <span className="text-xs text-red-500 font-normal">
                {$at("Not connected")}
              </span>
            )}
          </div>

          <div className="w-[1px]"></div>
          <div
            className={clsx(`
              p-1 px-4  flex items-center gap-1 font-medium rounded-r   border  
              ${isSerialEnabled ? "cursor-pointer" : "cursor-not-allowed"}
              relative -mr-[2px]
            `,
              activeTab === "2" ? `${button_primary_color} !text-white` : `bg-[transparent] ${dark_font_style}`,
              `${isDark ? "border-[rgba(56,56,56,1)]" : "border-[rgba(229,229,229,1)]"}`,
            )
            }
            onClick={() => {
              if (isSerialEnabled) {
                setActiveTab("2");
                setDisableKeyboardFocusTrap(true);
              }
            }}
          >
            <span title={serialDotTitle} className={serialDotClass} />
            {$at("Serial Console")}
            {!isSerialEnabled && (
              <span className="text-xs text-red-500 font-normal">
                {$at("Not connected")}
              </span>
            )}
          </div>
        </div>

      </div>
    );
  };

  const renderContent = (type: "kvm" | "serial", dataChannel: RTCDataChannel | null) => {
    if (!dataChannel) {
      return (
        <div className="flex justify-center items-center h-[30vh] flex-col gap-4">
          <Empty
            image={Empty.PRESENTED_IMAGE_SIMPLE}
            description={
              <span className="text-gray-500">
                  {type === "kvm" ? "KVM Terminal" : "Serial Console"} not connected
                </span>
            }
          />
          <Alert
              message="Terminal is not ready."
              description={`Please check ${type === "kvm" ? "KVM Terminal" : "Serial Console"} connection status.`}
              type="warning"
              showIcon
              className="w-4/5"
            />
        </div>
      );
    }

    return type === "kvm" ? (
      <TerminalKVM
        type="kvm"
        dataChannel={dataChannel}
        checkPater={checkPater}
      />
    ) : (
      <TerminalSerial
        type="serial"
        dataChannel={dataChannel}
        checkPater={checkPater}
      />
    );
  };

  const tabItems: TabsProps["items"] = [
    {
      key: "1",
      label: (
        <div className={`
          inline-flex items-center border-2 border-gray-800 px-2.5 m-0
          ${activeTab === "1" ? "text-black font-medium" : isKVMEnabled ? "text-gray-600" : "text-gray-300"}
          ${isKVMEnabled ? "cursor-pointer" : "cursor-not-allowed"}
          rounded-l
        `}>
          {$at("KVM Terminal")}
          {!isKVMEnabled && (
            <span className="text-xs text-red-500 font-normal ml-1">
              {$at("Not connected")}
            </span>
          )}
        </div>
      ),
      children: renderContent("kvm", kvmTerminal),
      disabled: !isKVMEnabled,
    },
    {
      key: "2",
      label: (
        <div className={clsx(`
          inline-flex items-center border-2 border-gray-800 px-2.5 m-0  rounded-r`,
          activeTab === "2"  ? `${button_primary_color } !text-white` : `bg-[transparent] ${dark_font_style}`
          )}>
          {$at("Serial Console")}
          {!isSerialEnabled && (
            <span className="text-xs text-red-500 font-normal ml-1">
              {$at("Not connected")}
            </span>
          )}
        </div>
      ),
      children: renderContent("serial", serialConsole),
      disabled: !isSerialEnabled,
    },
  ];

  const handleTabChange = (key: string) => {
    const targetTab = tabItems.find(item => item.key === key);
    if (targetTab?.disabled) {
      return;
    }
    setActiveTab(key);
  };

  return (
    <div
      className="transition-all duration-500 ease-in-out"
      style={{
        marginBottom: shouTabs && !isMobile ? "0px" : `-${500}px`,
      }}
    >
      <AnimatePresence>
        {shouTabs && !isMobile && (
          <motion.div
            initial={{ opacity: 0, y: "100%" }}
            animate={{ opacity: 1, y: "0%" }}
            exit={{ opacity: 0, y: "100%" }}
            transition={{
              duration: 0.5,
              ease: "easeInOut",
            }}
          >
            <div ref={selfRef} className="w-screen h-[45vh]  transition-all duration-500 ease-in-out">
              <Tabs
                items={tabItems}
                activeKey={activeTab}
                onChange={handleTabChange}
                renderTabBar={renderCustomTabBar}
                tabBarStyle={{
                  margin: 0,
                  padding: "0 0px",
                  backgroundColor: "#fff",
                }}
                indicator={{
                  size: 0,
                }}
              />
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
};

export default IndexPc;