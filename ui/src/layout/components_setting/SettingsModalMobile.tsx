import React, { useState } from "react";
import { Layout, Menu, MenuProps, theme as AntTheme, Button, Drawer } from "antd";
import {
  RightOutlined,
  ArrowLeftOutlined,
} from "@ant-design/icons";
import GeneralSvg from "@assets/second/general.svg?react";
import NetworkSvg from "@assets/second/network.svg?react";
import AccessSvg from "@assets/second/access.svg?react";
import AdvancedSvg from "@assets/second/advanced.svg?react";
import HardwareSvg from "@assets/second/hardware.svg?react";
import VersionSvg from "@assets/second/version.svg?react";
import { useReactAt } from "i18n-auto-extractor/react";
import { createStyles } from 'antd-style';
import { cx } from "cva";

import SettingsGeneral from "@/layout/components_setting/general/GeneralContent";
import SettingsAccessIndex from "@/layout/components_setting/access/AccessContent";
import SettingsNetwork from "@/layout/components_setting/network/NetworkContent";
import SettingsHardware from "@/layout/components_setting/hardware/HardwareContent";
import SettingsAdvanced from "@/layout/components_setting/advanced/AdvancedContent";
import SettingsVersion from "@/layout/components_setting/version/VersionContent";
import { KvmSwitchSettings } from "@/components/KvmSwitchSettings";
import { dark_bg2_style, text_color, text_primary_color } from "@/layout/theme_color";

const { Header, Content } = Layout;

interface MenuItem {
  key: string;
  label: string;
  icon: React.ReactNode;
}

type PageType = "menu" | "general" | "network" | "access" | "hardware" | "kvmswitch" | "advanced" | "version";

interface SettingsDialogProps {
  visible?: boolean;
  onClose?: () => void;
}

const useStyles = createStyles(({ css }) => ({
  customMenu: css`
    .ant-menu-item {
      margin: 0 !important;
      width: 100% !important;
      border-radius: 0 !important;
      padding-inline: 10px !important;
    }
    .ant-menu-item-selected {
      background-color: #e6f7ff;
    }
  `,
}));

const SettingsModalMobile: React.FC<SettingsDialogProps> = () => {
  const { styles } = useStyles();
  const [currentPage, setCurrentPage] = useState<PageType>("menu");
  const { $at } = useReactAt();
  const token = AntTheme.useToken();

  const menuItems: MenuItem[] = [
    { key: "general", label: "General", icon: <GeneralSvg /> },
    { key: "network", label: "Network", icon: <NetworkSvg /> },
    { key: "access", label: "Access", icon: <AccessSvg /> },
    { key: "hardware", label: "Hardware", icon: <HardwareSvg /> },
    { key: "kvmswitch", label: "KVM Switch", icon: <HardwareSvg /> },
    { key: "advanced", label: "Advanced", icon: <AdvancedSvg /> },
    { key: "version", label: "Version", icon: <VersionSvg /> },
  ];

  const handleMenuSelect: MenuProps["onClick"] = ({ key }) => {
    setCurrentPage(key as PageType);
  };

  const handleBack2 = () => {
    setCurrentPage("menu");
  };

  const getPageTitle = () => {
    const item = menuItems.find(item => item.key === currentPage);
    return item ? $at(item.label) : "Settings";
  };

  const renderContent = () => {
    switch (currentPage) {
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
        return null;
    }
  };

  const renderMenuPage = () => {
    return (

      <div style={{ height: "100%", overflow: "hidden" }} className={"py-[10px]"}>
        <Menu
          mode="inline"
          selectedKeys={[]}
          className={`border-r-0 ${styles.customMenu}`}
          onClick={handleMenuSelect}
          style={{
            border: "none",
            height: "100%",
            width: "100%",
            padding: "0px 0",
            backgroundColor: token.token.colorBgContainer,
            overscrollBehavior: "none",
          }}
          items={menuItems.map(item => ({
            key: item.key,
            label: (
              <div style={{
                display: "flex",
                justifyContent: "space-between",
                alignItems: "center",
                width: "100%",
                padding: "0px 0",
              }}>
                <span style={{ fontSize: "16px" }}>{$at(item.label)}</span>
                <RightOutlined style={{
                  fontSize: "12px",
                  color: token.token.colorTextSecondary,
                }} />
              </div>
            ),
            icon: React.cloneElement(item.icon as React.ReactElement, {
              // style: { fontSize: "18px" },
            }),
          }))}
        />
      </div>

    );
  };

  const renderContentPage = () => {
    return (
      <Drawer
        placement={"right"}
        open={currentPage !== "menu"}
        mask={false}
        closable={false}
        width={"100%"}
        drawerRender={() => (
          <div
            className={cx(dark_bg2_style, "!pointer-events-auto")}
            style={{
              width: "100vw",
              height: "100vh",
              position: "fixed",
              top: 0,
              left: 0,
              zIndex: 30000,
            }}>
            <Layout style={{ height: "100%" }} className={dark_bg2_style}>
              <Header
                className={dark_bg2_style}
                style={{
                  backgroundColor: token.token.colorBgContainer,
                  borderBottom: `1px solid ${token.token.colorBorderSecondary}`,
                  padding: "0 16px",
                  display: "flex",
                  alignItems: "center",
                  justifyContent: "center",
                  height: "56px",
                  lineHeight: "56px",
                  position: "relative",
                }}>
                <Button
                  type="text"
                  icon={<ArrowLeftOutlined />}
                  onClick={handleBack2}
                  className={text_primary_color}
                  style={{
                    marginRight: "16px",
                    border: "none",
                    boxShadow: "none",
                    position: "absolute",
                    left: "4px",
                  }}
                >Back</Button>
                <span
                  style={{
                    fontSize: "16px",
                    fontWeight: "400",
                  }}
                  className={text_color}>
              {getPageTitle()}
            </span>
              </Header>
              <Content style={{
                padding: "16px",
                overflow: "auto",
                height: "calc(100% - 56px)",
                paddingBottom: "60px",
                scrollbarWidth: "none",
                msOverflowStyle: "none",
              }}
              className="hide-scrollbar"
              >
                {renderContent()}
              </Content>
            </Layout>
          </div>
        )}
        styles={{header:{height:"0px"}}}
      >

      </Drawer>
    );
  };

  return (
    <>
      {renderMenuPage()}
      {renderContentPage()}  
    </>
  );
};

export default SettingsModalMobile;