import { useCallback, useEffect, useRef } from "react";

import {
  BUTTON_LEFT,
  BUTTON_RIGHT,
  LONG_PRESS_MS,
  LONG_PRESS_VIBRATION_MS,
  createTouchGestureState,
  isBeyondDeadZone,
  isWithinDoubleTapWindow,
  remainingTapGap,
  smoothToward,
} from "@/utils/touchGesture";

export type TouchEmit = (clientX: number, clientY: number, buttons: number) => void;

interface Options {
  /** Where reports go; the caller maps client coordinates to HID coordinates. */
  emit: TouchEmit;
  /** Taps press the primary button. A double tap drags even when this is off. */
  tapToClick: boolean;
}

export interface TouchGestureHandlers {
  handleDown: (event: PointerEvent) => void;
  handleMove: (event: PointerEvent) => void;
  handleUp: (event: PointerEvent) => void;
  /** Drop an in-flight gesture and its timers; a second finger took over. */
  reset: () => void;
}

/**
 * Single-finger gesture arbitration for the mobile video surface: long press
 * selects the secondary button, double tap unlocks dragging, and everything
 * inside the dead zone stays silent. See ADR-0003 and ADR-0004.
 */
export const useTouchGestures = ({ emit, tapToClick }: Options): TouchGestureHandlers => {
  const gesture = useRef(createTouchGestureState());
  const longPressTimer = useRef<number | null>(null);
  const deferredPressTimer = useRef<number | null>(null);
  // A press is only released after it reached the host. Releasing one that is
  // still waiting in the timer would press-and-release for a tap nobody saw.
  const pressPending = useRef(false);
  const pressSent = useRef(false);
  const lastEmitPoint = useRef({ x: 0, y: 0 });
  const lastTapUpAt = useRef(0);
  const lastTapPoint = useRef<{ x: number; y: number } | null>(null);

  // Kept in refs so the handlers stay stable across re-renders; recreating them
  // mid-gesture would strand the timers of the previous closures.
  const emitRef = useRef(emit);
  useEffect(() => {
    emitRef.current = emit;
  }, [emit]);
  const tapToClickRef = useRef(tapToClick);
  useEffect(() => {
    tapToClickRef.current = tapToClick;
  }, [tapToClick]);

  const clearDeferredPress = useCallback(() => {
    if (deferredPressTimer.current !== null) {
      window.clearTimeout(deferredPressTimer.current);
      deferredPressTimer.current = null;
    }
    pressPending.current = false;
  }, []);

  const clearLongPress = useCallback(() => {
    if (longPressTimer.current !== null) {
      window.clearTimeout(longPressTimer.current);
      longPressTimer.current = null;
    }
  }, []);

  const sendPress = useCallback((clientX: number, clientY: number) => {
    const state = gesture.current;
    state.leftButtonActive = tapToClickRef.current;
    pressPending.current = false;
    pressSent.current = true;
    lastEmitPoint.current = { x: clientX, y: clientY };
    emitRef.current(clientX, clientY, state.leftButtonActive ? BUTTON_LEFT : 0);
  }, []);

  // Releases whatever this gesture pressed, primary or secondary. Both are
  // reported with an empty button mask, so one call is enough.
  const sendRelease = useCallback((clientX: number, clientY: number) => {
    const state = gesture.current;
    if (!pressSent.current && !state.rightButtonActive) return;
    pressSent.current = false;
    state.leftButtonActive = false;
    state.rightButtonActive = false;
    lastEmitPoint.current = { x: clientX, y: clientY };
    emitRef.current(clientX, clientY, 0);
  }, []);

  const reset = useCallback(() => {
    // The caller has already released whatever the first finger pressed, so this
    // only forgets the bookkeeping instead of reporting a second release.
    clearLongPress();
    clearDeferredPress();
    gesture.current = createTouchGestureState();
    pressSent.current = false;
  }, [clearLongPress, clearDeferredPress]);

  // A surface that disappears mid-press must not leave the host's button down.
  useEffect(() => {
    return () => {
      clearLongPress();
      clearDeferredPress();
      const state = gesture.current;
      if (!pressSent.current && !state.rightButtonActive) return;
      pressSent.current = false;
      state.leftButtonActive = false;
      state.rightButtonActive = false;
      emitRef.current(lastEmitPoint.current.x, lastEmitPoint.current.y, 0);
    };
  }, [clearLongPress, clearDeferredPress]);

  const handleDown = useCallback(
    (event: PointerEvent) => {
      clearLongPress();
      clearDeferredPress();
      pressSent.current = false;
      const now = event.timeStamp;
      const previousTap = lastTapPoint.current;
      const doubleTap =
        previousTap !== null &&
        isWithinDoubleTapWindow(lastTapUpAt.current, now) &&
        !isBeyondDeadZone(previousTap.x, previousTap.y, event.clientX, event.clientY);

      gesture.current = {
        ...createTouchGestureState(),
        anchorX: event.clientX,
        anchorY: event.clientY,
        x: event.clientX,
        y: event.clientY,
        startedAt: now,
        doubleTap,
      };

      const wait = remainingTapGap(lastTapUpAt.current, now);
      if (wait > 0) {
        // The host drops a press that lands too soon after the previous release,
        // which is exactly what a deliberate double tap looks like.
        pressPending.current = true;
        deferredPressTimer.current = window.setTimeout(() => {
          deferredPressTimer.current = null;
          if (gesture.current.startedAt !== now) return;
          if (gesture.current.rightButtonActive) return;
          sendPress(event.clientX, event.clientY);
        }, wait);
      } else {
        sendPress(event.clientX, event.clientY);
      }

      longPressTimer.current = window.setTimeout(() => {
        longPressTimer.current = null;
        const state = gesture.current;
        if (state.startedAt !== now || state.moved || state.rightButtonActive) return;
        clearDeferredPress();
        // The press already went out as the primary button. Releasing it first
        // keeps the host from seeing both buttons down at once.
        if (pressSent.current) sendRelease(state.anchorX, state.anchorY);
        state.leftButtonActive = false;
        state.rightButtonActive = true;
        pressSent.current = true;
        lastEmitPoint.current = { x: state.anchorX, y: state.anchorY };
        emitRef.current(state.anchorX, state.anchorY, BUTTON_RIGHT);
        if (typeof navigator !== "undefined" && typeof navigator.vibrate === "function") {
          navigator.vibrate(LONG_PRESS_VIBRATION_MS);
        }
      }, LONG_PRESS_MS);
    },
    [clearLongPress, clearDeferredPress, sendPress, sendRelease],
  );

  const handleMove = useCallback(
    (event: PointerEvent) => {
      const state = gesture.current;
      if (state.startedAt <= 0) return;

      if (!state.moved) {
        if (!isBeyondDeadZone(state.anchorX, state.anchorY, event.clientX, event.clientY)) return;
        state.moved = true;
        clearLongPress();
        if (pressPending.current) {
          // The drag needs the button now; a press that lands later would drag
          // from the anchor and jump the cursor backwards.
          clearDeferredPress();
          sendPress(state.anchorX, state.anchorY);
        }
        // The first reported point grows out of the anchor, so crossing the dead
        // zone does not shift the cursor by its own radius.
        state.x = smoothToward(state.anchorX, event.clientX);
        state.y = smoothToward(state.anchorY, event.clientY);
      } else {
        state.x = smoothToward(state.x, event.clientX);
        state.y = smoothToward(state.y, event.clientY);
      }

      if (state.rightButtonActive) {
        lastEmitPoint.current = { x: state.x, y: state.y };
        emitRef.current(state.x, state.y, BUTTON_RIGHT);
        return;
      }
      // A double tap keeps the primary button down for the rest of the gesture
      // even when tap-to-click is off, which is what makes double-tap-drag work.
      if (!state.leftButtonActive && state.doubleTap) state.leftButtonActive = true;
      lastEmitPoint.current = { x: state.x, y: state.y };
      emitRef.current(state.x, state.y, state.leftButtonActive ? BUTTON_LEFT : 0);
    },
    [clearLongPress, clearDeferredPress, sendPress],
  );

  const handleUp = useCallback(
    (event: PointerEvent) => {
      const state = gesture.current;
      if (state.startedAt <= 0) return;
      const wasRightClick = state.rightButtonActive;
      clearLongPress();
      clearDeferredPress();
      // A cancelled pointer has no usable coordinates; release where the gesture
      // actually was so the click cannot land in the corner of the screen.
      const cancelled = event.type === "pointercancel";
      const releaseX = cancelled ? (state.moved ? state.x : state.anchorX) : event.clientX;
      const releaseY = cancelled ? (state.moved ? state.y : state.anchorY) : event.clientY;
      // Release at the raw finger position so the click lands where the user aimed
      // rather than where the low pass had caught up to.
      sendRelease(releaseX, releaseY);

      if (!state.moved && !wasRightClick) {
        lastTapUpAt.current = event.timeStamp;
        lastTapPoint.current = { x: event.clientX, y: event.clientY };
      } else {
        lastTapUpAt.current = 0;
        lastTapPoint.current = null;
      }
      gesture.current = createTouchGestureState();
    },
    [clearLongPress, clearDeferredPress, sendRelease],
  );

  return { handleDown, handleMove, handleUp, reset };
};
