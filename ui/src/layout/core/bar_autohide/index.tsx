import { useCallback, useEffect, useRef } from "react";
import { useReactAt } from "i18n-auto-extractor/react";
import { BsPin, BsPinFill } from "react-icons/bs";

import { useUiStore } from "@/hooks/stores";
import { dark_font_style, text_primary_color } from "@/layout/theme_color";

/** Default linger time before the unpinned bars slide back out (ms). */
const HIDE_DELAY_MS = 5000;
/**
 * Shorter linger time for touch screens: every reveal there is an explicit tap
 * rather than a hover, and the bars cover the remote picture while they show.
 */
const HIDE_DELAY_TOUCH_MS = 3000;
/** Height of the screen edge strip that reveals the bars while they are hidden. */
const HOT_ZONE_HEIGHT_PX = 10;
/**
 * True on pointer devices: an edge strip plus mouseleave drives the hide. Touch
 * screens have neither, so they reveal with a tap and hide on an idle timer.
 */
const HOVER_CAPABLE =
  typeof window !== "undefined" && !!window.matchMedia?.("(hover: hover)").matches;

/**
 * True while a panel opened from the bars is on screen. Such panels (top bar
 * view, sidebar, right drawer, terminal) cover the content, so the pointer never
 * re-enters the bar area and the bars would be lost without their entry point.
 */
function useBarsPanelOpen() {
  const sidebarView = useUiStore(state => state.sidebarView);
  const topBarView = useUiStore(state => state.topBarView);
  const terminalType = useUiStore(state => state.terminalType);
  return sidebarView !== null || topBarView !== null || terminalType !== "none";
}

/**
 * Drives the shared visibility of the top bar and the bottom bar: they are
 * hidden by default, revealed together by the edge hot zones, and kept visible
 * together while pinned.
 */
export function useBarsAutoHide() {
  const barsPinned = useUiStore(state => state.barsPinned);
  const setBarsPinned = useUiStore(state => state.setBarsPinned);
  const barsTemporarilyVisible = useUiStore(state => state.barsTemporarilyVisible);
  const setBarsTemporarilyVisible = useUiStore(state => state.setBarsTemporarilyVisible);
  const panelOpen = useBarsPanelOpen();

  const hideTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  // Whether the pointer is currently on the bars; used to decide if closing a
  // panel should immediately resume the auto-hide.
  const pointerOnBarsRef = useRef(false);

  const cancelScheduledHide = useCallback(() => {
    if (hideTimerRef.current !== null) {
      clearTimeout(hideTimerRef.current);
      hideTimerRef.current = null;
    }
  }, []);

  const showBars = useCallback(() => {
    pointerOnBarsRef.current = true;
    cancelScheduledHide();
    setBarsTemporarilyVisible(true);
  }, [cancelScheduledHide, setBarsTemporarilyVisible]);

  const armHideTimer = useCallback(() => {
    cancelScheduledHide();
    const state = useUiStore.getState();
    if (
      state.barsPinned ||
      state.sidebarView !== null ||
      state.topBarView !== null ||
      state.terminalType !== "none"
    )
      return;

    hideTimerRef.current = setTimeout(() => {
      hideTimerRef.current = null;
      setBarsTemporarilyVisible(false);
    }, HOVER_CAPABLE ? HIDE_DELAY_MS : HIDE_DELAY_TOUCH_MS);
  }, [cancelScheduledHide, setBarsTemporarilyVisible]);

  const scheduleHideBars = useCallback(() => {
    pointerOnBarsRef.current = false;
    armHideTimer();
  }, [armHideTimer]);

  /**
   * One-off reveal used by touch input, where there is no pointer hovering the
   * bars and therefore no mouseleave to schedule the hide: the idle timer is
   * armed right away so the bars still slide back out after the linger time.
   */
  const revealBars = useCallback(() => {
    pointerOnBarsRef.current = false;
    setBarsTemporarilyVisible(true);
    armHideTimer();
  }, [armHideTimer, setBarsTemporarilyVisible]);

  const toggleBarsPinned = useCallback(() => {
    const nextPinned = !useUiStore.getState().barsPinned;
    setBarsPinned(nextPinned);
    if (nextPinned) {
      cancelScheduledHide();
      setBarsTemporarilyVisible(false);
    } else if (!HOVER_CAPABLE) {
      // Touch input has no mouseleave to fall back on after unpinning.
      pointerOnBarsRef.current = false;
      armHideTimer();
    }
  }, [armHideTimer, cancelScheduledHide, setBarsPinned, setBarsTemporarilyVisible]);

  useEffect(() => cancelScheduledHide, [cancelScheduledHide]);

  // Opening a panel cancels a pending hide and forces the bars back on screen.
  // Closing it restores the usual behaviour, unless the pointer already sits on
  // the bars again (then leaving them schedules the hide as usual).
  useEffect(() => {
    if (panelOpen) {
      cancelScheduledHide();
      setBarsTemporarilyVisible(true);
    } else if (!pointerOnBarsRef.current) {
      scheduleHideBars();
    }
  }, [panelOpen, cancelScheduledHide, scheduleHideBars, setBarsTemporarilyVisible]);

  return {
    barsVisible: barsPinned || panelOpen || barsTemporarilyVisible,
    barsPinned,
    showBars,
    revealBars,
    scheduleHideBars,
    toggleBarsPinned,
  };
}

/** Invisible strips at the top and bottom screen edges that reveal the bars. */
export function BarsHotZones({
  barsVisible,
  showBars,
  revealBars,
  scheduleHideBars,
}: {
  barsVisible: boolean;
  /** Pointer-driven reveal; omitted on touch layouts, where hover does not exist. */
  showBars?: () => void;
  revealBars: () => void;
  scheduleHideBars: () => void;
}) {
  const zoneClassName = `absolute inset-x-0 z-40 ${
    barsVisible ? "pointer-events-none" : "pointer-events-auto"
  }`;

  const zoneHandlers = {
    onMouseEnter: showBars ?? revealBars,
    onMouseLeave: scheduleHideBars,
    onTouchStart: revealBars,
    onClick: revealBars,
  };

  return (
    <>
      <div
        {...zoneHandlers}
        style={{ height: HOT_ZONE_HEIGHT_PX }}
        className={`${zoneClassName} top-0`}
      />
      <div
        {...zoneHandlers}
        style={{ height: HOT_ZONE_HEIGHT_PX }}
        className={`${zoneClassName} bottom-0`}
      />
    </>
  );
}

/**
 * Handle shown while the bars are hidden on touch screens, where there is no
 * pointer to hover the edge hot zones. Tapping it reveals both bars.
 */
export function BarsHiddenHandle({
  visible,
  revealBars,
}: {
  visible: boolean;
  revealBars: () => void;
}) {
  const { $at } = useReactAt();
  if (!visible) return null;
  const label = $at("Show bars");

  return (
    <button
      type="button"
      title={label}
      aria-label={label}
      onClick={revealBars}
      className="absolute left-1/2 top-0 z-40 flex h-9 w-24 -translate-x-1/2 items-center justify-center border-0 bg-transparent"
    >
      <span className="block h-[5px] w-14 rounded-full bg-white shadow-[0_1px_4px_rgba(0,0,0,0.45)]" />
    </button>
  );
}

/**
 * Pin toggle that sits at the right end of the top bar, right after the macros.
 * Outline pin = the bars auto-hide, filled pin = they stay visible. Lives inside
 * the bar, so it never floats over the remote screen or a side panel.
 */
export function BarsPinButton({ fontSize = 16 }: { fontSize?: number }) {
  const { $at } = useReactAt();
  const barsPinned = useUiStore(state => state.barsPinned);
  const setBarsPinned = useUiStore(state => state.setBarsPinned);

  const label = barsPinned ? $at("Unpin bars") : $at("Pin bars");

  return (
    <button
      type="button"
      title={label}
      aria-label={label}
      aria-pressed={barsPinned}
      onClick={() => setBarsPinned(!barsPinned)}
      className={`flex shrink-0 cursor-pointer items-center border-0 bg-transparent px-2 transition-colors duration-200 ${
        barsPinned ? text_primary_color : `text-black ${dark_font_style}`
      }`}
    >
      {barsPinned ? <BsPinFill fontSize={fontSize} /> : <BsPin fontSize={fontSize} />}
    </button>
  );
}
