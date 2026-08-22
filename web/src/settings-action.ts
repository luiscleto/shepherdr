export function settingsAction(
  document: Document,
  run: () => void,
  className: string,
): HTMLButtonElement {
  const button = document.createElement("button");
  button.type = "button";
  button.className = `settings-action ${className}`;
  button.setAttribute("aria-label", "Settings");

  const icon = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  icon.setAttribute("viewBox", "0 0 24 24");
  icon.setAttribute("aria-hidden", "true");
  icon.setAttribute("focusable", "false");

  const lines = document.createElementNS("http://www.w3.org/2000/svg", "path");
  lines.setAttribute("d", "M4 7h5m4 0h7M4 17h9m4 0h3");
  const upperControl = document.createElementNS("http://www.w3.org/2000/svg", "circle");
  upperControl.setAttribute("cx", "11");
  upperControl.setAttribute("cy", "7");
  upperControl.setAttribute("r", "2");
  const lowerControl = document.createElementNS("http://www.w3.org/2000/svg", "circle");
  lowerControl.setAttribute("cx", "15");
  lowerControl.setAttribute("cy", "17");
  lowerControl.setAttribute("r", "2");
  icon.append(lines, upperControl, lowerControl);
  button.append(icon);
  button.addEventListener("click", run);
  return button;
}
