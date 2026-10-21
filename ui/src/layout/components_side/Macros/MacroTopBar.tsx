import { useEffect, useState } from "react";
import { Dropdown, MenuProps , Typography, Button as AntdButton } from "antd";
import DownSvg from "@assets/second/dwon.svg?react";
import MingLingSvg from "@assets/second/MingLing.svg?react";
import { useReactAt } from "i18n-auto-extractor/react";
import { isMobile } from "react-device-detect";

import { useJsonRpc } from "@/hooks/useJsonRpc";
import useKeyboard from "@/hooks/useKeyboard";
import { useMacrosStore, useUiStore } from "@/hooks/stores";
import {
  dark_bd_style,
  dark_bg_style_fun,
  dark_font_style,
  text_primary_color,
} from "@/layout/theme_color";
import { useTheme } from "@/layout/contexts/ThemeContext";
import SmartButton from "@components/SmartButton";

export default function MacroTopBar() {
  const { macros, initialized, loadMacros, setSendFn } = useMacrosStore();
  const { executeMacro } = useKeyboard();
  const [send] = useJsonRpc();
  const [dropdownVisible, setDropdownVisible] = useState(false);
  const setDisableFocusTrap = useUiStore(state => state.setDisableVideoFocusTrap);
  const toggleSidebarView = useUiStore(state => state.toggleSidebarView);
  const { $at } = useReactAt();
  const toggleTopBarView = useUiStore(state => state.toggleTopBarView);
  const sidebarView = useUiStore(state => state.sidebarView);
  const topBarView = useUiStore(state => state.topBarView);
  const { isDark } = useTheme();

  useEffect(() => {
    setSendFn(send);

    if (!initialized) {
      loadMacros();
    }
  }, [initialized, loadMacros, setSendFn, send]);

  const dropdownItems: MenuProps["items"] = macros.slice(4).map(macro => ({
    key: macro.id,
    label: (
      <AntdButton
        key={macro.id}
        color="default" variant="outlined"
        className={"text-[rgba(51,51,51,1)] dark:text-white"}
        style={{ backgroundColor: "transparent", width: "100%" }}
        onClick={() => executeMacro(macro.steps)}
      >{macro.name}</AntdButton>


    ),
  }));
  //${dark_bg_style}
  if (isMobile) {
    return (
      <div
        className={`flex h-full w-full items-center gap-1 overflow-hidden px-[5px] ${dark_bg_style_fun(isDark)}`}
      >
        <button
          type="button"
          className={`shrink-0 bg-transparent border-0 p-0 cursor-pointer ${
            sidebarView == "Macros" ? text_primary_color : `text-black ${dark_font_style}`
          }`}
          onClick={() => {
            // setDisableFocusTrap(false);
            setDisableFocusTrap(true);
            toggleSidebarView("Macros");
          }}
        >
          <MingLingSvg />
        </button>

        <div className="flex min-w-0 flex-1 items-center gap-1 overflow-hidden">
          {macros.slice(0, 4).map(macro => (
            <SmartButton
              key={macro.id}
              maxWidth={110}
              color="default"
              variant="outlined"
              text={macro.name}
              style={{
                backgroundColor: "transparent",
                flex: "0 1 auto",
                minWidth: 0,
                maxHeight: "80%",
                padding: "2px 6px",
                fontSize: 12,
              }}
              onClick={() => executeMacro(macro.steps)}
            />
          ))}

          {macros.length > 4 && (
            <button
              type="button"
              className={`shrink-0 bg-transparent border-0 p-0 cursor-pointer ${
                topBarView == "MacroMoreList" ? text_primary_color : dark_font_style
              }`}
              onClick={(e) => {
                e.stopPropagation();
                toggleTopBarView("MacroMoreList");
              }}
            >
              <DownSvg fontSize={18} />
            </button>
          )}
        </div>
      </div>
    );
  }

  return (
    <div className="flex items-center">
      <div className="flex flex-wrap gap-1 items-center">
        <AntdButton
          type={"text"}
          icon={<MingLingSvg />}
          className={dark_bd_style}
          onClick={() => {
            // setDisableFocusTrap(false);
            setDisableFocusTrap(true);
            toggleSidebarView("Macros");
          }}
        >{$at("Macros")}</AntdButton>
        {/*   aria-label={macro.name}*/}
        {macros.slice(0, 4).map(macro => (
          <SmartButton
            key={macro.id}
            maxWidth={120}
            color="default" variant="outlined"
            className={"text-[rgba(51,51,51,1)] dark:text-white"}
            text={macro.name}
            style={{
              backgroundColor: "transparent",
              height: "95%",
              width: "60px",
              lineHeight: "95%",
              padding: "6px 6px",
            }}
            onClick={() => executeMacro(macro.steps)}
          />
        ))}
        {macros.length > 4 && (
          <Dropdown
            menu={{ items: dropdownItems }}
            placement="bottomRight"
            trigger={["click"]}
            className={dark_bd_style}
            open={dropdownVisible}
          >

            <AntdButton
              onClick={(e) => {
                e.stopPropagation();
                setDropdownVisible(!dropdownVisible);
              }}
              color="default" variant="outlined"
              style={{ backgroundColor: "transparent", height: "95%", lineHeight: "95%", padding: "3px 3px" }}
              icon={<DownSvg fontSize={20} className={dark_font_style} />}
            />
          </Dropdown>
        )}
      </div>
    </div>
  );
}

function MacroMoreList() {
  const { macros } = useMacrosStore();
  const { executeMacro } = useKeyboard();
  return (
    <div className={"flex flex-col-reverse w-full"}>
      {
        macros.slice(4).map(macro => (
          <AntdButton
            key={macro.id}
            onClick={() => executeMacro(macro.steps)}
            color="default" variant="outlined"
            style={{ backgroundColor: "transparent" }}
            className={"my-1 w-full  text-[rgba(51,51,51,1)] dark:text-white dark:bg-black"}
          >
            {macro.name}
          </AntdButton>

        ))
      }
    </div>
  );

}

export { MacroMoreList };