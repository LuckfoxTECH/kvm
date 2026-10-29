import React, { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";

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
  /**
   * iconOnly renders just the icon and slides the name out to the right on
   * hover, so a bar full of icons stays compact without hiding what they are.
   */
  iconOnly?: boolean;
}

// Clicking opens the panel, clicking the button again, pressing Escape or
// clicking anywhere outside closes it. Hover opening was removed on purpose:
// the bar is crowded enough that merely crossing it kept pulling panels open.
//
// align is a preference, not a rule: the panel is anchored to the button that
// opened it, but flips to the other edge (and finally clamps) when the preferred
// side would push it past the viewport. Which side a button needs therefore
// follows from where it sits in the bar instead of being hard-coded per button.
const BottomPopoverButton: React.FC<PopoverButtonProps> = ({
  buttonText,
  buttonIconNode,
  panelContent,
  align = "left",
  style,
  iconOnly = false,
}) => {
  const setDisableFocusTrap = useUiStore(state => state.setDisableVideoFocusTrap);
  const [open, setOpen] = useState(false);
  const wrapRef = useRef<HTMLDivElement | null>(null);
  const panelRef = useRef<HTMLDivElement | null>(null);
  // Resolved before paint (layout effect), so the panel never flashes on the
  // wrong side first.
  const [resolvedAlign, setResolvedAlign] = useState<"left" | "right">(align);
  const [shiftX, setShiftX] = useState(0);

  const measure = useCallback(() => {
    const wrap = wrapRef.current;
    const panel = panelRef.current;
    if (!wrap || !panel) return;

    const wrapRect = wrap.getBoundingClientRect();
    const panelWidth = panel.offsetWidth;
    const viewport = window.innerWidth;
    const margin = 8;

    const leftEdgeFor = (side: "left" | "right") =>
      side === "left" ? wrapRect.left : wrapRect.right - panelWidth;
    const fits = (side: "left" | "right") => {
      const left = leftEdgeFor(side);
      return left >= margin && left + panelWidth <= viewport - margin;
    };

    const other = align === "left" ? "right" : "left";
    const next = fits(align) ? align : fits(other) ? other : align;

    // Neither side fits (a wide panel opened by a button near an edge): keep the
    // preferred edge and slide the panel back inside rather than clipping it.
    const naturalLeft = leftEdgeFor(next);
    const clampedLeft = Math.min(
      Math.max(naturalLeft, margin),
      Math.max(margin, viewport - margin - panelWidth),
    );

    setResolvedAlign(next);
    setShiftX(clampedLeft - naturalLeft);
  }, [align]);

  useLayoutEffect(() => {
    if (!open) return;

    measure();

    // The panel's width changes as its content loads (sliders, labels), and the
    // bar itself re-flows on window resize.
    const panel = panelRef.current;
    const observer = new ResizeObserver(measure);
    if (panel) observer.observe(panel);
    window.addEventListener("resize", measure);

    return () => {
      observer.disconnect();
      window.removeEventListener("resize", measure);
    };
  }, [open, measure]);

  const handleClick = useCallback(() => {
    setDisableFocusTrap(true);
    setOpen(prev => !prev);
  }, [setDisableFocusTrap]);

  useEffect(() => {
    if (!open) return;

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") setOpen(false);
    };
    const onMouseDown = (event: MouseEvent) => {
      const target = event.target as Element | null;
      if (wrapRef.current?.contains(target as Node)) return;
      // Antd portals its overlays to document.body, so a Modal or an open Select
      // dropdown is outside wrapRef by construction. Treating those clicks as
      // "outside" closed the panel, which unmounted the panel content and tore
      // down the dialog the user was interacting with. The mask is deliberately
      // not in this list: clicking it should close the panel as well.
      const portalled = target?.closest?.(
        ".ant-select-dropdown, .ant-dropdown, .ant-popover, .ant-modal-wrap, .ant-modal-content",
      );
      if (portalled) return;
      setOpen(false);
    };

    document.addEventListener("keydown", onKeyDown);
    document.addEventListener("mousedown", onMouseDown);
    return () => {
      document.removeEventListener("keydown", onKeyDown);
      document.removeEventListener("mousedown", onMouseDown);
    };
  }, [open]);

  const labelExpanded = "max-w-[160px] opacity-100 pr-2";

  return (
    <div ref={wrapRef} className="relative flex justify-start">
      <div style={{ display: "flex", justifyContent: "center", alignItems: "center", ...style }}>
        <div
          onClick={handleClick}
          className={`group flex items-center justify-center text-xs h-[24px] cursor-pointer hover:bg-[rgba(0,0,0,0.06)] dark:hover:bg-[rgba(255,255,255,0.06)] transition-colors ${open ? selected_bt_bg : ""}`}
        >
          {buttonIconNode && (
            <div className={`flex items-center justify-center ${iconOnly ? "pl-1.5 pr-1.5" : "pl-2 pr-2"}`}>
              {buttonIconNode}
            </div>
          )}
          {buttonText && !iconOnly && (
            <span className="pr-2" style={{ position: "relative", top: "1px" }}>{buttonText}</span>
          )}
          {buttonText && iconOnly && (
            <span
              className={`overflow-hidden whitespace-nowrap max-w-0 opacity-0 transition-all duration-200 ease-out group-hover:opacity-100 group-hover:max-w-[160px] group-hover:pr-2 ${open ? labelExpanded : ""}`}
              style={{ position: "relative", top: "1px" }}>
              {buttonText}
            </span>
          )}
        </div>
      </div>

      {open && (
        <div
          ref={panelRef}
          className={`absolute z-50 bottom-full mb-1 ${resolvedAlign === "left" ? "left-0" : "right-0"}`}
          style={shiftX ? { transform: `translateX(${shiftX}px)` } : undefined}
        >
          {panelContent}
        </div>
      )}
    </div>
  );
};

export default BottomPopoverButton;
