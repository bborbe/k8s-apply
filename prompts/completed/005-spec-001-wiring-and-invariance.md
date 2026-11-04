---
status: completed
summary: Wired the sun gateway as a dev-only startup requirement, logged the consumed command identifier on both sides of the crossing, and pinned the prod path with invariance tests
spec: [001-health-gated-promotion]
created: "2026-11-03T22:30:50Z"
queued: "2026-11-03T23:05:51Z"
---

# feat: wire the promotion on dev only and prove the other stages did not move

<summary>
- A dev instance now refuses to start without a sun gateway URL and initiator, naming the missing setting.
- A sun or prod instance starts and serves with neither configured, and its promotion path is a noop that refuses rather than silently accepting.
- The gateway URL and the initiator are configuration only; neither can be supplied by a command.
- A routable plain-http gateway URL is refused at startup, by the same rule the approval store URL already follows.
- Every instance logs one line per consumed apply command carrying that command's own event identifier, which is how a sun-side arrival is proven.
- A prod apply still refuses without a recorded approval, with the same text and the same order as before.
- A prod instance publishes no promotion entry for any apply.
- Both verdicts are proven to come from one decision entry point by a table with one named row per verdict.
- The two new settings are documented in the README and the example environment.
</summary>

<objective>
Complete the promotion by making the sun gateway configuration a dev-only startup requirement, wiring the real gateway in place of the noop, logging the identifiers on both sides of the crossing, and pinning the prod path with tests that would fail if any part of the existing gate moved.
</objective>

<context>
Read `docs/dod.md` for project conventions.

The approved spec is `specs/in-progress/001-health-gated-promotion.md`. Read Desired Behaviors 7 and 8, the Failure Modes rows for a missing gateway URL and for a redelivered command, the Security section, and the Constraints before writing. Prompts 1 and 2 of this series created `pkg/promotion` (record, decision, record publisher, noop gateway) and the HTTP gateway; this prompt wires them and adds the invariance evidence.

Read before writing:
- `main.go` — `createApprover` is the precedent for a stage-conditional startup requirement, and `validateAttentionURL` is the URL rule to reuse. `Run` is where both are called.
- `main_internal_test.go` — `TestCreateApprover` is the table-test precedent to mirror for the gateway.
- `pkg/promotion/promotion.go`, `pkg/promotion/promoter.go`, `pkg/promotion/gateway.go` — `Promoter`, `NewPromoter`, `NewNoopGateway`, `NewGateway`, `VerdictPromote`, `VerdictWithhold`, `ApplyOperation`.
- `pkg/factory/command.go` — `CreateCommandConsumer` now takes a `promotion.Gateway` as its last parameter.
- `pkg/command/command-object-executor-apply.go` — the executor closure, where the consumed-command log line goes.
- `README.md` — the Runtime Configuration table.
- `example.env` — the per-setting comment style.
- `CHANGELOG.md` — the release-note style; there is no `## Unreleased` section today, `## v0.14.0` is at the top.

Verified facts:
- `apply.Stage.Promotes()` returns true for `StageDev` only.
- `service.Main` returns a non-zero exit code when `application.Run` returns an error, so a startup error is what makes a misconfigured dev instance exit non-zero.
- `commandObject.Command.ID` (`base.EventID`) is the event identifier the gateway put in the URL path, so on sun that value is the promotion's identifier.
- `github.com/bborbe/cqrs/iam` is already a transitive dependency through `github.com/bborbe/cqrs`; `iam.Initiator` is a `string` type.

Open questions for the reviewer (state the assumption in a code comment rather than resolving silently):
- Whether the sun gateway requires an additional credential beyond the initiator header. The spec names only the initiator header, so this prompt adds only that; if a credential is also required, it is a second setting and a second header.
- Whether the sun gateway's auth middleware overwrites `X-Gateway-Initiator` on a service-to-service call. The header name is a constant, so the answer changes one line.
</context>

<requirements>
1. `main.go`: add two fields to `application`, in the style of `AttentionURL` / `AttentionToken`:

```go
SunGatewayURL       string `required:"false" arg:"sun-gateway-url"       env:"SUN_GATEWAY_URL"       usage:"sun command gateway base URL; required on dev, where a healthy apply promotes the component through it"`
SunGatewayInitiator string `required:"false" arg:"sun-gateway-initiator" env:"SUN_GATEWAY_INITIATOR" usage:"initiator the sun command gateway attributes the promotion to" display:"length"`
```

2. `main.go`: generalize the URL rule instead of duplicating it. Rename `validateAttentionURL(ctx context.Context, raw string) error` to `validateCredentialURL(ctx context.Context, setting string, raw string) error`, interpolate `setting` into both error messages (so they read `ATTENTION_URL must be http or https, got '%s'` and `SUN_GATEWAY_URL ... is plain http to a routable host: ...`), and update the one existing call in `createApprover` to pass `"ATTENTION_URL"`. Keep the accepted forms exactly as they are: `https` always, `http` only for an in-cluster Service host (`.svc.cluster.local`, `.svc`, or a host with no dot). Keep the doc comment's reasoning and add one sentence that the rule is shared because both settings carry a credential.

3. `main.go`: add `createGateway` next to `createApprover`:

```go
// createGateway picks the promotion gateway the stage demands.
//
// Only a promoting stage gets the real gateway, and only a promoting stage is
// required to configure it: a sun or prod instance has no promotion target, and
// making the URL globally required would crash-loop both. The noop gateway is
// fail-closed — a promotion wired to it is recorded as a failed delivery with a
// reason, never as accepted.
func (a *application) createGateway(ctx context.Context, stage apply.Stage) (promotion.Gateway, error)
```

- `if !stage.Promotes() { return promotion.NewNoopGateway(), nil }`.
- Empty URL → `errors.Errorf(ctx, "stage '%s' promotes but SUN_GATEWAY_URL is empty", string(stage))`. The text `SUN_GATEWAY_URL is empty` is asserted by a test.
- `validateCredentialURL(ctx, "SUN_GATEWAY_URL", a.SunGatewayURL)`.
- Empty initiator → `errors.Errorf(ctx, "stage '%s' promotes but SUN_GATEWAY_INITIATOR is empty", string(stage))`.
- Otherwise `promotion.NewGateway(a.SunGatewayURL, iam.Initiator(a.SunGatewayInitiator), &http.Client{Timeout: 10 * time.Second})`.

4. `main.go`: in `Run`, call `createGateway` right after `createApprover` (same error-handling shape: return the error), and pass the result as the last argument to `factory.CreateCommandConsumer`. Do not touch the `createDeploymentClient` call or anything else in `Run`.

5. `pkg/command/command-object-executor-apply.go`: add one log line per consumed apply command at default level (plain `glog.Infof`, not a `V(n)` call), immediately after the command unmarshals and before `command.Validate` runs, so a command that is later refused is still logged:

```go
glog.Infof(
	"consumed apply command '%s' for component '%s' on stage '%s'",
	commandObject.Command.ID,
	command.Path,
	string(stage),
)
```

This is the line that proves a promotion arrived on sun (the sun side consumes the command with the promotion identifier as its event id) and the line that shows which operator command started the chain on dev. Add a comment saying exactly that.

6. `pkg/promotion/promoter_test.go`: add the cross-verdict table test — the evidence that one entry point produces both verdicts:

```go
DescribeTable("decides one verdict per attempted apply",
	func(applyErr error, wantVerdict promotion.Verdict, wantTarget apply.Stage) {
		// call Promote once, then assert the single captured record's
		// Verdict and ToStage
	},
	Entry("promote on a healthy apply", nil, promotion.VerdictPromote, apply.StageSun),
	Entry("withhold on a failed apply", errors.Errorf(context.Background(), "post-apply health check failed"), promotion.VerdictWithhold, apply.StageSun),
)
```

Each row asserts both the verdict value and the target stage on the captured record. Keep the row names exactly as above so the test output names both verdicts.

⚠️ The `Entry` argument must use `context.Background()`, not the suite's `ctx`. `Entry` parameters are evaluated eagerly when the Ginkgo tree is built, before any `BeforeEach` runs, so the suite's `ctx` is still nil there — `errors.Errorf` would call `ctx.Value(...)` on a nil interface and panic, failing `make precommit`. The error is only the withheld apply's `Reason`, and the assertion reads its text, not its context data.

7. `pkg/command/command-object-executor-apply_test.go`: add the prod invariance test. Build a `StageProd` executor with a real `promotion.NewPromoter(apply.StageProd, gateway, publisher, generator)` over `*mocks.PromotionGateway` and `*mocks.PromotionRecordPublisher`, with `approver.ApproveReturns(nil)`, `revision.VerifyReturns(nil)`, the resolver returning a component and the applier returning nil, and the command carrying an approval payload (build it as the existing "refuses when the checkout is not the approved commit" test does). Assert: the executor returns the component path as the event id and a nil error, `publisher.PublishCallCount()` is 0 and `gateway.SendCallCount()` is 0. Add a second case with `applier.ApplyReturns(errors.Errorf(ctx, "make exited 2"))` asserting the same zero counts — a failing prod apply records no promotion either.

8. `pkg/command/command-object-executor-apply_test.go`: add the operation drift test — `Expect(string(command.Operation)).To(Equal(string(promotion.ApplyOperation)))` — with a comment that the gateway path segment and the consumed operation must not drift apart, and that `pkg/promotion` cannot import `pkg/command` because this package imports it.

9. `main_internal_test.go`: add `TestCreateGateway` mirroring `TestCreateApprover`'s table shape:

| stage | url | initiator | expectation |
|---|---|---|---|
| dev | (empty) | (empty) | error containing `SUN_GATEWAY_URL is empty` |
| dev | `https://gateway.example.com` | `k8s-apply` | no error, gateway non-nil |
| dev | `http://command.sun.svc.cluster.local:8080` | `k8s-apply` | no error |
| dev | `http://gateway.example.com:8080` | `k8s-apply` | error containing `in clear text` |
| dev | `https://gateway.example.com` | (empty) | error containing `SUN_GATEWAY_INITIATOR is empty` |
| sun | (empty) | (empty) | no error, gateway non-nil |
| prod | (empty) | (empty) | no error, gateway non-nil |

Also add one test asserting the sun and prod gateways refuse when called — `createGateway` for `StageSun`, then `Send` on the returned gateway must return an error, so a future wiring mistake fails loudly instead of silently dropping a promotion. Add a comment stating that the non-zero exit code for the dev case is `service.Main`'s consequence of `Run` returning this error, and that the unit test asserts the message, not the process exit.

Also add `TestApplicationSunGatewayTags` to `application_test.go`, mirroring the existing `TestApplicationStageTags`: reflect on the `application` type and assert `SunGatewayURL` carries `arg:"sun-gateway-url"` and `env:"SUN_GATEWAY_URL"`, and `SunGatewayInitiator` carries `arg:"sun-gateway-initiator"` and `env:"SUN_GATEWAY_INITIATOR"`. `TestCreateGateway` sets the struct fields directly and never exercises the arg/env parser, so a mistyped tag would ship undetected and a dev pod would crash-loop on an env var nobody set — the spec's own Failure Modes row.

10. `README.md`: add two rows to the Runtime Configuration table after `REPO_DIR`:

| `SUN_GATEWAY_URL` | Sun command gateway base URL; required on `dev`, where a healthy apply promotes the component through it | (empty on `sun` and `prod`) |
| `SUN_GATEWAY_INITIATOR` | Initiator the sun command gateway attributes the promotion to | (empty on `sun` and `prod`) |

11. `example.env`: add, after the `ATTENTION_URL` block and in the same comment style:

```
# Set on the dev stage: a dev apply that passes its post-apply health check
# promotes the component to sun through this gateway. Must be https unless the
# host is an in-cluster Service. Empty on sun and prod, which never promote.
export SUN_GATEWAY_URL=
# The initiator the sun gateway attributes the promotion to. Never written to a
# record or a log line.
export SUN_GATEWAY_INITIATOR=
```

12. `CHANGELOG.md`: add a `## Unreleased` section above `## v0.14.0` with one bullet:

`- feat: promote a component to sun after its dev apply passes its post-apply health check. A healthy dev apply now issues one apply command for the same component through the sun command gateway, carrying a service-chosen identifier that is recorded on the dev result topic and appears in both the dev and the sun log; a failed apply or a failed health check records an explicit refusal naming sun as withheld, and the gateway is not called at all. The refusal is its own entry (\`kind: promotion\`, no \`operation\`, no \`success\`) so an existing result consumer skips it instead of reading it as a failed apply. Only dev promotes: \`SUN_GATEWAY_URL\` and \`SUN_GATEWAY_INITIATOR\` are required on dev only, a routable plain-http gateway URL is refused at startup, and the prod approval gate is unchanged`

13. Before finishing, re-run the verification and walk every requirement above against the change.
</requirements>

<constraints>
- The shape and content of the existing Success and Failure result entries do not change. The rollback text that a failed health check produces stays where it is.
- Approval enforcement keeps its current order: before the component resolves, before any make invocation, and before any filesystem access. The existing refusal text does not change.
- Stage semantics do not change: the branch passed to make, the topic branch consumed, the master-head guard and the approval requirement are all derived from the stage exactly as they are today.
- Topic names do not change: `develop-k8s-apply-v1-request`, `develop-k8s-apply-v1-result`, `sun-k8s-apply-v1-request`, `sun-k8s-apply-v1-result`.
- The applier's make invocation does not change. No new flag, environment variable or target is passed to make.
- The gateway URL and the initiator are configuration, never derived from a command payload. A routable plain-`http` gateway URL is refused at startup.
- The initiator value is never written to a record, a log line or a command payload, and never appears in an error text. Any test asserting a record's contents asserts the initiator's absence.
- Sun-to-prod promotion does not exist: only dev promotes, and prod remains an operator-issued, approved command.
- The promotion command carries only the component path. It carries no approval, no commit and no free-form metadata.
- No automatic retry, backoff or queueing of a failed gateway delivery. One attempt per promotion.
- Do NOT add a promotion metric, dashboard or alert.
- Do NOT add an enable/disable switch for promotion, a configurable promotion target, or a configurable promotion chain.
- Do NOT add a second approval field or a second approver for the promotion path.
- Do NOT touch the deploy repo, the sun component's image pin, or the gateway's own handler — the sun repoint is a deployment change outside this repository.
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass.
- Use `github.com/bborbe/errors` for wrapping and `github.com/golang/glog` for logging, as the surrounding files do.
</constraints>

<verification>
Run `make precommit` — must pass.

Then run `go test ./... -count=1` and confirm every package passes, and `go test ./pkg/promotion/... -count=1 -v` and confirm the output names both table rows (`promote on a healthy apply`, `withhold on a failed apply`).
</verification>
