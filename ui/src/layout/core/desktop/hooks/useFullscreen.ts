import { useCallback, useEffect } from "react";

import { useSettingsStore } from "@/hooks/stores";
import { usePointerLock } from "./usePointerLock";

// WebKit exposes the fullscreen API under vendor prefixes.
type FullscreenDocument = Document & {
  webkitFullscreenElement?: Element | null;
};

type FullscreenCapableElement = HTMLElement & {
  webkitRequestFullscreen?: (options?: FullscreenOptions) => Promise<void> | void;
};

type NativeVideoFullscreen = HTMLVideoElement & {
  webkitEnterFullscreen?: () => void;
};

const currentFullscreenElement = (): Element | null => {
  const doc = document as FullscreenDocument;
  return doc.fullscreenElement ?? doc.webkitFullscreenElement ?? null;
};

export const useFullscreen = (
  videoElm: React.RefObject<HTMLVideoElement>,
  pointerLock: ReturnType<typeof usePointerLock>,
  isFullscreen?: number
) => {
  const fullscreenMode = useSettingsStore(state => state.fullscreenMode);

  const requestKeyboardLock = useCallback(async () => {
    if (!videoElm.current) return;

    if ("keyboard" in navigator) {
      try {
        // @ts-expect-error - keyboard lock API
        await navigator.keyboard.lock();
      } catch {
        // ignore errors
      }
    }
  }, [videoElm]);

  const releaseKeyboardLock = useCallback(async () => {
    if ("keyboard" in navigator) {
      try {
        // @ts-expect-error - keyboard lock API
        await navigator.keyboard.unlock();
      } catch {
        // ignore errors
      }
    }
  }, []);

  const requestFullscreen = useCallback(async () => {
    await requestKeyboardLock();
    await pointerLock.requestPointerLock();

    if (currentFullscreenElement()) return;

    // "page" fullscreens the document instead of the <video> element. A video
    // element gets the browser's native video player: every overlay drawn on top
    // of the stream (virtual keys, toolbars, statistics) disappears, and Android
    // derives the screen orientation from the video, which rotates a portrait
    // phone sideways. The document element keeps our layout, overlays and
    // orientation intact — that is why it is the default.
    // "video" is kept as an opt-in for webviews and players that only know how to
    // fullscreen a media element.
    const useVideoElement = fullscreenMode === "video" && !!videoElm.current;
    const target = (useVideoElement
      ? videoElm.current
      : document.documentElement) as FullscreenCapableElement;
    const request = target.requestFullscreen ?? target.webkitRequestFullscreen;

    if (!request) {
      // iOS Safari has no fullscreen on arbitrary elements, only the native video
      // player — which is exactly what "video" mode asks for.
      const video = videoElm.current as NativeVideoFullscreen | null;
      if (useVideoElement && video?.webkitEnterFullscreen) video.webkitEnterFullscreen();
      return;
    }

    try {
      const result = request.call(target, { navigationUI: "hide" });
      if (result instanceof Promise) await result;
    } catch (err) {
      console.warn("[fullscreen] requestFullscreen failed:", err);
    }
  }, [requestKeyboardLock, pointerLock, fullscreenMode, videoElm]);

  // Depends on the counter only: requestFullscreen changes identity on every
  // render (usePointerLock returns a fresh object), and re-running it would fire
  // requestPointerLock in a loop.
  useEffect(() => {
    if (isFullscreen) void requestFullscreen();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isFullscreen]);

  useEffect(() => {
    const handleFullscreenChange = () => {
      if (!currentFullscreenElement()) {
        releaseKeyboardLock();
      }
    };

    document.addEventListener("fullscreenchange", handleFullscreenChange);
    document.addEventListener("webkitfullscreenchange", handleFullscreenChange);
    return () => {
      document.removeEventListener("fullscreenchange", handleFullscreenChange);
      document.removeEventListener("webkitfullscreenchange", handleFullscreenChange);
    };
  }, [releaseKeyboardLock]);

  return {
    requestFullscreen,
    releaseKeyboardLock,
  };
};
