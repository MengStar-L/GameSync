import test from "node:test";
import assert from "node:assert/strict";

import { bindNaturalCoverAspect, naturalCoverAspect } from "./game-card-cover-aspect.js";

test("formats valid natural cover dimensions for CSS", () => {
  assert.equal(naturalCoverAspect(600, 900), "600 / 900");
  assert.equal(naturalCoverAspect(564, 800), "564 / 800");
});

test("rejects missing or invalid natural cover dimensions", () => {
  assert.equal(naturalCoverAspect(0, 900), "");
  assert.equal(naturalCoverAspect(600, 0), "");
  assert.equal(naturalCoverAspect(Number.NaN, 900), "");
});

test("applies cached image dimensions immediately", () => {
  const writes = [];
  const image = { complete: true, naturalWidth: 600, naturalHeight: 900, addEventListener() {} };
  const cover = { querySelector: () => image };
  const coverBox = { style: { setProperty: (...args) => writes.push(args) } };

  bindNaturalCoverAspect(coverBox, cover);

  assert.deepEqual(writes, [["--lib-cover-aspect", "600 / 900"]]);
});

test("applies dimensions after an asynchronous image load", () => {
  const writes = [];
  let onLoad;
  const image = {
    complete: false,
    naturalWidth: 600,
    naturalHeight: 900,
    addEventListener(type, callback) {
      if (type === "load") onLoad = callback;
    },
  };
  const cover = { querySelector: () => image };
  const coverBox = { style: { setProperty: (...args) => writes.push(args) } };

  bindNaturalCoverAspect(coverBox, cover);
  assert.deepEqual(writes, []);
  onLoad();
  assert.deepEqual(writes, [["--lib-cover-aspect", "600 / 900"]]);
});
