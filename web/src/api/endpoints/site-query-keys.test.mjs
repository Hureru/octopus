import assert from "node:assert/strict";
import test from "node:test";
import { SITE_INVALIDATION_QUERY_KEYS } from "./site-query-keys.ts";

test("site mutations invalidate batch failure summaries", () => {
  assert.ok(
    SITE_INVALIDATION_QUERY_KEYS.some(
      (key) => key.length === 2 && key[0] === "sites" && key[1] === "batch-summary",
    ),
  );
});
