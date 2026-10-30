// Mouse modes that report an absolute position rather than a movement delta. The
// pen and the touch screen are absolute too, they only address a different HID
// collection, so all three share the coordinate mapping.
export const ABSOLUTE_POSITION_MOUSE_MODES = [
  "absolute",
  "digitizer-stylus",
  "touch-screen",
] as const;

export const usesAbsolutePosition = (mode: string): boolean =>
  (ABSOLUTE_POSITION_MOUSE_MODES as readonly string[]).includes(mode);
