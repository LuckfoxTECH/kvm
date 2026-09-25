import React, { useState } from "react";
import { Layout, Menu, MenuProps } from "antd";
import {
  SettingOutlined,
  WifiOutlined,
  SafetyCertificateOutlined,
  DesktopOutlined,
  ToolOutlined,
  TagOutlined,
  RightOutlined,
  AppstoreOutlined,
} from "@ant-design/icons";
import { useReactAt } from "i18n-auto-extractor/react";

import SettingsAccessIndex from "@/layout/components_setting/access/AccessContent";
import SettingsGeneral from "@/layout/components_setting/general/GeneralContent";
import SettingsNetwork from "@/layout/components_setting/network/NetworkContent";
import SettingsHardware from "@/layout/components_setting/hardware/HardwareContent";
import SettingsAdvanced from "@/layout/components_setting/advanced/AdvancedContent";
import SettingsVersion from "@/layout/components_setting/version/VersionContent";
import { KvmSwitchSettings } from "@/components/KvmSwitchSettings";
import { dark_bd_style, dark_bg2_style } from "@/layout/theme_color";

interface MenuItem {
  key: string;
  label: string;
  icon: React.ReactNode;
}

interface SettingsDialogProps {
  visible?: boolean;
  onClose?: () => void;
}

const SettingsModalPC: React.FC<SettingsDialogProps> = ({ visible = true }) => {
  const [selectedMenu, setSelectedMenu] = useState<string>("general");
  const { $at } = useReactAt();
  const menuItems: MenuItem[] = [
    { key: "general", label: "General", icon: <SettingOutlined /> },
    { key: "network", label: "Network", icon: <WifiOutlined /> },
    { key: "access", label: "Access", icon: <SafetyCertificateOutlined /> },
    { key: "hardware", label: "Hardware", icon: <DesktopOutlined /> },
    { key: "kvmswitch", label: "KVM Switch", icon: <AppstoreOutlined /> },
    { key: "advanced", label: "Advanced", icon: <ToolOutlined /> },
    { key: "version", label: "Version", icon: <TagOutlined /> },
  ];

  const handleMenuSelect: MenuProps["onClick"] = ({ key }) => {
    setSelectedMenu(key as string);
  };

  const renderContent = () => {
    switch (selectedMenu) {
      case "general":
        return <SettingsGeneral />;
      case "network":
        return <SettingsNetwork />;
      case "access":
        return <SettingsAccessIndex />;
      case "hardware":
        return <SettingsHardware />;
      case "kvmswitch":
        return <KvmSwitchSettings />;
      case "advanced":
        return <SettingsAdvanced />;
      case "version":
        return <SettingsVersion />;
      default:
        return <SettingsGeneral />;
    }
  };

  if (!visible) return null;

  return (
    <div style={{
      width: "80vw",
      maxWidth: 900,
      height: "80%",
      borderRadius: 8,
      display: "flex",
      overflow: "hidden",
      boxShadow: "0 4px 12px rgba(0,0,0,0.15)",
    }}
    className={`${dark_bg2_style} dark:border-[0.5px] dark:border-[rgba(80,80,80,1)]`}>
      <div className={`border-r ${dark_bg2_style} ${dark_bd_style}`}>
        <Menu
          mode="inline"
          selectedKeys={[selectedMenu]}
          onClick={handleMenuSelect}
          style={{ border: "none", height: "100%", width: 200 }}
          className={dark_bg2_style}
          items={menuItems.map(item => ({
            key: item.key,
            label: (
              <div style={{
                display: "flex",
                justifyContent: "space-between",
                alignItems: "center",
                width: "100%",
                padding: "4px 0",
              }}>
                <span>{$at(item.label)}</span>
                <RightOutlined style={{
                  fontSize: "12px",
                  marginLeft: "8px",
                }} />
              </div>
            ),
            icon: item.icon,
          }))}
        />
      </div>
      <Layout className={`${dark_bg2_style} hide-scrollbar`} style={{ flex: 1, padding: 24, overflow: "auto", maxHeight: "92vh", scrollbarWidth: "none", msOverflowStyle: "none" }}>
        {renderContent()}

      </Layout>
    </div>
  );
};


export default SettingsModalPC;
