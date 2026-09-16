export const keyModifiers = ["ctrl", "alt", "shift", "super", "hyper"] as const;
export type KeyModifier = typeof keyModifiers[number];
export type KeySelection = { base: string } & Partial<Record<KeyModifier, boolean>>;
export type KeyResult = "accepted" | "not_sent" | "unknown";

export const commonKeys = [
  ["esc", "Esc"], ["tab", "Tab"], ["enter", "Enter"], ["backspace", "Backspace"],
  ["up", "↑"], ["down", "↓"], ["left", "←"], ["right", "→"],
] as const;
export const modifierLabels: Record<KeyModifier, string> = {
  ctrl: "Ctrl", alt: "Alt / Option", shift: "Shift", super: "Super / Command", hyper: "Hyper",
};
const aliases: Record<string, string> = {
  escape: "esc", return: "enter", bs: "backspace", space: " ", plus: "+", minus: "-", comma: ",",
  period: ".", slash: "/", backslash: "\\", quote: "'", double_quote: '"', "double-quote": '"',
  semicolon: ";", colon: ":", percent: "%", ampersand: "&", backtick: "`",
};

export function validCharacter(base: string): boolean {
  return [...base].length === 1 && !/[\p{C}\p{Z}]/u.test(base) || base === " ";
}

export function validateKey(value: unknown): KeySelection | undefined {
  if (!value || typeof value !== "object" || Array.isArray(value)) return undefined;
  const record = value as Record<string, unknown>;
  if (Object.keys(record).some(key => key !== "base" && !keyModifiers.includes(key as KeyModifier)) || typeof record.base !== "string") return undefined;
  let base = record.base;
  if (!validCharacter(base)) base = aliases[base.toLowerCase()] ?? base.toLowerCase();
  if (!validCharacter(base) && !commonKeys.some(([key]) => key === base) && !/^f([1-9]|1[0-2])$/.test(base)) return undefined;
  const key: KeySelection = { base };
  for (const modifier of keyModifiers) {
    if (modifier in record && typeof record[modifier] !== "boolean") return undefined;
    if (record[modifier]) key[modifier] = true;
  }
  return key;
}

export function keyLabel(key: KeySelection): string {
  const base = commonKeys.find(([base]) => base === key.base)?.[1] ??
    (key.base === " " ? "Space" : /^f\d+$/.test(key.base) ? key.base.toUpperCase() : key.base);
  return [...keyModifiers.filter(modifier => key[modifier]).map(modifier => modifier === "alt" ? "Alt" : modifier === "super" ? "Super" : modifierLabels[modifier]), base].join(" + ");
}

const storageKey = "shepherdr.terminal.shortcuts";
const defaults = (): KeySelection[] => [{ base: "esc" }, { base: "c", ctrl: true }, { base: "up" }, { base: "down" }];
let visitShortcuts: KeySelection[] | undefined;
let shortcutWriteFailed = false;

export function readShortcuts(): KeySelection[] {
  try {
    // A readable store may still contain older preferences after a failed write.
    const raw = shortcutWriteFailed ? undefined : window.localStorage.getItem(storageKey);
    if (raw === null) visitShortcuts = defaults();
    else if (raw !== undefined) {
      const parsed: unknown = JSON.parse(raw);
      if (Array.isArray(parsed)) {
        visitShortcuts = parsed.map(validateKey).filter((key): key is KeySelection => key !== undefined);
      }
    }
  } catch { /* Keep this visit's preferences when storage is unavailable. */ }
  return (visitShortcuts ?? defaults()).map(key => ({ ...key }));
}

export function saveShortcuts(keys: KeySelection[]): void {
  visitShortcuts = keys.map(validateKey).filter((key): key is KeySelection => key !== undefined);
  try {
    window.localStorage.setItem(storageKey, JSON.stringify(visitShortcuts));
    shortcutWriteFailed = false;
  } catch { shortcutWriteFailed = true; }
}

export function defaultShortcuts(): KeySelection[] { return defaults(); }
