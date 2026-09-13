export const terminalKeySequences: Readonly<Record<string, string>> = {
  escape: "\x1b",
  "ctrl-c": "\x03",
  "ctrl-d": "\x04",
  "ctrl-z": "\x1a",
  tab: "\t",
  left: "\x1b[D",
  up: "\x1b[A",
  down: "\x1b[B",
  right: "\x1b[C",
  enter: "\r",
  backspace: "\x7f",
};

export function terminalPaste(text: string): string {
  const safePaste = text.replaceAll("\x1b", "").replace(/\r\n|\r|\n/g, "\r");
  return `\x1b[200~${safePaste}\x1b[201~`;
}

export function terminalSubmission(text: string): string[] {
  return [terminalPaste(text), "\r"];
}
