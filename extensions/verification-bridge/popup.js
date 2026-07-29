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
  renderTask();
}

async function saveConnection() {
  await runBusy(elements.save, async () => {
    const baseURL = normalizeBaseURL(elements.baseURL.value);
    const pairingToken = elements.pairingToken.value.trim();
    if (!pairingToken) {
      throw new Error("Pairing token is required.");
    }
    await ensureOriginPermission(baseURL);
    state.baseURL = baseURL;
    state.pairingToken = pairingToken;
    await persistState();
    setStatus("Connection saved.", "success");
  });
}

async function claimTask() {
  await runBusy(elements.claim, async () => {
    await syncConnectionFromInputs();
    const data = await callBridge("/claim", {
      pairing_token: state.pairingToken,
    });
    state.claim = data;
    state.verificationWindowID = null;
    await persistState();
    renderTask();
    setStatus("Verification task claimed.", "success");
  });
}

async function openVerification() {
  await runBusy(elements.open, async () => {
    const task = requireActiveTask();
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
    setStatus("Verification window opened.", "success");
  });
}

async function submitSession() {
  await runBusy(elements.submit, async () => {
    const task = requireActiveTask();
    await ensureOriginPermission(task.target_url);
    const cookies = await chrome.cookies.getAll({url: task.target_url});
    if (!cookies.length) {
      throw new Error("No cookies are available for the verification target.");
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
    setStatus("Verification session submitted.", "success");
  });
}

async function releaseTask() {
  await runBusy(elements.release, async () => {
    requireActiveTask();
    await callBridge("/release", {
      pairing_token: state.pairingToken,
      task_token: state.claim.task_token,
    });
    await clearActiveTask(true);
    setStatus("Verification task released.", "success");
  });
}

async function syncConnectionFromInputs() {
  const baseURL = normalizeBaseURL(elements.baseURL.value);
  const pairingToken = elements.pairingToken.value.trim();
  if (!pairingToken) {
    throw new Error("Pairing token is required.");
  }
  await ensureOriginPermission(baseURL);
  state.baseURL = baseURL;
  state.pairingToken = pairingToken;
  await persistState();
}

async function callBridge(path, body) {
  if (!state.baseURL || !state.pairingToken) {
    throw new Error("Save the Octopus connection first.");
  }
  const response = await fetch(
    `${state.baseURL}/api/v1/site/recovery/verification/bridge${path}`,
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
    throw new Error(`Octopus returned HTTP ${response.status}.`);
  }
  if (!response.ok || payload.code !== 200) {
    throw new Error(payload.message || `Octopus returned HTTP ${response.status}.`);
  }
  return payload.data;
}

function requireActiveTask() {
  const task = state.claim?.task;
  if (!task || !state.claim?.task_token) {
    throw new Error("No active verification task.");
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
  state.claim = null;
  state.verificationWindowID = null;
  await persistState();
  renderTask();
  if (removeTargetPermission && targetURL) {
    const targetOrigin = originPattern(targetURL);
    const baseOrigin = state.baseURL ? originPattern(state.baseURL) : "";
    if (targetOrigin !== baseOrigin) {
      await chrome.permissions.remove({origins: [targetOrigin]});
    }
  }
}

async function ensureOriginPermission(value) {
  const origin = originPattern(value);
  if (await chrome.permissions.contains({origins: [origin]})) {
    return;
  }
  const granted = await chrome.permissions.request({origins: [origin]});
  if (!granted) {
    throw new Error("Host permission was not granted.");
  }
}

function normalizeBaseURL(value) {
  const parsed = new URL(value.trim());
  if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
    throw new Error("Octopus URL must use HTTP or HTTPS.");
  }
  parsed.hash = "";
  parsed.search = "";
  return parsed.href.replace(/\/+$/, "");
}

function originPattern(value) {
  const parsed = new URL(value);
  if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
    throw new Error("Only HTTP and HTTPS targets are supported.");
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
  elements.taskOperation.textContent = task.operation || "manual";
  elements.taskExpires.textContent = new Date(task.expires_at).toLocaleString();
}

async function runBusy(button, action) {
  button.disabled = true;
  setStatus("");
  try {
    await action();
  } catch (error) {
    setStatus(error instanceof Error ? error.message : String(error), "error");
  } finally {
    button.disabled = false;
  }
}

function setStatus(message, tone = "") {
  elements.status.textContent = message;
  elements.status.className = `status${tone ? ` ${tone}` : ""}`;
}
