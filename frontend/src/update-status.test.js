import test from "node:test";
import assert from "node:assert/strict";

import { isUpdateAvailable } from "./views/settings.js";

test("recognizes the backend update availability status", () => {
  assert.equal(isUpdateAvailable({ status: "available" }), true);
  assert.equal(isUpdateAvailable({ status: "update_available" }), false);
  assert.equal(isUpdateAvailable({ status: "latest" }), false);
});
