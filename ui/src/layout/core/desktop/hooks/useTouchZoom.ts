import { useEffect, useRef, useState } from "react";

import { accumulateScroll, clampWheelNotches } from "@/utils/touchScroll";

export type TouchScrollHandler = (wheelY: number, wheelX: number) => void;

// The View Scale outlives the session: a refresh used to drop the user back to 1x,
// which on a phone means repeating the pinch that was set up once on purpose.
const VIEW_STATE_KEY = "videoViewState";
const VIEW_STATE_SAVE_DELAY_MS = 250;
const MAX_SCALE = 4;

interface ViewState {
  scale: number;
  tx: number;
  ty: number;
}

const readViewState = (): ViewState => {
  const fallback: ViewState = { scale: 1, tx: 0, ty: 0 };
  try {
    const raw = window.localStorage.getItem(VIEW_STATE_KEY);
    if (!raw) return fallback;
    const stored = JSON.parse(raw) as Partial<ViewState>;
    const scale = Number(stored.scale);
    if (!Number.isFinite(scale)) return fallback;
    const tx = Number(stored.tx);
    const ty = Number(stored.ty);
    return {
      scale: Math.max(1, Math.min(MAX_SCALE, scale)),
      tx: Number.isFinite(tx) ? tx : 0,
      ty: Number.isFinite(ty) ? ty : 0,
    };
  } catch {
    return fallback;
  }
};

/**
 * Two-finger view manipulation for the mobile video surface.
 *
 * The Gesture Toggle decides which of two mutually exclusive behaviours two
 * fingers have: while it is on they pinch and pan the View Scale, while it is off
 * (Zoom Lock) they scroll the controlled host instead. See ADR-0001.
 *
 * Both are local view behaviour, so they are available in every mouse mode.
 */
export const useTouchZoom = (
  containerRef: React.RefObject<HTMLDivElement>,
  gestureEnabled = true,
  onTouchScroll?: TouchScrollHandler,
) => {
  const localZoom = gestureEnabled;
  const localScroll = !gestureEnabled;
  const [restoredView] = useState(readViewState);
  const [mobileScale, setMobileScale] = useState(restoredView.scale);
  const [mobileTx, setMobileTx] = useState(restoredView.tx);
  const [mobileTy, setMobileTy] = useState(restoredView.ty);
  // The listeners read the scale through a ref: depending on the state directly
  // would re-bind them on every pinch sample.
  const mobileScaleRef = useRef(mobileScale);
  useEffect(() => {
    mobileScaleRef.current = mobileScale;
  }, [mobileScale]);
  const activeTouchPointers = useRef<Map<number, { x: number; y: number }>>(new Map());
  const initialPinchDistance = useRef<number | null>(null);
  const initialPinchScale = useRef<number>(1);
  const lastPanPoint = useRef<{ x: number; y: number } | null>(null);
  const lastTapAt = useRef<number>(0);

  // Zoom Lock state: centroid tracking, per-axis remainder and the coalesced
  // report that goes out at most once per frame.
  const lastCentroid = useRef<{ x: number; y: number; time: number } | null>(null);
  const lastPointerCount = useRef(0);
  const scrollRemainder = useRef({ x: 0, y: 0 });
  const pendingScroll = useRef({ x: 0, y: 0 });
  const scrollFrame = useRef<number | null>(null);
  const scrollStartedOnVideo = useRef(false);
  const scrollHandler = useRef(onTouchScroll);

  useEffect(() => {
    scrollHandler.current = onTouchScroll;
  }, [onTouchScroll]);

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const abortController = new AbortController();
    const signal = abortController.signal;
    const isPointInVideo = (x: number, y: number) => {
      const video = el.querySelector("video") as HTMLVideoElement | null;
      if (!video) return false;
      const vRect = video.getBoundingClientRect();
      return x >= vRect.left && x <= vRect.right && y >= vRect.top && y <= vRect.bottom;
    };

    const centroid = () => {
      const pts = Array.from(activeTouchPointers.current.values());
      if (pts.length < 2) return null;
      let x = 0;
      let y = 0;
      for (const pt of pts) {
        x += pt.x;
        y += pt.y;
      }
      return { x: x / pts.length, y: y / pts.length };
    };

    const flushScroll = () => {
      scrollFrame.current = null;
      const handler = scrollHandler.current;
      const pending = pendingScroll.current;
      pendingScroll.current = { x: 0, y: 0 };
      if (!handler) return;
      const wheelY = clampWheelNotches(pending.y);
      const wheelX = clampWheelNotches(pending.x);
      if (wheelY === 0 && wheelX === 0) return;
      handler(wheelY, wheelX);
    };

    const queueScroll = (wheelY: number, wheelX: number) => {
      if (wheelY === 0 && wheelX === 0) return;
      const pending = pendingScroll.current;
      pending.y = clampWheelNotches(pending.y + wheelY);
      pending.x = clampWheelNotches(pending.x + wheelX);
      if (scrollFrame.current !== null) return;
      scrollFrame.current = requestAnimationFrame(flushScroll);
    };

    const resetScrollGesture = () => {
      lastCentroid.current = null;
      scrollRemainder.current = { x: 0, y: 0 };
      scrollStartedOnVideo.current = false;
    };

    const startScrollGesture = (event: PointerEvent) => {
      const center = centroid();
      if (!center) return false;
      lastCentroid.current = { ...center, time: event.timeStamp };
      scrollRemainder.current = { x: 0, y: 0 };
      scrollStartedOnVideo.current = isPointInVideo(center.x, center.y);
      return scrollStartedOnVideo.current;
    };

    const trackScrollGesture = (event: PointerEvent) => {
      const center = centroid();
      if (!center) return false;
      const prev = lastCentroid.current;
      lastCentroid.current = { ...center, time: event.timeStamp };
      if (!prev) return false;

      const dx = center.x - prev.x;
      const dy = center.y - prev.y;
      if (dx === 0 && dy === 0) return false;

      const elapsed = Math.max(1, event.timeStamp - prev.time);
      const speed = Math.hypot(dx, dy) / elapsed;

      const vertical = accumulateScroll(dy, scrollRemainder.current.y, speed);
      scrollRemainder.current.y = vertical.remainderPx;
      const horizontal = accumulateScroll(dx, scrollRemainder.current.x, speed);
      scrollRemainder.current.x = horizontal.remainderPx;

      queueScroll(vertical.ticks, horizontal.ticks);
      return true;
    };

    const onPointerDown = (e: PointerEvent) => {
      if (e.pointerType !== "touch") return;
      activeTouchPointers.current.set(e.pointerId, { x: e.clientX, y: e.clientY });
      const pointerCount = activeTouchPointers.current.size;
      let shouldHandleLocalGesture = false;

      if (localZoom) {
        if (pointerCount === 1) {
          const now = Date.now();
          const isInVideo = isPointInVideo(e.clientX, e.clientY);
          if (!isInVideo) {
            if (now - lastTapAt.current < 300) {
              setMobileScale(1);
              setMobileTx(0);
              setMobileTy(0);
            }
            shouldHandleLocalGesture = true;
          }
          lastTapAt.current = now;
          if (mobileScaleRef.current > 1) {
            lastPanPoint.current = { x: e.clientX, y: e.clientY };
            shouldHandleLocalGesture = true;
          } else {
            lastPanPoint.current = null;
          }
        } else if (pointerCount === 2) {
          const pts = Array.from(activeTouchPointers.current.values());
          const d = Math.hypot(pts[0].x - pts[1].x, pts[0].y - pts[1].y);
          initialPinchDistance.current = d;
          initialPinchScale.current = mobileScaleRef.current;
          shouldHandleLocalGesture = true;
        }
      } else if (localScroll && pointerCount >= 2) {
        // Zoom Lock: two fingers scroll. A third finger keeps the gesture alive
        // but restarts the centroid so the report does not jump.
        if (pointerCount !== lastPointerCount.current) {
          shouldHandleLocalGesture = startScrollGesture(e);
        } else {
          shouldHandleLocalGesture = scrollStartedOnVideo.current;
        }
      }

      lastPointerCount.current = pointerCount;

      // A second finger must still reach the video handler: that is where the
      // first finger's press is released and its pending long press is dropped.
      // This holds whichever two-finger behaviour is active, pinch or scroll.
      const multiTouchTakeover = pointerCount >= 2;
      if (shouldHandleLocalGesture) {
        e.preventDefault();
        if (!multiTouchTakeover) e.stopPropagation();
      }
    };

    const onPointerMove = (e: PointerEvent) => {
      if (e.pointerType !== "touch") return;
      const prev = activeTouchPointers.current.get(e.pointerId);
      activeTouchPointers.current.set(e.pointerId, { x: e.clientX, y: e.clientY });
      const pts = Array.from(activeTouchPointers.current.values());
      let shouldHandleLocalGesture = false;

      if (localZoom) {
        if (pts.length === 2 && initialPinchDistance.current) {
          const d = Math.hypot(pts[0].x - pts[1].x, pts[0].y - pts[1].y);
          const factor = d / initialPinchDistance.current;
          const next = Math.max(1, Math.min(MAX_SCALE, initialPinchScale.current * factor));
          setMobileScale(next);
          shouldHandleLocalGesture = true;
        } else if (pts.length === 1 && mobileScaleRef.current > 1 && lastPanPoint.current && prev) {
          const dx = e.clientX - lastPanPoint.current.x;
          const dy = e.clientY - lastPanPoint.current.y;
          lastPanPoint.current = { x: e.clientX, y: e.clientY };
          setMobileTx(v => v + dx);
          setMobileTy(v => v + dy);
          shouldHandleLocalGesture = true;
        }
      } else if (localScroll && pts.length >= 2) {
        if (pts.length !== lastPointerCount.current) {
          startScrollGesture(e);
        } else if (scrollStartedOnVideo.current) {
          // Only a gesture that began on the video scrolls the host. One that
          // began on the toolbar or the letterbox belongs to the page.
          trackScrollGesture(e);
        }
        lastPointerCount.current = pts.length;
        // Keep consuming once the gesture started on the video, so straying over
        // the toolbar mid-scroll does not hand the movement to the page.
        shouldHandleLocalGesture = scrollStartedOnVideo.current;
      }

      if (shouldHandleLocalGesture) {
        e.preventDefault();
        e.stopPropagation();
      }
    };

    const onPointerUp = (e: PointerEvent) => {
      if (e.pointerType !== "touch") return;
      const wasHandlingLocalGesture =
        localZoom &&
        (initialPinchDistance.current !== null || mobileScaleRef.current > 1);
      const wasScrolling = localScroll && scrollStartedOnVideo.current;
      activeTouchPointers.current.delete(e.pointerId);
      const pointerCount = activeTouchPointers.current.size;
      lastPointerCount.current = pointerCount;
      if (pointerCount < 2) {
        initialPinchDistance.current = null;
      }
      if (pointerCount === 0) {
        lastPanPoint.current = null;
        resetScrollGesture();
        if (scrollFrame.current !== null) {
          cancelAnimationFrame(scrollFrame.current);
          scrollFrame.current = null;
        }
        pendingScroll.current = { x: 0, y: 0 };
      }

      if (wasHandlingLocalGesture || wasScrolling) {
        e.preventDefault();
        e.stopPropagation();
      }
    };

    // Capture phase: the video element is a descendant, so its own pointerdown
    // handler would otherwise run first and see a second finger as the first one
    // (the pointer map is only updated here). Capturing lets this handler register
    // every finger before the single-finger arbitration looks at the count.
    el.addEventListener("pointerdown", onPointerDown, { signal, capture: true });
    el.addEventListener("pointermove", onPointerMove, { signal });
    el.addEventListener("pointerup", onPointerUp, { signal });
    el.addEventListener("pointercancel", onPointerUp, { signal });
    // The video surface is an input device, not a document: a native context menu
    // on long press both covers the stream and cancels the pointer stream that
    // the gesture needs to finish its own click.
    const onContextMenu = (e: Event) => e.preventDefault();
    el.addEventListener("contextmenu", onContextMenu, { signal });

    return () => {
      abortController.abort();
      // A queued frame would otherwise fire after unmount and scroll a host that
      // no longer has a surface to stop it.
      if (scrollFrame.current !== null) {
        cancelAnimationFrame(scrollFrame.current);
        scrollFrame.current = null;
      }
      pendingScroll.current = { x: 0, y: 0 };
    };
  }, [containerRef, gestureEnabled, localZoom, localScroll]);

  const resetTransform = () => {
    initialPinchDistance.current = null;
    lastPanPoint.current = null;
    setMobileScale(1);
    setMobileTx(0);
    setMobileTy(0);
  };

  // Turning the toggle off only stops the view from capturing two fingers; it
  // must not undo what the user already zoomed to.

  // Storing every pinch sample would mean one synchronous write per pointermove,
  // so the settled value is written once the gesture stops moving.
  useEffect(() => {
    const timeout = window.setTimeout(() => {
      try {
        if (mobileScale <= 1 && mobileTx === 0 && mobileTy === 0) {
          window.localStorage.removeItem(VIEW_STATE_KEY);
          return;
        }
        const state: ViewState = { scale: mobileScale, tx: mobileTx, ty: mobileTy };
        window.localStorage.setItem(VIEW_STATE_KEY, JSON.stringify(state));
      } catch {
        // Storage can be unavailable (private mode); the view is then session
        // scoped and behaves exactly as it did before.
      }
    }, VIEW_STATE_SAVE_DELAY_MS);
    return () => window.clearTimeout(timeout);
  }, [mobileScale, mobileTx, mobileTy]);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;
    const cw = container.clientWidth;
    const ch = container.clientHeight;
    if (!cw || !ch) return;
    const maxX = (cw * (mobileScale - 1)) / 2;
    const maxY = (ch * (mobileScale - 1)) / 2;
    setMobileTx(x => Math.max(-maxX, Math.min(maxX, x)));
    setMobileTy(y => Math.max(-maxY, Math.min(maxY, y)));
  }, [mobileScale, containerRef]);

  return {
    mobileScale,
    mobileTx,
    mobileTy,
    activeTouchPointers,
    lastPanPoint,
    resetTransform,
  };
};
