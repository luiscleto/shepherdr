import { copyFile, mkdir } from "node:fs/promises";

import { build } from "esbuild";

await mkdir("dist", { recursive: true });
await build({ bundle: true, entryPoints: ["src/main.ts"], minify: true, outfile: "dist/app.js" });
await build({ bundle: true, entryPoints: ["src/terminal-lab.ts"], minify: true, outfile: "dist/terminal-lab.js" });
await build({ bundle: true, entryPoints: ["src/terminal-lab.css"], minify: true, outfile: "dist/terminal-lab.css" });

for (const name of [
  "index.html",
  "terminal-lab.html",
  "manifest.webmanifest",
  "service-worker.js",
  "icon.svg",
  "icon-192.png",
  "icon-512.png",
  "apple-touch-icon.png",
]) {
  await copyFile(`src/${name}`, `dist/${name}`);
}
await copyFile("node_modules/@wterm/ghostty/wasm/ghostty-vt.wasm", "dist/ghostty-vt.wasm");
