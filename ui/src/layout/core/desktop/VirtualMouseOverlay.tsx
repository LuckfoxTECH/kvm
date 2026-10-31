import type { CSSProperties, PointerEvent as ReactPointerEvent, ReactNode } from "react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Slider } from "antd";
import { LuKeyboard, LuMouse, LuRotateCcw, LuSettings, LuZoomIn } from "react-icons/lu";
import { useReactAt } from "i18n-auto-extractor/react";

import { useJsonRpc } from "@/hooks/useJsonRpc";
import { cx } from "@/cva.config";

// Widget identity, also the key each layout is persisted under.
export type VirtualMouseWidgetKey = "joystick" | "left" | "right" | "wheel";

// One widget's placement and appearance. Positions are fractions of the overlay
// area so a saved layout survives any screen size.
export interface VirtualMouseWidgetLayout {
  x: number;
  y: number;
  scale: number;
  opacity: number;
  enabled: boolean;
}

export interface VirtualMouseLayout {
  enabled: boolean;
  speed: number;
  widgets: Record<VirtualMouseWidgetKey, VirtualMouseWidgetLayout>;
}

interface VirtualMouseOverlayProps {
  isDark: boolean;
  toolsHidden: boolean;
  disabled: boolean;
  isTouchGestureEnabled: boolean;
  onToggleTouchGesture: () => void;
  onResetView: () => void;
  virtualKeyboardEnabled: boolean;
  onToggleVirtualKeyboard: () => void;
  onPressButtons: (mask: number) => void;
  onReleaseButtons: (mask: number) => void;
  // The tools drive the relative pointer collection, so every other mouse mode
  // would report into a channel that is not producing the cursor on screen. The
  // panel stays open in those modes: the controls only go inert and offer a
  // shortcut back to relative mode.
  mouseToolsAllowed: boolean;
  // Lets the panel switch the mouse mode itself, so a user who opened the tools
  // in an absolute mode can recover without hunting for the settings page.
  onSetMouseMode?: (mode: string) => void;
  onRelativeMove: (dx: number, dy: number) => void;
  // Absolute modes have to move the cursor through the same channel that the
  // button reports use, otherwise pressing a button would teleport the cursor
  // back to the last absolute coordinate.
  onMoveAbsolute?: (dxNorm: number, dyNorm: number) => void;
  onWheel: (delta: number) => void;
  // Tells the video layer that a widget is on screen, so a tap does not also
  // register as a click on the remote desktop.
  onActiveChange: (active: boolean) => void;
}

const WIDGET_KEYS: VirtualMouseWidgetKey[] = ["joystick", "left", "right", "wheel"];

// Base size in px at scale 1, matching the prototype's proportions.
const WIDGET_SIZE: Record<VirtualMouseWidgetKey, { w: number; h: number }> = {
  joystick: { w: 80, h: 80 },
  left: { w: 56, h: 56 },
  right: { w: 56, h: 56 },
  wheel: { w: 40, h: 106 },
};

const SCALE_MIN = 0.5;
const SCALE_MAX = 2;
const OPACITY_MIN = 0.2;
const SPEED_MIN = 0.5;
const SPEED_MAX = 8;

// One wheel tap scrolls several notches, and holding repeats it, because a
// single notch per tap feels far too slow on a phone screen.
const WHEEL_STEP = 3;
const WHEEL_REPEAT_MS = 90;

const RPC_GET = "getMobileVirtualMouse";
const RPC_SET = "setMobileVirtualMouse";

function defaultLayout(): VirtualMouseLayout {
  return {
    // The toolbar stays off until the user turns it on: most people never need
    // it, and an overlay the user did not ask for blocks the video.
    enabled: false,
    speed: 1,
    widgets: {
      joystick: { x: 0.169, y: 0.452, scale: 1, opacity: 1, enabled: true },
      left: { x: 0.4, y: 0.467, scale: 1, opacity: 1, enabled: true },
      right: { x: 0.559, y: 0.467, scale: 1, opacity: 1, enabled: true },
      wheel: { x: 0.728, y: 0.437, scale: 1, opacity: 1, enabled: true },
    },
  };
}

function clamp(value: number, low: number, high: number) {
  return Math.min(high, Math.max(low, value));
}

// hydrateLayout keeps a stored layout usable even when it was written by an
// older firmware that knew fewer widgets.
function hydrateLayout(raw: unknown): VirtualMouseLayout {
  const fallback = defaultLayout();
  if (!raw || typeof raw !== "object") return fallback;
  const source = raw as Partial<VirtualMouseLayout>;
  const widgets = (source.widgets ?? {}) as Partial<Record<VirtualMouseWidgetKey, VirtualMouseWidgetLayout>>;
  const next = defaultLayout();
  next.enabled = source.enabled === true;
  if (typeof source.speed === "number" && source.speed > 0) {
    next.speed = clamp(source.speed, SPEED_MIN, SPEED_MAX);
  }
  WIDGET_KEYS.forEach(key => {
    const widget = widgets[key];
    if (!widget) return;
    next.widgets[key] = {
      x: typeof widget.x === "number" ? clamp(widget.x, 0, 1) : fallback.widgets[key].x,
      y: typeof widget.y === "number" ? clamp(widget.y, 0, 1) : fallback.widgets[key].y,
      scale: typeof widget.scale === "number" && widget.scale > 0 ? clamp(widget.scale, SCALE_MIN, SCALE_MAX) : 1,
      opacity:
        typeof widget.opacity === "number" && widget.opacity > 0 ? clamp(widget.opacity, OPACITY_MIN, 1) : 1,
      enabled: widget.enabled !== false,
    };
  });
  return next;
}

// Round icon button with a press animation. Android ignores :active for touch
// handlers that call preventDefault, so the state is tracked by hand.
function ToolButton({
  active,
  isDark,
  label,
  onClick,
  children,
}: {
  active: boolean;
  isDark: boolean;
  label: string;
  onClick: () => void;
  children: ReactNode;
}) {
  const [down, setDown] = useState(false);
  return (
    <button
      type="button"
      aria-label={label}
      style={{ touchAction: "none" }}
      className={cx(
        "flex h-8 w-8 items-center justify-center rounded-full transition-all duration-100 ease-out",
        active
          ? "bg-blue-500 text-white"
          : isDark
            ? "bg-white/15 text-white"
            : "bg-black/[0.08] text-black",
        down ? "scale-90 brightness-125" : "scale-100",
      )}
      onPointerDown={() => setDown(true)}
      onPointerUp={() => setDown(false)}
      onPointerCancel={() => setDown(false)}
      onPointerLeave={() => setDown(false)}
      onClick={onClick}
    >
      {children}
    </button>
  );
}

const Toggle = ({
  checked,
  isDark,
  onChange,
  className = "",
}: {
  checked: boolean;
  isDark: boolean;
  onChange: () => void;
  className?: string;
}) => (
  <button
    type="button"
    role="switch"
    aria-checked={checked}
    onClick={event => {
      event.stopPropagation();
      onChange();
    }}
    className={cx(
      "relative h-[22px] w-[38px] shrink-0 rounded-full transition-all duration-150 active:scale-95",
      checked ? "bg-blue-500" : isDark ? "bg-white/25" : "bg-black/20",
      className,
    )}
  >
    <span
      className={cx(
        "absolute left-[2px] top-[2px] h-[18px] w-[18px] rounded-full bg-white shadow transition-transform duration-150",
        checked && "translate-x-4",
      )}
    />
  </button>
);

export default function VirtualMouseOverlay({
  isDark,
  toolsHidden,
  disabled,
  isTouchGestureEnabled,
  onToggleTouchGesture,
  onResetView,
  virtualKeyboardEnabled,
  onToggleVirtualKeyboard,
  onPressButtons,
  onReleaseButtons,
  mouseToolsAllowed,
  onSetMouseMode,
  onRelativeMove,
  onMoveAbsolute,
  onWheel,
  onActiveChange,
}: VirtualMouseOverlayProps) {
  const { $at } = useReactAt();
  const [send] = useJsonRpc();
  const [layout, setLayout] = useState<VirtualMouseLayout>(defaultLayout);
  const [editing, setEditing] = useState(false);
  const [selected, setSelected] = useState<VirtualMouseWidgetKey | null>(null);
  const [vector, setVector] = useState({ x: 0, y: 0 });
  // Press animation state, keyed by control so multi-touch presses stay independent.
  const [pressed, setPressed] = useState<Record<string, boolean>>({});
  const markPressed = useCallback((key: string, down: boolean) => {
    setPressed(current => (current[key] === down ? current : { ...current, [key]: down }));
  }, []);

  const rootRef = useRef<HTMLDivElement>(null);
  const vectorRef = useRef({ x: 0, y: 0 });
  const layoutRef = useRef(layout);
  const joystickPointerRef = useRef<number | null>(null);
  const dragRef = useRef<{ key: VirtualMouseWidgetKey; dx: number; dy: number } | null>(null);
  const frameRef = useRef<number | null>(null);
  const lastTsRef = useRef<number | null>(null);
  const widgetRefs = useRef<Partial<Record<VirtualMouseWidgetKey, HTMLDivElement | null>>>({});
  // Repeating wheel timers, keyed by direction so opposite buttons stay independent.
  const wheelTimersRef = useRef<Partial<Record<"up" | "down", number>>>({});

  // Mirror the layout for the pointer handlers: they run outside React's render
  // cycle and must not close over a stale copy.
  useEffect(() => {
    layoutRef.current = layout;
  }, [layout]);

  // The layout belongs to the device, not to this browser: it is read back on
  // mount so every client sees the same widget positions.
  useEffect(() => {
    send(RPC_GET, {}, resp => {
      if ("error" in resp) return;
      setLayout(hydrateLayout(resp.result));
    });
  }, [send]);

  useEffect(() => {
    return () => {
      (["up", "down"] as const).forEach(direction => {
        const timer = wheelTimersRef.current[direction];
        if (timer !== undefined) window.clearInterval(timer);
      });
      wheelTimersRef.current = {};
    };
  }, []);

  const visible = useMemo(() => {
    const out = {} as Record<VirtualMouseWidgetKey, boolean>;
    WIDGET_KEYS.forEach(key => {
      out[key] = layout.enabled && layout.widgets[key].enabled;
    });
    return out;
  }, [layout]);

  const anyVisible = useMemo(
    () => mouseToolsAllowed && WIDGET_KEYS.some(key => visible[key]),
    [visible, mouseToolsAllowed],
  );

  // The panel stays readable in absolute modes, but only the shortcut back to
  // relative mode may be used: everything else writes to a channel that is not
  // driving the cursor on screen.
  const panelLocked = !mouseToolsAllowed;
  const lockedClass = panelLocked && "pointer-events-none opacity-30 grayscale";

  useEffect(() => {
    onActiveChange(anyVisible);
  }, [anyVisible, onActiveChange]);

  useEffect(() => {
    vectorRef.current = vector;
  }, [vector]);

  // Drive the pointer at a steady frame rate: a joystick held still still has to
  // keep sending relative deltas, so this cannot live on the pointer handler.
  useEffect(() => {
    const speedFactor = layout.speed;
    if (editing || disabled || !visible.joystick || !mouseToolsAllowed) {
      setVector({ x: 0, y: 0 });
      return;
    }

    const tick = (timestamp: number) => {
      const previous = lastTsRef.current ?? timestamp;
      lastTsRef.current = timestamp;
      const frameScale = Math.min(2, Math.max(0.5, (timestamp - previous) / 16.67));
      const current = vectorRef.current;
      const width = rootRef.current?.clientWidth ?? 1280;
      const height = rootRef.current?.clientHeight ?? 720;
      // Small screens need a lower base speed or the cursor overshoots.
      const resolutionFactor = Math.max(0.35, Math.min(1, width / 1280));
      const speed = 12 * resolutionFactor * speedFactor;
      const dx = Math.round(current.x * speed * frameScale);
      const dy = Math.round(current.y * speed * frameScale);
      if (dx !== 0 || dy !== 0) {
        if (onMoveAbsolute) onMoveAbsolute(dx / width, dy / height);
        else onRelativeMove(dx, dy);
      }
      frameRef.current = requestAnimationFrame(tick);
    };

    frameRef.current = requestAnimationFrame(tick);
    return () => {
      if (frameRef.current !== null) {
        cancelAnimationFrame(frameRef.current);
        frameRef.current = null;
      }
      lastTsRef.current = null;
    };
  }, [editing, disabled, visible.joystick, mouseToolsAllowed, layout.speed, onRelativeMove, onMoveAbsolute]);

  const updateWidget = useCallback((key: VirtualMouseWidgetKey, patch: Partial<VirtualMouseWidgetLayout>) => {
    setLayout(current => ({
      ...current,
      widgets: { ...current.widgets, [key]: { ...current.widgets[key], ...patch } },
    }));
  }, []);

  const widgetStyle = useCallback(
    (key: VirtualMouseWidgetKey) => {
      const widget = layout.widgets[key];
      return {
        left: `${widget.x * 100}%`,
        top: `${widget.y * 100}%`,
        transform: `scale(${widget.scale})`,
        transformOrigin: "top left",
        opacity: widget.opacity,
      } as CSSProperties;
    },
    [layout.widgets],
  );

  const moveWidget = useCallback(
    (key: VirtualMouseWidgetKey, clientX: number, clientY: number, drag: { dx: number; dy: number }) => {
      const area = rootRef.current?.getBoundingClientRect();
      if (!area) return;
      const widget = layoutRef.current.widgets[key];
      const size = WIDGET_SIZE[key];
      const scaledW = size.w * widget.scale;
      const scaledH = size.h * widget.scale;
      const maxX = Math.max(0, area.width - scaledW);
      const maxY = Math.max(0, area.height - scaledH);
      const nextX = clamp(clientX - area.left - drag.dx, 0, maxX);
      const nextY = clamp(clientY - area.top - drag.dy, 0, maxY);
      updateWidget(key, { x: nextX / area.width, y: nextY / area.height });
    },
    [updateWidget],
  );

  const startDrag = (event: ReactPointerEvent<HTMLDivElement>, key: VirtualMouseWidgetKey) => {
    const target = event.currentTarget;
    const rect = target.getBoundingClientRect();
    dragRef.current = { key, dx: event.clientX - rect.left, dy: event.clientY - rect.top };
    target.setPointerCapture(event.pointerId);
    setSelected(key);
    event.preventDefault();
    event.stopPropagation();
  };

  const updateJoystickVector = (clientX: number, clientY: number) => {
    const element = widgetRefs.current.joystick;
    if (!element) return;
    const rect = element.getBoundingClientRect();
    const scale = layoutRef.current.widgets.joystick.scale;
    const rawX = clientX - (rect.left + rect.width / 2);
    const rawY = clientY - (rect.top + rect.height / 2);
    const maxRadius = 32 * scale;
    const length = Math.hypot(rawX, rawY);
    if (!length) {
      setVector({ x: 0, y: 0 });
      return;
    }
    const factor = length > maxRadius ? maxRadius / length : 1;
    const limit = maxRadius || 1;
    setVector({ x: (rawX * factor) / limit, y: (rawY * factor) / limit });
  };

  const persistLayout = useCallback(
    (next: VirtualMouseLayout, onSaved?: () => void) => {
      send(RPC_SET, { layout: JSON.stringify(next) }, resp => {
        if ("error" in resp) return;
        onSaved?.();
      });
    },
    [send],
  );

  const saveLayout = () => {
    persistLayout(layout, () => {
      setEditing(false);
      setSelected(null);
    });
  };

  const restoreLayout = () => {
    // Restore positions and sizes only: the master switch is the user's choice.
    const next = { ...defaultLayout(), enabled: layout.enabled };
    setLayout(next);
    setSelected(null);
    persistLayout(next);
  };

  const widgetClass = (key: VirtualMouseWidgetKey) =>
    cx("pointer-events-auto absolute", editing && "cursor-grab touch-none", editing && dragRef.current?.key === key && "cursor-grabbing");

  const renderGhostBadge = (key: VirtualMouseWidgetKey) => {
    if (!editing) return null;
    const reason = !layout.enabled ? $at("Master switch off") : !layout.widgets[key].enabled ? $at("Disabled") : null;
    if (!reason) return null;
    return (
      <div
        className={cx(
          "pointer-events-none absolute -top-6 left-1/2 -translate-x-1/2 whitespace-nowrap rounded-full border px-1.5 py-[1px] text-[10px]",
          isDark ? "border-white/20 bg-black/65 text-white/60" : "border-black/15 bg-white/85 text-black/60",
        )}
      >
        {reason}
      </div>
    );
  };

  // The buttons hold for as long as the finger stays down: pressing reports the
  // button mask, lifting releases it again. Nothing latches.
  const beginHold = (key: VirtualMouseWidgetKey, mask: number) => {
    markPressed(key, true);
    onPressButtons(mask);
  };

  const endHold = (key: VirtualMouseWidgetKey, mask: number) => {
    markPressed(key, false);
    onReleaseButtons(mask);
  };

  const startWheel = (direction: "up" | "down") => {
    if (editing || disabled || !visible.wheel || !mouseToolsAllowed) return;
    if (wheelTimersRef.current[direction] !== undefined) return;
    const delta = direction === "up" ? 1 : -1;
    onWheel(delta * WHEEL_STEP);
    wheelTimersRef.current[direction] = window.setInterval(() => {
      onWheel(delta * WHEEL_STEP);
    }, WHEEL_REPEAT_MS);
  };

  const stopWheel = (direction: "up" | "down") => {
    const timer = wheelTimersRef.current[direction];
    if (timer === undefined) return;
    window.clearInterval(timer);
    wheelTimersRef.current[direction] = undefined;
  };

  const renderCore = (key: VirtualMouseWidgetKey) => {
    const active = editing ? true : visible[key];
    const dimmed = editing && (!layout.enabled || !layout.widgets[key].enabled);
    const base = cx("flex items-center justify-center", dimmed && "opacity-20 grayscale");

    if (key === "joystick") {
      return (
        <div
          className={cx(
            base,
            "h-20 w-20 rounded-full border-2 transition-all duration-100 ease-out",
            isDark ? "border-white/55 bg-white/10" : "border-black/40 bg-white/55 shadow-md shadow-black/20",
            pressed.joystick && "scale-95 brightness-125",
          )}
          style={{ touchAction: "none" }}
          onPointerDown={event => {
            if (editing || !active || disabled) return;
            if (joystickPointerRef.current !== null) return;
            joystickPointerRef.current = event.pointerId;
            event.currentTarget.setPointerCapture(event.pointerId);
            markPressed("joystick", true);
            updateJoystickVector(event.clientX, event.clientY);
            event.preventDefault();
          }}
          onPointerMove={event => {
            if (editing || joystickPointerRef.current !== event.pointerId) return;
            updateJoystickVector(event.clientX, event.clientY);
            event.preventDefault();
          }}
          onPointerUp={event => {
            if (joystickPointerRef.current !== event.pointerId) return;
            joystickPointerRef.current = null;
            markPressed("joystick", false);
            event.currentTarget.releasePointerCapture(event.pointerId);
            setVector({ x: 0, y: 0 });
          }}
          onPointerCancel={() => {
            joystickPointerRef.current = null;
            markPressed("joystick", false);
            setVector({ x: 0, y: 0 });
          }}
        >
          <div
            className={cx(
              "h-9 w-9 rounded-full transition-all duration-100",
              isDark ? "bg-white" : "bg-black/70",
              pressed.joystick && "scale-95 brightness-125",
            )}
            style={{ transform: `translate(${vector.x * 24}px, ${vector.y * 24}px)` }}
          />
        </div>
      );
    }

    if (key === "wheel") {
      return (
        <div
          className={cx(
            base,
            "flex h-[106px] w-10 flex-col overflow-hidden rounded-full transition-all duration-100 ease-out",
            isDark
              ? "bg-neutral-700/85 ring-1 ring-white/40 shadow-lg shadow-black/50"
              : "bg-white/85 ring-1 ring-black/35 shadow-lg shadow-black/25",
            // The capsule shrinks as a whole on any press; the section that was
            // hit shrinks further so the tap still reads as segmented.
            (pressed.wheelUp || pressed.wheelDown || pressed.middle) && "scale-95",
          )}
          style={{ touchAction: "none" }}
        >
          <button
            type="button"
            className={cx(
              "flex flex-1 items-center justify-center border-b text-[11px] transition-all duration-100 ease-out",
              isDark ? "border-white/30 text-white" : "border-black/25 text-black/80",
              pressed.wheelUp && "scale-95 brightness-125",
            )}
            style={{ touchAction: "none" }}
            onPointerDown={event => {
              event.preventDefault();
              if (!mouseToolsAllowed) return;
              markPressed("wheelUp", true);
              startWheel("up");
            }}
            onPointerUp={() => {
              if (!mouseToolsAllowed) return;
              markPressed("wheelUp", false);
              stopWheel("up");
            }}
            onPointerCancel={() => {
              if (!mouseToolsAllowed) return;
              markPressed("wheelUp", false);
              stopWheel("up");
            }}
            onPointerLeave={() => {
              if (!mouseToolsAllowed) return;
              markPressed("wheelUp", false);
              stopWheel("up");
            }}
          >
            ▲
          </button>
          <button
            type="button"
            className={cx(
              "flex items-center justify-center border-b text-[11px] font-bold transition-all duration-100 ease-out",
              isDark ? "border-white/30 text-white" : "border-black/25 text-black/80",
              pressed.middle && "scale-95 brightness-125",
            )}
            style={{ flex: "0.8", touchAction: "none" }}
            onPointerDown={() => {
              if (editing || !active || disabled) return;
              markPressed("middle", true);
              onPressButtons(4);
            }}
            onPointerUp={() => {
              markPressed("middle", false);
              if (editing || !active || disabled) return;
              onReleaseButtons(4);
            }}
            onPointerCancel={() => {
              markPressed("middle", false);
              if (editing || !active || disabled) return;
              onReleaseButtons(4);
            }}
            onPointerLeave={() => {
              if (!pressed.middle) return;
              markPressed("middle", false);
              onReleaseButtons(4);
            }}
          >
            M
          </button>
          <button
            type="button"
            className={cx(
              "flex flex-1 items-center justify-center text-[11px] transition-all duration-100 ease-out",
              isDark ? "text-white" : "text-black/80",
              pressed.wheelDown && "scale-95 brightness-125",
            )}
            style={{ touchAction: "none" }}
            onPointerDown={event => {
              event.preventDefault();
              if (!mouseToolsAllowed) return;
              markPressed("wheelDown", true);
              startWheel("down");
            }}
            onPointerUp={() => {
              if (!mouseToolsAllowed) return;
              markPressed("wheelDown", false);
              stopWheel("down");
            }}
            onPointerCancel={() => {
              if (!mouseToolsAllowed) return;
              markPressed("wheelDown", false);
              stopWheel("down");
            }}
            onPointerLeave={() => {
              if (!mouseToolsAllowed) return;
              markPressed("wheelDown", false);
              stopWheel("down");
            }}
          >
            ▼
          </button>
        </div>
      );
    }

    const mask = key === "left" ? 1 : 2;
    return (
      <div
        className={cx(
          base,
          "h-14 w-14 rounded-full text-xs transition-all duration-100 ease-out",
          isDark
            ? "bg-neutral-700/85 text-white ring-1 ring-white/40 shadow-lg shadow-black/50"
            : "bg-white/85 text-black ring-1 ring-black/35 shadow-lg shadow-black/25",
          pressed[key] && "scale-90 brightness-125",
        )}
        style={{ touchAction: "none" }}
        onPointerDown={() => {
          if (editing || !active || disabled) return;
          beginHold(key, mask);
        }}
        onPointerUp={() => {
          if (editing || !active || disabled) return;
          endHold(key, mask);
        }}
        onPointerCancel={() => {
          if (editing || !active || disabled) return;
          endHold(key, mask);
        }}
        onPointerLeave={() => {
          if (!pressed[key]) return;
          endHold(key, mask);
        }}
      >
        {key === "left" ? "L" : "R"}
      </div>
    );
  };

  const selectedWidget = selected ? layout.widgets[selected] : null;

  const sliderRow = (
    label: string,
    value: number,
    min: number,
    max: number,
    step: number,
    format: (value: number) => string,
    onChange: (value: number) => void,
  ) => (
    <div className="flex items-center gap-1.5 py-[3px]">
      <span className={cx("w-[46px] shrink-0 truncate text-[11px]", isDark ? "text-white/60" : "text-black/55")}>
        {label}
      </span>
      <button
        type="button"
        className={cx(
          "flex h-[22px] w-[22px] shrink-0 items-center justify-center rounded-md border text-[13px] leading-none transition-all duration-100 active:scale-90 active:bg-blue-500/25",
          isDark ? "border-white/15 bg-white/10 text-white" : "border-black/12 bg-black/[0.06] text-black/75",
        )}
        onClick={event => {
          event.stopPropagation();
          onChange(clamp(value - step, min, max));
        }}
      >
        −
      </button>
      <div className="min-w-0 flex-1">
        <Slider
          min={min}
          max={max}
          step={step}
          value={value}
          tooltip={{ open: false }}
          onChange={next => onChange(next as number)}
        />
      </div>
      <button
        type="button"
        className={cx(
          "flex h-[22px] w-[22px] shrink-0 items-center justify-center rounded-md border text-[13px] leading-none transition-all duration-100 active:scale-90 active:bg-blue-500/25",
          isDark ? "border-white/15 bg-white/10 text-white" : "border-black/12 bg-black/[0.06] text-black/75",
        )}
        onClick={event => {
          event.stopPropagation();
          onChange(clamp(value + step, min, max));
        }}
      >
        +
      </button>
      <span
        className={cx(
          "w-11 shrink-0 text-right text-[11px] tabular-nums",
          isDark ? "text-white/80" : "text-black/70",
        )}
      >
        {format(value)}
      </span>
    </div>
  );

  return (
    <div ref={rootRef} className="pointer-events-none absolute inset-0">
      <div
        className={cx(
          "pointer-events-auto absolute right-3 top-3 z-40 grid grid-cols-2 gap-1.5 transition-all duration-300",
          toolsHidden && "pointer-events-none translate-x-4 opacity-0",
        )}
      >
        <ToolButton
          active={isTouchGestureEnabled}
          isDark={isDark}
          label={$at("Zoom")}
          onClick={onToggleTouchGesture}
        >
          <LuZoomIn className="h-[18px] w-[18px]" />
        </ToolButton>
        <ToolButton active={false} isDark={isDark} label={$at("Reset view")} onClick={onResetView}>
          <LuRotateCcw className="h-[18px] w-[18px]" />
        </ToolButton>
        <ToolButton
          active={virtualKeyboardEnabled}
          isDark={isDark}
          label={$at("Virtual keyboard")}
          onClick={onToggleVirtualKeyboard}
        >
          <LuKeyboard className="h-[18px] w-[18px]" />
        </ToolButton>
        <ToolButton
          active={editing}
          isDark={isDark}
          label={$at("Mouse Tools")}
          onClick={() => {
            if (editing) {
              setEditing(false);
              setSelected(null);
              return;
            }
            setEditing(true);
            setSelected(null);
          }}
        >
          <LuMouse className="h-[18px] w-[18px]" />
        </ToolButton>
      </div>

      {editing && (
        <div
          className={cx(
            "pointer-events-auto absolute left-2.5 right-[88px] top-3 z-[60] overflow-hidden rounded-xl border shadow-lg",
            isDark
              ? "border-white/[0.18] bg-black/[0.62] text-white"
              : "border-black/[0.12] bg-white/[0.88] text-black",
          )}
          onPointerDown={event => event.stopPropagation()}
        >
          <div className="flex items-center gap-2 px-2.5 py-2">
            <span className="flex flex-1 items-center gap-1.5 whitespace-nowrap text-[12.5px] font-semibold">
              <LuSettings className="h-3.5 w-3.5 shrink-0" />
              {$at("Mouse Tools")}
            </span>
            <span
              className={cx(
                "h-[7px] w-[7px] shrink-0 rounded-full",
                layout.enabled ? "bg-blue-400" : isDark ? "bg-white/30" : "bg-black/20",
                panelLocked && "opacity-30 grayscale",
              )}
            />
            <Toggle
              checked={layout.enabled}
              isDark={isDark}
              className={lockedClass || undefined}
              onChange={() => {
                // The master switch is changed without opening the editor, so it
                // saves itself instead of waiting for the Save button.
                const next = { ...layout, enabled: !layout.enabled };
                setLayout(next);
                persistLayout(next);
              }}
            />
          </div>
          {!mouseToolsAllowed && (
            <div
              className={cx(
                "mx-2.5 mb-2.5 flex items-center gap-2 rounded-[9px] border px-2.5 py-2 text-[11.5px] leading-snug",
                isDark
                  ? "border-blue-400/45 bg-blue-500/[0.14] text-blue-100"
                  : "border-blue-500/40 bg-blue-500/[0.10] text-blue-700",
              )}
            >
              <span className="flex-1">
                {$at("Mouse tools move the cursor in relative mode.")}
              </span>
              <button
                type="button"
                className="shrink-0 rounded-[7px] bg-blue-500 px-2.5 py-1 text-[11px] font-semibold text-white transition-all duration-100 active:scale-95 active:bg-blue-600"
                onClick={event => {
                  event.stopPropagation();
                  onSetMouseMode?.("relative");
                }}
              >
                {$at("Switch")}
              </button>
            </div>
          )}
          <div
            className={cx(
              "flex gap-2 border-b px-2.5 pb-2.5",
              isDark ? "border-white/[0.14]" : "border-black/[0.10]",
              lockedClass,
            )}
          >
            <button
              type="button"
              className={cx(
                "flex flex-1 items-center justify-center gap-1.5 rounded-[9px] border py-2 text-xs transition-all duration-100 active:scale-95",
                isDark
                  ? "border-blue-400/55 bg-blue-500/[0.16] text-blue-100 active:bg-blue-500/30"
                  : "border-blue-500/45 bg-blue-500/[0.10] text-blue-600 active:bg-blue-500/20",
              )}
              onClick={restoreLayout}
            >
              <LuRotateCcw className="h-3.5 w-3.5 shrink-0" />
              {$at("Restore")}
            </button>
            <button
              type="button"
              className="flex flex-1 items-center justify-center rounded-[9px] bg-blue-500 py-2 text-xs font-semibold text-white transition-all duration-100 active:scale-95 active:bg-blue-600"
              onClick={saveLayout}
            >
              {$at("Save")}
            </button>
          </div>
          <div className={cx("px-2.5 pb-2 pt-1", lockedClass)}>
            {panelLocked || !selected || !selectedWidget ? (
              <div className={cx("py-2 text-[11.5px]", isDark ? "text-white/55" : "text-black/50")}>
                {$at(
                  panelLocked
                    ? "Switch to relative mode to show the controls"
                    : "Tap a control to adjust its size and opacity",
                )}
              </div>
            ) : (
              <>
                <div className="flex items-center gap-2 pb-1 pt-1">
                  <span
                    className={cx(
                      "h-[7px] w-[7px] shrink-0 rounded-full",
                      visible[selected] ? "bg-blue-400" : isDark ? "bg-white/30" : "bg-black/20",
                    )}
                  />
                  <span className="flex-1 text-xs font-semibold">
                    {$at(
                      selected === "joystick"
                        ? "Joystick"
                        : selected === "left"
                          ? "Left button"
                          : selected === "right"
                            ? "Right button"
                            : "Wheel",
                    )}
                  </span>
                  <Toggle
                    checked={layout.widgets[selected].enabled}
                    isDark={isDark}
                    onChange={() => updateWidget(selected, { enabled: !layout.widgets[selected].enabled })}
                  />
                </div>
                {sliderRow(
                  $at("Scale"),
                  selectedWidget.scale,
                  SCALE_MIN,
                  SCALE_MAX,
                  0.05,
                  value => `${value.toFixed(2)}×`,
                  value => updateWidget(selected, { scale: value }),
                )}
                {sliderRow(
                  $at("Opacity"),
                  selectedWidget.opacity,
                  OPACITY_MIN,
                  1,
                  0.05,
                  value => `${Math.round(value * 100)}%`,
                  value => updateWidget(selected, { opacity: value }),
                )}
                {selected === "joystick" &&
                  sliderRow(
                    $at("Speed"),
                    layout.speed,
                    SPEED_MIN,
                    SPEED_MAX,
                    0.05,
                    value => `${value.toFixed(2)}×`,
                    value => setLayout(current => ({ ...current, speed: value })),
                  )}
              </>
            )}
          </div>
        </div>
      )}

      {editing && (
        <div
          className="pointer-events-auto absolute inset-0 z-30"
          onPointerDown={() => setSelected(null)}
        />
      )}

      {WIDGET_KEYS.map(key => {
        // The widgets only exist while the relative channel is driving the cursor.
        const shown = mouseToolsAllowed && (editing || visible[key]);
        if (!shown) return null;
        return (
          <div
            key={key}
            ref={element => {
              widgetRefs.current[key] = element;
            }}
            className={cx(
              widgetClass(key),
              editing && "z-50 outline-dashed outline-1 outline-offset-[5px]",
              editing && (isDark ? "outline-white/60" : "outline-black/45"),
              editing && selected === key && "outline-2 outline-blue-400",
              editing && (!layout.enabled || !layout.widgets[key].enabled) && "z-50",
            )}
            style={widgetStyle(key)}
            onPointerDown={event => {
              if (!editing) return;
              startDrag(event, key);
            }}
            onPointerMove={event => {
              const drag = dragRef.current;
              if (!drag || drag.key !== key) return;
              moveWidget(key, event.clientX, event.clientY, drag);
              event.preventDefault();
            }}
            onPointerUp={event => {
              if (dragRef.current?.key !== key) return;
              dragRef.current = null;
              event.currentTarget.releasePointerCapture(event.pointerId);
            }}
            onPointerCancel={() => {
              dragRef.current = null;
            }}
          >
            {renderGhostBadge(key)}
            <div className={cx("relative", editing && "pointer-events-none")}>{renderCore(key)}</div>
          </div>
        );
      })}
    </div>
  );
}
