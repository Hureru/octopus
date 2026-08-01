(function initializeOctopusBridgeCommon(scope) {
  const API_PREFIX = "/api/v1/site/recovery/verification/bridge";

  function normalizeBaseURL(value) {
    const parsed = new URL(String(value || "").trim());
    if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
      throw new Error("Octopus 地址必须使用 HTTP 或 HTTPS。");
    }
    parsed.hash = "";
    parsed.search = "";
    return parsed.href.replace(/\/+$/, "");
  }

  function originPattern(value) {
    const parsed = new URL(value);
    if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
      throw new Error("仅支持 HTTP 和 HTTPS 地址。");
    }
    return `${parsed.protocol}//${parsed.host}/*`;
  }

  function sameOrigin(left, right) {
    try {
      return new URL(left).origin === new URL(right).origin;
    } catch {
      return false;
    }
  }

  function isClaimActive(claim, pairingID, latestTask, now = Date.now()) {
    const claimExpiresAt = new Date(claim?.claim_expires_at).getTime();
    return (
      Number.isFinite(claimExpiresAt) &&
      claimExpiresAt > now &&
      latestTask?.id === claim?.task?.id &&
      latestTask.status === "claimed" &&
      latestTask.pairing_id === pairingID
    );
  }

  async function callBridge(baseURL, path, body) {
    const response = await fetch(`${normalizeBaseURL(baseURL)}${API_PREFIX}${path}`, {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify(body),
      cache: "no-store",
    });
    let payload;
    try {
      payload = await response.json();
    } catch {
      throw new Error(`Octopus 返回 HTTP ${response.status}。`);
    }
    if (!response.ok || payload.code !== 200) {
      const error = new Error(
        payload.message || `Octopus 返回 HTTP ${response.status}。`,
      );
      error.terminal = /expired|revoked|not paired|not found|already consumed|superseded/i.test(
        error.message,
      );
      throw error;
    }
    return payload.data ?? null;
  }

  function formatTaskOperation(operation) {
    if (operation === "sync") return "同步";
    if (operation === "checkin") return "签到";
    return operation || "验证";
  }

  scope.OctopusBridgeCommon = Object.freeze({
    callBridge,
    formatTaskOperation,
    isClaimActive,
    normalizeBaseURL,
    originPattern,
    sameOrigin,
  });
})(globalThis);
