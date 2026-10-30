import { Select } from "antd";
import type { SelectProps } from "antd";
import { isMobile } from "react-device-detect";

import { cx } from "@/cva.config";

/**
 * SettingsSelect is the shared dropdown for the side panels.
 *
 * The trigger sits in a narrow right-hand column, so it gets a minimum width and
 * must not shrink below it. The popup, on the other hand, sizes itself to its
 * widest option instead of matching the trigger: matching would truncate labels
 * like "Touch Pen" or "Chinese (Traditional) (zh_TW)" to "Pen..."
 */
export default function SettingsSelect(props: SelectProps) {
  const { className, ...rest } = props;

  return (
    <Select
      popupMatchSelectWidth={false}
      className={cx("shrink-0", isMobile ? "w-full" : "min-w-[130px]", className)}
      {...rest}
    />
  );
}
