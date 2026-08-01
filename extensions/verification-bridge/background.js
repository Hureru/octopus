importScripts("bridge-common.js");

const {
  callBridge,
  isClaimActive,
  normalizeBaseURL,
  sameOrigin,
} = OctopusBridgeCommon;
const STATE_KEY = "octopusVerificationBridgeV2";
const LEGACY_KEY = "octopusVerificationBridge";
const STATE_VERSION = 2;
const PUMP_ALARM = "octopus-verification-bridge-pump";
const activePumps = new Map();
let stateCache = null;
let statePromise = null;

chrome.runtime.onInstalled.addListener(() => {
  void initializeBackground();
});
chrome.runtime.onStartup.addListener(() => {
  void initializeBackground();
});
chrome.alarms.onAlarm.addListener((alarm) => {
  if (alarm.name === PUMP_ALARM) {
    void refreshAndResume();
  }
});
chrome.runtime.onMessage.addListener((message, _sender, sendResponse) => {
  void handleMessage(message)
    .then((data) => sendResponse({ok: true, data}))
    .catch((error) => sendResponse({
      ok: false,
      error: error instanceof Error ? error.message : String(error),
    }));
  return true;
});

void initializeBackground();

async function initializeBackground() {
  await chrome.alarms.create(PUMP_ALARM, {periodInMinutes: 1});
  await loadState();
  await refreshAndResume();
}

async function handleMessage(message) {
  switch (message?.type) {
    case "state.get":
      return publicState(await loadState());
    case "pairing.add":
      return publicState(await addPairing(message.baseURL, message.pairingToken));
    case "pairing.select":
      return publicState(await selectPairing(message.key));
    case "pairing.remove":
      return publicState(await removePairing(message.key));
    case "pairing.refresh":
      await refreshPairingByKey(message.key);
      return publicState(await loadState());
    case "task.claim":
      await claimTask(message.key);
      return publicState(await loadState());
    case "task.open":
      await openTaskWindow(message.key);
      return publicState(await loadState());
    case "task.start":
      await startBrowserTask(message.key);
      return publicState(await loadState());
    case "task.release":
      await releaseTask(message.key);
      return publicState(await loadState());
    default:
      throw new Error("不支持的扩展操作。");
  }
}

async function loadState() {
  if (stateCache) return stateCache;
  if (statePromise) return statePromise;
  statePromise = (async () => {
    const stored = await chrome.storage.local.get([STATE_KEY, LEGACY_KEY]);
    const current = stored[STATE_KEY];
    if (current?.version === STATE_VERSION && Array.isArray(current.pairings)) {
      stateCache = normalizeState(current);
      return stateCache;
    }
    stateCache = migrateLegacyState(stored[LEGACY_KEY]);
    await persistState();
    if (stored[LEGACY_KEY]) {
      await chrome.storage.local.remove(LEGACY_KEY);
    }
    return stateCache;
  })();
  try {
    return await statePromise;
  } finally {
    statePromise = null;
  }
}

function normalizeState(value) {
  const pairings = value.pairings
    .filter((item) => item && item.key && item.baseURL && item.pairingToken)
    .map((item) => ({
      key: item.key,
      baseURL: normalizeBaseURL(item.baseURL),
      pairingToken: item.pairingToken,
      identity: item.identity || null,
      claim: item.claim || null,
      task: item.task || null,
      phase: item.phase || "idle",
      windowId: Number.isInteger(item.windowId) ? item.windowId : null,
      tabId: Number.isInteger(item.tabId) ? item.tabId : null,
      lastMessage: item.lastMessage || "",
      tone: item.tone || "",
      updatedAt: item.updatedAt || new Date().toISOString(),
    }));
  const selectedKey = pairings.some((item) => item.key === value.selectedKey)
    ? value.selectedKey
    : (pairings[0]?.key || null);
  return {version: STATE_VERSION, selectedKey, pairings};
}

function migrateLegacyState(legacy) {
  if (!legacy?.baseURL || !legacy?.pairingToken) {
    return {version: STATE_VERSION, selectedKey: null, pairings: []};
  }
  const key = crypto.randomUUID();
  const claim = legacy.claim || null;
  return {
    version: STATE_VERSION,
    selectedKey: key,
    pairings: [{
      key,
      baseURL: normalizeBaseURL(legacy.baseURL),
      pairingToken: legacy.pairingToken,
      identity: null,
      claim,
      task: null,
      phase: claim ? "claimed" : "idle",
      windowId: Number.isInteger(legacy.verificationWindowID)
        ? legacy.verificationWindowID
        : null,
      tabId: null,
      lastMessage: "已迁移旧版配对，正在验证连接。",
      tone: "",
      updatedAt: new Date().toISOString(),
    }],
  };
}

async function persistState() {
  await chrome.storage.local.set({[STATE_KEY]: stateCache});
  await updateBadge();
  chrome.runtime.sendMessage({type: "state.updated"}).catch(() => {});
}

function publicState(state) {
  return {
    version: state.version,
    selectedKey: state.selectedKey,
    pairings: state.pairings.map((record) => ({
      key: record.key,
      baseURL: record.baseURL,
      identity: record.identity,
      claim: record.claim ? {
        task: record.claim.task,
        claim_expires_at: record.claim.claim_expires_at,
      } : null,
      task: record.task,
      phase: record.phase,
      windowId: record.windowId,
      tabId: record.tabId,
      lastMessage: record.lastMessage,
      tone: record.tone,
      updatedAt: record.updatedAt,
    })),
  };
}

async function addPairing(baseURLValue, pairingTokenValue) {
  const state = await loadState();
  const baseURL = normalizeBaseURL(baseURLValue);
  const pairingToken = String(pairingTokenValue || "").trim();
  if (!pairingToken) throw new Error("请输入配对令牌。");
  const identity = await callBridge(baseURL, "/identify", {
    pairing_token: pairingToken,
  });
  const existing = state.pairings.find((record) =>
    record.baseURL === baseURL &&
    record.identity?.pairing?.id === identity.pairing.id
  );
  if (existing) {
    existing.pairingToken = pairingToken;
    existing.identity = identity;
    existing.phase = "idle";
    existing.lastMessage = "配对已重新连接。";
    existing.tone = "success";
    state.selectedKey = existing.key;
  } else {
    const key = crypto.randomUUID();
    state.pairings.push({
      key,
      baseURL,
      pairingToken,
      identity,
      claim: null,
      task: null,
      phase: "idle",
      windowId: null,
      tabId: null,
      lastMessage: "配对成功。",
      tone: "success",
      updatedAt: new Date().toISOString(),
    });
    state.selectedKey = key;
  }
  await persistState();
  return state;
}

async function selectPairing(key) {
  const state = await loadState();
  requirePairing(state, key);
  state.selectedKey = key;
  await persistState();
  return state;
}

async function removePairing(key) {
  const state = await loadState();
  const record = requirePairing(state, key);
  if (record.claim?.task_token) {
    try {
      await callBridge(record.baseURL, "/release", {
        pairing_token: record.pairingToken,
        task_token: record.claim.task_token,
      });
    } catch {
      // Local removal must remain possible when the server is unavailable.
    }
  }
  await closeTaskWindow(record);
  state.pairings = state.pairings.filter((item) => item.key !== key);
  state.selectedKey = state.pairings[0]?.key || null;
  await persistState();
  return state;
}

async function claimTask(key) {
  const state = await loadState();
  const record = requirePairing(state, key);
  if (record.claim) throw new Error("当前配对已经领取任务。");
  const claim = await callBridge(record.baseURL, "/claim", {
    pairing_token: record.pairingToken,
  });
  record.claim = claim;
  record.task = null;
  record.phase = "claimed";
  record.lastMessage = "已领取验证任务。";
  record.tone = "success";
  record.updatedAt = new Date().toISOString();
  await persistState();
}

async function openTaskWindow(key) {
  const state = await loadState();
  const record = requirePairing(state, key);
  const task = requireClaimedTask(record);
  if (record.tabId !== null) {
    try {
      const tab = await chrome.tabs.get(record.tabId);
      if (tab?.id) {
        await chrome.windows.update(tab.windowId, {focused: true});
        await chrome.tabs.update(tab.id, {active: true});
        return;
      }
    } catch {
      record.tabId = null;
      record.windowId = null;
    }
  }
  const created = await chrome.windows.create({
    url: task.target_url,
    type: "popup",
    width: 1120,
    height: 820,
    focused: true,
  });
  record.windowId = created.id ?? null;
  record.tabId = created.tabs?.[0]?.id ?? null;
  record.lastMessage = "验证窗口已打开。";
  record.tone = "success";
  record.updatedAt = new Date().toISOString();
  await persistState();
}

async function startBrowserTask(key) {
  const state = await loadState();
  const record = requirePairing(state, key);
  const task = requireClaimedTask(record);
  const tab = await requireTargetTab(record, task.target_url);
  const userAgent = await readTabUserAgent(tab.id);
  const ready = await callBridge(record.baseURL, "/browser/ready", {
    pairing_token: record.pairingToken,
    task_token: record.claim.task_token,
    user_agent: userAgent,
  });
  record.claim = null;
  record.task = ready.task;
  record.identity = {
    ...(record.identity || {}),
    latest_task: ready.task,
  };
  record.phase = "running";
  record.lastMessage = "正在通过浏览器执行同步任务。";
  record.tone = "";
  record.updatedAt = new Date().toISOString();
  await persistState();
  startPump(key);
}

async function releaseTask(key) {
  const state = await loadState();
  const record = requirePairing(state, key);
  requireClaimedTask(record);
  await callBridge(record.baseURL, "/release", {
    pairing_token: record.pairingToken,
    task_token: record.claim.task_token,
  });
  record.claim = null;
  record.phase = "idle";
  record.lastMessage = "验证任务已释放。";
  record.tone = "success";
  record.updatedAt = new Date().toISOString();
  await closeTaskWindow(record);
  await persistState();
}

function startPump(key) {
  if (activePumps.has(key)) return;
  const promise = pumpBrowserRequests(key).finally(() => activePumps.delete(key));
  activePumps.set(key, promise);
}

async function pumpBrowserRequests(key) {
  for (;;) {
    const state = await loadState();
    const record = state.pairings.find((item) => item.key === key);
    if (!record || record.phase !== "running") return;
    try {
      await refreshPairing(record);
      if (record.phase !== "running") {
        await persistState();
        return;
      }
      const request = await callBridge(
        record.baseURL,
        "/browser/request/claim",
        {pairing_token: record.pairingToken},
      );
      if (request) {
        const completion = await executeBrowserRequest(record, request);
        await callBridge(record.baseURL, "/browser/request/complete", {
          pairing_token: record.pairingToken,
          request_id: request.request_id,
          request_token: request.request_token,
          ...completion,
        });
        await refreshPairing(record);
      } else {
        await new Promise((resolve) => setTimeout(resolve, 500));
      }
      record.updatedAt = new Date().toISOString();
      await persistState();
    } catch (error) {
      record.lastMessage = error instanceof Error ? error.message : String(error);
      record.tone = "error";
      if (error?.terminal) record.phase = "invalid";
      record.updatedAt = new Date().toISOString();
      await persistState();
      return;
    }
  }
}

async function executeBrowserRequest(record, request) {
  const tab = await requireTargetTab(record, request.url);
  const expiresAt = new Date(request.expires_at).getTime();
  const timeoutMs = Math.max(
    1000,
    Math.min(50_000, Number.isFinite(expiresAt) ? expiresAt - Date.now() : 50_000),
  );
  const results = await chrome.scripting.executeScript({
    target: {tabId: tab.id},
    world: "MAIN",
    func: async (browserRequest, requestTimeoutMs) => {
      const controller = new AbortController();
      const timer = window.setTimeout(() => controller.abort(), requestTimeoutMs);
      try {
        const response = await fetch(browserRequest.url, {
          method: browserRequest.method,
          headers: browserRequest.headers || {},
          body: browserRequest.body || undefined,
          credentials: "include",
          cache: "no-store",
          redirect: "follow",
          signal: controller.signal,
        });
        const body = await response.text();
        if (new TextEncoder().encode(body).byteLength > 4 * 1024 * 1024) {
          throw new Error("浏览器响应超过 4 MiB 限制。");
        }
        const headers = {};
        for (const [name, value] of response.headers.entries()) {
          headers[name] = value;
        }
        return {
          status: response.status,
          headers,
          body,
          final_url: response.url,
          error: "",
        };
      } catch (error) {
        return {
          status: 0,
          headers: {},
          body: "",
          final_url: location.href,
          error: error instanceof Error ? error.message : String(error),
        };
      } finally {
        window.clearTimeout(timer);
      }
    },
    args: [request, timeoutMs],
  });
  const completion = results?.[0]?.result;
  if (!completion) throw new Error("目标标签页没有返回浏览器请求结果。");
  if (completion.final_url && !sameOrigin(request.url, completion.final_url)) {
    throw new Error("浏览器请求离开了任务目标站点。");
  }
  return completion;
}

async function readTabUserAgent(tabId) {
  const results = await chrome.scripting.executeScript({
    target: {tabId},
    world: "MAIN",
    func: () => navigator.userAgent,
  });
  return String(results?.[0]?.result || "");
}

async function requireTargetTab(record, targetURL) {
  if (!Number.isInteger(record.tabId)) {
    throw new Error("请先打开验证窗口。");
  }
  const tab = await chrome.tabs.get(record.tabId);
  if (!tab?.id || !tab.url || !sameOrigin(tab.url, targetURL)) {
    throw new Error("验证窗口当前不在任务目标站点。");
  }
  return tab;
}

async function refreshAndResume() {
  const state = await loadState();
  for (const record of state.pairings) {
    try {
      await refreshPairing(record);
    } catch (error) {
      record.lastMessage = error instanceof Error ? error.message : String(error);
      record.tone = "error";
      if (error?.terminal) record.phase = "invalid";
    }
  }
  await persistState();
  for (const record of state.pairings) {
    if (record.phase === "running") startPump(record.key);
  }
}

async function refreshPairingByKey(key) {
  const state = await loadState();
  const record = requirePairing(state, key);
  await refreshPairing(record);
  await persistState();
  if (record.phase === "running") startPump(record.key);
}

async function refreshPairing(record) {
  const identity = await callBridge(record.baseURL, "/identify", {
    pairing_token: record.pairingToken,
  });
  record.identity = identity;
  const latest = identity.latest_task;
  if (await reconcileStoredClaim(record, identity, latest)) {
    record.updatedAt = new Date().toISOString();
    return;
  }
  if (!latest || !record.task || latest.id !== record.task.id) return;
  record.task = latest;
  await applyRetryState(record, latest);
  record.updatedAt = new Date().toISOString();
}

async function reconcileStoredClaim(record, identity, latest) {
  const claim = record.claim;
  if (!claim) return false;
  if (isClaimActive(claim, identity.pairing.id, latest)) return false;

  record.claim = null;
  if (latest?.id === claim.task?.id && latest.status === "completed") {
    record.task = latest;
    await applyRetryState(record, latest);
    return true;
  }
  record.task = null;
  record.phase = "idle";
  record.lastMessage = "验证任务领取已过期或已释放，请重新领取。";
  record.tone = "error";
  await closeTaskWindow(record);
  return true;
}

async function applyRetryState(record, latest) {
  switch (latest.retry_status) {
    case "succeeded":
      record.phase = "succeeded";
      record.lastMessage = latest.retry_message || "同步任务已完成。";
      record.tone = "success";
      await closeTaskWindow(record);
      break;
    case "failed":
      record.phase = "failed";
      record.lastMessage = latest.retry_message || "同步任务失败。";
      record.tone = "error";
      break;
    case "canceled":
      record.phase = "canceled";
      record.lastMessage = latest.retry_message || "同步任务已取消。";
      record.tone = "error";
      await closeTaskWindow(record);
      break;
    case "pending":
    case "running":
      record.phase = "running";
      record.lastMessage = latest.retry_message || "正在通过浏览器执行同步任务。";
      record.tone = "";
      break;
  }
}

async function closeTaskWindow(record) {
  if (Number.isInteger(record.windowId)) {
    try {
      await chrome.windows.remove(record.windowId);
    } catch {
      // The administrator may already have closed the temporary window.
    }
  }
  record.windowId = null;
  record.tabId = null;
}

function requirePairing(state, key) {
  const record = state.pairings.find((item) => item.key === key);
  if (!record) throw new Error("找不到已保存的配对。");
  return record;
}

function requireClaimedTask(record) {
  const task = record.claim?.task;
  if (!task || !record.claim?.task_token) {
    throw new Error("当前没有已领取的验证任务。");
  }
  const expiresAt = new Date(
    record.claim.claim_expires_at || task.expires_at,
  ).getTime();
  if (!Number.isFinite(expiresAt) || expiresAt <= Date.now()) {
    throw new Error("验证任务领取凭据已过期，请重新领取。");
  }
  return task;
}

async function updateBadge() {
  const state = stateCache;
  if (!state) return;
  const activeCount = state.pairings.filter((record) =>
    record.phase === "claimed" || record.phase === "running"
  ).length;
  await chrome.action.setBadgeBackgroundColor({color: "#176b51"});
  await chrome.action.setBadgeText({text: activeCount ? String(activeCount) : ""});
}
