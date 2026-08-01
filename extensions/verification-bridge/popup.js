const {
  formatTaskOperation,
  normalizeBaseURL,
  originPattern,
} = OctopusBridgeCommon;
const BRIDGE_STATE_KEY = "octopusVerificationBridgeV2";
const LEGACY_BRIDGE_STATE_KEY = "octopusVerificationBridge";
const RUNTIME_REVISION_KEY = "octopusVerificationBridgeRuntimeRevision";
// Bump when an upgrade must replace an already-registered service worker.
const RUNTIME_REVISION = "0.2.3";

const elements = {
  pairingList: document.querySelector("#pairing-list"),
  empty: document.querySelector("#pairing-empty"),
  selected: document.querySelector("#selected-pairing"),
  selectedTitle: document.querySelector("#selected-title"),
  selectedMeta: document.querySelector("#selected-meta"),
  selectedStatus: document.querySelector("#selected-status"),
  taskDetails: document.querySelector("#task-details"),
  taskHost: document.querySelector("#task-host"),
  taskOperation: document.querySelector("#task-operation"),
  taskRetry: document.querySelector("#task-retry"),
  claim: document.querySelector("#claim"),
  open: document.querySelector("#open"),
  start: document.querySelector("#start"),
  release: document.querySelector("#release"),
  refresh: document.querySelector("#refresh"),
  addForm: document.querySelector("#add-form"),
  baseURL: document.querySelector("#base-url"),
  pairingToken: document.querySelector("#pairing-token"),
  showToken: document.querySelector("#show-token"),
  status: document.querySelector("#status"),
};

let state = {selectedKey: null, pairings: []};

document.addEventListener("DOMContentLoaded", initialize);
chrome.runtime.onMessage.addListener((message) => {
  if (message?.type === "state.updated") void reloadState();
});
elements.showToken.addEventListener("change", () => {
  elements.pairingToken.type = elements.showToken.checked ? "text" : "password";
});
elements.addForm.addEventListener("submit", addPairing);
elements.claim.addEventListener("click", () => runAction("task.claim"));
elements.open.addEventListener("click", openTask);
elements.start.addEventListener("click", startTask);
elements.release.addEventListener("click", () => runAction("task.release"));
elements.refresh.addEventListener("click", () => runAction("pairing.refresh"));

async function initialize() {
  if (await reloadStaleRuntime()) return;
  await reloadState();
}

async function reloadStaleRuntime() {
  const stored = await chrome.storage.local.get([
    RUNTIME_REVISION_KEY,
    BRIDGE_STATE_KEY,
    LEGACY_BRIDGE_STATE_KEY,
  ]);
  if (stored[RUNTIME_REVISION_KEY] === RUNTIME_REVISION) return false;
  await chrome.storage.local.set({[RUNTIME_REVISION_KEY]: RUNTIME_REVISION});
  const hasSavedPairing = Boolean(
    stored[LEGACY_BRIDGE_STATE_KEY]?.pairingToken ||
    stored[BRIDGE_STATE_KEY]?.pairings?.length
  );
  if (!hasSavedPairing) return false;
  chrome.runtime.reload();
  return true;
}

async function reloadState() {
  try {
    state = await sendMessage({type: "state.get"});
    render();
  } catch (error) {
    setStatus(errorMessage(error), "error");
  }
}

async function addPairing(event) {
  event.preventDefault();
  await runBusy(event.submitter, async () => {
    const baseURL = normalizeBaseURL(elements.baseURL.value);
    const pairingToken = elements.pairingToken.value.trim();
    if (!pairingToken) throw new Error("请输入配对令牌。");
    await ensureOriginPermission(baseURL);
    state = await sendMessage({
      type: "pairing.add",
      baseURL,
      pairingToken,
    });
    elements.pairingToken.value = "";
    elements.showToken.checked = false;
    elements.pairingToken.type = "password";
    render();
    setStatus("配对已保存。", "success");
  });
}

async function openTask() {
  await runBusy(elements.open, async () => {
    const record = selectedPairing();
    const task = record?.claim?.task;
    if (!task) throw new Error("当前没有已领取的验证任务。");
    await ensureOriginPermission(task.target_url);
    state = await sendMessage({type: "task.open", key: record.key});
    render();
  });
}

async function startTask() {
  await runBusy(elements.start, async () => {
    const record = selectedPairing();
    const task = record?.claim?.task;
    if (!task) throw new Error("当前没有已领取的验证任务。");
    await ensureOriginPermission(task.target_url);
    state = await sendMessage({type: "task.start", key: record.key});
    render();
  });
}

async function runAction(type) {
  const record = selectedPairing();
  if (!record) throw new Error("请先选择配对。");
  const button = {
    "task.claim": elements.claim,
    "task.release": elements.release,
    "pairing.refresh": elements.refresh,
  }[type];
  await runBusy(button, async () => {
    state = await sendMessage({type, key: record.key});
    render();
  });
}

async function removePairing(key, button) {
  await runBusy(button, async () => {
    state = await sendMessage({type: "pairing.remove", key});
    render();
    setStatus("配对已从扩展移除。", "success");
  });
}

async function selectPairing(key) {
  state = await sendMessage({type: "pairing.select", key});
  render();
}

function render() {
  renderPairingList();
  renderSelectedPairing();
}

function renderPairingList() {
  elements.pairingList.replaceChildren();
  elements.empty.hidden = state.pairings.length !== 0;
  for (const record of state.pairings) {
    const row = document.createElement("div");
    row.className = `pairing-row${record.key === state.selectedKey ? " selected" : ""}`;

    const select = document.createElement("button");
    select.type = "button";
    select.className = "pairing-select";
    select.addEventListener("click", () => void selectPairing(record.key));
    const identity = record.identity;
    const title = document.createElement("strong");
    title.textContent = identity
      ? `${identity.site_name} / ${identity.site_account_name}`
      : "未验证的旧版配对";
    const meta = document.createElement("span");
    meta.textContent = `${identity?.pairing?.name || "验证桥"} · ${hostLabel(record.baseURL)}`;
    select.append(title, meta);

    const phase = document.createElement("span");
    phase.className = `phase phase-${record.phase}`;
    phase.textContent = phaseLabel(record.phase);

    const remove = document.createElement("button");
    remove.type = "button";
    remove.className = "icon-button danger";
    remove.title = "从扩展移除";
    remove.setAttribute("aria-label", "从扩展移除");
    remove.textContent = "×";
    remove.addEventListener("click", () => void removePairing(record.key, remove));

    row.append(select, phase, remove);
    elements.pairingList.append(row);
  }
}

function renderSelectedPairing() {
  const record = selectedPairing();
  elements.selected.hidden = !record;
  if (!record) return;
  const identity = record.identity;
  elements.selectedTitle.textContent = identity
    ? `${identity.site_name} / ${identity.site_account_name}`
    : "未验证的旧版配对";
  elements.selectedMeta.textContent = identity
    ? `${identity.pairing.name} · 到期 ${formatDate(identity.pairing.expires_at)}`
    : record.baseURL;
  elements.selectedStatus.textContent = record.lastMessage || phaseLabel(record.phase);
  elements.selectedStatus.className = `selected-status ${record.tone || ""}`;

  const task = record.claim?.task || record.task || identity?.latest_task || null;
  elements.taskDetails.hidden = !task;
  if (task) {
    elements.taskHost.textContent = task.target_host || hostLabel(task.target_url);
    elements.taskOperation.textContent = formatTaskOperation(task.operation);
    elements.taskRetry.textContent = retryLabel(task.retry_status);
  }

  const claimed = Boolean(record.claim) && [
    "claimed",
    "waiting",
    "permission_required",
  ].includes(record.phase);
  const running = record.phase === "running";
  const invalid = record.phase === "invalid";
  elements.claim.hidden = claimed || running;
  elements.claim.disabled = invalid;
  elements.open.hidden = !claimed;
  elements.start.hidden = !claimed;
  elements.start.disabled = !record.tabId;
  elements.release.hidden = !claimed;
}

function selectedPairing() {
  return state.pairings.find((record) => record.key === state.selectedKey) || null;
}

async function ensureOriginPermission(value) {
  const origin = originPattern(value);
  if (await chrome.permissions.contains({origins: [origin]})) return;
  const granted = await chrome.permissions.request({origins: [origin]});
  if (!granted) throw new Error("未授予站点访问权限。");
}

async function sendMessage(message) {
  const response = await chrome.runtime.sendMessage(message);
  if (!response?.ok) throw new Error(response?.error || "扩展后台没有响应。");
  return response.data;
}

async function runBusy(button, action) {
  if (button) button.disabled = true;
  setStatus("");
  try {
    await action();
  } catch (error) {
    setStatus(errorMessage(error), "error");
  } finally {
    if (button) button.disabled = false;
  }
}

function setStatus(message, tone = "") {
  elements.status.textContent = message;
  elements.status.className = `status${tone ? ` ${tone}` : ""}`;
}

function errorMessage(error) {
  return error instanceof Error ? error.message : String(error);
}

function hostLabel(value) {
  try {
    return new URL(value).host;
  } catch {
    return value || "";
  }
}

function formatDate(value) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "未知" : date.toLocaleString();
}

function phaseLabel(phase) {
  return {
    idle: "已配对",
    claimed: "待验证",
    waiting: "等待验证",
    permission_required: "待授权",
    running: "执行中",
    succeeded: "已完成",
    failed: "失败",
    canceled: "已取消",
    invalid: "已失效",
  }[phase] || phase;
}

function retryLabel(status) {
  return {
    none: "未安排",
    pending: "等待执行",
    running: "执行中",
    succeeded: "成功",
    failed: "失败",
    canceled: "已取消",
  }[status] || status || "未安排";
}
