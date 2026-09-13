import { createHash } from "node:crypto";
import { copyFile, mkdir, readFile, readdir, rm, writeFile } from "node:fs/promises";

import { build } from "esbuild";

await rm("dist", { force: true, recursive: true });
await mkdir("dist", { recursive: true });
await build({ bundle: true, entryPoints: ["src/main.ts"], loader: { ".woff2": "file" }, minify: true, outfile: "dist/app.js" });
await build({ bundle: true, entryPoints: ["src/terminal-lab.ts"], minify: true, outfile: "dist/terminal-lab.js" });
await build({ bundle: true, entryPoints: ["src/terminal-lab.css"], loader: { ".woff2": "file" }, minify: true, outfile: "dist/terminal-lab.css" });

for (const name of [
  "index.html",
  "IBM-Plex-Mono-LICENSE.txt",
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

const files = (await readdir("dist", { withFileTypes: true }))
  .filter((entry) => entry.isFile())
  .map((entry) => entry.name)
  .sort();
const manifest = [];
for (const name of files) {
  const digest = createHash("sha256").update(await readFile(`dist/${name}`)).digest("hex");
  manifest.push(`${digest}  ${name}`);
}
await writeFile("dist/manifest.sha256", `${manifest.join("\n")}\n`);
