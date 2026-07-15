import assert from "node:assert/strict";
import test from "node:test";
import { selectSessionURL } from "./route.js";

test("selects and encodes session while preserving split", () => {
  assert.equal(
    selectSessionURL("http://pudding.local/?session=old&split=sess_b&draft=1", "sess a/b"),
    "/?session=sess+a%2Fb&split=sess_b",
  );
});

test("drops invalid split values", () => {
  assert.equal(selectSessionURL("/?split=../bad&theme=dark", "sess_2"), "/?session=sess_2");
});
