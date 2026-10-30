import { useCallback, useEffect, useRef } from "react";
import { isMobile } from "react-device-detect";

import { MAX_TOUCH_CONTACTS, PEN_FLAGS } from "@/hooks/hidRpc";
import { usesAbsolutePosition } from "@/hooks/mouseMode";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import { useHidRpc } from "@/hooks/useHidRpc";
import { useMouseStore, useSettingsStore, useVideoStore, useHidStore } from "@/hooks/stores";

import { usePointerLock } from "./usePointerLock";

// Pen pressure while the tip switch is down. The pen collection cannot measure
// pressure, so the value only has to say "real contact" to hosts that gate
// strokes on it.
const PEN_CONTACT_PRESSURE = 255;

// Control bits of the HID modifier byte (left 0x01, right 0x10), used to tell a
// real Ctrl+wheel from the ctrl+wheel a trackpad pinch synthesises.
const MODIFIER_CONTROL = 0x01 | 0x10;

// How long a released contact stays in the report with its tip up but still in
// range before the slot is dropped entirely.
const TOUCH_RELEASE_HOLD_MS = 80;

// Touch state is the only stateful report we send, so it is re-sent while there is
// anything to keep alive: a single dropped write would otherwise leave the host
// holding contacts forever.
const TOUCH_HEARTBEAT_MS = 100;
const TOUCH_HEARTBEAT_GRACE_MS = 1000;

export const useMouseEvents = (
  videoElm: React.RefObject<HTMLVideoElement>,
  pointerLock: ReturnType<typeof usePointerLock>,
  touchZoom?: {
    mobileScale: number;
    mobileTx: number;
    mobileTy: number;
    activeTouchPointers: React.MutableRefObject<Map<number, { x: number; y: number }>>;
    lastPanPoint: React.MutableRefObject<{ x: number; y: number } | null>;
  },
  disableTouchClick?: boolean,
  externalButtons = 0
) => {
  const [, sendNotification] = useJsonRpc();
  // Mouse reports prefer the binary HID channel; JSON-RPC stays as the fallback
  // for devices whose firmware predates the hidrpc handshake.
  const {
    reportAbsMouseEvent,
    reportRelMouseEvent,
    reportPenEvent,
    reportTouchEvent,
    rpcHidReady,
  } = useHidRpc();
  // Wheel throttling is a timestamp rather than state: a state flag changes the
  // identity of mouseWheelHandler on every scroll burst, and that handler is in the
  // dependency list of the effect that binds every pointer listener, so each burst
  // re-bound all of them and dropped pending touch releases on the way out.
  const wheelThrottleUntilRef = useRef(0);
  const settings = useSettingsStore();
  const { setMousePosition, setMouseMove } = useMouseStore();
  const { width: videoWidth, height: videoHeight } = useVideoStore();
  const isReinitializingGadget = useHidStore(state => state.isReinitializingGadget);
  // Both mouse interfaces are switched together, so one flag covers absolute and
  // relative. The emulation switch detaches the whole gadget, so it gates too.
  const mouseEnabled = useHidStore(
    state => state.usbEmulationEnabled && state.usbMouseEnabled,
  );
  const touchDragActiveRef = useRef(false);
  const relMouseFrameRef = useRef<number | null>(null);
  const absMouseFrameRef = useRef<number | null>(null);
  const touchFrameRef = useRef<number | null>(null);
  const pendingRelMouseRef = useRef<{ x: number; y: number; buttons: number } | null>(null);
  const pendingAbsMouseRef = useRef<{ x: number; y: number; buttons: number } | null>(null);
  // Last position sent in an absolute-position mode, in HID units. A release
  // report has to repeat it, because the position is only sent when it changes.
  const lastPointerRef = useRef({ x: 0, y: 0 });
  // Fingers currently down, keyed by pointer ID, with their contact slot.
  const activeTouchesRef = useRef<Map<number, { slot: number; x: number; y: number; down: boolean }>>(new Map());
  // A lifted contact stays in the map just long enough to carry its release.
  const touchReleaseTimersRef = useRef<Map<number, number>>(new Map());
  const lastTouchChangeRef = useRef(0);

  const calcDelta = (pos: number) => {
    const sensitivity = settings.mouseSensitivity || 1.0;
    const scaledPos = pos * sensitivity;
    return Math.abs(scaledPos) < 10 ? scaledPos * 2 : scaledPos;
  };

  const sendRelMouseMovement = useCallback(
    (x: number, y: number, buttons: number, force = false) => {
      if (!force && settings.mouseMode !== "relative") return;
      // Don't send mouse events while reinitializing gadget
      if (isReinitializingGadget) return;
      // The mouse interfaces are switched off on the device. Reporting movement
      // would only fail and can retrigger a gadget reinitialization.
      if (!mouseEnabled) return;
      const dx = calcDelta(x);
      const dy = calcDelta(y);
      if (rpcHidReady) {
        reportRelMouseEvent(dx, dy, buttons);
      } else {
        // kept for backward compatibility
        sendNotification("relMouseReport", { dx, dy, buttons });
      }
      setMouseMove({ x, y, buttons });
    },
    [sendNotification, reportRelMouseEvent, rpcHidReady, setMouseMove, settings.mouseMode, settings.mouseSensitivity, isReinitializingGadget, mouseEnabled],
  );

  // Mouse collection of the absolute HID function: the mode that desktop hosts
  // turn into a real cursor.
  const sendAbsMovement = useCallback(
    (x: number, y: number, buttons: number) => {
      if (settings.mouseMode !== "absolute") return;
      // Don't send mouse events while reinitializing gadget
      if (isReinitializingGadget) return;
      // The mouse interfaces are switched off on the device. Reporting movement
      // would only fail and can retrigger a gadget reinitialization.
      if (!mouseEnabled) return;
      if (rpcHidReady) {
        reportAbsMouseEvent(x, y, buttons);
      } else {
        // kept for backward compatibility
        sendNotification("absMouseReport", { x, y, buttons });
      }
      lastPointerRef.current = { x, y };
      setMousePosition(x, y);
    },
    [sendNotification, reportAbsMouseEvent, rpcHidReady, setMousePosition, settings.mouseMode, isReinitializingGadget, mouseEnabled],
  );

  // Pen collection of the absolute HID function. Android ignores the absolute
  // axes of a mouse collection but does track a stylus, which is the whole point
  // of this mode: the host sees a hovering pen cursor and a tip press as a click.
  const sendPenMovement = useCallback(
    (x: number, y: number, buttons: number) => {
      if (settings.mouseMode !== "digitizer-stylus") return;
      if (isReinitializingGadget) return;
      if (!mouseEnabled) return;

      const tip = (buttons & 1) !== 0;
      const barrel = (buttons & 2) !== 0;
      // In Range keeps the cursor alive between clicks: without it the host ends
      // the stroke and drops the hover position.
      const flags =
        PEN_FLAGS.inRange | (tip ? PEN_FLAGS.tip : 0) | (barrel ? PEN_FLAGS.barrel : 0);

      if (rpcHidReady) {
        reportPenEvent(x, y, flags, tip ? PEN_CONTACT_PRESSURE : 0);
      } else {
        // No pen collection exists behind the legacy JSON-RPC path, so the
        // absolute mouse is the closest thing a pre-hidrpc firmware can do.
        sendNotification("absMouseReport", { x, y, buttons });
      }
      lastPointerRef.current = { x, y };
      setMousePosition(x, y);
    },
    [sendNotification, reportPenEvent, rpcHidReady, setMousePosition, settings.mouseMode, isReinitializingGadget, mouseEnabled],
  );

  // Virtual mouse buttons on touch layouts drive this entry point, so it has to
  // follow whichever absolute-position mode is selected.
  const sendAbsMouseMovement = useCallback(
    (x: number, y: number, buttons: number) => {
      if (settings.mouseMode === "digitizer-stylus") {
        sendPenMovement(x, y, buttons);
        return;
      }
      sendAbsMovement(x, y, buttons);
    },
    [settings.mouseMode, sendPenMovement, sendAbsMovement],
  );

  // Touch screen collection: every report carries the complete set of fingers, so
  // this one is stateful and cannot reuse the pointer-report path.
  const sendTouchContacts = useCallback(() => {
    if (settings.mouseMode !== "touch-screen") return;
    if (isReinitializingGadget) return;
    if (!mouseEnabled) return;

    const contacts = Array.from(activeTouchesRef.current.values()).map(touch => ({
      id: touch.slot,
      x: touch.x,
      y: touch.y,
      down: touch.down,
      inRange: true,
    }));

    reportTouchEvent(contacts);
  }, [reportTouchEvent, settings.mouseMode, isReinitializingGadget, mouseEnabled]);

  const queueTouchContacts = useCallback(() => {
    if (touchFrameRef.current !== null) return;

    touchFrameRef.current = requestAnimationFrame(() => {
      touchFrameRef.current = null;
      sendTouchContacts();
    });
  }, [sendTouchContacts]);

  // Forgetting contacts has to cancel their pending release timers as well: a timer
  // left over from the previous session would delete a slot of the next one.
  const clearTouchContacts = useCallback(() => {
    touchReleaseTimersRef.current.forEach(timer => window.clearTimeout(timer));
    touchReleaseTimersRef.current.clear();
    activeTouchesRef.current.clear();
  }, []);

  // Re-send the touch state for a moment after every change. The report channel
  // drops the oldest entry when it backs up and the gadget can refuse a write, and
  // a lost report is a finger the host never sees lift.
  useEffect(() => {
    if (settings.mouseMode !== "touch-screen") return;

    const timer = window.setInterval(() => {
      const busy = activeTouchesRef.current.size > 0;
      const settled = Date.now() - lastTouchChangeRef.current;
      if (!busy && settled > TOUCH_HEARTBEAT_GRACE_MS) return;
      sendTouchContacts();
    }, TOUCH_HEARTBEAT_MS);

    return () => window.clearInterval(timer);
  }, [settings.mouseMode, sendTouchContacts]);

  const dispatchAbsMovement = useCallback(
    (x: number, y: number, buttons: number) => {
      if (settings.mouseMode === "digitizer-stylus") {
        sendPenMovement(x, y, buttons);
        return;
      }
      sendAbsMovement(x, y, buttons);
    },
    [settings.mouseMode, sendPenMovement, sendAbsMovement],
  );

  const sendVirtualRelativeMovement = useCallback(
    (x: number, y: number, buttons = 0) => {
      sendRelMouseMovement(x, y, buttons, true);
    },
    [sendRelMouseMovement],
  );

  const queueRelMouseMovement = useCallback(
    (x: number, y: number, buttons: number) => {
      const pending = pendingRelMouseRef.current;
      if (pending) {
        pending.x += x;
        pending.y += y;
        pending.buttons = buttons;
      } else {
        pendingRelMouseRef.current = { x, y, buttons };
      }

      if (relMouseFrameRef.current !== null) return;

      // Merge high-frequency pointermove events into a single RPC per frame.
      relMouseFrameRef.current = requestAnimationFrame(() => {
        relMouseFrameRef.current = null;
        const next = pendingRelMouseRef.current;
        pendingRelMouseRef.current = null;
        if (!next) return;
        sendRelMouseMovement(next.x, next.y, next.buttons);
      });
    },
    [sendRelMouseMovement],
  );

  const queueAbsMouseMovement = useCallback(
    (x: number, y: number, buttons: number) => {
      pendingAbsMouseRef.current = { x, y, buttons };

      if (absMouseFrameRef.current !== null) return;

      // Absolute modes only need the latest pointer position for the next frame.
      absMouseFrameRef.current = requestAnimationFrame(() => {
        absMouseFrameRef.current = null;
        const next = pendingAbsMouseRef.current;
        pendingAbsMouseRef.current = null;
        if (!next) return;
        dispatchAbsMovement(next.x, next.y, next.buttons);
      });
    },
    [dispatchAbsMovement],
  );

  // Convert a pointer position inside the video element to HID absolute
  // coordinates, compensating for the letterboxing of a mismatched aspect ratio.
  const mapToHidCoords = useCallback(
    (clientX: number, clientY: number) => {
      const videoElmRefValue = videoElm.current;
      if (!videoElmRefValue) return null;
      if (!videoWidth || !videoHeight) return null;

      const rect = videoElmRefValue.getBoundingClientRect();
      const displayedWidth = rect.width;
      const displayedHeight = rect.height;
      if (!displayedWidth || !displayedHeight) return null;

      const videoElementAspectRatio = displayedWidth / displayedHeight;
      const videoStreamAspectRatio = videoWidth / videoHeight;

      let effectiveWidth = displayedWidth;
      let effectiveHeight = displayedHeight;
      let offsetX = 0;
      let offsetY = 0;

      if (videoElementAspectRatio > videoStreamAspectRatio) {
        effectiveWidth = displayedHeight * videoStreamAspectRatio;
        offsetX = (displayedWidth - effectiveWidth) / 2;
      } else if (videoElementAspectRatio < videoStreamAspectRatio) {
        effectiveHeight = displayedWidth / videoStreamAspectRatio;
        offsetY = (displayedHeight - effectiveHeight) / 2;
      }

      // Use visual coordinates relative to the video element's bounding rect
      const localX = clientX - rect.left;
      const localY = clientY - rect.top;

      const clampedX = Math.min(Math.max(offsetX, localX), offsetX + effectiveWidth);
      const clampedY = Math.min(Math.max(offsetY, localY), offsetY + effectiveHeight);

      // transform to HID absolute coordinates (0-32767 range)
      return {
        x: Math.round(((clampedX - offsetX) / effectiveWidth) * 32767),
        y: Math.round(((clampedY - offsetY) / effectiveHeight) * 32767),
      };
    },
    [videoElm, videoWidth, videoHeight],
  );

  const relMouseMoveHandler = useCallback(
    (e: MouseEvent) => {
      const pt = (e as unknown as PointerEvent).pointerType as unknown as string;
      const eventType = (e as unknown as PointerEvent).type;
      if (pt === "touch") {
        if (touchZoom) {
            const touchCount = touchZoom.activeTouchPointers.current.size;
            if (touchCount >= 2) return;
            if (touchZoom.mobileScale > 1 && touchZoom.lastPanPoint.current) return;
        }
      }
      
      if(isMobile){
        e.preventDefault();
      }
      if (settings.mouseMode !== "relative") return;
      if (!pointerLock.isPointerLockActive && pointerLock.isPointerLockPossible) return;

      const { buttons } = e;
      if (eventType === "pointermove") {
        queueRelMouseMovement(e.movementX, e.movementY, buttons);
        return;
      }

      sendRelMouseMovement(e.movementX, e.movementY, buttons);
    },
    [pointerLock.isPointerLockActive, pointerLock.isPointerLockPossible, queueRelMouseMovement, sendRelMouseMovement, settings.mouseMode, touchZoom],
  );

  // Absolute, pen and a single-finger drag: one handler for all absolute-position
  // modes, differing only in which HID collection the report is addressed to.
  const absMouseMoveHandler = useCallback(
    (e: MouseEvent) => {
      const pt = (e as unknown as PointerEvent).pointerType as unknown as string;
      const pointerEvent = e as unknown as PointerEvent;
      const eventType = pointerEvent.type;
      if (pt === "touch") {
        if (touchZoom) {
          const touchCount = touchZoom.activeTouchPointers.current.size;
          if (touchCount >= 2) {
            if (eventType === "pointerup" || eventType === "pointercancel") {
              touchDragActiveRef.current = false;
            }
            return;
          }
        }
      }

      if(isMobile){
        e.preventDefault();
      }

      if (!usesAbsolutePosition(settings.mouseMode)) return;

      const coords = mapToHidCoords(e.clientX, e.clientY);
      if (!coords) return;

      let buttons = e.buttons;

      if (pt === "touch") {
        if (eventType === "pointerdown") {
          touchDragActiveRef.current = !disableTouchClick;
        }
        if (eventType === "pointerup" || eventType === "pointercancel") {
          buttons = 0;
          touchDragActiveRef.current = false;
        } else {
          buttons = touchDragActiveRef.current ? 1 : 0;
        }
      }

      buttons |= externalButtons;

      if (eventType === "pointermove") {
        queueAbsMouseMovement(coords.x, coords.y, buttons);
        return;
      }

      dispatchAbsMovement(coords.x, coords.y, buttons);
    },
    [settings.mouseMode, mapToHidCoords, queueAbsMouseMovement, dispatchAbsMovement, touchZoom, disableTouchClick, externalButtons, videoElm],
  );

  // Touch screen mode: every pointer on the video becomes a finger of its own, so
  // two fingers drag and pinch on the host instead of being reinterpreted as a
  // mouse drag.
  const touchScreenHandler = useCallback(
    (e: MouseEvent) => {
      const pointerEvent = e as unknown as PointerEvent;
      const eventType = pointerEvent.type;

      if (settings.mouseMode !== "touch-screen") return;
      if (isReinitializingGadget) return;
      if (!mouseEnabled) return;

      if (isMobile) {
        e.preventDefault();
      }

      const touches = activeTouchesRef.current;

      if (eventType === "pointerdown") {
        // A primary pointer can only start when the browser sees no other active
        // pointer, so anything still in the map belongs to a pointer whose up or
        // cancel never reached us (the finger lifted outside the video). Dropping
        // it here is what recovers from a ghost finger instead of leaving it
        // pressed on the host until the gadget is re-initialised.
        if (pointerEvent.isPrimary && touches.size > 0) {
          clearTouchContacts();
        }

        const coords = mapToHidCoords(e.clientX, e.clientY);
        if (!coords) return;

        // Capture the pointer so move/up keep arriving even when the finger
        // leaves the video element. Two-finger gestures drift over overlays all
        // the time, and a lost pointerup strands that contact on the host.
        try {
          videoElm.current?.setPointerCapture(pointerEvent.pointerId);
        } catch {
          // Capture is best effort; the sweep above is the fallback.
        }

        const usedSlots = new Set(Array.from(touches.values()).map(touch => touch.slot));
        let slot = -1;
        for (let i = 0; i < MAX_TOUCH_CONTACTS; i += 1) {
          if (!usedSlots.has(i)) {
            slot = i;
            break;
          }
        }
        // Out of contact slots: the host would drop the extra finger anyway.
        if (slot < 0) return;

        touches.set(pointerEvent.pointerId, { slot, x: coords.x, y: coords.y, down: true });
        lastTouchChangeRef.current = Date.now();

        // Dropping a pending release for a recycled pointer id would otherwise
        // delete the contact the new press just created.
        const pendingRelease = touchReleaseTimersRef.current.get(pointerEvent.pointerId);
        if (pendingRelease !== undefined) {
          window.clearTimeout(pendingRelease);
          touchReleaseTimersRef.current.delete(pointerEvent.pointerId);
        }

        sendTouchContacts();
        return;
      }

      const touch = touches.get(pointerEvent.pointerId);
      if (!touch) return;

      if (eventType === "pointermove") {
        const coords = mapToHidCoords(e.clientX, e.clientY);
        if (!coords) return;
        touches.set(pointerEvent.pointerId, { slot: touch.slot, x: coords.x, y: coords.y, down: touch.down });
        queueTouchContacts();
        return;
      }

      // pointerup and pointercancel: keep the slot one report longer with the tip
      // up but still in range, then forget it. Clearing both flags at once is
      // what leaves Android holding a finger it never sees released, and shifting
      // the remaining contacts into the free slot would do the same to them.
      if (touch.down) {
        touches.set(pointerEvent.pointerId, { ...touch, down: false });
        lastTouchChangeRef.current = Date.now();
        sendTouchContacts();
      }

      const pendingRelease = touchReleaseTimersRef.current.get(pointerEvent.pointerId);
      if (pendingRelease !== undefined) window.clearTimeout(pendingRelease);

      const releaseTimer = window.setTimeout(() => {
        touchReleaseTimersRef.current.delete(pointerEvent.pointerId);
        activeTouchesRef.current.delete(pointerEvent.pointerId);
        // Announce the dropped slot. The heartbeat stops once nothing is down, so
        // without this report the host keeps a contact that is still in range:
        // the finger looks lifted, but its slot never goes away.
        sendTouchContacts();
      }, TOUCH_RELEASE_HOLD_MS);
      touchReleaseTimersRef.current.set(pointerEvent.pointerId, releaseTimer);
      lastTouchChangeRef.current = Date.now();
    },
    [
      settings.mouseMode,
      isReinitializingGadget,
      mouseEnabled,
      mapToHidCoords,
      sendTouchContacts,
      queueTouchContacts,
      clearTouchContacts,
      videoElm,
    ],
  );

  const mouseWheelHandler = useCallback(
    (e: WheelEvent) => {
      // Don't send wheel events while reinitializing gadget
      if (isReinitializingGadget) return;
      if (!mouseEnabled) return;

      if (e.ctrlKey) {
        // A trackpad pinch arrives as ctrl+wheel with no physical Ctrl held, so the
        // chord has to be told apart from a real Ctrl press before it reaches the
        // host: only a real press is something the target should act on.
        const physicalCtrl = useHidStore
          .getState()
          .activeModifiers.some(m => m & MODIFIER_CONTROL);
        if (!physicalCtrl) return;
      }

      if (settings.scrollThrottling && Date.now() < wheelThrottleUntilRef.current) {
        return;
      }

      // Keep the magnitude on both axes: a trackpad flick carries tens of pixels per
      // event, and collapsing the horizontal one to its sign made panning crawl
      // while the vertical axis accelerated.
      const isAccel = Math.abs(e.deltaY) >= 100;
      const isAccelX = Math.abs(e.deltaX) >= 100;
      const scrollValue = isAccel ? e.deltaY / 100 : Math.sign(e.deltaY);
      const scrollValueX = isAccelX ? e.deltaX / 100 : Math.sign(e.deltaX);

      const clampedScrollValue = Math.max(-127, Math.min(127, scrollValue));
      const clampedScrollValueX = Math.max(-127, Math.min(127, scrollValueX));
      const wheelY = settings.invertScroll ? clampedScrollValue : -clampedScrollValue;

      // Horizontal scrolling ignores invertScroll on purpose: the setting is
      // about the wheel, and a trackpad swipe already carries its own direction.
      const wheelX = -clampedScrollValueX;

      if (wheelY === 0 && wheelX === 0) return;

      sendNotification("wheelReport", { wheelY, wheelX });

      if (settings.scrollThrottling) {
        wheelThrottleUntilRef.current = Date.now() + settings.scrollThrottling;
      }
    },
    [sendNotification, settings, isReinitializingGadget, mouseEnabled],
  );

  // Release whatever the current mode has pressed, so switching modes, losing the
  // window or leaving the page cannot leave a stuck tip, finger or button.
  const resetMousePosition = useCallback(() => {
    if (settings.mouseMode === "touch-screen") {
      // Always send the empty report: the host may still hold a contact we no
      // longer track, and one idle report costs nothing next to a stuck finger.
      clearTouchContacts();
      sendTouchContacts();
      return;
    }

    if (settings.mouseMode === "digitizer-stylus") {
      if (!rpcHidReady || !mouseEnabled || isReinitializingGadget) return;
      // Flags without In Range take the pen off the surface.
      reportPenEvent(lastPointerRef.current.x, lastPointerRef.current.y, 0, 0);
      return;
    }

    sendAbsMovement(0, 0, 0);
  }, [
    settings.mouseMode,
    rpcHidReady,
    mouseEnabled,
    isReinitializingGadget,
    reportPenEvent,
    clearTouchContacts,
    sendTouchContacts,
    sendAbsMovement,
  ]);

  // Mode changes must release in the mode being left, while its report type is
  // still the one the host is listening to.
  const previousMouseModeRef = useRef(settings.mouseMode);
  useEffect(() => {
    const previousMode = previousMouseModeRef.current;
    if (previousMode === settings.mouseMode) return;

    previousMouseModeRef.current = settings.mouseMode;

    if (isReinitializingGadget || !rpcHidReady) return;

    const position = lastPointerRef.current;
    switch (previousMode) {
      case "touch-screen":
        clearTouchContacts();
        reportTouchEvent([]);
        break;
      case "digitizer-stylus":
        reportPenEvent(position.x, position.y, 0, 0);
        break;
      case "absolute":
        reportAbsMouseEvent(position.x, position.y, 0);
        break;
    }
  }, [settings.mouseMode, isReinitializingGadget, rpcHidReady, reportTouchEvent, reportPenEvent, reportAbsMouseEvent, clearTouchContacts]);

  const isRelativeMouseMode = (settings.mouseMode === "relative");
  const isTouchScreenMode = (settings.mouseMode === "touch-screen");
  const mouseMoveHandler = isRelativeMouseMode
    ? relMouseMoveHandler
    : isTouchScreenMode
      ? touchScreenHandler
      : absMouseMoveHandler;
  const handlerRef = useRef(mouseMoveHandler);

  useEffect(() => {
    handlerRef.current = mouseMoveHandler;
  }, [mouseMoveHandler]);

  const setupMouseEvents = useCallback(() => {
    const videoElmRefValue = videoElm.current;
    if (!videoElmRefValue) return;

    const abortController = new AbortController();
    const signal = abortController.signal;

    const eventHandler = (e: Event) => {
      if (handlerRef.current) {
        handlerRef.current(e as any);
      }
    };

    videoElmRefValue.addEventListener("pointermove", eventHandler, { signal });
    videoElmRefValue.addEventListener("pointerdown", eventHandler, { signal });
    videoElmRefValue.addEventListener("pointerup", eventHandler, { signal });
    videoElmRefValue.addEventListener("pointercancel", eventHandler, { signal });
    videoElmRefValue.addEventListener("wheel", mouseWheelHandler, {
      signal,
      passive: true,
    });

    if (isRelativeMouseMode) {
      videoElmRefValue.addEventListener("click",
        () => {
          if (pointerLock.isPointerLockPossible && !pointerLock.isPointerLockActive && !document.pointerLockElement) {
            pointerLock.requestPointerLock();
          }
        },
        { signal },
      );
    } else {
      window.addEventListener("blur", resetMousePosition, { signal });
      document.addEventListener("visibilitychange", resetMousePosition, { signal });
    }

    const preventContextMenu = (e: MouseEvent) => e.preventDefault();
    videoElmRefValue.addEventListener("contextmenu", preventContextMenu, { signal });

    return () => {
      if (relMouseFrameRef.current !== null) {
        cancelAnimationFrame(relMouseFrameRef.current);
        relMouseFrameRef.current = null;
      }
      if (absMouseFrameRef.current !== null) {
        cancelAnimationFrame(absMouseFrameRef.current);
        absMouseFrameRef.current = null;
      }
      if (touchFrameRef.current !== null) {
        cancelAnimationFrame(touchFrameRef.current);
        touchFrameRef.current = null;
      }
      pendingRelMouseRef.current = null;
      pendingAbsMouseRef.current = null;
      touchReleaseTimersRef.current.forEach(timer => window.clearTimeout(timer));
      touchReleaseTimersRef.current.clear();
      abortController.abort();
    };
  }, [
    videoElm,
    settings.mouseMode,
    isRelativeMouseMode,
    // Removed relMouseMoveHandler and absMouseMoveHandler from dependencies to prevent re-binding
    mouseWheelHandler,
    pointerLock,
    resetMousePosition
  ]);

  return {
    setupMouseEvents,
    sendVirtualRelativeMovement,
    sendAbsMouseMovement,
  };
};
