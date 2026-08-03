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

  function taskTabCreateProperties(targetURL, foreground, windowId) {
    const properties = {
      url: String(targetURL || ""),
      active: Boolean(foreground),
    };
    if (Number.isInteger(windowId)) properties.windowId = windowId;
    return properties;
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

  function shouldAutoHandleTask(record, pairingID, latestTask, now = Date.now()) {
    if (!record || record.phase === "invalid" || record.phase === "running") {
      return false;
    }
    if (latestTask?.id === record.pausedTaskId) {
      return false;
    }
    if (record.claim) {
      return isClaimActive(record.claim, pairingID, latestTask, now);
    }
    return latestTask?.status === "pending";
  }

  function hasNewPendingTask(record, latestTask) {
    return Boolean(
      record?.task?.id &&
      latestTask?.id &&
      latestTask.id !== record.task.id &&
      latestTask.status === "pending"
    );
  }

  function isCloudflareChallengePage(snapshot) {
    if (!snapshot) return true;
    if (snapshot.challengeMarker) return true;
    const title = String(snapshot.title || "").toLowerCase();
    const text = String(snapshot.text || "").toLowerCase();
    return (
      title.includes("just a moment") ||
      title.includes("attention required") ||
      title.includes("checking your browser") ||
      text.includes("checking if the site connection is secure") ||
      text.includes("performing security verification") ||
      text.includes("sorry, you have been blocked") ||
      text.includes("cloudflare ray id") ||
      text.includes("enable javascript and cookies to continue")
    );
  }

  function isPairingTerminalBridgeError(message) {
    const normalized = String(message || "").toLowerCase();
    return (
      normalized.includes("verification bridge is not paired") ||
      normalized.includes("verification bridge pairing expired or revoked") ||
      normalized.includes("verification bridge pairing was revoked") ||
      normalized.includes("paired site account not found") ||
      normalized.includes("paired site not found")
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
      error.terminal = isPairingTerminalBridgeError(error.message);
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
    hasNewPendingTask,
    isCloudflareChallengePage,
    isClaimActive,
    isPairingTerminalBridgeError,
    normalizeBaseURL,
    originPattern,
    sameOrigin,
    shouldAutoHandleTask,
    taskTabCreateProperties,
  });
})(globalThis);
