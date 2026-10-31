// Turns two-finger travel into wheel notches.
//
// Deliberately free of DOM and React: the pacing rules (pixels per notch, speed
// boost, remainder carry-over) are the part worth reasoning about in isolation,
// and are the seam the feature is tested at.

export const SCROLL_PIXELS_PER_NOTCH = 40;
export const MAX_WHEEL_NOTCHES = 127;

// Below this the finger is barely moving and the boost would just amplify jitter.
// 1 px/ms is already a brisk drag (≈1000 px/s), so ordinary scrolling stays 1:1.
const NO_BOOST_SPEED = 1; // px per ms
const FULL_BOOST_SPEED = 3; // px per ms
const MAX_SPEED_BOOST = 3;

export interface ScrollStep {
  /** Signed notches to report now. */
  ticks: number;
  /** Unspent pixels, to be fed back in as the next call's remainder. */
  remainderPx: number;
}

/**
 * Distance an average finger travel outpaces the raw notch rate.
 *
 * 1x up to NO_BOOST_SPEED, rising linearly to MAX_SPEED_BOOST at FULL_BOOST_SPEED,
 * and clamped there so a violent swipe cannot turn into a teleport.
 */
export function scrollSpeedBoost(speedPxPerMs: number): number {
  if (!Number.isFinite(speedPxPerMs) || speedPxPerMs <= NO_BOOST_SPEED) return 1;
  if (speedPxPerMs >= FULL_BOOST_SPEED) return MAX_SPEED_BOOST;
  const ratio = (speedPxPerMs - NO_BOOST_SPEED) / (FULL_BOOST_SPEED - NO_BOOST_SPEED);
  return 1 + ratio * (MAX_SPEED_BOOST - 1);
}

/**
 * Convert one movement sample into whole notches, keeping the remainder so slow
 * drags still add up to a notch eventually.
 *
 * The sign follows the movement: a downward finger movement yields positive
 * ticks, which is a wheel-up (scroll toward the top) report on the host.
 */
export function accumulateScroll(
  deltaPx: number,
  remainderPx: number,
  speedPxPerMs: number,
): ScrollStep {
  const carry = Number.isFinite(remainderPx) ? remainderPx : 0;
  const delta = Number.isFinite(deltaPx) ? deltaPx : 0;
  const total = carry + delta;
  if (total === 0) return { ticks: 0, remainderPx: carry };

  const whole = Math.trunc(total / SCROLL_PIXELS_PER_NOTCH);
  const remainder = total - whole * SCROLL_PIXELS_PER_NOTCH;
  if (whole === 0) return { ticks: 0, remainderPx: remainder };

  const boosted = Math.trunc(whole * scrollSpeedBoost(speedPxPerMs));
  // A boosted whole notch must never disappear, and never flip direction.
  const ticks = boosted === 0 ? Math.sign(whole) : boosted;
  return { ticks, remainderPx: remainder };
}

/** A single wheel report carries one signed byte per axis. */
export function clampWheelNotches(notches: number): number {
  if (!Number.isFinite(notches)) return 0;
  const whole = Math.trunc(notches);
  return Math.max(-MAX_WHEEL_NOTCHES, Math.min(MAX_WHEEL_NOTCHES, whole));
}
