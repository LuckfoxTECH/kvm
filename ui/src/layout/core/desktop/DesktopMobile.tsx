import React, { useCallback, useEffect, useRef, useState } from "react";
import { useReactAt } from "i18n-auto-extractor/react";

import VirtualKeyboard from "@components/VirtualKeyboard";
import {
  HDMIErrorOverlay,
  LoadingVideoOverlay,
  NoAutoplayPermissionsOverlay,
  PointerLockBar,
} from "@components/VideoOverlay";
import { cx } from "@/cva.config";
import { useVideoEffects } from "@/layout/core/desktop/hooks/useVideoEffects";
import { useVideoStream } from "@/layout/core/desktop/hooks/useVideoStream";
import { usePointerLock } from "@/layout/core/desktop/hooks/usePointerLock";
import { useFullscreen } from "@/layout/core/desktop/hooks/useFullscreen";
import { useKeyboardEvents } from "@/layout/core/desktop/hooks/useKeyboardEvents";
import { useMouseEvents } from "@/layout/core/desktop/hooks/useMouseEvents";
import { usesAbsolutePosition } from "@/hooks/mouseMode";
import { useVideoOverlays } from "@/layout/core/desktop/hooks/useVideoOverlays";
import { VideoContainer } from "@components/Video/VideoContainer";
import { VideoElement } from "@components/Video/VideoElement";
import StatsTobbar from "@components/Sidebar/StatsTopbar";
import KeyboardSide from "@/layout/components_side/Keyboard/KeyboardSide";
import Clipboard from "@/layout/components_side/Clipboard/Clipboard";
import SettingsModal from "@/layout/components_setting";
import  { MacroMoreList } from "@/layout/components_side/Macros/MacroTopBar";
import { useMacrosSideTitleState , useHidStore, useMouseStore, useSettingsStore, useUiStore } from "@/hooks/stores";
import MobileTerminal from "@/layout/components_bottom/terminal/index.mobile";
import { dark_bg_desktop, dark_bg_style_fun } from "@/layout/theme_color";
import PowerControl from "@/layout/components_side/Power";
import MouseSide from "@/layout/components_side/Mouse/MouseSide";
import EnhancedDrawer from "@components/Sidebar/SidebarDrawer";
import SettingsVideoSide from "@/layout/components_side/Video/SettingsVideoSide";
import VirtualMouseOverlay from "@/layout/core/desktop/VirtualMouseOverlay";
import ConnectionStatsSidebar from "@components/ConnectionStats";
import { useTheme } from "@/layout/contexts/ThemeContext";
import SettingsMacros from "@/layout/components_side/Macros";
import { useTouchZoom } from "@/layout/core/desktop/hooks/useTouchZoom";
import { usePasteHandler } from "@/layout/core/desktop/hooks/usePasteHandler";
import UsbEpModeSelect from "@/layout/components_bottom/usbepmode/UsbEpModeSelect";
import { RemoteStatusPanelConnected } from "@/layout/core/bar_bottom/RemoteStatusPanel";
import { HdmiStatusPanelConnected } from "@/layout/core/bar_bottom/HdmiStatusPanel";
import VirtualMediaSource from "@/layout/components_side/VirtualMediaSource";
import OcrOverlay from "@components/OcrOverlay";

export default function MobileDesktop({ isFullscreen }: { isFullscreen?: number }) {
  const { $at } = useReactAt();
  const { isDark } = useTheme();
  const videoElm = useRef<HTMLVideoElement>(null);
  const audioElm = useRef<HTMLAudioElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const zoomContainerRef = useRef<HTMLDivElement>(null);
  const pasteCaptureRef = useRef<HTMLTextAreaElement>(null);
  const isReinitializingGadget = useHidStore(state => state.isReinitializingGadget);
  const isOcrMode = useUiStore(state => state.isOcrMode);
  const mobileToolsHidden = useUiStore(state => state.mobileToolsHidden);
  const macrosSideTitle = useMacrosSideTitleState(state => state.sideTitle);

  const videoEffects = useVideoEffects();
  const videoStream = useVideoStream(videoElm as React.RefObject<HTMLVideoElement>, audioElm as React.RefObject<HTMLAudioElement>);
  const pointerLock = usePointerLock(videoElm as React.RefObject<HTMLVideoElement>);
  useFullscreen(videoElm as React.RefObject<HTMLVideoElement>, pointerLock, isFullscreen);
  const isTouchGestureEnabled = useSettingsStore(state => state.touchGestureEnabled);
  const setIsTouchGestureEnabled = useSettingsStore(state => state.setTouchGestureEnabled);

  // Two-finger scroll ends up in the same wheel report the physical wheel uses.
  // That report lives in useMouseEvents, which needs this hook's result first, so
  // the two are joined through a ref instead of each depending on the other.
  const touchScrollRef = useRef<(wheelY: number, wheelX: number) => void>(() => {});
  const handleTouchScroll = useCallback((wheelY: number, wheelX: number) => {
    touchScrollRef.current(wheelY, wheelX);
  }, []);
  const mouseMode = useSettingsStore(state => state.mouseMode);
  const touchZoom = useTouchZoom(
    zoomContainerRef as React.RefObject<HTMLDivElement>,
    isTouchGestureEnabled,
    handleTouchScroll,
  );
  const { handleGlobalPaste } = usePasteHandler(pasteCaptureRef as React.RefObject<HTMLTextAreaElement>);
  const keyboardEvents = useKeyboardEvents(pasteCaptureRef as React.RefObject<HTMLTextAreaElement>, isReinitializingGadget);
  const [virtualMouseActive, setVirtualMouseActive] = useState(false);
  const mouseEvents = useMouseEvents(videoElm as React.RefObject<HTMLVideoElement>, pointerLock, touchZoom, virtualMouseActive, 0);
  useEffect(() => {
    touchScrollRef.current = mouseEvents.sendWheelReport;
  }, [mouseEvents.sendWheelReport]);
  const overlays = useVideoOverlays(videoStream, pointerLock, videoEffects);

  const forceHttp = useSettingsStore(state => state.forceHttp);
  const setMouseMode = useSettingsStore(state => state.setMouseMode);
  const mouseX = useMouseStore(state => state.mouseX);
  const mouseY = useMouseStore(state => state.mouseY);
  const allowTapToOpenVirtualKeyboard = useHidStore(state => state.allowTapToOpenVirtualKeyboard);
  const setAllowTapToOpenVirtualKeyboard = useHidStore(state => state.setAllowTapToOpenVirtualKeyboard);
  const activeButtonsRef = useRef(0);
  // Absolute HID coordinates map 0..1 onto the whole screen, so a zero default
  // would fling the host cursor into the top-left corner on the first button
  // press. Start at the centre and only track real pointer positions.
  const lastAbsPosRef = useRef({ x: 0.5, y: 0.5 });

  useEffect(() => {
    if (usesAbsolutePosition(mouseMode)) {
      // (0,0) is the store default and not a real pointer position.
      if (mouseX === 0 && mouseY === 0) return;
      lastAbsPosRef.current = { x: mouseX, y: mouseY };
    }
  }, [mouseX, mouseY, mouseMode]);

  const updateButtons = (mask: number, isDown: boolean) => {
    if (isReinitializingGadget) return;

    let newButtons = activeButtonsRef.current;
    if (isDown) {
      newButtons |= mask;
    } else {
      newButtons &= ~mask;
    }

    activeButtonsRef.current = newButtons;
    if (mouseMode === "relative") {
      mouseEvents.sendVirtualRelativeMovement(0, 0, newButtons);
    } else {
      mouseEvents.sendAbsMouseMovement(lastAbsPosRef.current.x, lastAbsPosRef.current.y, newButtons);
    }
  };
  
  // Every relative report carries the button mask, so the joystick has to send the
  // mask that is currently held down: a zero mask reads as "button released" on
  // the host and drops the drag (window move, rubber-band selection) mid-gesture.
  const moveVirtualRelative = useCallback(
    (dx: number, dy: number) => {
      mouseEvents.sendVirtualRelativeMovement(dx, dy, activeButtonsRef.current);
    },
    [mouseEvents.sendVirtualRelativeMovement],
  );

  // Joystick deltas are pixel based, but absolute modes must move the cursor
  // through the same channel that button reports use: otherwise a button press
  // would carry the stale coordinate and pull the cursor back.
  const moveVirtualAbsolute = useCallback(
    (dxNorm: number, dyNorm: number) => {
      if (isReinitializingGadget) return;
      const current = lastAbsPosRef.current;
      const next = {
        x: Math.min(1, Math.max(0, current.x + dxNorm)),
        y: Math.min(1, Math.max(0, current.y + dyNorm)),
      };
      lastAbsPosRef.current = next;
      mouseEvents.sendAbsMouseMovement(next.x, next.y, activeButtonsRef.current);
    },
    [isReinitializingGadget, mouseEvents.sendAbsMouseMovement],
  );

  useEffect(() => {
    const keyboardCleanup = keyboardEvents.setupKeyboardEvents();
    const videoCleanup = videoStream.setupVideoEventListeners();
    const mouseCleanup = mouseEvents.setupMouseEvents();

    return () => {
      keyboardCleanup?.();
      videoCleanup?.();
      mouseCleanup?.();
    };
  }, [keyboardEvents.setupKeyboardEvents, videoStream.setupVideoEventListeners, mouseEvents.setupMouseEvents]);

  return (
    <div className=" h-full w-full flex flex-col justify-evenly overflow-hidden  bg-[#d3d3d3] dark:bg-[#1a1a1a]">
      <div></div>
      <StatsTobbar title={""} targetView={"SettingsModal"}> <SettingsModal /></StatsTobbar>
      <StatsTobbar title={"Clipboard"} targetView={"ClipboardMobile"}> <Clipboard /></StatsTobbar>

      <StatsTobbar title={""} targetView={"MacroMoreList"}> <MacroMoreList /></StatsTobbar>
      <EnhancedDrawer
        className={""}
        targetView={"TerminalTabsMobile"}
        placement={"top"}
        drawerRender={() => (<MobileTerminal/>)}
      />
      <EnhancedDrawer
        title={$at("PowerControl")}
        targetView={"PowerControl"}
        placement={"top"}
        drawerRender={() => (<PowerControl/>)}
      />

      <EnhancedDrawer
       title={macrosSideTitle}
       targetView={"Macros"}
       placement={"top"}
       drawerRender={() => (<SettingsMacros/>)}
      />

      <EnhancedDrawer
        title={$at("Keyboard")}
        targetView={"KeyboardPanel"}
        className={"p-[20px]"}
        placement={"bottom"}
        drawerRender={() => (<KeyboardSide/>)}
      />
      <EnhancedDrawer
        title={$at("Mouse")}
        targetView={"MousePanel"}
        className={"p-[20px]"}
        placement={"bottom"}
        drawerRender={() => (<MouseSide/>)}
      />
      <EnhancedDrawer
        title={$at("USB")}
        targetView={"UsbEpModeSelect"}
        placement={"bottom"}
        drawerRender={() => (<UsbEpModeSelect/>)}
      />
      <EnhancedDrawer
        title={$at("HDMI")}
        targetView={"HdmiStatus"}
        placement={"bottom"}
        drawerRender={() => (<HdmiStatusPanelConnected/>)}
      />

      <EnhancedDrawer
        title={$at("Remote Access")}
        targetView={"RemoteStatus"}
        placement={"bottom"}
        drawerRender={() => (<RemoteStatusPanelConnected/>)}
      />

      <EnhancedDrawer
        title={$at("Virtual Media Source")}
        targetView={"VirtualMedia"}
        placement={"bottom"}
        drawerRender={() => (<VirtualMediaSource/>)}
      />
      <EnhancedDrawer
        title={$at("Video")}
        targetView={"SettingsVideo"}
        placement={"bottom"}
        drawerRender={() => (<SettingsVideoSide/>)}
      />
      <EnhancedDrawer
        title={$at("Connection Stats")}
        targetView={"connection-stats"}
        placement={"bottom"}
        drawerRender={() => (<ConnectionStatsSidebar/>)}
      />

      <audio
        id="global-audio"
        ref={audioElm}
        autoPlay
        muted={true}
        controls={false}
      />

      <VideoContainer containerRef={containerRef as React.RefObject<HTMLDivElement>}>
        <div className="flex h-full flex-col">
          <div className="relative grow h-full w-full overflow-hidden">
            <div className="flex h-full flex-col">
              <div className="grid grow grid-rows-(--grid-bodyFooter) overflow-hidden h-full w-full">
                <PointerLockBar show={overlays.showPointerLockBar} />

                <div
                  className={`relative h-full w-full  flex items-center justify-center overflow-hidden`}>
                  <div
                      ref={zoomContainerRef}
                      className={cx("relative flex h-full w-full items-center justify-center ", dark_bg_desktop)}
                      style={{
                          transform: `translate(${touchZoom.mobileTx}px, ${touchZoom.mobileTy}px) scale(${touchZoom.mobileScale})`,
                          transformOrigin: "center center",
                          touchAction: "none",
                      }}
                  >
                      <VideoElement
                        ref={videoElm}
                        onPlaying={videoStream.onVideoPlaying}
                        style={videoEffects.videoStyle}
                        className={cx(
                          `h-full  w-full  ${dark_bg_style_fun(isDark)} object-contain transition-all duration-1000`,
                          {
                            "cursor-none": videoEffects.settings.isCursorHidden,
                            "pointer-events-none": isOcrMode,
                            "opacity-0": overlays.shouldHideVideo,
                            "opacity-60!": overlays.showPointerLockBar,
                            "animate-slideUpFade  dark:border-slate-300/20":
                            videoStream.isPlaying,
                          },
                        )}
                      />
                      <OcrOverlay
                        videoRef={videoElm as React.RefObject<HTMLVideoElement>}
                        containerRef={zoomContainerRef as React.RefObject<HTMLDivElement>}
                      />

                    {(videoStream.peerConnectionState === "connected" || forceHttp) && (
                      <div
                        style={{ animationDuration: "500ms" }}
                        className="animate-slideUpFade pointer-events-none absolute inset-0 flex items-center justify-center"
                      >
                        <div className="relative h-full w-full rounded-md">
                          <LoadingVideoOverlay show={overlays.showLoadingOverlay} />
                          <HDMIErrorOverlay show={overlays.showHDMIError} hdmiState={overlays.hdmiState} />

                          <NoAutoplayPermissionsOverlay
                            show={overlays.showNoAutoplayOverlay}
                            onPlayClick={videoStream.handlePlayClick}
                          />
                        </div>
                      </div>
                    )}
                  </div>
                  <div className="pointer-events-none absolute inset-0">
                    <VirtualMouseOverlay
                      isDark={isDark}
                      toolsHidden={mobileToolsHidden}
                      disabled={isReinitializingGadget}
                      isTouchGestureEnabled={isTouchGestureEnabled}
                      onToggleTouchGesture={() => setIsTouchGestureEnabled(!isTouchGestureEnabled)}
                      onResetView={() => touchZoom.resetTransform()}
                      virtualKeyboardEnabled={allowTapToOpenVirtualKeyboard}
                      onToggleVirtualKeyboard={() => setAllowTapToOpenVirtualKeyboard(!allowTapToOpenVirtualKeyboard)}
                      onPressButtons={mask => updateButtons(mask, true)}
                      onReleaseButtons={mask => updateButtons(mask, false)}
                      mouseToolsAllowed={mouseMode === "relative"}
                      onSetMouseMode={setMouseMode}
                      onRelativeMove={moveVirtualRelative}
                      onMoveAbsolute={usesAbsolutePosition(mouseMode) ? moveVirtualAbsolute : undefined}
                      onWheel={delta => mouseEvents.sendWheelReport(delta, 0)}
                      onActiveChange={setVirtualMouseActive}
                    />
                  </div>
                </div>
                <VirtualKeyboard />
              </div>
            </div>
          </div>
        </div>
      </VideoContainer>

      <textarea
        ref={pasteCaptureRef}
        aria-hidden="true"
        tabIndex={-1}
        style={{ position: "fixed", left: -9999, top: -9999, width: 1, height: 1, opacity: 0, pointerEvents: "none" }}
        onPaste={handleGlobalPaste}
      />
    </div>
  );
}
