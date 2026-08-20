export function terminalReaderForDevice(media: Pick<Window, "matchMedia"> = window): boolean {
  return media.matchMedia("(pointer: coarse)").matches;
}
