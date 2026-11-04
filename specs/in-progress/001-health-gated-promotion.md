---
status: completed
approved: "2026-11-03T22:23:03Z"
generating: "2026-11-03T22:39:41Z"
prompted: "2026-11-03T22:39:41Z"
branch: dark-factory/health-gated-promotion
---

## Summary

- A dev apply that lands **and passes its post-apply health check** promotes the same component to the sun cluster on its own — no operator command in between.
- The promotion leaves sun through the sun HTTPS command gateway, carrying an identifier the service chose, and the service records that identifier in its own result record.
- A dev apply that fails (either the make step or the health check) promotes nothing and records a **promotion refusal** that names the withheld stage — a record a reader can tell apart from the apply/rollback failure entry that already exists.
- One decision point produces both outcomes: promote and withhold. There is no second path that can decide to promote.
- The nuke prod approval gate is consumed as-is. This work neither rebuilds it nor routes around it, and prod is never a promotion target.

## Problem

The service already refuses to report a broken dev rollout as success: it waits for the rollout and restores the previous images when the check fails. What it does not do is anything with a *good* result. A component that passed its dev health check still sits on dev until someone remembers to run the same apply against sun by hand — and that hand-run leaves no record tying the sun deploy to the dev one. So the healthy path is the one nobody can prove, while the unhealthy path is the one with a durable trace.

Two facts make the gap worse than it looks. The sun cluster's Kafka listener is internal-only, so a dev-side promotion cannot be a Kafka publish; it has to enter through sun's HTTPS command gateway. And the gateway mints its own request identifier, so the only value that survives the crossing is the event identifier the caller puts in the URL path. Any design that assumes a shared request identifier correlates the two sides will look correct in tests and correlate nothing in production.

## Goal

After this work, a successful dev apply of a component is followed by a sun apply of the same component issued by the service itself, and both sides of that crossing carry one and the same service-chosen identifier. A failed dev apply is followed by an explicit, durable refusal naming sun as withheld — never by silence, and never by a record an operator would confuse with the rollback failure entry. The prod approval gate behaves exactly as it does today.

## Non-goals

- Rollback of a failed sun or prod apply. That is a separate task; this spec only covers the dev-side decision and the crossing.
- Canary, progressive or percentage traffic. The promotion is one full apply.
- Rebuilding, widening or duplicating the prod approval mechanism. The existing approver and approval payload are the only gate into prod.
- Sun-to-prod promotion. Only dev promotes. Prod remains an operator-issued, approved command.
- Automatic retry, backoff or queueing of a failed gateway delivery. One attempt per promotion; the record says what happened.
- A promotion metric, dashboard or alert. Not requested; the record is the evidence.
- Do NOT add an enable/disable switch for promotion, a configurable promotion target, or a configurable promotion chain — the promotion is the behavior this spec ships; an escape hatch that disables the Goal is itself a regression.
- Do NOT add a second approval field or a second approver for the promotion path — the existing gate is the only one.

## Assumptions

- The dev instance can reach the sun gateway over HTTPS. No egress rule is changed by this work; if the path is blocked, that is a deployment prerequisite, not a code behavior.
- The sun gateway accepts `POST /api/1.0/command/k8s-apply-v1/apply/<eventID>` with the apply command data as the body, and carries `<eventID>` through as the command's own event identifier. This is the established gateway contract, not something this spec changes.
- The sun deploy checkout is synced independently of the dev checkout. A promotion asserts the component, not the commit; sun's existing `guardMaster` remains the only revision check on that side.
- The sun `nuke-apply` component is **repointed to the `bborbe/k8s-apply` image before the sun Post-Deploy criterion is verified**. Today it pins `docker.io/bborbe/nuke-apply:v0.11.0`, a different image on an unrelated tag stream, so the deploy gate cannot match this repo's tag until the repoint lands. The repoint is a deploy-repo change, not part of this spec; a promotion issues an *apply*, which never repoints an image.

## Acceptance Criteria

- [ ] `make precommit` exits 0 in the repository — evidence: exit code.
- [ ] A dev apply that lands and passes its health check produces exactly one promotion record with `kind: promotion`, `verdict: promote`, `to_stage: sun`, `component` equal to the applied path, `delivery: accepted`, and a non-empty `event_id` — evidence: the captured record message.
- [ ] The promotion record's `event_id` differs from the event identifier of the dev apply command that caused it — evidence: the two captured values compared, not equal.
- [ ] The promotion request the service issues to the gateway is a POST whose path ends in that same `event_id` and whose body is `{"path":"<component>"}` — evidence: the request captured by the fake gateway.
- [ ] A dev apply whose health check fails produces exactly one promotion record with `verdict: withhold`, `to_stage: sun`, and a `reason` naming the failed health check — evidence: the captured record message.
- [ ] The same failed dev apply still produces the pre-existing Failure result entry carrying the rollback text, and the promotion record is distinguishable from it without reading prose — the promotion record carries `kind: promotion` and no `success` field; the Failure entry carries `success: false` and no `kind` field — evidence: both captured messages.
- [ ] A dev apply whose make step fails produces exactly one promotion record with `verdict: withhold` and a `reason` naming that failure — evidence: the captured record message.
- [ ] On `verdict: withhold`, the gateway receives zero requests — evidence: fake-gateway request count is 0 in the withhold test window.
- [ ] A prod instance produces no promotion record for any apply, and a prod apply without an approval still fails with the pre-existing refusal text — evidence: zero promotion records captured plus the failure message matching the existing approval refusal.
- [ ] A dev instance started without a sun gateway URL configured exits non-zero and its output names the missing setting — evidence: exit code plus the stderr/stdout line.
- [ ] A sun instance and a prod instance both start successfully with no sun gateway URL configured — evidence: exit code 0 plus the listening log line for each.
- [ ] The promote and withhold verdicts are produced by one decision entry point, exercised by a table test with one row per verdict, each row asserting the verdict value and the target stage — evidence: test output listing both row names, exit 0.
- [ ] The captured promotion record contains no `"operation"` key — evidence: `grep -c '"operation"' <captured record>` returns 0, so a result consumer skips it by operation lookup instead of unmarshalling it as a failed apply.
- [ ] When the gateway returns 500, the dev result topic still carries the pre-existing Success entry for that apply and the promotion record carries `delivery: failed` — evidence: both captured messages, with the Success entry present.
- [ ] **Post-Deploy (Rung-2):** after a real dev apply of a component, the dev result topic carries a promotion record with `verdict: promote`, and the dev pod log contains a line with that record's `event_id` — evidence: kafka message plus log line.
  - `deploy_check:` `kubectlnukedev -n dev get deploy k8s-apply -o jsonpath='{.spec.template.spec.containers[0].image}' | awk -F: '{print $NF}'`
  - `deploy_target:` `$(git describe --tags --abbrev=0)`
- [ ] **Post-Deploy (Rung-3):** the sun `nuke-apply` pod log contains a line carrying the same `event_id` as the dev-side promotion record — evidence: log line.
  - `deploy_check:` `kubectlsun -n agent get deploy nuke-apply -o jsonpath='{.spec.template.spec.containers[0].image}' | awk -F: '{print $NF}'`
  - `deploy_target:` `$(git describe --tags --abbrev=0)`

## Verification

### Container-executable (runs inside the YOLO container at prompt time)

```
make precommit
make test
```

The unit and integration tests carry the first fourteen acceptance criteria: the fake gateway records every request, the record publisher captures every record, and the table test exercises both verdicts through the single decision entry point.

### Operator-executable (runs on the host after PR merge)

```
kubectlnukedev -n dev logs deploy/k8s-apply --since=30m | grep <event_id>
kafka-topic-reader -topic develop-k8s-apply-v1-result --since=30m
```

Sun-side evidence — the wrapper is `kubectlsun`, the namespace is `agent` and the deployment is `nuke-apply` (sun's component directory keeps its pre-rename name even after the image is repointed):

```
kubectlsun -n agent logs deploy/nuke-apply --since=30m | grep <event_id>
```

The two `grep` results must contain the identical identifier string. A hand-run `make apply` produces no such identifier on either side, which is what makes this evidence attributable.

## Desired Behavior

1. **The decision runs once per attempted dev apply.** Once the component has resolved and the apply has been attempted, the promotion decision runs exactly once and yields one of two verdicts: `promote` or `withhold`. A command that never reaches an apply attempt (unparseable payload, failed validation, unknown component) yields no promotion record — the existing Failure entry is its whole record.

2. **Healthy means promote.** When the apply succeeded and every watched Deployment passed its health check, the verdict is `promote`, and the service issues one apply command for the same component path to the sun gateway.

3. **Unhealthy or unapplied means withhold.** When the make step failed or any watched Deployment failed its health check, the verdict is `withhold`, nothing is sent to the gateway, and the record names sun as the withheld stage together with the cause.

4. **The service chooses the identifier before it crosses.** Each `promote` verdict carries a freshly generated, URL-safe identifier, distinct from the dev apply command's own event identifier. That identifier is the last path segment of the gateway request and is recorded on the dev side before or with the record, so the two sides can be matched after the fact.

5. **The record is its own entry on the dev result topic.** Every attempted dev apply produces exactly one promotion record on the dev instance's own result topic, in addition to the existing Success or Failure entry. The record carries `kind: promotion`, the component, the target stage, the verdict, and either the identifier plus delivery state (on promote) or the reason (on withhold). It carries **no `operation` field**, so an existing result consumer skips it by operation lookup instead of handling it: `base.Result.Success` has no `omitempty`, so a record carrying `operation: apply` while omitting `success` would unmarshal as a failed apply. The existing Success and Failure entries keep their current shape.

6. **Delivery is reported, not assumed.** A `promote` verdict that the gateway does not accept is still recorded with its identifier, with `delivery: failed` and a reason naming the transport failure. The identifier stays in the record so an operator can check the sun side for a command that may have arrived. The dev apply's own result is published regardless — a failed promotion never retroactively fails a dev apply that succeeded.

7. **Every instance logs the identifiers it consumes and the one it generates.** Each instance writes one log line per consumed apply command at the default log level, containing that command's event identifier. On sun this is the line that proves the promotion arrived; on dev it is the line that shows which operator command started the chain. The dev instance additionally writes one line carrying the **promotion identifier it generated**, so the dev log and the sun log can be matched — the consumed command's identifier and the promotion identifier are deliberately different (see the criterion that requires them to differ), so one line cannot carry both.

8. **Only dev promotes, and prod's gate is untouched.** A sun or prod instance never promotes. A prod apply still requires the recorded operator approval, checked before the component is resolved, with the existing refusal text and the existing approval payload. No new approval field, approver or bypass is introduced. The sun gateway URL is **required on `dev` only**: a sun or prod instance starts and serves without it, following the stage-conditional wiring `createApprover` already uses. Making it globally required would crash-loop sun and prod, which is itself a regression.

## Constraints

- The shape and content of the existing Success and Failure result entries do not change. The rollback text that a failed health check produces stays where it is.
- Approval enforcement keeps its current order: before the component resolves, before any make invocation, and before any filesystem access.
- Stage semantics do not change: the branch passed to make, the topic branch consumed, the master-head guard and the approval requirement are all derived from the stage exactly as they are today.
- Topic names do not change: `develop-k8s-apply-v1-request`, `develop-k8s-apply-v1-result`, `sun-k8s-apply-v1-request`, `sun-k8s-apply-v1-result`.
- The applier's make invocation does not change. No new flag, environment variable or target is passed to make.
- The promotion command carries only the component path. It carries no approval, no commit and no free-form metadata.
- A promotion delays the dev apply's own result entry by **at most the gateway timeout** and never blocks it indefinitely. The gateway client carries a bounded timeout of 10s, matching `NewAttentionStoreApprover`'s `&http.Client{Timeout: 10 * time.Second}`.
- The gateway URL is configuration, never derived from a command payload. The gateway authenticates the caller with an initiator header (`iam.Initiator`, from `github.com/bborbe/cqrs/iam` — the same type this service already carries in its command objects); the value comes from configuration and is never written to a record, a log line or a command payload. The gateway's own handler lives in another repository and is not a modification target here. A routable plain-`http` gateway URL is refused at startup.
- Existing tests for the applier, health-checked applier, approver and command executor keep passing unchanged.

## Failure Modes

| Trigger | Expected behavior | Recovery | Detection | Reversibility |
|---------|-------------------|----------|-----------|---------------|
| Sun gateway unreachable, times out, or returns 5xx | Verdict `promote` is recorded with `delivery: failed` and a reason naming the transport error; the dev apply still reports success | Operator checks the sun log for the recorded identifier; if absent, re-issues the dev apply (new identifier) or the sun apply directly | The promotion record on the dev result topic | Partial — unknown whether the gateway accepted before failing; the identifier makes the question answerable |
| Gateway returns 429 | Same as 5xx: `delivery: failed`, no automatic retry | Operator re-issues the dev apply after the window passes | Promotion record | Reversible — nothing was applied |
| Gateway returns 401 or 403 | `delivery: failed` with a reason naming the status; the credential is never echoed | Operator fixes the configured credential; re-issues | Promotion record | Reversible |
| Sun's `guardMaster` refuses because the sun checkout does not contain `origin/master` | Sun publishes its own Failure entry; the dev record already says `delivery: accepted` | Sync the sun checkout to `origin/master`, re-promote | Sun result topic Failure entry, correlated by identifier | Reversible — nothing was applied |
| Sun accepts the command and its apply fails | Sun publishes its own Failure entry; the dev side is unaffected | Fix the cause on sun, re-promote | Sun result topic Failure entry | Partial |
| The service crashes after the gateway accepted and before the record was written | Sun applies the component; the dev result topic has no promotion record for it | None needed for correctness (apply is idempotent); the operator sees a sun log line with no matching dev record | Sun log identifier with no dev-side match | Irreversible record gap, harmless outcome |
| The dev command is redelivered by Kafka | A second promotion attempt with a new identifier; the same component is applied to sun twice | None needed — applying identical manifests is idempotent | Two promotion records for one operator intent | Reversible |
| The promotion record cannot be published to Kafka | The dev apply result is still published; the promotion may have happened with no record | Operator correlates through the sun log identifier | Missing record on the dev result topic | Partial |
| An existing consumer reads the dev result topic | The promotion record is skipped by consumers that expect a result envelope; the apply result entries still flow | None | Consumer log line for the skipped entry | Reversible |
| The dev instance starts without the sun gateway URL | Startup fails with a message naming the missing setting; the instance does not consume commands | Add the setting in the deploy repo, restart | Pod crash loop and startup message | Reversible |
| The image rolls before the gateway URL reaches the deploy repo | Dev's apply service crash loops | Add the setting; no state is lost | Pod crash loop | Reversible |

## Security / Abuse Cases

- **Trust boundary widens by one hop.** Before this work, publishing an apply command to the dev request topic could only change the dev cluster. After it, the same publish can change sun. The widening is bounded by the existing component resolver: the docker refusal, the missing-target refusal and the by-name exclusion apply to the promoted component exactly as they apply to the dev one. The promotion adds no component it could reach that a dev apply could not.
- **The gateway URL and credential are operator configuration.** Neither is derived from a command payload, so no command can redirect the promotion to another host. A routable plain-`http` gateway URL is refused at startup, matching the rule the approval store URL already follows, because the credential would otherwise travel in clear text.
- **The credential never enters the record.** The promotion record, the dev log and the sun log carry the identifier and the component only. Any test asserting a record's contents must assert the credential's absence.
- **Failure text is reused, not widened.** The `reason` on a withhold carries the same error text the apply already publishes on the result topic. It introduces no new environment-derived field, so it adds no new secret-bearing surface.
- **No user-controlled URL, no retry loop.** The only user-controlled value that reaches the promotion is the component path, which the resolver validates before any promotion is considered. The gateway call is one attempt; nothing retries forever, and nothing can hang the dev apply, because the dev result is published independently of the promotion.

## Suggested Decomposition

| # | Prompt focus | Covers DBs | Covers ACs | Depends on |
|---|---|---|---|---|
| 1 | Promotion decision and record: both verdicts, record shape (incl. no `operation` key), withhold path, zero gateway calls on withhold, `make precommit` green | 1, 2, 3, 5 | 1, 5, 6, 7, 8, 13 | — |
| 2 | Gateway publisher: identifier generation, request shape, delivery outcome, promote record, failed delivery never fails the dev apply | 4, 6 | 2, 3, 4, 14 | prompt 1 |
| 3 | Wiring: dev-only startup requirement, sun-side identifier log line, prod invariance, cross-verdict table test | 7, 8 | 9, 10, 11, 12 | prompts 1, 2 |
| 4 | Spec verification only: live dev and sun evidence | — | 15, 16 | prompts 1-3 plus a deploy |

Rationale: the decision and its record are the contract everything else consumes, so they land first and can be tested against a fake publisher. The gateway publisher then plugs into a verdict that already exists, which keeps the crossing testable in isolation. Wiring and the prod invariance checks come last because they need both pieces in place to assert that nothing else moved. Row 4 is not a prompt — the two live criteria are verified by the spec-verification ladder after the merge and deploy.

## Do-Nothing Option

Nothing breaks if this is not done — dev keeps deploying, sun keeps being applied by hand. The cost is that the health check's good outcome produces no action and no evidence: every promotion stays a manual step whose only trace is the operator's memory, and a component that passed on dev can sit unpromoted indefinitely. The rollback work already shipped makes the failure path durable; leaving the success path silent keeps the durable half and the invisible half exactly inverted.
