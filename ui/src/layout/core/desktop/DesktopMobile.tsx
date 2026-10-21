import React, { useEffect, useRef, useState } from "react";
import { BsKeyboardFill, BsLockFill, BsUnlockFill } from "react-icons/bs";
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
import { useVideoOverlays } from "@/layout/core/desktop/hooks/useVideoOverlays";
import { VideoContainer } from "@components/Video/VideoContainer";
import { VideoElement } from "@components/Video/VideoElement";
import StatsTobbar from "@components/Sidebar/StatsTopbar";
import KeyboardPanel from "@/layout/components_bottom/keyboard/KeyboardPanel";
import Clipboard from "@/layout/components_side/Clipboard/Clipboard";
import SettingsModal from "@/layout/components_setting";
import  { MacroMoreList } from "@/layout/components_side/Macros/MacroTopBar";
import { useMacrosSideTitleState , useHidStore, useMouseStore, useSettingsStore, useUiStore } from "@/hooks/stores";
import MobileTerminal from "@/layout/components_bottom/terminal/index.mobile";
import { dark_bg_desktop, dark_bg_style_fun } from "@/layout/theme_color";
import PowerControl from "@/layout/components_side/Power";
import MousePanel from "@components/MousePanel";
import EnhancedDrawer from "@components/Sidebar/SidebarDrawer";
import SettingsVideoSide from "@/layout/components_side/Video/SettingsVideoSide";
import ConnectionStatsSidebar from "@components/ConnectionStats";
import { useTheme } from "@/layout/contexts/ThemeContext";
import SettingsMacros from "@/layout/components_side/Macros";
import { useTouchZoom } from "@/layout/core/desktop/hooks/useTouchZoom";
import { usePasteHandler } from "@/layout/core/desktop/hooks/usePasteHandler";
import UsbEpModeSelect from "@/layout/components_bottom/usbepmode/UsbEpModeSelect";
import UsbStatusPanel from "@/layout/components_bottom/usb_status/UsbStatusPanel";
import VirtualMediaSource from "@/layout/components_side/VirtualMediaSource";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import OcrOverlay from "@components/OcrOverlay";

const GestureIcon = ({ className = "h-4 w-4" }: { className?: string }) => (
  <svg viewBox="0 0 24 24" className={className} fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    <circle cx="10" cy="10" r="6.5" />
    <path d="M14.8 14.8L21 21" />
    <path d="M10 7.5V12.5" />
    <path d="M7.5 10H12.5" />
  </svg>
);

const ResetViewIcon = ({ className = "h-4 w-4" }: { className?: string }) => (
  <svg viewBox="0 0 24 24" className={className} fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    <path d="M19 12A7 7 0 1 1 12 5" />
    <path d="M12 2L12 6L16 6" />
  </svg>
);

const MouseStickIcon = ({ className = "h-4 w-4" }: { className?: string }) => (
  <svg viewBox="0 0 24 24" className={className} fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    <rect x="7" y="3" width="10" height="18" rx="5" />
    <path d="M12 3V8" />
    <circle cx="12" cy="13" r="1.5" fill="currentColor" stroke="none" />
  </svg>
);

const FourWayMoveIcon = ({ className = "h-5 w-5" }: { className?: string }) => (
  <svg viewBox="0 0 24 24" className={className} fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    <path d="M12 3V21" />
    <path d="M3 12H21" />
    <path d="M12 3L9 6" />
    <path d="M12 3L15 6" />
    <path d="M12 21L9 18" />
    <path d="M12 21L15 18" />
    <path d="M3 12L6 9" />
    <path d="M3 12L6 15" />
    <path d="M21 12L18 9" />
    <path d="M21 12L18 15" />
  </svg>
);

export default function MobileDesktop({ isFullscreen }: { isFullscreen?: number }) {
  const joystickSpeedLevels = [1.4, 1.05, 0.7];
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
  const [isTouchGestureEnabled, setIsTouchGestureEnabled] = useState(true);
  const touchZoom = useTouchZoom(zoomContainerRef as React.RefObject<HTMLDivElement>, isTouchGestureEnabled);
  const { handleGlobalPaste } = usePasteHandler(pasteCaptureRef as React.RefObject<HTMLTextAreaElement>);
  const keyboardEvents = useKeyboardEvents(pasteCaptureRef as React.RefObject<HTMLTextAreaElement>, isReinitializingGadget);
  const [showVirtualMouseButtons, setShowVirtualMouseButtons] = useState(false);
  const [showVirtualJoystick, setShowVirtualJoystick] = useState(false);
  const [joystickVector, setJoystickVector] = useState({ x: 0, y: 0 });
  const [joystickSensitivity, setJoystickSensitivity] = useState(1);
  const [joystickPos, setJoystickPos] = useState({ x: 16, y: 24 });
  const [lockedButtons, setLockedButtons] = useState(0);
  const mouseEvents = useMouseEvents(videoElm as React.RefObject<HTMLVideoElement>, pointerLock, touchZoom, showVirtualMouseButtons, lockedButtons);
  const overlays = useVideoOverlays(videoStream, pointerLock, videoEffects);

  const forceHttp = useSettingsStore(state => state.forceHttp);
  const mouseMode = useSettingsStore(state => state.mouseMode);
  const mouseX = useMouseStore(state => state.mouseX);
  const mouseY = useMouseStore(state => state.mouseY);
  const allowTapToOpenVirtualKeyboard = useHidStore(state => state.allowTapToOpenVirtualKeyboard);
  const setAllowTapToOpenVirtualKeyboard = useHidStore(state => state.setAllowTapToOpenVirtualKeyboard);
  const [send, sendNotification] = useJsonRpc();
  const [leftBtnPos, setLeftBtnPos] = useState({ x: 40, y: 40 });
  const [rightBtnPos, setRightBtnPos] = useState({ x: 120, y: 40 });
  const [wheelPos, setWheelPos] = useState({ x: 184, y: 140 });

  const [draggingBtn, setDraggingBtn] = useState<"left" | "right" | "wheel" | null>(null);
  const dragOffset = useRef({ x: 0, y: 0 });
  const joystickAreaRef = useRef<HTMLDivElement>(null);
  const joystickPointerIdRef = useRef<number | null>(null);
  const joystickVectorRef = useRef({ x: 0, y: 0 });
  const joystickFrameRef = useRef<number | null>(null);
  const joystickLastTsRef = useRef<number | null>(null);
  const joystickMovePointerIdRef = useRef<number | null>(null);
  const joystickMoveHoldTimerRef = useRef<number | null>(null);
  const joystickMoveEnabledRef = useRef(false);

  const activeButtonsRef = useRef(0);
  const lastAbsPosRef = useRef({ x: 0, y: 0 });

  useEffect(() => {
    if (mouseMode === "absolute") {
      lastAbsPosRef.current = { x: mouseX, y: mouseY };
    }
  }, [mouseX, mouseY, mouseMode]);

  useEffect(() => {
    if (isFullscreen) {
      setShowVirtualMouseButtons(false);
      setShowVirtualJoystick(false);
    }
  }, [isFullscreen]);

  useEffect(() => {
    joystickVectorRef.current = joystickVector;
  }, [joystickVector]);

  useEffect(() => {
    if (!showVirtualJoystick) {
      joystickPointerIdRef.current = null;
      joystickMovePointerIdRef.current = null;
      if (joystickMoveHoldTimerRef.current !== null) {
        window.clearTimeout(joystickMoveHoldTimerRef.current);
        joystickMoveHoldTimerRef.current = null;
      }
      joystickMoveEnabledRef.current = false;
      joystickLastTsRef.current = null;
      setJoystickVector({ x: 0, y: 0 });
      if (joystickFrameRef.current !== null) {
        cancelAnimationFrame(joystickFrameRef.current);
        joystickFrameRef.current = null;
      }
      return;
    }

    const tick = (timestamp: number) => {
      const prevTs = joystickLastTsRef.current ?? timestamp;
      joystickLastTsRef.current = timestamp;
      const frameScale = Math.min(2, Math.max(0.5, (timestamp - prevTs) / 16.67));
      const vector = joystickVectorRef.current;
      const container = containerRef.current;
      const containerWidth = container?.clientWidth ?? 1280;
      // Reduce sensitivity on small screens to avoid over-shooting.
      const resolutionFactor = Math.max(0.35, Math.min(1, containerWidth / 1280));
      const speed = 12 * resolutionFactor * joystickSensitivity;
      const dx = Math.round(vector.x * speed * frameScale);
      const dy = Math.round(vector.y * speed * frameScale);
      if (dx !== 0 || dy !== 0) {
        mouseEvents.sendVirtualRelativeMovement(dx, dy, 0);
      }
      joystickFrameRef.current = requestAnimationFrame(tick);
    };

    joystickFrameRef.current = requestAnimationFrame(tick);
    return () => {
      if (joystickFrameRef.current !== null) {
        cancelAnimationFrame(joystickFrameRef.current);
        joystickFrameRef.current = null;
      }
      joystickLastTsRef.current = null;
    };
  }, [showVirtualJoystick, mouseEvents, joystickSensitivity]);

  const updateButtons = (mask: number, isDown: boolean) => {
    if (isReinitializingGadget) return;
    
    let newButtons = activeButtonsRef.current;
    if (isDown) {
        newButtons |= mask;
    } else if (lockedButtons & mask) {
        // Keep pressed while lock is enabled.
        newButtons |= mask;
    } else {
        newButtons &= ~mask;
    }

    activeButtonsRef.current = newButtons;
    if (mouseMode === "relative") {
      mouseEvents.sendVirtualRelativeMovement(0, 0, newButtons);
    } else {
      sendNotification("absMouseReport", { x: lastAbsPosRef.current.x, y: lastAbsPosRef.current.y, buttons: newButtons });
    }
  };
  
  const toggleLock = (mask: number) => {
     if (isReinitializingGadget) return;
     const isLocked = (lockedButtons & mask) !== 0;
     let newButtons = activeButtonsRef.current;
     
     if (isLocked) {
         setLockedButtons(prev => prev & ~mask);
         newButtons &= ~mask;
     } else {
         setLockedButtons(prev => prev | mask);
         newButtons |= mask;
     }
     
     activeButtonsRef.current = newButtons;
     if (mouseMode === "relative") {
       mouseEvents.sendVirtualRelativeMovement(0, 0, newButtons);
     } else {
       sendNotification("absMouseReport", { x: lastAbsPosRef.current.x, y: lastAbsPosRef.current.y, buttons: newButtons });
     }
  };

  const handlePointerDown = (e: React.PointerEvent<HTMLDivElement>, type: "left" | "right" | "wheel") => {
    const target = e.currentTarget;
    const rect = target.getBoundingClientRect();
    dragOffset.current = {
      x: e.clientX - rect.left,
      y: e.clientY - rect.top,
    };
    setDraggingBtn(type);
    target.setPointerCapture(e.pointerId);
  };

  const handlePointerMove = (e: React.PointerEvent<HTMLDivElement>) => {
    if (!draggingBtn) return;
    const container = containerRef.current;
    if (!container) return;
    const containerRect = container.getBoundingClientRect();
    const x = e.clientX - containerRect.left - dragOffset.current.x;
    const y = e.clientY - containerRect.top - dragOffset.current.y;
    const dragWidth = draggingBtn === "wheel" ? 32 : 56;
    const dragHeight = draggingBtn === "wheel" ? 68 : 56;
    const clampedX = Math.max(0, Math.min(containerRect.width - dragWidth, x));
    const clampedY = Math.max(0, Math.min(containerRect.height - dragHeight, y));
    if (draggingBtn === "left") {
      setLeftBtnPos({ x: clampedX, y: clampedY });
    } else if (draggingBtn === "right") {
      setRightBtnPos({ x: clampedX, y: clampedY });
    } else if (draggingBtn === "wheel") {
      setWheelPos({ x: clampedX, y: clampedY });
    }
  };

  const handlePointerUp = (e: React.PointerEvent<HTMLDivElement>, type: "left" | "right" | "wheel") => {
    const wasDragging = draggingBtn === type;
    setDraggingBtn(null);
    (e.currentTarget as HTMLDivElement).releasePointerCapture(e.pointerId);
    if (!wasDragging) return;
  };

  const updateJoystickVector = (clientX: number, clientY: number) => {
    const joystickElm = joystickAreaRef.current;
    if (!joystickElm) return;
    const rect = joystickElm.getBoundingClientRect();
    const centerX = rect.left + rect.width / 2;
    const centerY = rect.top + rect.height / 2;
    const rawX = clientX - centerX;
    const rawY = clientY - centerY;
    const maxRadius = 32;
    const length = Math.hypot(rawX, rawY);
    if (!length || length <= maxRadius) {
      setJoystickVector({ x: rawX / maxRadius, y: rawY / maxRadius });
      return;
    }
    const scale = maxRadius / length;
    setJoystickVector({ x: (rawX * scale) / maxRadius, y: (rawY * scale) / maxRadius });
  };

  const handleJoystickPointerDown = (e: React.PointerEvent<HTMLDivElement>) => {
    if (joystickMovePointerIdRef.current !== null) return;
    e.preventDefault();
    joystickPointerIdRef.current = e.pointerId;
    e.currentTarget.setPointerCapture(e.pointerId);
    updateJoystickVector(e.clientX, e.clientY);
  };

  const handleJoystickPointerMove = (e: React.PointerEvent<HTMLDivElement>) => {
    if (joystickPointerIdRef.current !== e.pointerId) return;
    e.preventDefault();
    updateJoystickVector(e.clientX, e.clientY);
  };

  const handleJoystickPointerUp = (e: React.PointerEvent<HTMLDivElement>) => {
    if (joystickPointerIdRef.current !== e.pointerId) return;
    joystickPointerIdRef.current = null;
    e.currentTarget.releasePointerCapture(e.pointerId);
    setJoystickVector({ x: 0, y: 0 });
  };

  const handleJoystickMoveStart = (e: React.PointerEvent<HTMLDivElement>) => {
    joystickMovePointerIdRef.current = e.pointerId;
    joystickMoveEnabledRef.current = false;
    if (joystickMoveHoldTimerRef.current !== null) {
      window.clearTimeout(joystickMoveHoldTimerRef.current);
    }
    joystickMoveHoldTimerRef.current = window.setTimeout(() => {
      joystickMoveEnabledRef.current = true;
    }, 350);
    e.currentTarget.setPointerCapture(e.pointerId);
    e.preventDefault();
    e.stopPropagation();
  };

  const handleJoystickMove = (e: React.PointerEvent<HTMLDivElement>) => {
    if (joystickMovePointerIdRef.current !== e.pointerId) return;
    if (!joystickMoveEnabledRef.current) return;
    const container = containerRef.current;
    if (!container) return;
    const containerRect = container.getBoundingClientRect();
    const joystickSize = 80;
    const nextLeft = e.clientX - containerRect.left - joystickSize / 2;
    const nextTop = e.clientY - containerRect.top - joystickSize / 2;
    const clampedLeft = Math.max(0, Math.min(containerRect.width - joystickSize, nextLeft));
    const clampedTop = Math.max(0, Math.min(containerRect.height - joystickSize, nextTop));
    const nextBottom = containerRect.height - joystickSize - clampedTop;
    setJoystickPos({ x: clampedLeft, y: nextBottom });
    e.preventDefault();
    e.stopPropagation();
  };

  const handleJoystickMoveEnd = (e: React.PointerEvent<HTMLDivElement>) => {
    if (joystickMovePointerIdRef.current !== e.pointerId) return;
    if (joystickMoveHoldTimerRef.current !== null) {
      window.clearTimeout(joystickMoveHoldTimerRef.current);
      joystickMoveHoldTimerRef.current = null;
    }
    joystickMoveEnabledRef.current = false;
    joystickMovePointerIdRef.current = null;
    e.currentTarget.releasePointerCapture(e.pointerId);
    e.preventDefault();
    e.stopPropagation();
  };

  const isMouseControlEnabled = showVirtualJoystick || showVirtualMouseButtons;
  const joystickSpeedIndex = joystickSpeedLevels.reduce((bestIndex, value, index, arr) => {
    const bestDistance = Math.abs(arr[bestIndex] - joystickSensitivity);
    const currentDistance = Math.abs(value - joystickSensitivity);
    return currentDistance < bestDistance ? index : bestIndex;
  }, 0);
  const toggleMouseControl = () => {
    const nextEnabled = !isMouseControlEnabled;
    setShowVirtualJoystick(nextEnabled);
    setShowVirtualMouseButtons(nextEnabled);
  };

  useEffect(() => {
    const keyboardCleanup = keyboardEvents.setupKeyboardEvents();
    const videoCleanup = videoStream.setupVideoEventListeners();
    const mouseCleanup = mouseEvents.setupMouseEvents();

    return () => {
      keyboardCleanup?.();
      videoCleanup?.();
      mouseCleanup?.();
    };
  }, [keyboardEvents, videoStream, mouseEvents]);

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
        targetView={"KeyboardPanel"}
        className={""}
        placement={"bottom"}
        drawerRender={() => (<KeyboardPanel/>)}
      />
      <EnhancedDrawer
        targetView={"MousePanel"}
        className={""}
        placement={"bottom"}
        drawerRender={() => (<MousePanel/>)}
      />
      <EnhancedDrawer
        targetView={"UsbEpModeSelect"}
        placement={"bottom"}
        drawerRender={() => (<UsbEpModeSelect/>)}
        className={"px-[20px]"}
      />
      <EnhancedDrawer
        targetView={"UsbStatusPanel"}
        placement={"bottom"}
        drawerRender={() => (<UsbStatusPanel/>)}
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
                  <div
                    className="pointer-events-none absolute inset-0"
                    onPointerMove={handlePointerMove}
                  >
                    <div
                      className={cx(
                        "pointer-events-auto absolute right-3 top-3 grid grid-cols-[auto_auto_auto] grid-rows-2 gap-1.5 transition-all duration-300",
                        mobileToolsHidden && "pointer-events-none translate-x-4 opacity-0",
                      )}
                    >
                      <div
                        className={cx(
                          "flex h-8 w-8 items-center justify-center rounded-full text-white",
                          isTouchGestureEnabled
                            ? "bg-green-600/80"
                            : "bg-gray-500/70",
                        )}
                        style={{
                          touchAction: "none",
                        }}
                        onClick={() => setIsTouchGestureEnabled(prev => !prev)}
                      >
                        <GestureIcon />
                      </div>
                      <div
                        className={cx(
                          "flex h-8 w-8 items-center justify-center rounded-full text-white",
                          isMouseControlEnabled
                            ? "bg-green-600/80"
                            : "bg-gray-500/70",
                        )}
                        style={{
                          touchAction: "none",
                        }}
                        onClick={toggleMouseControl}
                      >
                        <MouseStickIcon />
                      </div>
                      <div
                        className={cx(
                          "flex h-8 w-8 items-center justify-center rounded-full text-white",
                          allowTapToOpenVirtualKeyboard
                            ? "bg-green-600/80"
                            : "bg-gray-500/70",
                        )}
                        style={{
                          touchAction: "none",
                        }}
                        onClick={() => setAllowTapToOpenVirtualKeyboard(!allowTapToOpenVirtualKeyboard)}
                      >
                        <BsKeyboardFill className="h-4 w-4" />
                      </div>
                      <div
                        className={cx(
                          "col-span-3 flex h-8 items-center justify-center rounded-full text-white",
                          isDark ? "bg-gray-500/70" : "bg-black/30",
                        )}
                        style={{
                          touchAction: "none",
                        }}
                        onClick={() => touchZoom.resetTransform()}
                      >
                        <ResetViewIcon />
                      </div>
                    </div>
                    {showVirtualMouseButtons && (
                      <>
                        <div
                          className={cx(
                            "pointer-events-auto absolute flex h-14 w-14 items-center justify-center rounded-full text-white text-xs active:scale-90 transition-transform duration-100 relative",
                            (lockedButtons & 1) ? "bg-green-600/80" : (isDark ? "bg-gray-500/70" : "bg-black/30"),
                          )}
                          style={{
                            left: leftBtnPos.x,
                            top: leftBtnPos.y,
                            touchAction: "none",
                          }}
                          onPointerDown={() => {
                            updateButtons(1, true);
                          }}
                          onPointerUp={() => {
                            updateButtons(1, false);
                          }}
                        >
                          L
                          <div
                            className="absolute -left-1 -top-1 flex h-5 w-5 items-center justify-center rounded-full bg-black/45 text-white"
                            onPointerDown={e => {
                              e.stopPropagation();
                              handlePointerDown(e, "left");
                            }}
                            onPointerMove={e => {
                              e.stopPropagation();
                              handlePointerMove(e);
                            }}
                            onPointerUp={e => {
                              e.stopPropagation();
                              handlePointerUp(e, "left");
                            }}
                          >
                            <FourWayMoveIcon className="h-3 w-3" />
                          </div>
                          <div
                            className={cx(
                              "absolute -right-1 -top-1 flex h-5 w-5 items-center justify-center rounded-full text-white",
                              (lockedButtons & 1) ? "bg-green-600/90" : "bg-black/45",
                            )}
                            onPointerDown={e => {
                              e.stopPropagation();
                            }}
                            onClick={e => {
                              e.stopPropagation();
                              toggleLock(1);
                            }}
                          >
                            {(lockedButtons & 1) ? <BsLockFill className="h-3 w-3" /> : <BsUnlockFill className="h-3 w-3" />}
                          </div>
                        </div>

                        <div
                          className={cx(
                            "pointer-events-auto absolute flex h-14 w-14 items-center justify-center rounded-full text-white text-xs active:scale-90 transition-transform duration-100 relative",
                            (lockedButtons & 2) ? "bg-green-600/80" : (isDark ? "bg-gray-500/70" : "bg-black/30"),
                          )}
                          style={{
                            left: rightBtnPos.x,
                            top: rightBtnPos.y,
                            touchAction: "none",
                          }}
                          onPointerDown={() => {
                            updateButtons(2, true);
                          }}
                          onPointerUp={() => {
                            updateButtons(2, false);
                          }}
                        >
                          R
                          <div
                            className="absolute -left-1 -top-1 flex h-5 w-5 items-center justify-center rounded-full bg-black/45 text-white"
                            onPointerDown={e => {
                              e.stopPropagation();
                              handlePointerDown(e, "right");
                            }}
                            onPointerMove={e => {
                              e.stopPropagation();
                              handlePointerMove(e);
                            }}
                            onPointerUp={e => {
                              e.stopPropagation();
                              handlePointerUp(e, "right");
                            }}
                          >
                            <FourWayMoveIcon className="h-3 w-3" />
                          </div>
                          <div
                            className={cx(
                              "absolute -right-1 -top-1 flex h-5 w-5 items-center justify-center rounded-full text-white",
                              (lockedButtons & 2) ? "bg-green-600/90" : "bg-black/45",
                            )}
                            onPointerDown={e => {
                              e.stopPropagation();
                            }}
                            onClick={e => {
                              e.stopPropagation();
                              toggleLock(2);
                            }}
                          >
                            {(lockedButtons & 2) ? <BsLockFill className="h-3 w-3" /> : <BsUnlockFill className="h-3 w-3" />}
                          </div>
                        </div>

                        <div
                          className="pointer-events-auto absolute"
                          style={{
                            left: wheelPos.x,
                            top: wheelPos.y,
                            touchAction: "none",
                          }}
                        >
                          <div
                            className="absolute -top-7 left-0 flex items-center gap-1"
                          >
                            <div
                              className={cx(
                                "flex h-6 w-6 items-center justify-center rounded-full text-white transition-transform duration-100 active:scale-95",
                                isDark ? "bg-gray-500/70" : "bg-black/30",
                              )}
                              style={{ touchAction: "none" }}
                              onPointerDown={e => {
                                e.stopPropagation();
                                handlePointerDown(e, "wheel");
                              }}
                              onPointerMove={e => {
                                e.stopPropagation();
                                handlePointerMove(e);
                              }}
                              onPointerUp={e => {
                                e.stopPropagation();
                                handlePointerUp(e, "wheel");
                              }}
                            >
                              <FourWayMoveIcon className="h-4 w-4" />
                            </div>
                          </div>
                          <div
                            className={cx(
                              "flex h-8 w-8 items-center justify-center rounded-full text-white text-xs active:scale-90 transition-transform duration-100",
                              isDark ? "bg-gray-500/70" : "bg-black/30",
                            )}
                            onClick={() => { send("wheelReport", { wheelY: 1, mouseMode }); }}
                          >
                            ▲
                          </div>
                          <div
                            className={cx(
                              "mt-1 flex h-8 w-8 items-center justify-center rounded-full text-white text-xs font-bold active:scale-90 transition-transform duration-100",
                              (lockedButtons & 4) ? "bg-green-600/80" : (isDark ? "bg-gray-500/70" : "bg-black/30"),
                            )}
                            onPointerDown={e => {
                              e.stopPropagation();
                              updateButtons(4, true);
                            }}
                            onPointerUp={e => {
                              e.stopPropagation();
                              updateButtons(4, false);
                            }}
                          >
                            M
                          </div>
                          <div
                            className={cx(
                              "mt-1 flex h-8 w-8 items-center justify-center rounded-full text-white text-xs active:scale-90 transition-transform duration-100",
                              isDark ? "bg-gray-500/70" : "bg-black/30",
                            )}
                            onClick={() => { send("wheelReport", { wheelY: -1, mouseMode }); }}
                          >
                            ▼
                          </div>
                        </div>
                      </>
                    )}
                    {showVirtualJoystick && (
                      <div
                        className="pointer-events-auto absolute"
                        style={{ left: joystickPos.x, bottom: joystickPos.y }}
                      >
                        <div className="absolute -top-7 left-0 flex items-center gap-1">
                          <div
                            className={cx(
                              "flex h-6 w-6 items-center justify-center rounded-full text-white transition-transform duration-100 active:scale-95",
                              isDark ? "bg-gray-500/70" : "bg-black/30",
                            )}
                            style={{ touchAction: "none" }}
                            onPointerDown={handleJoystickMoveStart}
                            onPointerMove={handleJoystickMove}
                            onPointerUp={handleJoystickMoveEnd}
                            onPointerCancel={handleJoystickMoveEnd}
                          >
                            <FourWayMoveIcon className="h-4 w-4" />
                          </div>
                        </div>
                        <div className="absolute right-[-36px] top-0 flex h-20 items-center">
                          <div
                            className={cx(
                              "relative flex h-16 w-3 flex-col justify-between rounded-full py-1",
                              isDark ? "bg-white/25" : "bg-black/20",
                            )}
                            style={{ touchAction: "none" }}
                          >
                            {joystickSpeedLevels.map((level, index) => (
                              <button
                                key={level}
                                type="button"
                                className={cx(
                                  "relative z-10 h-3 w-3 rounded-full border",
                                  joystickSpeedIndex === index
                                    ? "border-blue-300 bg-blue-400"
                                    : (isDark ? "border-white/60 bg-white/40" : "border-black/40 bg-black/20"),
                                )}
                                onClick={() => setJoystickSensitivity(level)}
                                aria-label={`Set joystick speed ${level.toFixed(2)}`}
                              />
                            ))}
                            <div
                              className="pointer-events-none absolute left-full top-1/2 ml-[1px] -translate-y-1/2 border-y-[4px] border-l-[6px] border-y-transparent border-l-blue-400"
                              style={{
                                top: `${(joystickSpeedIndex / (joystickSpeedLevels.length - 1)) * 100}%`,
                              }}
                            />
                          </div>
                          <div className="ml-1 flex h-16 flex-col justify-between text-[8px] text-white/80">
                            <span>Fast</span>
                            <span>Slow</span>
                          </div>
                        </div>
                        <div
                          ref={joystickAreaRef}
                          className={cx(
                            "flex h-20 w-20 items-center justify-center rounded-full border",
                            isDark ? "border-white/40 bg-black/20" : "border-black/30 bg-white/20",
                          )}
                          style={{ touchAction: "none" }}
                          onPointerDown={handleJoystickPointerDown}
                          onPointerMove={handleJoystickPointerMove}
                          onPointerUp={handleJoystickPointerUp}
                          onPointerCancel={handleJoystickPointerUp}
                        >
                          <div
                            className={cx(
                              "h-9 w-9 rounded-full",
                              isDark ? "bg-white/70" : "bg-black/50",
                            )}
                            style={{
                              transform: `translate(${joystickVector.x * 24}px, ${joystickVector.y * 24}px)`,
                            }}
                          />
                        </div>
                      </div>
                    )}
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
