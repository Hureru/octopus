const assert = require("node:assert/strict");
const test = require("node:test");

require("./bridge-common.js");

const {
  hasNewPendingTask,
  isClaimActive,
  isCloudflareChallengePage,
  isPairingTerminalBridgeError,
  shouldAutoHandleTask,
  taskTabCreateProperties,
} = globalThis.OctopusBridgeCommon;
const now = Date.parse("2026-08-02T01:00:00+08:00");
const claim = {
  claim_expires_at: "2026-08-02T01:10:00+08:00",
  task: {id: 16},
};

test("recognizes a matching live claim", () => {
  assert.equal(isClaimActive(
    claim,
    2,
    {id: 16, status: "claimed", pairing_id: 2},
    now,
  ), true);
});

test("rejects an expired local claim", () => {
  assert.equal(isClaimActive(
    {...claim, claim_expires_at: "2026-08-02T00:59:59+08:00"},
    2,
    {id: 16, status: "claimed", pairing_id: 2},
    now,
  ), false);
});

test("rejects a claim released by the server", () => {
  assert.equal(isClaimActive(
    claim,
    2,
    {id: 16, status: "pending"},
    now,
  ), false);
});

test("rejects a task claimed by another pairing", () => {
  assert.equal(isClaimActive(
    claim,
    2,
    {id: 16, status: "claimed", pairing_id: 3},
    now,
  ), false);
});

test("automatically handles a new pending task", () => {
  assert.equal(shouldAutoHandleTask(
    {phase: "succeeded", claim: null},
    2,
    {id: 23, status: "pending"},
    now,
  ), true);
});

test("automatically resumes a matching live claim", () => {
  assert.equal(shouldAutoHandleTask(
    {phase: "waiting", claim},
    2,
    {id: 16, status: "claimed", pairing_id: 2},
    now,
  ), true);
});

test("does not start a second automation while retry is running", () => {
  assert.equal(shouldAutoHandleTask(
    {phase: "running", claim: null},
    2,
    {id: 16, status: "completed", pairing_id: 2},
    now,
  ), false);
});

test("respects a manually paused task", () => {
  assert.equal(shouldAutoHandleTask(
    {phase: "idle", claim: null, pausedTaskId: 23},
    2,
    {id: 23, status: "pending"},
    now,
  ), false);
});

test("detects a pending task that replaced a running retry", () => {
  assert.equal(hasNewPendingTask(
    {phase: "running", task: {id: 16}},
    {id: 23, status: "pending"},
  ), true);
  assert.equal(hasNewPendingTask(
    {phase: "running", task: {id: 16}},
    {id: 16, status: "completed"},
  ), false);
});

test("detects Cloudflare challenge pages", () => {
  assert.equal(isCloudflareChallengePage({
    title: "Just a moment...",
    text: "",
    challengeMarker: false,
  }), true);
  assert.equal(isCloudflareChallengePage({
    title: "42 API",
    text: "Welcome back",
    challengeMarker: false,
  }), false);
  assert.equal(isCloudflareChallengePage({
    title: "42 API",
    text: "Verify you are human to continue signing in",
    challengeMarker: false,
  }), false);
});

test("only invalidates the pairing for pairing-level bridge failures", () => {
  assert.equal(
    isPairingTerminalBridgeError("verification bridge pairing expired or revoked"),
    true,
  );
  assert.equal(
    isPairingTerminalBridgeError("verification task claim expired"),
    false,
  );
  assert.equal(
    isPairingTerminalBridgeError("verification task was already consumed"),
    false,
  );
});

test("opens automatic verification work in an inactive normal tab", () => {
  assert.deepEqual(
    taskTabCreateProperties("https://api.example.com", false, 42),
    {
      url: "https://api.example.com",
      active: false,
      windowId: 42,
    },
  );
});

test("manual verification can foreground a tab without a target window", () => {
  assert.deepEqual(
    taskTabCreateProperties("https://api.example.com", true, null),
    {
      url: "https://api.example.com",
      active: true,
    },
  );
});
