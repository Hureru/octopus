# Octopus

Octopus aggregates and routes requests to upstream LLM services while giving operators a shared language for managing access, providers, and connectivity.

## Language

**Proxy Pool**:
A global collection of named proxy configurations that can be reused by sites, site accounts, and channels.
_Avoid_: Per-site proxy list, channel proxy list

**Proxy Configuration**:
A single named proxy endpoint stored in the **Proxy Pool**.
_Avoid_: Raw proxy string, proxy address

**Proxy Usage Mode**:
The selected way a site, site account, or channel connects to upstream services: direct, system proxy, proxy pool, or inherited site setting for site accounts.
_Avoid_: Proxy enabled flag

**Managed Channel**:
A channel derived from a site account rather than configured independently.
_Avoid_: Auto-created ordinary channel

## Relationships

- A **Proxy Pool** contains zero or more **Proxy Configurations**.
- A **Proxy Configuration** may be selected by zero or more sites, site accounts, or channels.
- A site account may inherit the **Proxy Usage Mode** of its site.
- A **Managed Channel** follows the proxy choice resolved from its source site account.

## Example dialogue

> **Dev:** "When configuring a site account, should the operator paste a proxy address?"
> **Domain expert:** "No — they choose a **Proxy Configuration** from the **Proxy Pool**."

## Flagged ambiguities

- "代理" can mean either a raw proxy address or a reusable **Proxy Configuration** — resolved: user-managed reusable proxies are called **Proxy Configurations** and live in the **Proxy Pool**.
- A boolean "proxy enabled" flag does not distinguish direct, system proxy, proxy pool, and inherited behavior — resolved: connection choice is expressed as **Proxy Usage Mode**.
