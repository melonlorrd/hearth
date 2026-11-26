# Decision & fix log

One entry per design decision or non-trivial fix. Newest first.
Keep entries short; link to code, tests or design.md sections where useful.

Template:

```
## YYYY-MM-DD — <short title>
- **Context / failure:** what broke, or what would break without this.
- **Decision:** what we chose.
- **Alternatives:** what else we considered, and why not.
- **Test:** the test or measurement that proves it.
```

---

## 2026-11-27 — M1 router testing approach
- **Context / failure:** the failure cases (hang, refuse, 503 with reason, cut
  mid-response) can't be reproduced reliably with a real Prometheus.
- **Decision:** router tests use `httptest` fake backends, one test per row of the
  `X-Hearth-Error` table, plus the success path and a backend dying mid-response.
  Timeouts use short real values (~100–200ms). Separately, `test/contract` runs a real
  Prometheus to check assumptions Hearth depends on.
- **Alternatives:** injectable clock / `testing/synctest`: fully predictable, but
  needs logic split from network code and doesn't cover real sockets; revisit if tests
  get slow or flaky. Real Prometheus for router tests: can't fail on command.
- **Test:** n/a (this is the testing approach).

## 2026-11-27 — Sticky routing deferred to M5
- **Context / failure:** the main reason for stickiness is that nodes lag each other by
  different amounts, so a dashboard bouncing between nodes shows points appearing and
  disappearing. In M1 there is no replication, so that reason doesn't apply yet.
- **Decision:** M1 prefers the local node, otherwise any ready node. Sticky hash by
  client comes in M5.
- **Alternatives:** do it in M1 as §4 originally said: extra work (client key, hash,
  failover rules) for no benefit yet.
- **Test:** M5.

## 2026-11-27 — Response buffering on the routing Hearth
- **Context / failure:** if the routing Hearth streams a response and the node dies
  halfway, Grafana already has a `200` and half the JSON; no retry is possible.
- **Decision:** the routing Hearth buffers the full response before sending anything,
  up to 8 MiB (rough default, to be tuned from measured response sizes). Above that,
  switch to streaming and stop retrying that query. The serving Hearth doesn't retry,
  so it streams.
- **Alternatives:** return an error above the limit (large queries never work); spill
  to disk (complex, disk I/O on the read path). Envoy uses the same "stop retrying
  above the buffer limit" approach.
- **Test:** backend dies mid-response → retried; response over 8 MiB → streamed, not
  retried.

## 2026-11-27 — X-Hearth-Error header and error table
- **Context / failure:** P3 timing out a heavy query and Hearth 3 saying "P3 not
  ready" both return `503`. The routing Hearth must retry the second but not the first,
  and the status code alone can't tell them apart.
- **Decision:** every error Hearth produces itself carries `X-Hearth-Error: <reason>`
  (`not_ready`, `backend_unreachable`, `body_too_large`, `no_candidates`,
  `retries_exhausted`, `deadline_exceeded`); no header = Prometheus's own answer.
  Retry decisions use the reason; the status is for humans. Header is kept on the way
  to the client for debugging. Error bodies use Prometheus's JSON error format so
  Grafana shows the message (plain text would show a generic error). Full table in
  design.md §4.
- **Alternatives:** status code only (ambiguous); `X-Hearth-Status` /
  `X-Hearth-Response` (unclear names: sound like they're on every response).
- **Test:** one router test per row of the table.

## 2026-11-27 — Retry timing and startup checks
- **Context / failure:** if Hearth's retries take longer than the client's timeout,
  Grafana has already shown an error and the retry helps nobody. A fixed per-attempt
  response timeout would break heavy queries that work directly against Prometheus.
- **Decision:** one clock per query. Defaults, all tunable: total deadline 29s
  (Grafana's 30s minus 1s margin), connect timeout 2s (survives one lost SYN, same as
  etcd's peer dial timeout), max attempts 3 (each node at most once, never more than the
  number of ready nodes). Response timeout is whatever is left of the deadline. Hearth
  refuses to start unless `connect timeout < deadline` and
  `max attempts × connect timeout ≤ half the deadline`.
- **Alternatives:** fixed 10s response timeout (breaks slow-but-valid queries); 3s
  connect timeout (same packet-loss protection as 2s, slower on dead nodes); 5 max
  attempts (more load during outages, no difference with 3 nodes); warn instead of
  refusing to start (bad config fails later, at query time).
- **Test:** startup with invalid settings is refused; retry tests in the router suite.

## 2026-11-27 — Max query body: 10 MiB constant, 413
- **Context / failure:** the routing Hearth reads the body as raw bytes to resend it on
  retry, so Go's form limit doesn't apply to it; without a limit, a client could make
  Hearth hold a 2 GB body.
- **Decision:** hard-coded 10 MiB, matching the limit Prometheus actually applies
  (Go `net/http` `parsePostForm` default; Prometheus sets none of its own). Over it →
  `413` with `X-Hearth-Error: body_too_large`. `test/contract` checks the Prometheus
  side and fails if an upgrade changes it.
- **Alternatives:** a setting (documented or hidden): every setting is a promise, and
  the main reason to lower it would make Hearth reject queries Prometheus accepts.
  Copy Prometheus's `400` (misleading "no expression found" message).
- **Test:** `test/contract/prometheus_test.go`; router test for an oversized body.

## 2026-11-26 — Read health: check defaults
- **Context / failure:** the router needs to stop sending queries to a node that can't
  answer. Too slow → many queries pay a retry; too fast → a GC pause marks a healthy
  node down and it flaps.
- **Decision:** poll `/ready` every 10s, 1s timeout, down after 3 consecutive failures,
  up after 1 success. All tunable. Worst-case detection ~30s, acceptable because
  every query is retried on another node anyway.
- **Alternatives:** faster checks (more flapping, more load). Higher success
  threshold (less flapping, slower recovery); revisit if flapping shows up.
- **Test:** TBD in the M1 plan.

## 2026-11-26 — Read health: local reachability, readiness from owner, shared data state
- **Context / failure:** if Hearth 1 can't reach Hearth 3 but Hearth 2 can, one
  cluster-wide "up/down" value for node 3 is wrong for one of them. But whether P3
  *has all the data* is one fact for everyone; if each router judged it alone,
  Grafana refreshes through different Hearths would show gaps, then no gaps.
- **Decision:** split by who can see it.
  - Is P3 ready? Only Hearth 3 checks (local `/-/ready`), and reports it via its `/ready`.
  - Can I reach Hearth 3? Each router decides for itself. Not in Raft.
  - Does P3 have all the data? The write leader decides; shared via Raft (M5/M6).
- **Alternatives:**
  - All shared via Raft, including reachability: wrong under one-sided link failures; every health
    flip is a Raft write; who runs checks and how to merge conflicting results is unclear.
  - All local: each router must poll every node's offsets; routers disagree on who has
    the data, so results flicker.
  - References for the chosen split: Vitess (vtgate health check + shared topology),
    Patroni + HAProxy, Kafka ISR.
- **Test:** TBD in the M1 plan (reachability); M5 (data state).

## 2026-11-26 — Reads go through the owning node's Hearth
- **Context / failure:** each Prometheus is bound to localhost (§1), so Hearth 1 can't
  reach P3 directly.
- **Decision:** a router forwards a query to the owning node's Hearth, which passes it
  to its local Prometheus. Local node is used directly with no extra hop.
- **Alternatives:** let routers talk to every Prometheus directly for M1. Smaller M1,
  but contradicts §1 and would be thrown away later along with its retry logic and
  tests.
- **Test:** TBD in the M1 plan.

## 2026-11-25 — Split agent instructions from design
- **Context / failure:** a single long AGENTS.md loaded the whole design into every
  session, bloating context.
- **Decision:** AGENTS.md holds working rules + hard invariants only; full design lives
  in docs/design.md; decisions are logged here.
- **Alternatives:** one file per ADR under docs/decisions/ — more overhead than a
  single log for a solo learning project.
- **Test:** n/a.
