# 13 — Per-Device MQTT Topic: Design Research

> **Question.** Is `avandab/telemetry/devices/{imei}/gps` (one topic per GPS device) the
> right design, vs a single shared topic, a per-tenant/per-fleet topic, or a
> per-device topic with suffix?
>
> Every factual claim below cites its owner inline (repo `file:line` or URL).
> Items that cannot be settled from sources are marked **VERIFY**.

---

## 1. Verdict (up front)

**Yes — keep the per-device topic. It is the right call for this repo at its
scale (1k → 5k devices, single-node, Mosquitto, zero-cost constraint).**

- Identity lives in the topic (`config/mosquitto.conf:12-13`), so the broker can
  enforce per-device publish ACL with one `pattern` line instead of trusting the
  payload
  ([mosquitto.conf man page](https://mosquitto.org/man/mosquitto-conf-5.html)).
- The backend holds **one** TCP session with **one** wildcard subscription
  (`internal/mqttservice/mqtt.go:63`), so 25k device-topics cost no extra
  connections — Paho multiplexes many topic filters over a single connection
  ([MqttClient javadoc](https://eclipse.dev/paho/files/javadoc/org/eclipse/paho/client/mqttv3/MqttClient.html)).
- The spoof guard (`internal/telemetry/mqtt_ingest.go:47-51`) only works because
  topic-IMEI and payload-IMEI are two independent channels; a shared topic
  collapses them into one attacker-controlled field.
- Change only when: (a) fleet passes ~25k devices on one broker and wildcard
  fan-in is profiler-proven hot (**VERIFY** §6.1), (b) multi-tenant topic
  isolation becomes a billing/compliance requirement, or (c) the broker moves to
  managed AWS IoT where per-thing policy variables replace the ACL file
  ([thing policy variables](https://docs.aws.amazon.com/iot/latest/developerguide/thing-policy-variables.html)).

---

## 2. Alternatives compared

| Criterion | (a) Per-device topic (current) | (b) Single shared topic, IMEI in payload only | (c) Per-tenant / per-fleet topic |
|---|---|---|---|
| **Spoof resistance / ACL enforceability** | ✅ Broker-enforceable: `pattern write avandab/telemetry/devices/%u/gps` binds authenticated username to topic level ([mosquitto.conf man](https://mosquitto.org/man/mosquitto-conf-5.html)); app cross-checks topic vs payload IMEI (`internal/telemetry/mqtt_ingest.go:47-51`) | ❌ No broker check possible — any credential publishes any IMEI; sole defense is app-level payload inspection, which the attacker fully controls | ⚠️ Narrows blast radius to tenant, but any device in the tenant can still spoof any sibling IMEI in that tenant's topic |
| **Routing / filtering cost at 1k → 5k → 25k** | One wildcard sub `devices/+/gps` (`internal/mqttservice/mqtt.go:63`); Mosquitto matches via per-level hash + precomputed `+`/`#` hashes ([src/subs.c](https://github.com/eclipse-mosquitto/mosquitto/blob/master/src/subs.c)); single sub scales with message rate, not topic cardinality | Zero match cost (one exact sub) but every message pays full JSON parse + DB device lookup before identity is known (`internal/telemetry/ingest.go:197-200`) | Same wildcard mechanism as (a), one sub per tenant; marginally more subs, same match complexity |
| **Ordering / dedup** | Unchanged: MQTT QoS 1 gives at-least-once per stream ([MQTT 3.1.1 §4.3](https://docs.oasis-open.org/mqtt/mqtt/v3.1.1/csd01/mqtt-v3.1.1-csd01.html)); replays deduped by `mqtt:{seq}` via partial unique index (`internal/telemetry/ingest.go:580-598`); stale frames lose via `excluded.device_time >` guard (`internal/telemetry/ingest.go:653-683`) | Same pipeline, but `seq` namespaces collide across devices sharing one topic — dedup key must become composite (imei, seq) or replays leak through (**VERIFY** §6.4) | Same as (a) within a tenant |
| **Debuggability / per-device metrics** | ✅ Topic = device id: `mosquitto_sub -t devices/<imei>/gps` traces one truck; per-IMEI log fields already emitted (`internal/telemetry/mqtt_ingest.go:35-42`); matches AWS guidance to embed routing identity in topic namespace ([IoT Lens](https://docs.aws.amazon.com/wellarchitected/latest/iot-lens/identity-and-access-management.html)) and HiveMQ "specific topics, not general ones" ([MQTT Essentials Pt.5](https://www.hivemq.com/blog/mqtt-essentials-part-5-mqtt-topics-best-practices/)) | ❌ One firehose — isolating a device needs payload grep on every message; broker `$SYS` counters cannot split by device | ⚠️ Tenant-level split only; still needs payload grep inside a fleet |
| **Broker resource cost (topics)** | ~Zero: MQTT topics are created implicitly on first publish — "the broker accepts each topic without any prior" setup ([HiveMQ Pt.5](https://www.hivemq.com/blog/mqtt-essentials-part-5-mqtt-topics-best-practices/)); topic string ~35 B ≪ 65535 B UTF-8 limit ([MQTT 3.1.1 §1.5.3](https://docs.oasis-open.org/mqtt/mqtt/v3.1.1/csd01/mqtt-v3.1.1-csd01.html)); IMEIs are 15-digit ([docs/02-HARDWARE-GPS-TELEMETRY.md:41](docs/02-HARDWARE-GPS-TELEMETRY.md)) | Same zero topic cost, one topic node | Same as (a) |
| **Ops burden** | One static `pattern` ACL line covers all present + future devices; no ACL edit on onboarding (pattern applies to all users, [acl-file plugin docs](https://mosquitto.org/documentation/plugins/acl-file/)); device lifecycle stays in DB (`internal/telemetry/device_store.go:13-19`, `internal/telemetry/devices.go:345-562`) | Least broker config, most app burden: every frame needs authN lookup before trust | ACL per tenant (N lines for N tenants, not N devices); needs tenant→topic map kept in sync with onboarding |
| **Failure modes** | Wildcard-subscriber fan-in under load is the known MQTT scaling risk — broad `#` subs degrade throughput/latency first in authenticated high-load tests ([Uidaho 2025 study](https://verso.uidaho.edu/esploro/outputs/journalArticle/Experimental-Evaluation-of-Wildcard-Subscription-Impact/996989849901851)); here scope is one narrow `+/gps` filter with a single subscriber, not `#` | **Noisy neighbor**: one chatty/compromised device's flood shares the only subscription — cannot unsubscribe, throttle, or ACL-kill it without dropping the whole fleet; poison IMEI strings hit the DB lookup path directly | Flood contained to one tenant's topic; cross-tenant noisy neighbor eliminated, intra-tenant remains |

Suffix variant (`devices/{imei}/gps/{signal}`): rejected — multiplies wildcard
breadth for no ACL gain; the payload already carries signal separation
(`internal/telemetry/mqtt_ingest.go:105-132`), and HiveMQ warns against
over-broadening subscriptions at throughput
([MQTT Essentials Pt.5](https://www.hivemq.com/blog/mqtt-essentials-part-5-mqtt-topics-best-practices/)).

---

## 3. Evidence (per source)

### 3.1 Repo — broker wiring

- `config/mosquitto.conf:3-9` — TCP `:1883` for GPS hardware, WebSocket `:9001`
  for the mobile app; mirrored in `docker-compose.yml:23-32`
  (`eclipse-mosquitto:2`, same two ports).
- `config/mosquitto.conf:11-14` — dev runs `allow_anonymous true`; the comment
  is the design contract: production must set `allow_anonymous false` and ACL so
  "each device may only publish to `avandab/telemetry/devices/{own-imei}/gps`".
  **Not yet enforced — VERIFY §6.2.**
- `internal/mqttservice/mqtt.go:38` — backend connects once as
  `avandab_backend_server` (single session, not one per device).
- `internal/mqttservice/mqtt.go:63-68` — one canonical subscribe
  `avandab/telemetry/devices/+/gps` at QoS 1 (handler or log-only).
- `internal/mqttservice/mqtt.go:70-72` — legacy mobile topic
  `avandab/telemetry/drivers/+/gps` kept as **log-only** bridge pending mobile
  retrofit; the app still publishes there
  (`mobile/src/services/mqtt.ts:139-151`, QoS 1 at `:149`).

### 3.2 Repo — ingest path (why topic identity matters)

- `internal/mqttservice/mqtt.go:20-25` — Paho "does not expose the publishing
  client's username in the message callback (Spec 01 gotcha D8 #1)": topic IMEI
  is extracted and validated against payload IMEI; "Broker ACL (Mosquitto
  acl_file) provides connection-level spoof protection."
- `internal/telemetry/mqtt_ingest.go:135-143` — `extractIMEIFromTopic` accepts
  exactly 5 levels `avandab/telemetry/devices/{imei}/gps`, else `""`.
- `internal/telemetry/mqtt_ingest.go:33-51` — invalid topic dropped (`:35`);
  bad JSON dropped (`:42`); topic/payload IMEI mismatch dropped + audited via
  `auditSpoof` (`:47-51`, writer at `internal/telemetry/ingest.go:741-748`).
- `internal/telemetry/mqtt_ingest.go:79-82` — frame stamped `Provider: "own"`,
  `ProviderMsgID: mqtt:{seq}`; `:84-88` optional `device_time` parse;
  `:91-94` handed to `IngestAsync`.
- `internal/telemetry/ingest.go:168-183` — `IngestAsync`: nil queue → sync;
  saturated queue → **sync fallback** (backpressure, never silent drop).
- `internal/telemetry/ingest.go:197-214` — pipeline: unknown IMEI → quarantine
  (`QuarantineReasonUnknownDevice`); non-`active` → quarantine (retired /
  quarantined reasons); lifecycle states at
  `internal/telemetry/device_store.go:13-19`
  (`inventory → assigned → active → retired`, plus `quarantined`), transitions in
  `internal/telemetry/devices.go:345-562`, reasons in
  `internal/telemetry/quarantine.go:13-18`.
- `internal/telemetry/ingest.go:575-598` — dedup: `ON CONFLICT DO NOTHING` on
  `provider_msg_id`; empty id stored as NULL, never deduped (Decision D2).
- `internal/telemetry/ingest.go:653-683` — out-of-order guard: live-map upsert
  applies only when `excluded.device_time >` stored (reconnect dumps of ~1,000
  offline points go to history, map never jumps back; also
  `docs/02-HARDWARE-GPS-TELEMETRY.md:65`).
- `internal/telemetry/ingest.go:428-476` — parked/moving deadband dedup
  (~70% storage saving claim at `docs/02-HARDWARE-GPS-TELEMETRY.md:66`).

### 3.3 Repo — scale envelope (why cost arguments hold to 25k)

- `docs/02-HARDWARE-GPS-TELEMETRY.md:14-31` — MQTT `:1883` feeds the same async
  queue (cap 10,000) + 4-worker pool as TCP/REST; queue defaults at
  `internal/telemetry/async_queue.go:35-52`, non-blocking push `< 0.1ms` at
  `:54-56`.
- `docs/02-HARDWARE-GPS-TELEMETRY.md:74-78` — thresholds: 1–1k trucks ≈ 100
  req/s; 1k–5k ≈ 500 req/s (`ulimit -n 65535`); 5k–25k ≈ 2,500 req/s then
  TimescaleDB/Postgres migration. One wildcard subscription's match cost is
  noise against this DB/queue budget — **VERIFY §6.1** before doubting it.

### 3.4 MQTT spec (OASIS, first-party)

- Topic Name vs Topic Filter; wildcards `+` (single level) / `#` (multi-level)
  exist **only in subscriptions** — "The Topic Name in the PUBLISH Packet MUST
  NOT contain wildcard characters" (`[MQTT-3.3.2-2]`), so devices publish exact
  `devices/{imei}/gps` while the backend subscribes `devices/+/gps`. Source:
  [MQTT 3.1.1 §4.7 / §3.3.2](https://docs.oasis-open.org/mqtt/mqtt/v3.1.1/csd01/mqtt-v3.1.1-csd01.html);
  same split restated in [MQTT 5.0 §4.7](https://docs.oasis-open.org/mqtt/mqtt/v5.0/os/mqtt-v5.0-os.html).
- All topic strings are UTF-8 with a 2-byte length prefix: max **65535 bytes**
  ([MQTT 3.1.1 §1.5.3](https://docs.oasis-open.org/mqtt/mqtt/v3.1.1/csd01/mqtt-v3.1.1-csd01.html)).
  Current topic ≈ 30 + 15 + 4 ≈ 49 B — three orders of magnitude of headroom.
- QoS 1 = "at least once … duplicates may occur" — the protocol *requires* the
  app-side dedup the repo already has (`ingest.go:575-598`). Source: same §4.3.
- Session = per-client state; one backend session carries many subscriptions —
  no per-topic connection cost by construction. Paho confirms: one `MqttClient`
  over one `tcp://`/`ssl://` connection, `subscribe(String[])` multiplexes many
  filters, optionally in a **single SUBSCRIBE** packet
  ([MqttClient](https://eclipse.dev/paho/files/javadoc/org/eclipse/paho/client/mqttv3/MqttClient.html),
  [paho-mqtt Python `subscribe`](https://eclipse.dev/paho/files/paho.mqtt.python/html/client.html)).

### 3.5 Mosquitto (first-party)

- `acl_file` / acl-file plugin: `pattern [read|write|readwrite] <topic>` with
  `%c` = client id, `%u` = username; "The substitution pattern must be the only
  text for that level of hierarchy"; pattern ACLs apply to all users; `+`/`#`
  allowed in entries; leading `#` = comment
  ([man page](https://mosquitto.org/man/mosquitto-conf-5.html),
  [plugin docs](https://mosquitto.org/documentation/plugins/acl-file/)).
- Canonical community pattern for exactly this design: admin gets
  `topic readwrite v1/+/data/#`, every device gets `pattern readwrite
  v1/%u/data/#` with per-device username/password, plus an explicit warning to
  **deny `+`/`#` in usernames** or ACLs can be bypassed
  ([mosquitto #1610](https://github.com/eclipse-mosquitto/mosquitto/issues/1610)).
- Routing internals: per-level hash tables with precomputed `+`/`#` hashes —
  wildcard match is hash lookups per level, not a scan over topics
  ([src/subs.c](https://github.com/eclipse-mosquitto/mosquitto/blob/master/src/subs.c)).

### 3.6 AWS IoT Core / HiveMQ (industry first-party best practice)

- **Identity in topic**: "Use a consistent naming convention that aligns MQTT
  topics with your device identity … use the same client identifier for the
  device as the IoT Thing Name … include relevant routing information in the
  topic namespace" 
  ([IoT Lens](https://docs.aws.amazon.com/wellarchitected/latest/iot-lens/identity-and-access-management.html)).
- **One identity per device, least privilege**: X.509 per device, "single
  identity per device", scope publish/subscribe to "an identified and fixed set
  of topics", client-ID-scoped connect to avoid session takeover
  ([security best practices](https://docs.aws.amazon.com/iot/latest/developerguide/security-best-practices.html)).
- **One policy for the fleet**: thing policy variables (`iot:Connection.Thing.ThingName`,
  …) let a single policy grant each device its own topic space instead of N
  per-device policies — the managed equivalent of Mosquitto's one `pattern`
  line
  ([thing policy variables](https://docs.aws.amazon.com/iot/latest/developerguide/thing-policy-variables.html),
  [certificate policy examples](https://docs.aws.amazon.com/iot/latest/developerguide/certificate-policy-examples.html)).
- HiveMQ: "include the client ID of the publishing client in the permission …
  restricted to publish only to topics prefixed with its own client ID" (e.g.
  `client123/temperature`)
  ([authorization fundamentals](https://www.hivemq.com/blog/mqtt-security-fundamentals-authorization/));
  file-RBAC extension supports the same via `data/${{clientid}}/#` with
  `${{clientid}}` / `${{username}}` substitution
  ([extension README](https://github.com/hivemq/hivemq-file-rbac-extension/blob/master/README.adoc));
  roles (`sensor` → `factory/+/telemetry`) show the per-tenant variant when
  grouping is needed
  ([RBAC docs](https://docs.hivemq.com/hivemq-platform/connect/rbac.html)).
- HiveMQ topic hygiene: "Use specific topics, not general ones" (per-sensor
  topics, not one generic blob); no leading `/`; never subscribe the backend to
  bare `#` at throughput — use a broker extension instead
  ([MQTT Essentials Pt.5](https://www.hivemq.com/blog/mqtt-essentials-part-5-mqtt-topics-best-practices/)).
  The repo obeys all three: per-device topics, no leading slash, one narrow
  `+/gps` filter.

---

## 4. Risks / limits of the current design

1. **ACL is documented, not deployed.** `allow_anonymous true`
   (`config/mosquitto.conf:14`) means today *any* client can publish any IMEI's
   topic; only the app-level spoof guard (`mqtt_ingest.go:47-51`) stands guard.
   Without the ACL, per-device topics give debuggability but **no** spoof
   resistance. → §6.2.
2. **Username/credential model unproven on constrained trackers.** `pattern
   %u` needs per-device MQTT usernames (+ passwords/certs); GT06/AIS-140
   hardware speaks raw TCP today (`docs/02:38-52`), so MQTT-credential
   provisioning for own-hardware is a future onboarding step, not a free win.
   → §6.3.
3. **Wildcard fan-in ceiling unmeasured.** Single `+/gps` sub is cheap per
   message, but total 2,500 msg/s × (match + QoS1 ACK + queue + DB) on one node
   at 25k is still a single-broker bet shared with the whole pipeline. → §6.1.
4. **No per-topic broker metrics out of the box.** Mosquitto `$SYS` gives
   broker totals, not per-IMEI rates — per-device visibility comes from app logs
   (`mqtt_ingest.go:35-42`) and DB (`last_seen_at`, quarantine), not the broker.
   Managed IoT (CloudWatch per topic/policy) would be the upgrade path.
5. **Legacy driver topic is the weaker sibling.** `drivers/+/gps`
   (`mobile/src/services/mqtt.ts:141`) keys on *driverId*, not a hardware
   secret; its log-only backend status (`mqtt.go:70-72`) is correct — never
   promote it to the trusted path without the same ACL + spoof-guard treatment.

---

## 5. Recommendation

1. **Keep `avandab/telemetry/devices/{imei}/gps`.** No migration: (b) destroys
   broker-enforceable identity, (c) adds tenant-sync ops for no security gain at
   current scale, suffix-split adds wildcard breadth for nothing.
2. **Close the ACL gap (the one change that makes the verdict true):**
   `allow_anonymous false` + password file + single line
   `pattern write avandab/telemetry/devices/%u/gps` (devices) and a backend
   superuser `topic read avandab/telemetry/devices/+/gps`; reject `+`/`#` in
   device usernames per ([mosquitto #1610](https://github.com/eclipse-mosquitto/mosquitto/issues/1610)).
   Bind username = IMEI at provisioning ( alongside `ActivateDevice`
   secret issuance, `devices.go:491-527`).
3. **Keep the app spoof guard regardless** (`mqtt_ingest.go:47-51`) — defense in
   depth for the anonymous-dev window and any ACL misconfiguration.
4. **Revisit only on triggers:** single-broker CPU/match proven hot at ≥25k
   (then: shared subs / broker extension / sharded brokers per HiveMQ guidance);
   tenant-isolated topics when compliance/billing demands; AWS IoT thing
   policies when leaving self-hosted Mosquitto.

---

## 6. Needs verification at implementation (VERIFY)

- **6.1 Load-prove the 25k claim.** Replay 2,500 msg/s through
  `eclipse-mosquitto:2` (`docker-compose.yml:25`) with the single `+/gps` sub +
  full pipeline; record broker CPU, QoS1 ACK latency, queue depth
  (`async_queue.go:54-72`), worker lag. If match cost dominates, consider MQTT 5
  shared subscriptions or a broker-side ingest extension before re-topicing.
- **6.2 Prod ACL cutover.** Verify `allow_anonymous false` + `acl_file` with
  the `%u` pattern denies cross-IMEI publish (device A → device B's topic) and
  that the backend superuser still receives all; add a red test (publish as A
  to B, expect CONNACK/denial, frame never in `telemetry_raw_events`).
- **6.3 Tracker credential provisioning.** Verify own-GPS hardware can hold a
  per-device MQTT username/secret (or cert) flashed alongside the
  `ActivateDevice` raw secret (`devices.go:488-527`); fall back to gateway-level
  credentials + app quarantine (`quarantine.go:64-77`) where it cannot.
- **6.4 Shared-topic dedup key (only if (b) ever adopted).** Verify
  `provider_msg_id` becomes composite `(imei, seq)` — bare `mqtt:{seq}`
  (`mqtt_ingest.go:80`) collides across devices on one topic and the partial
  unique index (`ingest.go:580-598`) would false-dedupe.

---

## 7. Source index

- Repo: `config/mosquitto.conf`, `docker-compose.yml:23-32`,
  `internal/mqttservice/mqtt.go`, `internal/telemetry/mqtt_ingest.go`,
  `internal/telemetry/ingest.go:168-183,197-227,428-476,575-598,653-683,741-748`,
  `internal/telemetry/device_store.go:12-26`,
  `internal/telemetry/devices.go:344-562`,
  `internal/telemetry/quarantine.go`, `internal/telemetry/async_queue.go`,
  `docs/02-HARDWARE-GPS-TELEMETRY.md`, `mobile/src/services/mqtt.ts`.
- Spec/vendor: [MQTT 3.1.1](https://docs.oasis-open.org/mqtt/mqtt/v3.1.1/csd01/mqtt-v3.1.1-csd01.html) ·
  [MQTT 5.0](https://docs.oasis-open.org/mqtt/mqtt/v5.0/os/mqtt-v5.0-os.html) ·
  [mosquitto.conf man](https://mosquitto.org/man/mosquitto-conf-5.html) ·
  [acl-file plugin](https://mosquitto.org/documentation/plugins/acl-file/) ·
  [mosquitto #1610](https://github.com/eclipse-mosquitto/mosquitto/issues/1610) ·
  [subs.c](https://github.com/eclipse-mosquitto/mosquitto/blob/master/src/subs.c) ·
  [AWS security best practices](https://docs.aws.amazon.com/iot/latest/developerguide/security-best-practices.html) ·
  [IoT Lens IAM](https://docs.aws.amazon.com/wellarchitected/latest/iot-lens/identity-and-access-management.html) ·
  [thing policy variables](https://docs.aws.amazon.com/iot/latest/developerguide/thing-policy-variables.html) ·
  [certificate policy examples](https://docs.aws.amazon.com/iot/latest/developerguide/certificate-policy-examples.html) ·
  [HiveMQ authorization](https://www.hivemq.com/blog/mqtt-security-fundamentals-authorization/) ·
  [HiveMQ topics best practices](https://www.hivemq.com/blog/mqtt-essentials-part-5-mqtt-topics-best-practices/) ·
  [HiveMQ RBAC](https://docs.hivemq.com/hivemq-platform/connect/rbac.html) ·
  [Paho MqttClient](https://eclipse.dev/paho/files/javadoc/org/eclipse/paho/client/mqttv3/MqttClient.html).
