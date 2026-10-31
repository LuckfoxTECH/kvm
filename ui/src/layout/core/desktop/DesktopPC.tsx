import React, { useEffect, useRef } from "react";

import VirtualKeyboard from "@components/VirtualKeyboard";
import {
  HDMIErrorOverlay,
  LoadingVideoOverlay,
  NoAutoplayPermissionsOverlay,
  PointerLockBar,
} from "@components/VideoOverlay";
import IndexPc from "@/layout/components_bottom/terminal/index.pc";
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
import Clipboard from "@/layout/components_side/Clipboard/Clipboard";
import SettingsModal from "@/layout/components_setting";
import { MacroMoreList } from "@/layout/components_side/Macros/MacroTopBar";
import { useUiStore, useHidStore, useSettingsStore } from "@/hooks/stores";
import { useTouchZoom } from "@/layout/core/desktop/hooks/useTouchZoom";
import { usePasteHandler } from "@/layout/core/desktop/hooks/usePasteHandler";
import OcrOverlay from "@components/OcrOverlay";

export default function PCDesktop({ isFullscreen }: { isFullscreen?: number }) {
  const videoElm = useRef<HTMLVideoElement>(null);
  const audioElm = useRef<HTMLAudioElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const zoomContainerRef = useRef<HTMLDivElement>(null);
  const pasteCaptureRef = useRef<HTMLTextAreaElement>(null);

  const isVirtualKeyboardEnabled = useHidStore(state => state.isVirtualKeyboardEnabled);
  const isReinitializingGadget = useHidStore(state => state.isReinitializingGadget);
  const setTerminalType = useUiStore(state => state.setTerminalType);
  const terminalType = useUiStore(state => state.terminalType);
  const setVirtualKeyboardEnabled = useHidStore(state => state.setVirtualKeyboardEnabled);
  const isOcrMode = useUiStore(state => state.isOcrMode);

  const forceHttp = useSettingsStore(state => state.forceHttp);

  useEffect(() => {
    if (isVirtualKeyboardEnabled) {
      setTerminalType("none");
    }
  }, [isVirtualKeyboardEnabled, setTerminalType]);

  useEffect(() => {
    if (terminalType !== "none") {
      setVirtualKeyboardEnabled(false);
    }
  }, [terminalType, setVirtualKeyboardEnabled]);

  const videoEffects = useVideoEffects();
  const videoStream = useVideoStream(videoElm as React.RefObject<HTMLVideoElement>, audioElm as React.RefObject<HTMLAudioElement>);
  const pointerLock = usePointerLock(videoElm as React.RefObject<HTMLVideoElement>);
  useFullscreen(videoElm as React.RefObject<HTMLVideoElement>, pointerLock, isFullscreen);
  const touchZoom = useTouchZoom(
    zoomContainerRef as React.RefObject<HTMLDivElement>,
    true,
    undefined,
  );
  const { handleGlobalPaste } = usePasteHandler(pasteCaptureRef as React.RefObject<HTMLTextAreaElement>);

  const keyboardEvents = useKeyboardEvents(pasteCaptureRef as React.RefObject<HTMLTextAreaElement>, isReinitializingGadget);
  const mouseEvents = useMouseEvents(videoElm as React.RefObject<HTMLVideoElement>, pointerLock, touchZoom);
  const overlays = useVideoOverlays(videoStream, pointerLock, videoEffects);

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

      <StatsTobbar title={""} targetView={"SettingsModal"}> <SettingsModal /></StatsTobbar>
      <StatsTobbar title={"Clipboard"} targetView={"ClipboardMobile"}> <Clipboard /></StatsTobbar>
      <StatsTobbar title={""} targetView={"MacroMoreList"}> <MacroMoreList /></StatsTobbar>
      <audio
        id="global-audio"
        ref={audioElm}
        autoPlay
        muted={true}
        controls={false}
      />

      <VideoContainer containerRef={containerRef as React.RefObject<HTMLDivElement>} >
        <div className="flex  h-full flex-col">
          <div className="relative grow h-full w-full overflow-hidden">
            <div className="flex h-full flex-col">
              <div className="grid grow grid-rows-(--grid-bodyFooter) overflow-hidden h-full w-full">
                <PointerLockBar show={overlays.showPointerLockBar} />

                <div
                  className={`relative  mx-4  my-2 flex items-center justify-center overflow-hidden`}>
                  <div
                    ref={zoomContainerRef}
                    className="relative flex h-full w-full items-center justify-center "
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
                         `max-h-full min-h-[384px] max-w-full min-w-[512px]  object-contain transition-all duration-1000`,
                        {
                           "cursor-none": videoEffects.settings.isCursorHidden,
                           "pointer-events-none": isOcrMode,
                           "opacity-0": overlays.shouldHideVideo,
                           "opacity-60!": overlays.showPointerLockBar,
                           "animate-slideUpFade  shadow-xs ":
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
                </div>

                <VirtualKeyboard />
                <IndexPc />
              </div>
            </div>
          </div>
        </div>
      </VideoContainer>

      <textarea
        ref={pasteCaptureRef}
        aria-hidden="true"
        style={{ position: "fixed", left: -9999, top: -9999, width: 1, height: 1, opacity: 0 }}
        onPaste={handleGlobalPaste}
      />
    </div>
  );
}
