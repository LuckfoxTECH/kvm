export interface KeyStroke {
  modifier: number;
  keys: number[];
}

export interface KeyInfo {
  key: string | number;
  shift?: boolean;
  altRight?: boolean;
}

export interface KeyCombo extends KeyInfo {
  deadKey?: boolean;
  accentKey?: KeyInfo;
}

export interface KeyboardLayout {
  isoCode: string;
  name: string;
  chars: Record<string, KeyCombo>;
  modifierDisplayMap: Record<string, string>;
  keyDisplayMap: Record<string, string>;
  virtualKeyboard: {
    main: { default: string[]; shift: string[] };
    control?: { default: string[]; shift?: string[] };
    arrows?: { default: string[] };
    numpad?: {
      numlocked: string[];
      default: string[];
    };
  };
}

// Import all layouts
import { cs_CZ } from "./keyboardLayouts/cs_CZ";
import { da_DK } from "./keyboardLayouts/da_DK";
import { de_CH } from "./keyboardLayouts/de_CH";
import { de_DE } from "./keyboardLayouts/de_DE";
import { en_US } from "./keyboardLayouts/en_US";
import { en_UK } from "./keyboardLayouts/en_UK";
import { es_ES } from "./keyboardLayouts/es_ES";
import { fr_BE } from "./keyboardLayouts/fr_BE";
import { fr_CH } from "./keyboardLayouts/fr_CH";
import { fr_FR } from "./keyboardLayouts/fr_FR";
import { hu_HU } from "./keyboardLayouts/hu_HU";
import { it_IT } from "./keyboardLayouts/it_IT";
import { ja_JP } from "./keyboardLayouts/ja_JP";
import { ja_JP_mac } from "./keyboardLayouts/ja_JP_mac";
import { nb_NO } from "./keyboardLayouts/nb_NO";
import { pl_PL } from "./keyboardLayouts/pl_PL";
import { pt_PT } from "./keyboardLayouts/pt_PT";
import { sv_SE } from "./keyboardLayouts/sv_SE";
import { sl_SI } from "./keyboardLayouts/sl_SI";
import { ru_RU } from "./keyboardLayouts/ru_RU";

export const keyboards: KeyboardLayout[] = [
  cs_CZ,
  da_DK,
  de_CH,
  de_DE,
  en_UK,
  en_US,
  es_ES,
  fr_BE,
  fr_CH,
  fr_FR,
  hu_HU,
  it_IT,
  ja_JP,
  ja_JP_mac,
  nb_NO,
  pl_PL,
  pt_PT,
  sv_SE,
  sl_SI,
  ru_RU,
];

// Backward-compatible maps
export const layouts: Record<string, string> = {};
export const chars: Record<string, Record<string, KeyCombo>> = {};

keyboards.forEach(kb => {
  const oldCode = kb.isoCode.replace("-", "_");
  layouts[oldCode] = kb.name;
  chars[oldCode] = kb.chars;
});

/**
 * Resolves a layout value in any of the spellings that flow around this app to
 * the key used by `layouts` / `chars`.
 *
 * Three spellings coexist: the registry declares hyphenated isoCodes
 * ("ja-JP-mac"), the settings dropdown and the value persisted on the device
 * use an underscored form ("ja_JP-mac"), and the store default is hyphenated
 * ("en-US"). Comparing them literally made every non-English selection fall
 * back to en_US, which reads as "changing the layout does nothing".
 */
export function resolveKeyboardLayoutKey(value?: string): string | undefined {
  if (!value) return undefined;
  const canonical = (s: string) => s.toLowerCase().replace(/_/g, "-");
  const wanted = canonical(value);
  return Object.keys(chars).find(key => canonical(key) === wanted);
}
