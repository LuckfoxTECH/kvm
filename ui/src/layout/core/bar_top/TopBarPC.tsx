import { LuMaximize, LuSettings } from "react-icons/lu";
import { FaKeyboard } from "react-icons/fa6";
import { Popover, PopoverButton, PopoverPanel } from "@headlessui/react";
import { Fragment, useEffect } from "react";
import ZhongDuanSvg from "@assets/second/zhongduan.svg?react";
import ZhongDuanSvg2 from "@assets/second/zhongduan2.svg?react";
import { useReactAt } from "i18n-auto-extractor/react";
import OpenSvg from "@assets/second/open.svg?react"
import CopeSvg from "@assets/second/copy.svg?react"
import { Button as AntdButton } from "antd";

import {
  useHidStore,
  useUiStore,
  useAudioModeStore,
} from "@/hooks/stores";
import Container from "@components/Container";
import { cx } from "@/cva.config";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import LogoLuckfox from "@assets/logo-luckfox.png";
import MacroTopBar from "@/layout/components_side/Macros/MacroTopBar";
import { KvmSwitchSelector } from "@/components/KvmSwitchSelector";
import { dark_bg2_style } from "@/layout/theme_color";
import { useTheme } from "@/layout/contexts/ThemeContext";

import SettingsModal from "../../components_setting";

export default function TopBarPC({
                                 requestFullscreen,

                               }: {
  requestFullscreen: () => Promise<void>;

}) {
  const { isDark } = useTheme();
  const virtualKeyboard = useHidStore(state => state.isVirtualKeyboardEnabled);
  const setVirtualKeyboard = useHidStore(state => state.setVirtualKeyboardEnabled);
  const toggleSidebarView = useUiStore(state => state.toggleSidebarView);
  const setDisableFocusTrap = useUiStore(state => state.setDisableVideoFocusTrap);
  const terminalType = useUiStore(state => state.terminalType);
  const setTerminalType = useUiStore(state => state.setTerminalType);
  // Audio related
  const [send] = useJsonRpc();
  const setAudioMode = useAudioModeStore(state => state.setAudioMode);
  const { $at } = useReactAt();

  useEffect(() => {
    send("getAudioMode", {}, resp => {
      if ("error" in resp) return;
      setAudioMode(String(resp.result));
    });
  }, [send]);

  return (

    <div className={"h-36px"}>
      <Container className={`border-b border-b-slate-800/20 bg-white dark:border-b-slate-300/20 ${dark_bg2_style}`}>
        <div
          onKeyUp={e => e.stopPropagation()}
          onKeyDown={e => e.stopPropagation()}
          className="flex flex-wrap items-center justify-between"
        >
          <div className="relative flex flex-wrap items-center gap-y-2">
            <a
              href="https://wiki.luckfox.com/intro/"
              target="_blank"
              rel="noopener noreferrer"
              className="flex items-center gap-4 -ml-3"
            >
              <img src={LogoLuckfox} alt="" className="h-[20px] dark:hidden" />
              <img src={LogoLuckfox} alt="" className="hidden h-[20px] dark:block" />
            </a>

            <Popover>
              <PopoverButton as={Fragment}>
                <AntdButton
                  type={"text"}
                  icon={<LuSettings/>}
                  className={"!rounded-none"}
                  onClick={() => setDisableFocusTrap(true)}
                >
                  {$at("Settings")}
                </AntdButton>
              </PopoverButton>

              <PopoverPanel
                anchor="bottom start"
                transition
                className={cx(
                  "z-10 flex origin-top flex-col overflow-visible!",
                  "flex origin-top flex-col transition duration-300 ease-out data-closed:-translate-y-8 data-closed:opacity-0", // 修改了 translate 的值
                  "w-[800px]",
                )}
              >
                {() => (
                  <div className="mx-auto w-full">
                    <SettingsModal />
                  </div>
                )}
              </PopoverPanel>
            </Popover>
            <AntdButton
              type={"text"}
              icon={<CopeSvg/>}
              className={"!rounded-none"}
              onClick={() => {
                setDisableFocusTrap(true);
                toggleSidebarView("Clipboard");
              }}
            >
              {$at("Clipboard")}
            </AntdButton>

            <AntdButton
              type={"text"}
              icon={<OpenSvg/>}
              className={"!rounded-none"}
              onClick={() => {
                setDisableFocusTrap(true);
                toggleSidebarView("PowerControl");
              }}
            >
              {$at("Power")}
            </AntdButton>
            
            <KvmSwitchSelector />

            <AntdButton
              type={"text"}
              className={"!rounded-none"}
              onMouseDown={
                (e: { stopPropagation: () => void; }) => {
                  e.stopPropagation();
                }
              }
              icon={isDark ? <ZhongDuanSvg2/> : <ZhongDuanSvg/>}
              onClick={() => {
                console.log("terminalType", terminalType)
                setTerminalType(terminalType === "kvm" ? "none" : "kvm")
              }}
            >
              {$at("Terminal")}
            </AntdButton>

            <div className="hidden lg:block">

              <AntdButton
                type={"text"}
                className={"!rounded-none"}
                icon={<FaKeyboard/>}
                onClick={() => {
                    if (!virtualKeyboard) {
                      setTimeout(() => {
                        setVirtualKeyboard(true);
                      }, 200);
                    } else {
                      setVirtualKeyboard(false);
                    }
                }}
              >
                {$at("Virtual Keyboard")}
              </AntdButton>
            </div>
            {/*  text={$at("Fullscreen")}*/}
            <div className="hidden items-center gap-x-2 lg:flex">
              <div className="h-4 w-px bg-slate-300 dark:bg-slate-600" />
              <AntdButton
                type={"text"}
                className={"!rounded-none"}
                icon={<LuMaximize/>}
                onClick={() => requestFullscreen()}
              >
              </AntdButton>
            </div>
          </div>
          <div className="flex flex-wrap items-center gap-x-1 gap-y-2">
            <MacroTopBar />
          </div>

        </div>
      </Container>
    </div>

  );
}
