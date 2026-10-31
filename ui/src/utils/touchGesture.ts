// Single-finger touch gesture classification for the mobile video surface.
// Everything here is pure math: timers and refs belong to useTouchGestures.

export const LONG_PRESS_MS = 600;
export const LONG_PRESS_VIBRATION_MS = 20;
export const DOUBLE_TAP_WINDOW_MS = 300;
// Two presses closer than this are dropped by some host OSes, so the second one
// is delayed instead of sent immediately.
export const MIN_TAP_GAP_MS = 60;
export const DRAG_THRESHOLD_PX = 6;
// First-order low pass coefficient: higher tracks the finger, lower is steadier.
export const SMOOTHING_ALPHA = 0.35;

export const BUTTON_LEFT = 1;
export const BUTTON_RIGHT = 2;

export interface TouchGestureState {
  anchorX: number;
  anchorY: number;
  x: number;
  y: number;
  startedAt: number;
  moved: boolean;
  doubleTap: boolean;
  leftButtonActive: boolean;
  rightButtonActive: boolean;
}

export const createTouchGestureState = (): TouchGestureState => ({
  anchorX: 0,
  anchorY: 0,
  x: 0,
  y: 0,
  startedAt: 0,
  moved: false,
  doubleTap: false,
  leftButtonActive: false,
  rightButtonActive: false,
});

export const distanceBetween = (ax: number, ay: number, bx: number, by: number): number =>
  Math.hypot(bx - ax, by - ay);

export const isBeyondDeadZone = (ax: number, ay: number, bx: number, by: number): boolean => {
  const distance = distanceBetween(ax, ay, bx, by);
  return Number.isFinite(distance) && distance >= DRAG_THRESHOLD_PX;
};

export const isWithinDoubleTapWindow = (previousTapAt: number, now: number): boolean => {
  if (previousTapAt <= 0) return false;
  const gap = now - previousTapAt;
  return gap >= 0 && gap <= DOUBLE_TAP_WINDOW_MS;
};

// How long a press has to wait so its gap to the previous release reaches
// MIN_TAP_GAP_MS. Zero means "send now".
export const remainingTapGap = (previousTapUpAt: number, now: number): number => {
  if (previousTapUpAt <= 0) return 0;
  const gap = now - previousTapUpAt;
  if (!Number.isFinite(gap)) return 0;
  return Math.max(0, MIN_TAP_GAP_MS - Math.max(0, gap));
};

export const smoothToward = (
  current: number,
  target: number,
  alpha: number = SMOOTHING_ALPHA,
): number => {
  if (!Number.isFinite(target)) return Number.isFinite(current) ? current : 0;
  if (!Number.isFinite(current)) return target;
  const gain = Math.min(Math.max(alpha, 0), 1);
  return current + (target - current) * gain;
};
