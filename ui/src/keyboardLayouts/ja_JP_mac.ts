import { KeyboardLayout, KeyCombo } from "../keyboardLayouts";

import { ja_JP } from "./ja_JP";

const name = "Japanese (Mac)";
const isoCode = "ja-JP-mac";

// The physical JIS yen key uses International3 (0x89), also on macOS.
// Sending Backslash (0x31) targets the "]" position instead. Keep YenMac as a
// button alias, but do not substitute a different physical key to produce "¥".
//
// The IME keys differ as well. An Apple JIS keyboard has no 変換 key at all, and
// its 英数 / かな keys send HID LANG2 (0x91) / LANG1 (0x90) -- 英数 sits left of
// the space bar, かな right of it. That was measured on a real Apple Japanese
// keyboard (USB 05AC:0251), so the Apple usages and not the Windows ones are the
// target here; the Windows spellings Muhenkan (0x8b) / KatakanaHiragana (0x88)
// belong to the ja_JP layout. Henkan (0x8a) stays out because only Windows
// keyboards have that key.
//
// The HID report descriptor must encode the logical maximum 255 as 26 ff 00.
// With the one-byte 25 ff encoding, macOS reads these array values as negative
// and never generates LANG1/LANG2 elements, even with a JIS keyboard configured.
// This is a descriptor issue, not a reason to replace these usages with shortcuts.
const chars: Record<string, KeyCombo> = {
  ...ja_JP.chars,
  "\\": { key: "YenMac" },
  "¥": { key: "YenMac" },
  "|": { key: "YenMac", shift: true },
};

const keyDisplayMap = {
  ...ja_JP.keyDisplayMap,
  YenMac: "¥",
  "(YenMac)": "|",
  // macOS users know these keys by their Apple legends (英数 / かな), and the
  // usages are the Apple ones, not the Windows 無変換 / ひらがな pair.
  Lang2: "英数",
  Lang1: "かな",
};

// Preserve the Mac button alias in both the normal and shifted "(Yen)" rows.
const toMacRow = (row: string) => row.replace(/\bYen\b/g, "YenMac");

// 英数 sits left of the space bar, かな right of it. These are plain keys, so the
// shifted row uses the same names instead of the parenthesised spelling ja_JP
// needs for its ローマ字 legend.
const macModifierRow =
  "ControlLeft MetaLeft AltLeft Lang2 Space Lang1 AltRight MetaRight ContextMenu ControlRight";

const toMacRows = (rows: string[]) =>
  rows.map(row => (row.split(" ").includes("Space") ? macModifierRow : toMacRow(row)));

const virtualKeyboard = {
  ...ja_JP.virtualKeyboard,
  main: {
    ...ja_JP.virtualKeyboard.main,
    default: toMacRows(ja_JP.virtualKeyboard.main.default),
    shift: toMacRows(ja_JP.virtualKeyboard.main.shift),
  },
};

export const ja_JP_mac: KeyboardLayout = {
  isoCode,
  name,
  chars,
  keyDisplayMap,
  modifierDisplayMap: ja_JP.modifierDisplayMap,
  virtualKeyboard,
};
