// The on-screen keyboard has two independent dimensions:
//
//   - KeyboardLayout   (keyboardLayouts.ts) which character sits on each key
//   - KeyboardFormFactor (this file)        which keys physically exist
//
// Every language layout embeds the extra keys of the keyboard it was authored
// on, so switching the form factor has to add or remove those keys instead of
// assuming one fixed row shape. Transforming the layout keeps the 20 language
// layouts untouched.

export type KeyboardFormFactor = "ANSI" | "ISO" | "JIS";

export const keyboardFormFactorOptions: { value: KeyboardFormFactor; label: string }[] = [
  { value: "ANSI", label: "ANSI (US)" },
  { value: "ISO", label: "ISO (Europe)" },
  { value: "JIS", label: "JIS (Japan)" },
];

export function isKeyboardFormFactor(value: unknown): value is KeyboardFormFactor {
  return value === "ANSI" || value === "ISO" || value === "JIS";
}

// Left of the Z row on ISO keyboards, absent on ANSI and JIS.
const ISO_ONLY = ["IntlBackslash"];

// Keys only a JIS keyboard has, grouped by what inserts them. YenMac/Lang1/Lang2
// are the Apple spellings the Japanese (Mac) layout deliberately uses, so an
// existing one counts as "already present" instead of being replaced.
const JIS_YEN_KEYS = ["Yen", "YenMac"];
const JIS_RO_KEYS = ["KeyRO"];
const JIS_IME_KEYS = [
  "Henkan",
  "Muhenkan",
  "KatakanaHiragana",
  "Lang1",
  "Lang2",
];

// What a layout gets when it carries none of the keys above. Only the Windows
// 106/109 set: the Apple spellings belong to the Mac layout alone, and inserting
// them everywhere would put five IME keys on an English layout.
const JIS_IME_INSERT = ["Henkan", "Muhenkan", "KatakanaHiragana"];

const JIS_ONLY = [...JIS_YEN_KEYS, ...JIS_RO_KEYS, ...JIS_IME_KEYS];

const tokens = (row: string) => row.split(" ").filter(Boolean);

// Shift rows wrap a key in parentheses ("(Digit1)"), so every comparison and
// removal has to look at the bare key name.
const bare = (token: string) => token.replace(/[()]/g, "");

const has = (row: string, key: string) => tokens(row).some(t => bare(t) === key);

const hasAny = (row: string, keys: string[]) =>
  tokens(row).some(t => keys.includes(bare(t)));

// Presence has to be judged across the whole layout, not per row: ja_JP keeps its
// yen key on the digit row while the key name it occupies elsewhere is Backslash,
// so a per-row check inserts a second yen.
const rowsHave = (rows: string[], keys: string[]) =>
  rows.some(row => hasAny(row, keys));

function removeKeys(row: string, keys: string[]) {
  return tokens(row)
    .filter(t => !keys.includes(bare(t)))
    .join(" ");
}

function insertAfter(row: string, anchor: string, keys: string[]) {
  const list = tokens(row);
  const at = list.findIndex(t => bare(t) === anchor);
  if (at === -1) return row;
  list.splice(at + 1, 0, ...keys);
  return list.join(" ");
}

function insertBefore(row: string, anchor: string, keys: string[]) {
  const list = tokens(row);
  const at = list.findIndex(t => bare(t) === anchor);
  if (at === -1) return row;
  list.splice(at, 0, ...keys);
  return list.join(" ");
}

function applyToRow(row: string, formFactor: KeyboardFormFactor) {
  if (formFactor === "JIS") return removeKeys(row, ISO_ONLY);

  // Dropping a JIS key never needs a stand-in: ANSI keeps its own backslash key
  // wherever the language layout put it, and ISO gains IntlBackslash below.
  return removeKeys(row, [...ISO_ONLY, ...JIS_ONLY]);
}

// Shift rows carry parenthesised key names so each row can have its own label
// (see "(Backslash)": "|"). Keys added to a shift row have to follow that
// convention or they render the unshifted character.
export type LayoutVariant = "default" | "shift";

export function applyFormFactor(
  rows: string[],
  formFactor: KeyboardFormFactor,
  variant: LayoutVariant = "default",
): string[] {
  const wrap = (names: string[]) =>
    variant === "shift" ? names.map(name => `(${name})`) : names;

  const out = rows.map(row => applyToRow(row, formFactor));

  // ISO is ANSI plus the extra key left of Z; the right shift shrinks to make
  // room for it, which a uniform-width virtual keyboard cannot express, so the
  // key count is the only thing that changes here.
  if (formFactor === "ISO") {
    const zRow = out.findIndex(row => has(row, "KeyZ"));
    if (zRow !== -1 && !has(out[zRow], "IntlBackslash")) {
      out[zRow] = insertAfter(out[zRow], "ShiftLeft", wrap(ISO_ONLY));
    }
    return out;
  }

  if (formFactor === "JIS") {
    // A layout authored on a JIS keyboard (ja_JP, ja_JP_mac) already carries the
    // extra keys, and re-adding them would duplicate a key the author placed
    // deliberately, so each group is only added when the layout has none.

    // Yen takes the position where ANSI has its rightmost backslash.
    if (!rowsHave(out, JIS_YEN_KEYS)) {
      const backslashRow = out.findIndex(row => has(row, "Backslash"));
      if (backslashRow !== -1) {
        out[backslashRow] = insertAfter(out[backslashRow], "Backslash", wrap(JIS_YEN_KEYS));
      }
    }

    // "ro" sits right of the right shift.
    if (!rowsHave(out, JIS_RO_KEYS)) {
      const shiftRow = out.findIndex(row => has(row, "ShiftRight"));
      if (shiftRow !== -1) {
        out[shiftRow] = insertAfter(out[shiftRow], "ShiftRight", wrap(JIS_RO_KEYS));
      }
    }

    // The IME keys sit left of the space bar on a 106/109 keyboard. They are not
    // shifted characters, so they keep their plain names in both rows.
    if (!rowsHave(out, JIS_IME_KEYS)) {
      const spaceRow = out.findIndex(row => has(row, "Space"));
      if (spaceRow !== -1) {
        out[spaceRow] = insertBefore(out[spaceRow], "Space", JIS_IME_INSERT);
      }
    }
  }

  return out;
}
