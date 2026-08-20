interface ANSIStyle {
  background?: string;
  bold: boolean;
  dim: boolean;
  foreground?: string;
  hidden: boolean;
  inverse: boolean;
  italic: boolean;
  strike: boolean;
  underline: boolean;
}

const basePalette = [
  "#1a211f", "#df8271", "#94b48b", "#d9b56c", "#83a6c9", "#bd92ba", "#78b7ad", "#d9ded8",
  "#66726d", "#f29a87", "#aed0a4", "#efcb82", "#9bbfe2", "#d7acd1", "#92d1c6", "#f4f5ef",
] as const;

function newStyle(): ANSIStyle {
  return { bold: false, dim: false, hidden: false, inverse: false, italic: false, strike: false, underline: false };
}

function paletteColor(index: number): string | undefined {
  if (index < 0 || index > 255) return undefined;
  if (index < 16) return basePalette[index];
  if (index >= 232) {
    const value = 8 + (index - 232) * 10;
    return `rgb(${value} ${value} ${value})`;
  }
  const cube = index - 16;
  const levels = [0, 95, 135, 175, 215, 255];
  return `rgb(${levels[Math.floor(cube / 36)]} ${levels[Math.floor(cube / 6) % 6]} ${levels[cube % 6]})`;
}

function extendedColor(parameters: number[], position: number): { color?: string; consumed: number } {
  if (parameters[position + 1] === 5) {
    return { color: paletteColor(parameters[position + 2]), consumed: 2 };
  }
  if (parameters[position + 1] === 2) {
    const values = parameters.slice(position + 2, position + 5);
    if (values.length === 3 && values.every((value) => value >= 0 && value <= 255)) {
      return { color: `rgb(${values[0]} ${values[1]} ${values[2]})`, consumed: 4 };
    }
  }
  return { consumed: 0 };
}

function applySGR(style: ANSIStyle, source: string): ANSIStyle {
  const parameters = (source === "" ? [0] : source.replaceAll(":", ";").split(";").map((value) => Number(value || 0)));
  let next = { ...style };
  for (let index = 0; index < parameters.length; index += 1) {
    const code = parameters[index];
    if (code === 0) next = newStyle();
    else if (code === 1) next.bold = true;
    else if (code === 2) next.dim = true;
    else if (code === 3) next.italic = true;
    else if (code === 4 || code === 21) next.underline = true;
    else if (code === 7) next.inverse = true;
    else if (code === 8) next.hidden = true;
    else if (code === 9) next.strike = true;
    else if (code === 22) { next.bold = false; next.dim = false; }
    else if (code === 23) next.italic = false;
    else if (code === 24) next.underline = false;
    else if (code === 27) next.inverse = false;
    else if (code === 28) next.hidden = false;
    else if (code === 29) next.strike = false;
    else if (code >= 30 && code <= 37) next.foreground = paletteColor(code - 30);
    else if (code >= 40 && code <= 47) next.background = paletteColor(code - 40);
    else if (code >= 90 && code <= 97) next.foreground = paletteColor(code - 90 + 8);
    else if (code >= 100 && code <= 107) next.background = paletteColor(code - 100 + 8);
    else if (code === 39) next.foreground = undefined;
    else if (code === 49) next.background = undefined;
    else if (code === 38 || code === 48) {
      const extended = extendedColor(parameters, index);
      if (code === 38) next.foreground = extended.color;
      else next.background = extended.color;
      index += extended.consumed;
    }
  }
  return next;
}

function createSpan(text: string, style: ANSIStyle): HTMLSpanElement {
  const span = document.createElement("span");
  const foreground = style.inverse ? style.background ?? "#101715" : style.foreground;
  const background = style.inverse ? style.foreground ?? "#e5e7df" : style.background;
  if (foreground) span.style.color = foreground;
  if (background) span.style.backgroundColor = background;
  if (style.bold) span.style.fontWeight = "700";
  if (style.dim) span.style.opacity = "0.65";
  if (style.italic) span.style.fontStyle = "italic";
  const decorations = [style.underline ? "underline" : "", style.strike ? "line-through" : ""].filter(Boolean);
  if (decorations.length > 0) span.style.textDecoration = decorations.join(" ");
  if (style.hidden) span.style.visibility = "hidden";
  span.textContent = text;
  return span;
}

function newLine(): HTMLDivElement {
  const line = document.createElement("div");
  line.className = "reader-line";
  return line;
}

function appendLine(fragment: DocumentFragment, line: HTMLDivElement): void {
  const spans = Array.from(line.children).filter((node): node is HTMLSpanElement => node instanceof HTMLSpanElement);
  const background = spans[0]?.style.backgroundColor;
  const last = spans.at(-1);
  if (background && last?.textContent?.endsWith(" ") && spans.every((span) => span.style.backgroundColor === background)) {
    line.style.backgroundColor = background;
    last.textContent = last.textContent.replace(/ +$/, "");
    if (!last.textContent) last.remove();
  }
  fragment.append(line);
}

function appendStyled(fragment: DocumentFragment, line: HTMLDivElement, text: string, style: ANSIStyle): HTMLDivElement {
  const parts = text.split("\n");
  for (let index = 0; index < parts.length; index += 1) {
    if (parts[index]) line.append(createSpan(parts[index], style));
    if (index < parts.length - 1) {
      appendLine(fragment, line);
      line = newLine();
    }
  }
  return line;
}

export function renderANSI(host: HTMLElement, input: string): void {
  const fragment = document.createDocumentFragment();
  let line = newLine();
  let style = newStyle();
  let plain = "";
  const flush = () => {
    line = appendStyled(fragment, line, plain, style);
    plain = "";
  };

  for (let index = 0; index < input.length;) {
    const code = input.charCodeAt(index);
    if (code === 0x1b && input[index + 1] === "[") {
      let end = index + 2;
      while (end < input.length) {
        const final = input.charCodeAt(end);
        if (final >= 0x40 && final <= 0x7e) break;
        end += 1;
      }
      if (end >= input.length) break;
      flush();
      if (input[end] === "m") style = applySGR(style, input.slice(index + 2, end));
      index = end + 1;
      continue;
    }
    if (code === 0x1b) {
      flush();
      index += Math.min(2, input.length - index);
      continue;
    }
    if (code === 0x0d) {
      index += 1;
      continue;
    }
    if (code < 0x20 && code !== 0x0a && code !== 0x09) {
      index += 1;
      continue;
    }
    plain += input[index];
    index += 1;
  }
  flush();
  appendLine(fragment, line);
  host.replaceChildren(fragment);
}
