<div align="center">

# 🐙 Octopus Getting Started Guide

**A step-by-step guide to get Octopus up and running from scratch**

</div>

> This guide is for **first-time users**, covering the complete workflow from deployment to client integration.
> If you just want a quick start, jump to [II. 5-Minute Quick Start](#ii-5-minute-quick-start).
> If you run into problems, check [XIII. FAQ](#xiii-frequently-asked-questions) first — most common pitfalls are covered there.
>
> For deployment details, configuration files, and cross-platform builds, see [README.md](README.md).

---

## Table of Contents

- [I. What Is Octopus](#i-what-is-octopus)
- [II. 5-Minute Quick Start](#ii-5-minute-quick-start)
- [III. 6 Core Concepts You Must Understand](#iii-6-core-concepts-you-must-understand)
- [IV. Deployment & First Login](#iv-deployment--first-login)
- [V. Step 1: Add a Site and Sync (Site Page)](#v-step-1-add-a-site-and-sync-site-page)
- [VI. Step 2: Complete Keys on the Channel Page (Site Channels)](#vi-step-2-complete-keys-on-the-channel-page-site-channels)
- [VII. Step 3: Create a Group = External Model Name (Group Page)](#vii-step-3-create-a-group--external-model-name-group-page)
- [VIII. Step 4: Create an API Key and Connect Clients](#viii-step-4-create-an-api-key-and-connect-clients)
- [IX. Manual Channels: Connecting Non-Site Providers](#ix-manual-channels-connecting-non-site-providers)
- [X. Protocol Conversion](#x-protocol-conversion)
- [XI. Load Balancing / Circuit Breaker / Retry / Logs](#xi-load-balancing--circuit-breaker--retry--logs)
- [XII. Settings Page Overview](#xii-settings-page-overview)
- [XIII. Frequently Asked Questions](#xiii-frequently-asked-questions)
- [XIV. Performance & Operations](#xiv-performance--operations)

---

## I. What Is Octopus

Octopus is an **LLM API aggregation and load balancing service**. In simple terms, it unifies "a bunch of API relay sites with a bunch of Keys" into "one address + one Key + your own model names", then distributes requests to clients like Claude Code, Codex, Cherry Studio, Immersive Translate, and more.

This project builds on the original [bestruirui/octopus](https://github.com/bestruirui/octopus), adding two capabilities that beginners care about most:

1. **Site Module**: Manage your registered API relay sites, **auto-sync available models and auto check-in**, and project site groups/models into channels.
2. **More Complete Protocol Conversion**: Use `OpenAI Response` or even `Gemini` format models in Claude Code.

> 💡 Key design philosophy: **"Group" is your externally exposed model name**. This is the biggest difference from other tools and the most common stumbling block for beginners — please read [Chapter III](#iii-6-core-concepts-you-must-understand) carefully.

The UI has 7 pages, listed in order of use:

| Page | Purpose |
|------|---------|
| **Home** | Cost/request statistics, leaderboard, group health overview |
| **Sites** | Manage relay site accounts, auto-sync + check-in (the main battlefield for aggregation users) |
| **Channels** | Site channels (auto-projected) + Manual channels (manually added), complete Keys here |
| **Groups** | **Define external model names**, aggregate channels into one model |
| **Pricing** | Model pricing management |
| **Logs** | Request details, retries, circuit breaker records |
| **Settings** | Global config, API keys, account credentials, backup, etc. |

---

## II. 5-Minute Quick Start

If your typical use case is "importing relay sites for aggregated use", here's the complete flow:

```
1) Create the group Keys you need on the relay site (recommend: one Key per group, only for groups you need)
        ↓
2) Octopus [Sites] page → Add Site → Add Account (fill in Access Token) → Sync
        ↓
3) Octopus [Channels] page → Site Channels → Check if "Complete Key" is needed, paste the relay site's Key
        ↓
4) Octopus [Groups] page → Create Group (group name = the model name you'll use in requests) → Add/Auto-add models
        ↓
5) Octopus [Settings] → API Keys → Add Key (get the sk-octopus-... string)
        ↓
6) In your client: URL http://your-IP:8080/v1 + the Key from step 5 + the group name from step 4 as model
```

> ✅ Once complete, the site is ready to use. You generally won't need to touch the configuration again unless you **add a new site**.

If your provider isn't a relay site but gave you a Base URL + Key directly (e.g., Zhipu CodingPlan, DeepSeek), skip Sites and go directly to [Chapter IX: Manual Channels](#ix-manual-channels-connecting-non-site-providers).

---

## III. 6 Core Concepts You Must Understand

| Concept | One-Line Explanation | Common Misunderstanding |
|---------|---------------------|------------------------|
| **Site** | An API relay site, storing its **URL, platform type, proxy** | URL should be **domain only** — don't include `/v1` or similar paths |
| **Account** | Login credentials under a site, responsible for **sync + check-in**; one site can have multiple accounts | Access Token goes in the **Account**, not the Site |
| **Managed / Projected Channel** | Channels **auto-generated** after site sync, maintained automatically with the site | They **don't appear** in the "Manual Channels" list — look under the "Site Channels" tab |
| **Manual Channel** | Channels you **manually add** (when you have your own Base URL + Key) | Separate from site channels, they don't affect each other |
| **Group** | **Your externally exposed model name** — the client's `model` must equal the group name | **You must create a group first**, otherwise there are no available models and you can't create API keys |
| **API Key** | The `sk-octopus-...` for clients, created in "Settings → API Keys" | The "Name" field in the list ≠ the Key itself — the Key is the string generated after creation |

> 🔑 **The most important takeaway**: Octopus's "group" logic is different from other tools — **the group name IS the model name in requests**.
> For example, create a group called `gpt-5.5`, add various channels' GPT-5.5 models to it, and clients use `model: gpt-5.5` to call it.
> No groups = no available models = can't select models when creating keys. "Can't get models / can't create Key" is most commonly caused by not having created a group yet.

---

## IV. Deployment & First Login

### 4.1 Deployment (Choose One)

**Docker run:**
```bash
docker run -d --name octopus -v /path/to/data:/app/data -p 8080:8080 hureru/octopus
```

**docker compose:**
```bash
wget https://raw.githubusercontent.com/Hureru/octopus/refs/heads/dev/docker-compose.yml
docker compose up -d
```

> ⚠️ **You must use the `hureru/octopus` image** (the version with Site functionality). If you use `bestrui/octopus` or other upstream images, you won't see the "Sites" page. For Release binaries and building from source, see [README.md](README.md).

### 4.2 First Login

Open `http://your-IP:8080` in a browser. Default credentials:

- Username: `admin`
- Password: `admin`

> ⚠️ **Login says wrong password?** Most likely you previously installed the upstream version and the `data` directory has leftover data (the old credentials were preserved).
> Solution: Use the old password if you remember it; otherwise **clear the data directory and redeploy**. See [FAQ Q1](#q1-default-adminadmin-login-fails).

### 4.3 Change Password Immediately

After logging in, go to **Settings → Account Settings → Change Password** (you can also change the username). New password must be at least 6 characters.

> Note: This project persists login secrets in the database, so changing passwords is safe.

---

## V. Step 1: Add a Site and Sync (Site Page)

Go to the **Sites** page. When empty, you'll see a "No sites yet → Add your first site" guide button. Add Site / Import / Archive / Full Sync / Full Check-in entries are in the **toolbar at the top-right of the Sites page**.

### 5.1 Add a Site

Click Add Site and fill in:

| Field | Description |
|-------|-------------|
| **Site Name** | Any name for your reference, e.g., "Main OneAPI" |
| **Platform Type** | See table below; if unsure, leave as "Auto Detect" (only available during creation, occasionally inaccurate — select manually if so) |
| **Site URL** | **Domain only**, e.g., `https://example.com` — **don't include** `/v1`, `/api`, or other paths |
| **Default Protocol** | Only for "API Direct" platform: choose which protocol this site uses by default (OpenAI Chat / Anthropic / Gemini) |
| **Manual Check-in URL** (optional) | If set, you can "one-click open" this page from the site overview for manual check-in |
| **Proxy** | Direct / System Proxy / Proxy Pool (see [12.x Proxy Pool](#1213-proxy-pool)) |
| **Enable Site** | When disabled, no managed channels are projected |
| **Advanced → Per-Protocol Base URL** (optional) | Some upstreams use different path prefixes for different protocols (e.g., OpenAI at `<base>/v1`, Anthropic at `<base>/anthropic/v1`). You can specify a separate Base URL for each protocol here |
| **Advanced → Custom Headers** (optional) | Additional headers applied to all requests for this site |

Supported **Platform Types**:

| Platform | Description |
|----------|-------------|
| **New API** | The most common relay site type (New API / various forks) |
| **AnyRouter** | LinuxDO-based "any" sites (credentials use cookies, see below) |
| **One API / One Hub / Done Hub** | Corresponding open-source panels |
| **Sub2API** | sub2api sites |
| **API Direct** | Official or direct providers (OpenAI / Claude / Gemini). Requires selecting a **default protocol** (see note below) |

> 💡 **API Direct platform's "Default Protocol"**: After selecting API Direct, you need to specify a **default protocol type** (OpenAI Chat / Anthropic / Gemini) to tell Octopus which protocol to use by default. Platform detection will automatically recommend a suitable protocol. The old separate OpenAI / Claude / Gemini platform types have been automatically merged into "API Direct" — no manual action needed after upgrading.

### 5.2 Add an Account (Key Point: What to Fill for Access Token)

After creating a site, the "Add Account" dialog opens automatically. **Sync and check-in are done by accounts** — credentials go here.

**Credential types** (available options vary by platform): Username/Password, **Access Token** (recommended), API Key.

The way to fill in Access Token varies by platform — this is the biggest pitfall for beginners. Key reference:

| Platform | Recommended Credential | What to Put in Access Token | Extra Fields |
|----------|----------------------|---------------------------|--------------|
| **New API** | Access Token | The site's **"System Access Token"** (not the login password!) | Must fill **Platform User ID** (user ID on the relay site) |
| **AnyRouter (any)** | Access Token | **Cookie**, format: `session=MTc1234567890` | — |
| **Sub2API** | Access Token | The site's access token | Recommended to also fill `refresh_token` and `token_expires_at` (get via F12, auto-refreshes on 401) |
| **OpenAI/Claude/Gemini (API Direct)** | API Key or Access Token | The corresponding key | — |

> 🔎 **Where is the "System Access Token" for New API sites?**
> Log in to the relay site → Profile Settings → Account Management → Security Settings → **System Access Token**. The generated string is the Access Token.
> **Strongly discouraged: logging in with username/password** — many sites don't return an Access Token after login, resulting in `Site login succeeded but no Access Token returned`.
>
> 🔎 **What is Platform User ID?** New API needs it for syncing tokens, groups, and check-in (it's your user ID on the relay site, e.g., `11494`). The import feature tries to fill this automatically.

Account switches:

- **Enable Account**: When off, this account doesn't participate in sync/projection.
- **Auto Sync** (default on): Syncs automatically at the interval set in Settings.
- **Auto Check-in** (default on): Checks in automatically on schedule.
- **Random Check-in** (default off): When enabled, you can set "Minimum check-in interval (hours)" and "Random delay window (minutes)" to avoid predictable patterns.
- **Proxy**: Can "Inherit Site Proxy" or be set independently.

### 5.3 Sync: What Gets Pulled

After clicking "Sync Account" (or the toolbar's "Full Sync"), Octopus pulls from the site: **groups, Keys (masked), models, balance, today's income**.

The site card displays: account count / key count / model count / balance / today's income, plus a **health badge**:

| Badge | Meaning |
|-------|---------|
| Normal | Everything is ready |
| Not Executed | Haven't synced yet, click sync |
| Needs Configuration | No accounts under this site |
| N Partially Synced | Some groups synced successfully, some suspended (see below) |
| N Errors | Some accounts failed to sync or check in |
| N Disabled / Site Disabled | Some accounts/site are disabled |

**Sync status text**: `Last sync [time] · Success/Partial/Failed · details`. Common messages:

- **Partial success · Suspended N group projections missing usable Keys**: Some groups **don't have Keys** on your relay site yet, so Octopus can't project them.
  → Fix: Create Keys for those groups on the relay site, or complete Keys on the Octopus channel page (see [Chapter VI](#vi-step-2-complete-keys-on-the-channel-page-site-channels)), then re-sync to auto-recover.
- **Failed · HTTP 401**: Token/cookie expired, get a new one.
- **Cloudflare protection**: The site triggered CF. First verify the site is up in a browser (server down also shows CF); if it loads fine, try configuring a proxy for the account.
- **Platform doesn't support check-in**: Not an error — this platform (e.g., sub2api) simply doesn't support check-in, API usage is unaffected.

### 5.4 Batch Import (from All API Hub / Metapi)

Too many sites to add one by one? Use the **toolbar's "Import"** button:

1. Select **Import Source**: All API Hub or Metapi.
2. **Upload a JSON file** or **paste the exported JSON** directly.
3. Click "Start Import" — sites are auto-created or reused based on platform + URL, showing counts for new/reused/updated/skipped and any warnings.

> Note: Metapi import only migrates **sites, accounts, Keys, groups, models** — routing strategies and downstream Keys are skipped (this is intentional — the project aims to move away from Metapi's complex routing). For import errors, see [FAQ Q10](#q10-import-from-all-api-hub--metapi-fails).

### 5.5 Archive & Delete

- **Archive Site**: Removed from the main list and managed channels go offline, but **accounts/Keys/models are preserved**. You can restore anytime from the "Archived Sites" section (restored sites are disabled by default; enable them to auto-rebuild managed channels).
- **Delete Site/Account**: Deletes everything including managed channels — irreversible.

---

## VI. Step 2: Complete Keys on the Channel Page (Site Channels)

Go to the **Channels** page. There are two tabs at the top: **Site Channels** and **Manual Channels**. Channels from site sync appear under "Site Channels" (they don't show in the Manual Channels list by default).

### 6.1 Why "Complete Key"

Many relay sites **don't return plaintext Keys** — what's synced is a **masked value** (like `sk-****1234`). Masked Keys can't actually make requests, so you need to **manually paste in the full plaintext Key**. The channel page provides a "Bulk Complete" panel for this.

### 6.2 Two Ways to Complete

**Method A: Top "Bulk Complete Key" Button** (recommended, batch processing)
- A "Bulk Complete Key" button appears at the top of the Site Channels page, showing "N items to complete".
- Opens a panel organized by Site → Account → Group, with each row showing: group, key name, current masked value, and an **input box for the full Key**.
- Paste the plaintext Key, click "Save This Account" — saved keys are auto-enabled and rejoin projection.
- The dialog also has "Open Token Management" to jump directly to the site's token page to copy Keys.

**Method B: Complete/Create Within a Specific Group**
- In a site account panel, there's a **group dropdown** and row of buttons at the top.
- If a group **has no Keys yet**, a "N groups need Keys" prompt appears — click to **quick-create Keys** on supported sites (Octopus calls the site API to create them automatically).
- You can also manually "Complete/Edit" projected Keys for a specific group.

> 💡 Recommendation: **Keep only one Key per group per site, and only create Keys for groups you actually need** — this keeps sync and projection clean.

### 6.3 Other Operations in Site Channels

After selecting a group, the model table shows: **Model / Group / Endpoint Format / Source / Key / Status / Last Request / Channel / Actions**. Common operations:

- **Endpoint Format** (Chat / Response / Embedding): Each model can be switched via "Move to…"; those that can't be auto-detected are marked "Needs Manual Assignment".
- **Add** (custom model): Models not exposed by upstream `/models` but actually usable can be **manually added** here (fill in model name + endpoint format).
- **Project / Don't Project**: Stop/resume generating projected channels for this group.
- **Filter**: Only misconfigured / Only with request history / Only disabled.
- **Advanced**: Parameter override (JSON) for projected channels.
- **More → Reset Model Endpoint Format**: Restore endpoint format to auto-detected results.

**Group status badges**: Needs Key (no Key) / Needs Completion (has masked Key awaiting completion) / Suspended (system suspended projection) / Carried Over (recent sync didn't confirm new models, using last successful result).

> Seeing "Group projection suspended by system" — don't panic. Usually caused by missing usable Keys or upstream temporarily having no available models. **Re-syncing successfully will auto-recover**.

---

## VII. Step 3: Create a Group = External Model Name (Group Page)

Go to the **Groups** page and click the button at top-left to **Create Group**. This is where you aggregate channels into "one external model name".

### 7.1 Form Fields

| Field | Description |
|-------|-------------|
| **Group Name** | **The `model` name clients use in requests**, e.g., `gpt-5.5`, `claude-sonnet-4-5` |
| **Match Regex** | Leave empty for default "fuzzy match by group name" against upstream models; or write regex for exact matching |
| **First Token Timeout** | In seconds, only applies to streaming responses. 0 = no limit |
| **Session Affinity** | In seconds, keep using the same channel during a session. **0 = disabled** |
| **Same Channel Retry** + **Max Retries** | See below |
| **Load Balancing Mode** | Round Robin / Random / Failover / Weighted |
| **Selected Models** | Which "channel + model" combinations this group aggregates |

**Load Balancing Modes**:

| Mode | Description |
|------|-------------|
| 🔄 Round Robin | Cycles to the next channel with each request |
| 🎲 Random | Randomly picks an available channel each time |
| 🛡️ Failover | Prefers high-priority channels, falls back to lower priority only on failure |
| ⚖️ Weighted | Distributes requests based on channel weight ratios |

### 7.2 How to Add Models to a Group

In the "Add Models" area:

- **Manual Add**: Select by Channel → Model (each entry shows source: site / account / group for easy identification).
- **Auto Add** (✨ icon): Adds all models that "match the group name/regex" at once.

> Example: A group named `gpt-5.5` — click "Auto Add" to pull in all models matching `gpt-5.5` from various channels. In weighted mode, you can also set weights for each member.

### 7.3 Auto-Group Configuration (Auto-Classify Newly Added Upstream Models)

Relay sites may add new models over time. In the **"Auto-Group Configuration"** on the Groups page, you can have new models automatically assigned to matching groups:

- **Global Default Mode**: When enabled, all site projected channels use the selected matching method (Off / Fuzzy / Exact / Regex). When enabled, newly synced upstream models are automatically added to matching groups.
- **Disable Global**: New upstream models require you to manually go to the corresponding group and click "Auto Add".
- Note: Disabling auto-group for a channel or the global setting **does not delete** existing group members — you must remove them manually.

> ⚠️ It **will not** automatically sort all models into groups like Metapi does. You still need to **create groups first** — auto-group only assigns (new) models to **existing** groups.

### 7.4 Group Health Check (Optional)

Enable at **Settings → System → Group Health Check**, then the Home page shows a group health summary, group cards show health status, and you can manually run "Standard Probe / Full Probe".
(Note: This is a **manually** triggered health check, **not scheduled auto-probing** — most public relay sites prohibit automated probing.)

---

## VIII. Step 4: Create an API Key and Connect Clients

### 8.1 Create an Octopus API Key

Go to **Settings → API Keys → Add Key**:

| Field | Description |
|-------|-------------|
| Name | For identification only — **not the Key itself** |
| Max Cost | Spending limit for this Key; can be set to "Unlimited" |
| Expiration Date | Can be set to "Never Expire" |
| Requests Per Minute (RPM) | Maximum requests per minute for this Key. 0 or empty = no limit; returns HTTP 429 when exceeded |
| Supported Models | **None selected = unlimited**; selecting specific groups limits the Key to those |
| Enabled | — |

> ⚠️ Common pitfall: The "Name" field in the list is for identification. **The actual Key is the `sk-octopus-...` string generated after creation** — don't use the name as your Key.

Verify with curl after creation (replace IP and Key with yours):

```bash
curl http://your-IP:8080/v1/models \
  -H "Authorization: Bearer sk-octopus-your-key"
```

If it lists models (which are your **group names**), you're good to go.

### 8.2 Common Client Integration

**OpenAI SDK / Cherry Studio / Immersive Translate**, etc.:
- URL (Base URL): `http://your-IP:8080/v1`
- API Key: `sk-octopus-...`
- Model: Your **group name**

```python
from openai import OpenAI
client = OpenAI(
    base_url="http://127.0.0.1:8080/v1",
    api_key="sk-octopus-xxxx",
)
resp = client.chat.completions.create(
    model="gpt-5.5",          # Use your group name here
    messages=[{"role": "user", "content": "Hello"}],
)
print(resp.choices[0].message.content)
```

**Claude Code** (`~/.claude/settings.json` — note the URL does **not** include `/v1`):
```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://127.0.0.1:8080",
    "ANTHROPIC_AUTH_TOKEN": "sk-octopus-xxxx",
    "ANTHROPIC_MODEL": "octopus-sonnet-4-5",
    "ANTHROPIC_SMALL_FAST_MODEL": "octopus-haiku-4-5"
  }
}
```

**Codex** (`~/.codex/config.toml` + `~/.codex/auth.json`):
```toml
model = "octopus-codex"        # Use your group name
model_provider = "octopus"
[model_providers.octopus]
name = "octopus"
base_url = "http://127.0.0.1:8080/v1"
```
```json
{ "OPENAI_API_KEY": "sk-octopus-xxxx" }
```

> For more client examples, see [README.md Client Integration](README.md).

---

## IX. Manual Channels: Connecting Non-Site Providers

When you're **not using relay site aggregation** but have your own "Base URL + Key" (e.g., Zhipu CodingPlan, DeepSeek, a direct provider), use **Channels page → Manual Channels → top-right + button** to add manually.

Key fields:

| Field | Description |
|-------|-------------|
| Channel Name | Custom |
| Channel Type | OpenAI Chat / OpenAI Response / Anthropic / Gemini / Volcengine / OpenAI Embedding |
| Base URLs | Only the base address — the program auto-appends `/chat/completions`, `/responses`, `/messages`, etc. based on type; multiple endpoints enable "lowest latency selection" |
| API Key | **Can add multiple Keys** (enabling multi-Key rotation, see FAQ Q6) |
| Advanced Settings | Custom Headers, channel proxy, parameter override (JSON), auto-sync, auto-group, Responses WS mode, match regex, notes |

After adding a manual channel, **you still need to add its models to a group on the Groups page** before it can be used externally.

> 💡 Integration tips:
> - For Zhipu **CodingPlan**, use a manual channel and select the correct channel type.
> - For DeepSeek (opencode go) with Anthropic format, add a manual channel directly; if it only accepts chat format, the Anthropic→Chat conversion also works.
> - Wrong channel type/format is the most common cause of "configured but no response".

---

## X. Protocol Conversion

Octopus supports **OpenAI Chat / OpenAI Responses / Anthropic** format interconversion (Gemini conversion also exists but isn't fully tested).

**Core rules**:

- **Just add a channel to a group** — when the downstream request format doesn't match the channel's format, **conversion happens automatically**. No extra configuration needed.
- **Don't configure two endpoint formats for the same model**. Recommend setting everything to **Response** — after adding to a group, Chat format requests still work (auto-converted).
- **Passthrough**: `Response` format and `Anthropic message` format pass through unchanged when both ends match; once format conversion occurs, it's no longer passthrough. Codex's `fast` and remote compact endpoints (Responses compact proxy) pass through when the entire pipeline is Response format.

> So "Claude Code calling Response-format GPT" basically works fine; for Gemini format you can try it — bug reports welcome.

---

## XI. Load Balancing / Circuit Breaker / Retry / Logs

### 11.1 Retry Rules (Frequently Asked)

- Manual channel: **a Key returns 401** (auth failure) → **will not** retry other Keys in the same channel; instead **switches directly to the next channel**.
- **429 / 5xx** and other retryable errors → depends on the group's **"Same Channel Retry"** toggle: if on, retries with the **same channel and same Key** first (count = max retries); after all retries fail, the status code is passed through to the client.

### 11.2 Circuit Breaker

When a channel has **consecutive failures** reaching the threshold, it gets circuit-broken for a period. Cool-down time grows via **exponential backoff** (threshold / base cool-down / max cool-down are configurable in Settings → Circuit Breaker).

> How to tell if a channel is in circuit breaker cooldown? Go to **Logs** → expand an entry → **Retry Details** — you'll see a "circuit broken" marker and **countdown until recovery**.

### 11.3 Passive Outlier Retirement (Optional)

Settings → Passive Outlier Retirement: Based on real request success/failure sequences, automatically **disables** (not deletes) continuously failing **site projected channels**. Auto-re-enables when probes recover. **Only affects site projected channels, disabled by default**.

### 11.4 What Log Cards Show

Each log entry can be expanded to see: error message, **time to first token**, total duration, input/output tokens, cost, **retry details** (each attempt's success/failure/skip/circuit-break), and WS-related markers (passthrough/conversion/continuation/replay/fallback, etc.). Log cards also display **cache tokens** inline (e.g., `R 148K`), making it easy to see how much prompt cache was hit.

---

## XII. Settings Page Overview

| Panel | Key Items |
|-------|-----------|
| **System** | Proxy address, **Statistics save interval (minutes)**, CORS whitelist, Responses WebSocket (default mode: passthrough/convert/off), SSE heartbeat, **Group health check** toggle |
| **Circuit Breaker** | Trigger threshold, base cooldown, max cooldown (exponential backoff) |
| **Passive Outlier Retirement** | Site projected channels only, disabled by default. Includes failure rate / min samples / consecutive failures / window parameters |
| **Channel Sync** | Auto-sync interval (hours), manual sync |
| **Model Pricing** | Auto-update interval (hours), manual update (data from models.dev) |
| **Site Automation** | Site auto-sync interval (hours), auto check-in interval (hours), manual full sync / full check-in |
| **Log Settings** | Enable history logs, log retention days, clear history logs |
| **Backup / Restore** | Export (optionally include logs/statistics; logs force ZIP), Import (incremental) |
| **API Keys** | Create/manage `sk-octopus-*`, supports **per-Key RPM rate limiting** (see [Chapter VIII](#viii-step-4-create-an-api-key-and-connect-clients)) |
| **Account Settings** | Change username / password |
| **Version Info** | Current/latest version, one-click update (backup first before updating) |
| **Appearance** | Theme (Light/Dark/System), Language |

### 12.13 Proxy Pool

In Settings you can enable a **Proxy Pool** to centrally manage reusable proxy configurations (supports `http / https / socks / socks5`). Sites, site accounts, and manual channels can all select the same proxy from the pool, avoiding redundant configuration. Proxy modes: Inherit / Direct / System Proxy / Proxy Pool. Proxies from older versions are automatically migrated to the pool.

---

## XIII. Frequently Asked Questions

> Common usage questions collected here.

### Q1. Default admin/admin login fails?
Most likely you **previously installed the upstream version and the `data` directory has leftover data** — the old credentials were preserved. Use the old password if you remember it; otherwise **clear the data directory and redeploy**. In a clean environment, admin/admin always works.

### Q2. Models exist, but creating a Key says no available models?
Because **you haven't created a group yet**. In Octopus, "group name = available model name". **Go to the Groups page and create a group first**, then you'll be able to select models when creating a Key.

### Q3. Site synced successfully, but channels show "no new / no usable Key"?
- If **no groups can be fetched** → most likely the site sync has errors (check sync status messages).
- If **groups exist but Keys are masked** → go to the Channels page and use **"Bulk Complete Key"** to paste in plaintext Keys.
- The relay site **never had a Key created for that group** → create a Key on the relay site first, or use "Quick Create Key" in Octopus.

### Q4. What exactly goes in Access Token?
- **New API**: The site's "Profile → Account Management → Security Settings → **System Access Token**" — **don't use username/password login**. New API also requires **Platform User ID**.
- **any (AnyRouter)**: A **cookie**, format: `session=MTc1234567890`. Platform type must be **AnyRouter**, not New API.
- **API Direct (OpenAI/Claude/Gemini)**: The corresponding API Key or Access Token, and select the correct **default protocol**.
- For all platforms except AnyRouter, use the System Access Token — **don't use cookies**.

### Q5. How to fill in the site URL?
**Domain only**, e.g., `https://wzw.pp.ua` — **don't include** `/v1` or other paths.

### Q6. One channel has multiple Keys — how to rotate them?
Use a **Manual Channel** and add multiple Keys inside it; then set the channel's **group "Session Affinity" to 0**. This way each Key's cumulative cost will tend to equalize — if each request costs about the same, it approximates rotation. Non-zero session affinity "sticks" to one Key. The manual channel panel shows each Key's usage cost — run a few requests and you'll see.

### Q7. Does 401 / 429 automatically switch Keys?
Manual channel with a Key returning **401 will not** retry other Keys — it switches directly to the next channel. **429** depends on whether the group has "Same Channel Retry" enabled — if so, it retries with the same channel and Key.

### Q8. Log shows a different model name than configured, and billing is 0?
Not a bug. The log shows the model name from the **upstream response's `model` field**. If upstream returns a date-suffixed variant (like `gpt-5.4-mini-2026-03-17`) and you haven't priced it, billing will be 0. Go to the **Pricing** page and **set a custom price** for that name. This is uncommon and acceptable.

### Q9. Sync error `decode response failed` / HTTP 401 / Cloudflare?
- **401**: Token/cookie expired — get a new one.
- **decode response failed**: Most likely the site URL has extra paths or the type is wrong. Confirm you've entered domain only and selected the correct type.
- **Cloudflare protection**: First verify the site is up in a browser (Ctrl+F5) — server down also shows CF. If the site loads normally, try configuring a proxy for the account.
- **Platform doesn't support check-in**: Not an error — platforms like sub2api that don't support check-in just skip it, API usage is unaffected.

### Q10. Import from All API Hub / Metapi fails?
- Confirm the import source is correct (don't swap All API Hub ↔ Metapi) and the JSON is complete.
- `cannot unmarshal string into ... version of type int`: A version field type issue from old Metapi versions — upgrade Metapi to a newer version and re-export.
- If imported sites still have issues, export data (without logs) and redeploy a clean instance, then re-import.

### Q11. What does Auto-Group Configuration do? Do I still need to create groups manually?
Yes, you still need to **create groups first**. Auto-group only assigns **newly added** upstream models to **existing** groups by name/regex matching (enable global default mode). When disabled, you must manually go to the group and click "Auto Add". It will not automatically sort all models into groups like Metapi.

### Q12. Same model — want both OpenAI Chat and Response to work?
No need to configure two. **Set it to Response**, and after adding to a group, Chat format requests still work (auto-converted). Delete unused Keys on the **upstream site**.

### Q13. Model fetching / conversations not going through proxy?
Model fetching and API requests both use the **site/account's configured proxy**. If they're not, check whether the account proxy is set to "Inherit Site Proxy" while the site has no proxy configured. If needed, directly select a proxy from the Proxy Pool on the account.

### Q14. Groups/Channels page is a bit slow?
Usually a brief lag on first load as the model list/groups are cached — it improves after caching. Heavy log data also increases load — control log retention days in Settings and clean up periodically.

### Q15. Vision/image models can't "see" images after distribution?
First confirm the **upstream provider itself** is configured correctly (the same image works with the original API). There have also been cases where "can't see images" was actually the model "thinking it can't see" — retrying a few times resolved it. Prioritize checking provider and endpoint format configuration.

---

## XIV. Performance & Operations

- **Resource usage**: ~50–60 MB memory; database file grows to roughly GB-scale over long-term use (~3GB over several months).
- **Statistics writes**: Statistics are held in memory first, then batch-written to the database at the "Statistics Save Interval".
  > ⚠️ **Always exit properly** (`Ctrl+C` or `SIGTERM` / `docker stop`) — **don't use `kill -9`**, or in-memory statistics will be lost.
- **Upgrades**: Safe to upgrade. **When log data is large (thousands of entries or more), upgrades run database migrations that may take several minutes** — this is normal. Recommend exporting via "Settings → Backup" before updating.
- **Background tasks**: Statistics persistence, model sync, price updates, site sync/check-in all run as scheduled background tasks (intervals configurable in Settings).

---

<div align="center">

For questions not covered in this guide, feel free to open an [Issue on GitHub](https://github.com/Hureru/octopus) or discuss on the [LinuxDO thread](https://linux.do/t/topic/2160826).

</div>
