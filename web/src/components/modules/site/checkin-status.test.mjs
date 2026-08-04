import assert from "node:assert/strict";
import test from "node:test";
import { deriveCheckinStatus } from "./checkin-status.ts";

const now = new Date("2026-08-04T12:00:00+08:00");

test("partial sync does not enter checkin filters when checkin is unsupported", () => {
  const status = deriveCheckinStatus(
    { enabled: true, platform: "api" },
    {
      enabled: true,
      auto_checkin: true,
      last_checkin_at: null,
      last_checkin_status: "idle",
      last_sync_status: "partial",
    },
    now,
  );

  assert.equal(status, null);
});

test("partial sync does not enter checkin filters when automatic checkin is off", () => {
  const status = deriveCheckinStatus(
    { enabled: true, platform: "new-api" },
    {
      enabled: true,
      auto_checkin: false,
      last_checkin_at: null,
      last_checkin_status: "idle",
      last_sync_status: "partial",
    },
    now,
  );

  assert.equal(status, null);
});
