import assert from "node:assert/strict";
import test from "node:test";
import { buildBatchFailureSiteIds } from "./batch-failure.ts";

function summary(groups) {
  return {
    phase: "sync",
    trigger: "test",
    total: 0,
    attempted: 0,
    success: 0,
    partial: 0,
    failed: 0,
    skipped: 0,
    warnings: 0,
    canceled: false,
    duration_ms: 0,
    finished_at: "",
    failure_groups: groups,
    warning_groups: [],
  };
}

function group(siteId, reason, failed = 1) {
  return {
    site_id: siteId,
    platform: "new-api",
    reason,
    count: failed,
    failed,
    skipped: 0,
    warnings: 0,
  };
}

test("builds unique site filters from visible batch failure categories", () => {
  const result = buildBatchFailureSiteIds([
    summary([
      group(1, "unauthorized"),
      group(1, "login_failed"),
      group(9, "missing_group_key"),
      group(2, "cloudflare_protection"),
      group(3, "timeout"),
    ]),
    summary([
      group(4, "database_error"),
      group(5, "future_reason"),
      group(6, "scheduled_later"),
      group(7, "context_canceled"),
      group(8, "unauthorized", 0),
    ]),
  ]);

  assert.deepEqual([...result.credential], [1, 9]);
  assert.deepEqual([...result.risk], [2]);
  assert.deepEqual([...result.transient], [3]);
  assert.deepEqual([...result.other], [4, 5]);
});

test("unsupported platform/checkin failures are categorized as other, not transient", () => {
  const result = buildBatchFailureSiteIds([
    summary([
      group(1, "unsupported_platform"),
      group(2, "unsupported_checkin"),
      group(3, "upstream_http_error"),
    ]),
  ]);

  assert.deepEqual([...result.transient], [3]);
  assert.deepEqual([...result.other], [1, 2]);
});
