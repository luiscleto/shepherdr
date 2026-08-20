import assert from "node:assert/strict";
import test from "node:test";

import { terminalReaderForDevice } from "./terminal/device";
import { pauseReaderLiveRefresh, readerAtLatest } from "./terminal/reader-view";

test("Reader pauses moving output while someone is reading away from latest", () => {
  assert.equal(readerAtLatest(2_000, 400, 600), false);
  assert.equal(readerAtLatest(2_000, 1_400, 600), true);
  assert.equal(readerAtLatest(2_000, 1_375, 600), true, "a small bottom tolerance avoids status flapping");
  assert.equal(readerAtLatest(2_000, 1_360, 600), false);
  assert.equal(pauseReaderLiveRefresh(false, false), true, "live replacement pauses away from latest");
  assert.equal(pauseReaderLiveRefresh(true, true), true, "live replacement pauses during selection");
  assert.equal(pauseReaderLiveRefresh(true, false), false, "returning to latest permits one fresh snapshot");
});

test("the production Reader follows the primary coarse pointer even when another fine pointer exists", () => {
  const media = {
    matchMedia(query: string) {
      return { matches: query === "(pointer: coarse)" } as MediaQueryList;
    },
  } as Pick<Window, "matchMedia">;
  assert.equal(terminalReaderForDevice(media), true);

  const desktop = {
    matchMedia() {
      return { matches: false } as MediaQueryList;
    },
  } as Pick<Window, "matchMedia">;
  assert.equal(terminalReaderForDevice(desktop), false);
});
