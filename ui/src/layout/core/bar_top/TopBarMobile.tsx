import { LuMaximize } from "react-icons/lu";
import { BsWrench } from "react-icons/bs";
import { useReactAt } from "i18n-auto-extractor/react";
import OpenSvg from "@assets/second/open.svg?react";
import CopeSvg from "@assets/second/copy.svg?react";
import Setting2Svg from "@assets/second/set2.svg?react";
import Setting1Svg from "@assets/second/set1.svg?react";
import ZhongDuanSvg from "@assets/second/zhongduan.svg?react";
import ZhongDuanSvg2 from "@assets/second/zhongduan2.svg?react";

import LogoLuckfox from "@assets/logo-luckfox.png";
import { useHidStore, useUiStore } from "@/hooks/stores";
import MacroTopBar from "@/layout/components_side/Macros/MacroTopBar";
import { BarsPinButton } from "@/layout/core/bar_autohide";
import {
  button_primary_color,
  dark_bd_style,
  dark_bg_style_fun,
  dark_font_style,
  text_primary_color,
} from "@/layout/theme_color";
import { useTheme } from "@/layout/contexts/ThemeContext";

export default function MobileTopBar({ requestFullscreen }: { requestFullscreen: () => Promise<void> }) {
  const setVirtualKeyboard = useHidStore(state => state.setVirtualKeyboardEnabled);
  const setDisableFocusTrap = useUiStore(state => state.setDisableVideoFocusTrap);
  const toggleTopBarView = useUiStore(state => state.toggleTopBarView);
  const toggleSidebarView = useUiStore(state => state.toggleSidebarView);
  const setSidebarView = useUiStore(state => state.setSidebarView);

  const { isDark } = useTheme();

  const sidebarView = useUiStore(state => state.sidebarView);
  const topBarView = useUiStore(state => state.topBarView);
  const topBarButtons = [
    {
      id: "logo",
      icon: Logo,
      onClick: () => { //pass
        },
      selected:false
    },
    {
      id: "settings",
      icon: isDark?Setting2Svg:Setting1Svg,
      onClick: () => {
        setDisableFocusTrap(true);
        setVirtualKeyboard(false);
        if(topBarView==="SettingsModal"){
          toggleTopBarView(topBarView);
          return
        }
        toggleTopBarView(topBarView);
        // toggleTopBarView("SettingsModal");
        setTimeout(() => {
          toggleTopBarView("SettingsModal");
        }, 30);
      },
      selected:topBarView==="SettingsModal"
    },
    {
      id: "clipboard",
      icon: CopeSvg,
      onClick: () => {
        // setDisableFocusTrap(true);
        setVirtualKeyboard(false);
        if(topBarView==="ClipboardMobile"){
          toggleTopBarView(topBarView);
          return
        }
        toggleTopBarView(topBarView);
        setTimeout(() => {
          toggleTopBarView("ClipboardMobile");
        }, 30);

      },
      selected:topBarView==="ClipboardMobile"
    },
    {
      id: "power",
      icon: OpenSvg,
      onClick: () => {
        setDisableFocusTrap(true);
        setVirtualKeyboard(false);
        toggleSidebarView("PowerControl");
      },
      selected:sidebarView==="PowerControl"
    },
    {
      id: "terminal",
      icon: isDark ? ZhongDuanSvg2 : ZhongDuanSvg,
      onClick: () => {
        setDisableFocusTrap(true);
        setVirtualKeyboard(false);
        toggleSidebarView("TerminalTabsMobile");
      },
      selected:sidebarView==="TerminalTabsMobile"
    },
    {
      id: "fullscreen",
      icon: LuMaximize,
      onClick: () =>{
        setSidebarView(null);
        setVirtualKeyboard(false);

        requestFullscreen();
        } ,
      selected:topBarView==="Fullscreen"
    }
  ];

  return (
    <div>
      <div className={`w-[100vw] flex flex-col ${dark_bg_style_fun(isDark)}`}>
        <div className={`h-10 w-full flex flex-row flex-wrap items-center ${dark_bg_style_fun(isDark)}
        justify-evenly  bg-white border-t border-b ${dark_bd_style}`}>

          {topBarButtons.map(button => (

              <div
                key={button.id}
                onClick={button.onClick}
                onMouseDown={button.id === "terminal" ? (e: {
                  stopPropagation: () => void;
                }) => e.stopPropagation() : undefined}
                className={`
                  h-full w-1/6 flex justify-center items-center cursor-pointer relative
                  transition-all duration-200 ease-in-out
                  hover:bg-gray-50
                  ${button.selected ? "text-[rgba(22,152,217,1)]" : `text-black ${dark_font_style}`}
                `}
              >
                <button.icon        style={{
                  width: 18,
                  height: 18,
                }}
                className={button.selected?text_primary_color:dark_font_style}
                />
                <div className={`
            absolute bottom-0 left-0 w-full h-0.5 ${button_primary_color} transition-all duration-200
            ${button.selected ? "opacity-100" : "opacity-0"}
          `} />
              </div>


          ))}

        </div>
      </div>
      {
        topBarView !== "SettingsModal" &&  topBarView !== "ClipboardMobile" &&
        sidebarView !== "TerminalTabsMobile"&&
        sidebarView !== "PowerControl"&&
        <div className={`flex h-10 w-full items-center bg-white border-b ${dark_bd_style} ${dark_bg_style_fun(isDark)}`}>
          <div className="min-w-0 flex-1">
            <MacroTopBar />
          </div>
          <ToolsToggleButton />
          <BarsPinButton fontSize={14} />
        </div>
      }

    </div>
  );
}

function ToolsToggleButton() {
  const { $at } = useReactAt();
  const mobileToolsHidden = useUiStore(state => state.mobileToolsHidden);
  const setMobileToolsHidden = useUiStore(state => state.setMobileToolsHidden);
  const label = mobileToolsHidden ? $at("Show tools") : $at("Hide tools");

  return (
    <button
      type="button"
      title={label}
      aria-label={label}
      aria-pressed={!mobileToolsHidden}
      onClick={() => setMobileToolsHidden(!mobileToolsHidden)}
      className={`h-full px-3 shrink-0 bg-transparent border-0 cursor-pointer ${
        mobileToolsHidden ? `text-black ${dark_font_style}` : text_primary_color
      }`}
    >
      <BsWrench fontSize={14} />
    </button>
  );
}

function Logo() {
  return (
    <a
      href="https://wiki.luckfox.com/intro/"
      target="_blank"
      rel="noopener noreferrer"
      className="flex items-center gap-4 -ml-3"
    >
      <img src={LogoLuckfox} alt="Luckfox Logo" className="h-[20px] dark:hidden" />
      <img src={LogoLuckfox} alt="Luckfox Logo" className="hidden h-[20px] dark:block" />
    </a>
  );
}