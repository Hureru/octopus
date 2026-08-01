const assert = require("node:assert/strict");
const test = require("node:test");

require("./bridge-common.js");

const {isClaimActive} = globalThis.OctopusBridgeCommon;
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
