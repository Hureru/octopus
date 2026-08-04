import assert from "node:assert/strict";
import test from "node:test";
import { buildBatchFailureAccountIds } from "./batch-failure.ts";

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

function group(siteId, reason, accountIds, failed = 1) {
  return {
    site_id: siteId,
    platform: "new-api",
    reason,
    account_ids: accountIds,
    count: failed,
    failed,
    skipped: 0,
    warnings: 0,
  };
}

test("builds unique account filters from visible batch failure categories", () => {
  const result = buildBatchFailureAccountIds([
    summary([
      group(1, "unauthorized", [11]),
      group(1, "login_failed", [12]),
      group(9, "missing_group_key", [91]),
      group(2, "cloudflare_protection", [21]),
      group(3, "timeout", [31]),
    ]),
    summary([
      group(4, "database_error", [41]),
      group(5, "future_reason", [51]),
      group(6, "scheduled_later", [61]),
      group(7, "context_canceled", [71]),
      group(8, "unauthorized", [81], 0),
    ]),
  ]);

  assert.deepEqual([...result.credential], [11, 12, 91]);
  assert.deepEqual([...result.risk], [21]);
  assert.deepEqual([...result.transient], [31]);
  assert.deepEqual([...result.other], [41, 51]);
});

test("unsupported platform/checkin failures are categorized as other, not transient", () => {
  const result = buildBatchFailureAccountIds([
    summary([
      group(1, "unsupported_platform", [11]),
      group(2, "unsupported_checkin", [21]),
      group(3, "upstream_http_error", [31]),
    ]),
  ]);

  assert.deepEqual([...result.transient], [31]);
  assert.deepEqual([...result.other], [11, 21]);
});

test("does not treat incomplete legacy samples as the full account set", () => {
  const legacy = summary([group(1, "unauthorized", undefined)]);
  legacy.samples = [
    {
      site_id: 1,
      platform: "new-api",
      account_id: 11,
      reason: "unauthorized",
      message: "sample only",
    },
  ];

  const result = buildBatchFailureAccountIds([legacy]);
  assert.deepEqual([...result.credential], []);
});
