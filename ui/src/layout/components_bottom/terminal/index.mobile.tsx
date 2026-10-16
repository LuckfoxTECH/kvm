import React, { useCallback, useState } from "react";
import { Button } from "antd";
import { useReactAt } from "i18n-auto-extractor/react";

import { useRTCStore, useSerialStore } from "@/hooks/stores";
import TerminalKVM from "@/layout/components_bottom/terminal/TerminalKVM";
import TerminalSerial from "@/layout/components_bottom/terminal/TerminalSerial";
import { dark_bd_style, dark_bg_style_fun } from "@/layout/theme_color";
import { useTheme } from "@/layout/contexts/ThemeContext";

type TabType = "kvm" | "serial";

const MobileTerminal: React.FC = () => {
  const { $at }= useReactAt(); 
  const [activeTab, setActiveTab] = useState<TabType>("kvm");
  const {isDark}=useTheme();
  const kvmTerminal = useRTCStore(state => state.kvmTerminal);
  const serialConsole = useRTCStore(state => state.serialConsole);

  // Indicator light: green only when the serial channel is up AND the backend
  // port is really open (the port state is owned by the server).
  const serialConnected = useSerialStore(state => state.isConnected);
  const serialChannelOpen = serialConsole?.readyState === "open";
  const isSerialLive = serialConsole !== null && serialChannelOpen && serialConnected;
  const serialDotClass = [
    "inline-block w-2.5 h-2.5 rounded-full shrink-0",
    isSerialLive
      ? "bg-emerald-500 shadow-[0_0_8px_2px_rgba(16,185,129,0.7)]"
      : "bg-gray-400 ring-1 ring-gray-500/50",
  ].join(" ");
  const serialDotTitle = isSerialLive
    ? $at("Serial port connected")
    : $at("Serial port disconnected");

  const renderContent = useCallback(() => {
    switch (activeTab) {
      case "kvm":
        return (kvmTerminal == null ? <></> :
            <TerminalKVM
              type="kvm"
              dataChannel={kvmTerminal}
            />
        );
      case "serial":
        return (serialConsole == null ? <></> :
          <TerminalSerial
            type="serial"
            dataChannel={serialConsole}
          />);
    }
  },[kvmTerminal, serialConsole,activeTab]);

  return (
    <div className="h-full w-full flex flex-col bg-transparent justify-between items-start">
      <div className="h-[calc(100%-50px)] w-full">
        {renderContent()}
      </div>

      <div className={`${dark_bg_style_fun(isDark)} w-screen h-[50px] flex flex-row justify-center items-center border-t ${dark_bd_style}`}>
        <Button
          type={activeTab === "kvm" ? "primary" : "default"}
          size="large"
          className="w-[48%] h-9 text-base font-medium !rounded-l-lg !rounded-r-none"
          onClick={() => setActiveTab("kvm")}
        >
          {$at("KVM Terminal")}
        </Button>

        <Button
          type={activeTab === "serial" ? "primary" : "default"}
          size="large"
          className="w-[48%] h-9 text-base font-medium !rounded-l-none !rounded-r-lg"
          onClick={() => setActiveTab("serial")}
        >
          <span className="inline-flex items-center gap-1">
            <span title={serialDotTitle} className={serialDotClass} />
            {$at("Serial Console")}
          </span>
        </Button>
      </div>
    </div>
  );
};

export default MobileTerminal;