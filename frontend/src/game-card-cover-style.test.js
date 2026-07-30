import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const css = readFileSync(
  fileURLToPath(new URL("../css/views/library.css", import.meta.url)),
  "utf8",
);

test("overlay cards keep complete 7:10 covers", () => {
  assert.match(css, /\.lib-mode-overlay-hover \.lib-card-cover,[\s\S]*?aspect-ratio: 7 \/ 10;/);
  assert.match(css, /\.lib-mode-overlay-hover \.lib-card-cover \.cover img,[\s\S]*?object-fit: contain;/);
});

test("overlay metadata has no panel or cover mask", () => {
  assert.match(css, /\.lib-mode-overlay-hover \.lib-card-body,[\s\S]*?background: transparent;/);
  assert.match(css, /\.lib-mode-overlay-hover \.lib-card-body::before,[\s\S]*?content: none;/);
  assert.match(css, /\.lib-mode-overlay-hover \.lib-card:hover \.lib-card-cover::after,[\s\S]*?opacity: 0;/);
  assert.match(css, /\.lib-mode-overlay-hover \.lib-card:hover \.lib-card-cover \.cover img,[\s\S]*?transform: none;/);
});
