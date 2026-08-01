---
status: accepted
---

# Run Cloudflare verification in the administrator browser

Octopus uses a paired administrator-side Verification Bridge to open temporary browser windows and complete interactive Cloudflare verification, following the effective request-replay mechanism demonstrated by `all-api-hub`. After pairing and one-time task-token validation, the bridge marks the task browser-ready. The original Site sync or check-in then sends same-origin API requests through a short-lived in-memory broker, and the extension executes them with `fetch` in the verified target tab's main browser world.

This keeps the browser Cookie jar, network path, User-Agent, and TLS fingerprint together. Octopus does not persist or receive browser Cookies on the primary path, and it rejects cross-origin URLs and browser-managed request headers. Pairing tokens remain hashed at rest and can be rotated, while broker request tokens are single-use and memory-only. The previous Cookie/User-Agent submission endpoint and manual Cookie import remain compatibility fallbacks; server-side Playwright or Chromium is not required.
