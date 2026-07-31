const STORAGE_KEY = "octopusVerificationBridge";

const elements = {
  baseURL: document.querySelector("#base-url"),
  pairingToken: document.querySelector("#pairing-token"),
  showToken: document.querySelector("#show-token"),
  save: document.querySelector("#save"),
  claim: document.querySelector("#claim"),
  taskSection: document.querySelector("#task-section"),
  taskHost: document.querySelector("#task-host"),
  taskOperation: document.querySelector("#task-operation"),
  taskExpires: document.querySelector("#task-expires"),
  open: document.querySelector("#open"),
  submit: document.querySelector("#submit"),
  release: document.querySelector("#release"),
  status: document.querySelector("#status"),
};

let state = {
  baseURL: "",
  pairingToken: "",
  claim: null,
  verificationWindowID: null,
};
let claimExpiryTimer = null;

document.addEventListener("DOMContentLoaded", initialize);
elements.showToken.addEventListener("change", () => {
  elements.pairingToken.type = elements.showToken.checked ? "text" : "password";
});
elements.save.addEventListener("click", saveConnection);
elements.claim.addEventListener("click", claimTask);
elements.open.addEventListener("click", openVerification);
elements.submit.addEventListener("click", submitSession);
elements.release.addEventListener("click", releaseTask);

async function initialize() {
  const stored = await chrome.storage.local.get(STORAGE_KEY);
  state = {...state, ...(stored[STORAGE_KEY] || {})};
  elements.baseURL.value = state.baseURL || "";
  elements.pairingToken.value = state.pairingToken || "";
  if (activeClaimExpired()) {
    await clearActiveTask(true);
    setStatus("已保存的验证任务领取凭据已过期。", "error");
    return;
  }
  renderTask();
  scheduleClaimExpiry();
}

async function saveConnection() {
  await runBusy(elements.save, async () => {
    const baseURL = normalizeBaseURL(elements.baseURL.value);
    const pairingToken = elements.pairingToken.value.trim();
    if (!pairingToken) {
      throw new Error("请输入配对令牌。");
    }
    if (
      state.claim &&
      (state.baseURL !== baseURL || state.pairingToken !== pairingToken)
    ) {
      throw new Error("更改连接前请先释放当前验证任务。");
    }
    await ensureOriginPermission(baseURL);
    state.baseURL = baseURL;
    state.pairingToken = pairingToken;
    await persistState();
    setStatus("连接设置已保存。", "success");
  });
}

async function claimTask() {
  await runBusy(elements.claim, async () => {
    if (state.claim) {
      const previousClaim = state.claim;
      const previousBaseURL = state.baseURL;
      const previousPairingToken = state.pairingToken;
      try {
        await callBridgeAt(
          previousBaseURL,
          previousPairingToken,
          "/release",
          {
            pairing_token: previousPairingToken,
            task_token: previousClaim.task_token,
          },
        );
      } catch (error) {
        if (!error?.terminal) {
          throw error;
        }
      }
      await clearActiveTask(true);
    }
    await syncConnectionFromInputs();
    const data = await callBridge("/claim", {
      pairing_token: state.pairingToken,
    });
    state.claim = data;
    state.verificationWindowID = null;
    await persistState();
    renderTask();
    scheduleClaimExpiry();
    setStatus("已领取验证任务。", "success");
  });
}

async function openVerification() {
  await runBusy(elements.open, async () => {
    const task = await requireActiveTask();
    await ensureOriginPermission(task.target_url);
    const created = await chrome.windows.create({
      url: task.target_url,
      type: "popup",
      width: 1120,
      height: 820,
      focused: true,
    });
    state.verificationWindowID = created.id ?? null;
    await persistState();
    setStatus("验证窗口已打开。", "success");
  });
}

async function submitSession() {
  await runBusy(elements.submit, async () => {
    const task = await requireActiveTask();
    await ensureOriginPermission(task.target_url);
    const cookies = (await chrome.cookies.getAll({url: task.target_url})).filter(
      (cookie) => isVerificationCookie(cookie.name),
    );
    if (!cookies.length) {
      throw new Error("验证目标没有可提交的 Cookie。");
    }
    await callBridge("/complete", {
      pairing_token: state.pairingToken,
      task_token: state.claim.task_token,
      user_agent: navigator.userAgent,
      cookies: cookies.map((cookie) => ({
        name: cookie.name,
        value: cookie.value,
        domain: cookie.domain,
        path: cookie.path,
        secure: cookie.secure,
        http_only: cookie.httpOnly,
      })),
    });
    await clearActiveTask(true);
    setStatus("验证会话已提交。", "success");
  });
}

async function releaseTask() {
  await runBusy(elements.release, async () => {
    await requireActiveTask();
    await callBridge("/release", {
      pairing_token: state.pairingToken,
      task_token: state.claim.task_token,
    });
    await clearActiveTask(true);
    setStatus("验证任务已释放。", "success");
  });
}

function isVerificationCookie(name) {
  const value = String(name || "").trim();
  return value === "cf_clearance"
    || value === "__cf_bm"
    || value.startsWith("cf_chl_");
}

async function syncConnectionFromInputs() {
  const baseURL = normalizeBaseURL(elements.baseURL.value);
  const pairingToken = elements.pairingToken.value.trim();
  if (!pairingToken) {
    throw new Error("请输入配对令牌。");
  }
  await ensureOriginPermission(baseURL);
  state.baseURL = baseURL;
  state.pairingToken = pairingToken;
  await persistState();
}

async function callBridge(path, body) {
  if (!state.baseURL || !state.pairingToken) {
    throw new Error("请先保存 Octopus 连接设置。");
  }
  return callBridgeAt(state.baseURL, state.pairingToken, path, body);
}

async function callBridgeAt(baseURL, pairingToken, path, body) {
  const response = await fetch(
    `${baseURL}/api/v1/site/recovery/verification/bridge${path}`,
    {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify(body),
    },
  );
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
    error.terminal = /expired|revoked|already consumed|not claimed|not found|superseded/i.test(
      error.message,
    );
    throw error;
  }
  return payload.data;
}

async function requireActiveTask() {
  const task = state.claim?.task;
  if (!task || !state.claim?.task_token) {
    throw new Error("当前没有验证任务。");
  }
  if (activeClaimExpired()) {
    await clearActiveTask(true);
    throw new Error("验证任务领取凭据已过期，请重新领取任务。");
  }
  return task;
}

async function clearActiveTask(removeTargetPermission) {
  const targetURL = state.claim?.task?.target_url || "";
  if (state.verificationWindowID !== null) {
    try {
      await chrome.windows.remove(state.verificationWindowID);
    } catch {
      // The administrator may have already closed the temporary window.
    }
  }
  clearClaimExpiryTimer();
  if (removeTargetPermission && targetURL) {
    const targetOrigin = originPattern(targetURL);
    const baseOrigin = state.baseURL ? originPattern(state.baseURL) : "";
    if (targetOrigin !== baseOrigin) {
      try {
        await chrome.permissions.remove({origins: [targetOrigin]});
      } catch {
        // State cleanup must not be blocked by a stale or already-removed
        // permission. The browser will reconcile the permission on its own.
      }
    }
  }
  state.claim = null;
  state.verificationWindowID = null;
  await persistState();
  renderTask();
}

async function ensureOriginPermission(value) {
  const origin = originPattern(value);
  if (await chrome.permissions.contains({origins: [origin]})) {
    return;
  }
  const granted = await chrome.permissions.request({origins: [origin]});
  if (!granted) {
    throw new Error("未授予站点访问权限。");
  }
}

function normalizeBaseURL(value) {
  const parsed = new URL(value.trim());
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
    throw new Error("仅支持 HTTP 和 HTTPS 目标。");
  }
  return `${parsed.protocol}//${parsed.host}/*`;
}

async function persistState() {
  await chrome.storage.local.set({[STORAGE_KEY]: state});
}

function renderTask() {
  const task = state.claim?.task;
  elements.taskSection.hidden = !task;
  if (!task) {
    return;
  }
  elements.taskHost.textContent = task.target_host || new URL(task.target_url).hostname;
  elements.taskOperation.textContent = task.operation === "manual"
    ? "手动验证"
    : (task.operation || "手动验证");
  const expiresAt = state.claim?.claim_expires_at || task.expires_at;
  elements.taskExpires.textContent = new Date(expiresAt).toLocaleString();
}

function activeClaimExpired() {
  const expiresAt = state.claim?.claim_expires_at || state.claim?.task?.expires_at;
  if (!expiresAt) {
    return false;
  }
  const deadline = new Date(expiresAt).getTime();
  return !Number.isFinite(deadline) || deadline <= Date.now();
}

function clearClaimExpiryTimer() {
  if (claimExpiryTimer !== null) {
    window.clearTimeout(claimExpiryTimer);
    claimExpiryTimer = null;
  }
}

function scheduleClaimExpiry() {
  clearClaimExpiryTimer();
  const expiresAt = state.claim?.claim_expires_at || state.claim?.task?.expires_at;
  if (!expiresAt) {
    return;
  }
  const delay = new Date(expiresAt).getTime() - Date.now();
  if (!Number.isFinite(delay) || delay <= 0) {
    void clearActiveTask(true);
    return;
  }
  claimExpiryTimer = window.setTimeout(async () => {
    claimExpiryTimer = null;
    await clearActiveTask(true);
    setStatus("验证任务领取凭据已过期，请重新领取任务。", "error");
  }, Math.min(delay, 2_147_000_000));
}

async function runBusy(button, action) {
  button.disabled = true;
  setStatus("");
  try {
    await action();
  } catch (error) {
    if (error?.terminal && state.claim) {
      await clearActiveTask(true);
    }
    setStatus(error instanceof Error ? error.message : String(error), "error");
  } finally {
    button.disabled = false;
  }
}

function setStatus(message, tone = "") {
  elements.status.textContent = message;
  elements.status.className = `status${tone ? ` ${tone}` : ""}`;
}
