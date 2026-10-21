import React from "react";
import { Popover, PopoverButton, PopoverPanel } from "@headlessui/react";

import { useUiStore } from "@/hooks/stores";
import { selected_bt_bg } from "@/layout/theme_color";

interface PopoverButtonProps {
  buttonText?: string;
  buttonIconNode?: React.ReactNode;
  panelContent: React.ReactNode;
  align?: "left" | "right";
  buttonClassName?: string;
  panelClassName?: string;
  style?: React.CSSProperties;
}

const BottomPopoverButton: React.FC<PopoverButtonProps> = ({
                                                             buttonText,
                                                             buttonIconNode,
                                                             panelContent,
                                                             align = "left",
                                                             style,
                                                           }) => {
  const setDisableFocusTrap = useUiStore(state => state.setDisableVideoFocusTrap);
  return (

    <div className="relative flex justify-start">
      <Popover className="relative">
        {({ open }) => (
          <>
            <PopoverButton
              as="div"
              style={{ display: "flex", justifyContent: "center", alignItems: "center", ...style }}>
              <div
                onClick={() => {
                  setDisableFocusTrap(true);
                }}
                className={`flex items-center justify-center text-xs h-[24px] cursor-pointer hover:bg-[rgba(0,0,0,0.06)] dark:hover:bg-[rgba(255,255,255,0.06)] transition-colors ${open ? selected_bt_bg : ""}`}
              >
                {buttonIconNode && (
                  <div className="flex items-center justify-center pl-2 pr-2">
                      {buttonIconNode}
                  </div>
                )}
                {buttonText && (
                   <span className="pr-2" style={{position: "relative", top: "1px"}}>{buttonText}</span>
                )}
              </div>
            </PopoverButton>

            <PopoverPanel
              className={`absolute z-50 bottom-full mb-1  ${align === "left" ? "left-0" : "right-0"} transition duration-300 ease-out data-closed:translate-y-8 data-closed:opacity-0`}
              transition
            >
              {panelContent}
            </PopoverPanel>
          </>
        )}
      </Popover>
    </div>
  )
    ;
};

export default BottomPopoverButton;