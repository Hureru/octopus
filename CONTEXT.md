# Octopus API Aggregation

Octopus manages upstream LLM access by grouping provider credentials and exposed models into routable channels.

## Language

**Ordinary Channel**:
A manually managed upstream route with its own credentials, available models, and request behavior.
_Avoid_: 普通渠道 when discussing code identifiers; use Ordinary Channel in domain docs.

**Site Channel**:
A managed channel view derived from a site account and one of its model groups.
_Avoid_: 手动渠道, ordinary channel

**Site**:
An upstream provider portal that can contain one or more accounts, credentials, model groups, and models.
_Avoid_: provider when referring to a configured portal instance

**Site Account**:
An account under a site whose credentials and model availability can be synchronized.
_Avoid_: user, key

**Site Model Group**:
A named availability bucket within a site account that determines which credentials and models belong together.
_Avoid_: model group, channel group

**Routing Group**:
A user-facing model routing pool that contains channel-model entries for load balancing and failover.
_Avoid_: site group, model group

**Automatic Routing Group Assignment**:
A channel setting that adds matching channel models into existing routing groups.
_Avoid_: automatic site grouping

**Global Projected Channel Automatic Assignment**:
A system setting that makes all projected channels use fuzzy automatic routing group assignment.
_Avoid_: global site grouping

**Custom Site Model**:
A site account model added by a user rather than discovered by site synchronization.
_Avoid_: manual model, custom channel model when referring to site accounts

**Model Route Type**:
The API protocol family through which a site model should be exposed.
_Avoid_: channel type when discussing site models

**Projected Channel**:
The exposed channel produced from a site account and site model group.
_Avoid_: generated ordinary channel

## Relationships

- A **Site** has one or more **Site Accounts**.
- A **Site Account** has zero or more **Site Model Groups**.
- A **Site Model Group** may produce one or more **Projected Channels**.
- A **Site Channel** presents **Projected Channels** for site-account site model groups.
- A **Routing Group** contains channel-model entries from **Ordinary Channels** or **Projected Channels**.
- **Automatic Routing Group Assignment** targets **Routing Groups**, not **Site Model Groups**.
- **Global Projected Channel Automatic Assignment** overrides per-**Projected Channel** automatic assignment while enabled.
- A **Custom Site Model** belongs to exactly one **Site Account** and **Site Model Group**.
- A **Custom Site Model** has one **Model Route Type**.
- A **Projected Channel** exposes site models with the same **Model Route Type** within a **Site Model Group**.
- An **Ordinary Channel** is managed directly and is not derived from a **Site Account**.

## Example dialogue

> **Dev:** "If I add a custom model in a Site Channel, does it become a custom model on the projected channel?"
> **Domain expert:** "No — it becomes a Custom Site Model, then projection exposes it through the matching Projected Channel."

## Flagged ambiguities

- "channel" can mean **Ordinary Channel**, **Site Channel**, or **Projected Channel**; use the precise term when discussing behavior.
- "group" can mean **Routing Group** or **Site Model Group**; automatic assignment refers to **Routing Groups**.
