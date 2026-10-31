// Tapping the video opens the virtual keyboard on touch layouts. The button that
// hides the keyboard sits on top of the video, so the very same tap can reach the
// video element and immediately reopen it. Ignore video taps for a short window
// after the keyboard was hidden.
const SUPPRESS_MS = 500;

let suppressUntil = 0;

export const suppressKeyboardTap = (ms: number = SUPPRESS_MS) => {
  suppressUntil = Date.now() + ms;
};

export const isKeyboardTapSuppressed = () => Date.now() < suppressUntil;
