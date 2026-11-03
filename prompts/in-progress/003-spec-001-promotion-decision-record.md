---
status: approved
spec: [001-health-gated-promotion]
created: "2026-11-03T22:30:50Z"
queued: "2026-11-03T23:05:51Z"
---

# feat: decide one promotion verdict per attempted dev apply and record it

<summary>
- A dev apply now ends in exactly one promotion decision: promote, or withhold.
- A failed dev apply records an explicit refusal that names sun as the withheld stage and carries the apply's own error text as the cause.
- The refusal is a separate entry on the result topic, so an operator never confuses it with the rollback failure entry.
- The refusal entry carries no operation and no success field, so an existing result consumer skips it instead of reading it as a failed apply.
- Nothing is sent anywhere on a refusal.
- A command that never reaches an apply attempt (bad payload, failed validation, refused approval, unknown component) still produces no promotion entry at all.
- A successful apply records a promote verdict with a freshly generated identifier, and the transport that carries it is a seam this change leaves unimplemented.
- Sun and prod instances produce no promotion entry for any apply, by construction rather than by wiring.
- The existing success and failure entries keep their exact shape.
</summary>

<objective>
Introduce the promotion decision as a single entry point that yields one of two verdicts per attempted dev apply, and publish its outcome as its own record on the dev instance's result topic, so the healthy half of the health check finally leaves a durable trace that is distinguishable from the rollback failure entry.
</objective>

<context>
Read `docs/dod.md` for project conventions.

The approved spec is `specs/in-progress/001-health-gated-promotion.md`. Read its Desired Behaviors 1, 2, 3, 5 and Constraints before writing. This prompt implements the decision and the record; the HTTP crossing and the startup wiring are later prompts.

Read before writing:
- `pkg/apply/stage.go` — `Stage`, `RequiresApproval`, `RequiresMasterHead`. The new `Promotes` method follows that pattern.
- `pkg/command/command-object-executor-apply.go` — the executor closure, its failure paths, and why every failure returns a real error.
- `pkg/command/command-object-executor-apply_test.go` — the ginkgo style and the three constructor call sites this change breaks.
- `pkg/apply/health_checked_applier.go` — the error text a failed health check returns (it already names the failed deployment and the restored images).
- `pkg/factory/command.go` — the one place the executor and the Kafka producer are wired.
- `pkg/apply/approver.go` — `NewNoopApprover` and `AttentionStoreApprover`: the fail-closed noop and the `http.Client` with a 10s timeout are the precedents to mirror.
- `pkg/apply/schema.go` — `SchemaID` and its `ResultTopic` method.

Verified library facts (do not re-derive, but do not contradict):
- `cdb.SchemaID.ResultTopic(prefix base.TopicPrefix) libkafka.Topic` and `libkafka.SyncProducer.SendMessage(ctx, *sarama.ProducerMessage) (int32, int64, error)`.
- `base.Result` has `Operation CommandOperation \`json:"operation"\`` and `Success bool \`json:"success"\`` with no `omitempty`, so a promotion record modelled as `base.Result` would always serialize both keys and a consumer would read it as a failed apply. That is exactly why the record below is its own type.
- `base.NewIdentifierGeneratorUUID[Identifier ~string | ~[]byte]() base.IdentifierGenerator[Identifier]` exists in `github.com/bborbe/cqrs/base`; instantiate it as `base.NewIdentifierGeneratorUUID[base.EventID]()`.
- `base.TopicPrefixFromBranch(base.Branch) base.TopicPrefix` maps dev to `develop`.
</context>

<requirements>
1. `pkg/apply/stage.go`: add a method next to `RequiresApproval` and `RequiresMasterHead`:

```go
// Promotes reports whether a successful, healthy apply on this stage promotes
// the component to the next stage. Only dev does: sun and prod are never
// promotion sources.
func (s Stage) Promotes() bool {
	return s == StageDev
}
```

2. New package `pkg/promotion`, four source files plus a ginkgo suite.

2a. `pkg/promotion/promotion.go` — the record contract and the three collaborator interfaces:

- Constants: `KindPromotion = "promotion"`; `ApplyOperation base.CommandOperation = "apply"` with a comment that it mirrors `command.Operation` in `pkg/command` and cannot import it back (that package imports this one); `Verdict` string type with `VerdictPromote Verdict = "promote"` and `VerdictWithhold Verdict = "withhold"`; `Delivery` string type with `DeliveryAccepted Delivery = "accepted"` and `DeliveryFailed Delivery = "failed"`.
- The record, exactly these fields and JSON tags, and nothing else:

```go
// Record is the promotion entry on this instance's result topic. It is its own
// type, never base.Result and never embedding it: base.Result always serializes
// "operation" and "success", and a consumer would read this record as a failed
// apply. Consumers skip it by finding no operation on it.
type Record struct {
	Kind      string       `json:"kind"`
	Component string       `json:"component"`
	ToStage   apply.Stage  `json:"to_stage"`
	Verdict   Verdict      `json:"verdict"`
	EventID   base.EventID `json:"event_id,omitempty"`
	Delivery  Delivery     `json:"delivery,omitempty"`
	Reason    string       `json:"reason,omitempty"`
}
```

- Three interfaces, each with a counterfeiter directive immediately above it, in the style of `pkg/apply/applier.go`:

```go
//counterfeiter:generate -o ../../mocks/promotion-record-publisher.go --fake-name PromotionRecordPublisher . RecordPublisher
type RecordPublisher interface {
	Publish(ctx context.Context, record Record) error
}

//counterfeiter:generate -o ../../mocks/promotion-gateway.go --fake-name PromotionGateway . Gateway
type Gateway interface {
	Send(ctx context.Context, componentPath string, eventID base.EventID) error
}

//counterfeiter:generate -o ../../mocks/promotion-promoter.go --fake-name PromotionPromoter . Promoter
type Promoter interface {
	Promote(ctx context.Context, componentPath string, applyErr error) error
}
```

2b. `pkg/promotion/promoter.go` — the one decision entry point:

```go
func NewPromoter(
	stage apply.Stage,
	gateway Gateway,
	publisher RecordPublisher,
	generator base.IdentifierGenerator[base.EventID],
) Promoter
```

`Promote(ctx context.Context, componentPath string, applyErr error) error` behaves exactly as follows, in this order:

- `if !p.stage.Promotes() { return nil }` — no record, no gateway call. Sun and prod are non-promoting by construction, not by which promoter the factory happened to wire.
- Build `Record{Kind: KindPromotion, Component: componentPath, ToStage: apply.StageSun}`.
- If `applyErr != nil`: set `Verdict = VerdictWithhold` and `Reason = applyErr.Error()` (verbatim — the same text the apply already publishes on the result topic; do not compose a new message, do not add a field, do not prefix "sun withheld"), then publish and return the publish error. Never call the gateway.
- Otherwise: `Verdict = VerdictPromote`, `EventID = p.generator.NewIdentifier()`, then `glog.Infof("promotion of component '%s' to stage '%s' issued with identifier '%s'", componentPath, apply.StageSun, record.EventID)` at default level (plain `glog.Infof`, not a `V(n)` call). Then `p.gateway.Send(ctx, componentPath, record.EventID)`; on error set `Delivery = DeliveryFailed` and `Reason = err.Error()`, on success set `Delivery = DeliveryAccepted`. The identifier stays in the record either way. Then publish and return the publish error — never the gateway error, which is reported through the record.
- The identifier must never be the applied component path and must never be derived from the command: it is a fresh UUID, distinct from the dev command's own event identifier.

2c. `pkg/promotion/record_publisher.go`:

```go
func NewRecordPublisher(syncProducer libkafka.SyncProducer, prefix base.TopicPrefix) RecordPublisher
```

`Publish` marshals the record, sends one message to `apply.SchemaID.ResultTopic(prefix)`, keyed by `record.EventID` when non-empty and otherwise by `record.Component`, and wraps a send failure with `errors.Wrapf(ctx, err, "send promotion record to topic %s failed", topic)`. Follow `cdb.NewResultObjectSender` for the message construction shape; do not import `cdb` for the send itself.

2d. `pkg/promotion/gateway_noop.go`:

```go
// NewNoopGateway returns the Gateway a stage that does not promote gets. It
// refuses instead of silently accepting, mirroring NewNoopApprover: a promotion
// wired to it is recorded as a failed delivery with a reason, never as accepted.
func NewNoopGateway() Gateway
```

Its `Send` returns `errors.Errorf(ctx, "no promotion gateway is configured")`.

2e. `pkg/promotion/promotion_suite_test.go`: a ginkgo suite named `Promotion Suite` with the `//go:generate go run github.com/maxbrunsfeld/counterfeiter/v6@v6.12.2 -generate` line, following `pkg/apply/apply_suite_test.go`.

3. `pkg/command/command-object-executor-apply.go`: add `promoter promotion.Promoter` to `NewCommandObjectExecutorApply`, between `revision apply.Revision` and `logSamplerFactory log.SamplerFactory`. In the closure, replace the current `if err := applier.Apply(ctx, component); err != nil { ... }` block with an attempt-then-decide sequence:

```go
applyErr := applier.Apply(ctx, component)

// The promotion decision runs once per attempted apply, and its failure is
// never allowed to change the apply's own result: a promotion that cannot be
// recorded must not turn a successful dev apply into a failed one.
if promoErr := promoter.Promote(ctx, component.Path, applyErr); promoErr != nil {
	glog.V(2).Infof("record promotion for component %s failed: %v", component.Path, promoErr)
}

if applyErr != nil {
	return nil, nil, errors.Wrapf(ctx, applyErr, "apply component '%s' failed", command.Path)
}
```

The existing success return (`base.EventID(component.Path).Ptr(), commandObject.Command.Data, nil`) and every earlier failure path (unmarshal, `Validate`, `enforceApproval`, `Resolve`) stay exactly as they are — those paths must return before the decision, so a command that never reached an apply attempt yields no promotion record. Extend the function's doc comment with one sentence saying the promotion decision runs once per attempted apply and never changes the apply's result.

4. `pkg/factory/command.go`: add `gateway promotion.Gateway` as the last parameter of `CreateCommandConsumer` (after `healthCheckTimeout time.Duration`), build the promoter there and pass it to the executor:

```go
promoter := promotion.NewPromoter(
	stage,
	gateway,
	promotion.NewRecordPublisher(syncProducer, base.TopicPrefixFromBranch(stage.Branch())),
	base.NewIdentifierGeneratorUUID[base.EventID](),
)
```

The prefix is the stage's own branch prefix, so on dev the record lands on `develop-k8s-apply-v1-result` — the same topic the existing result entries use.

5. `main.go`: pass `promotion.NewNoopGateway()` as the last argument to `factory.CreateCommandConsumer`. A later prompt replaces it with the real gateway built from configuration; until then a promote is recorded as a failed delivery with a reason rather than falsely as accepted.

6. Tests.

6a. `pkg/promotion/promoter_test.go` using `mocks.PromotionGateway` / `mocks.PromotionRecordPublisher` and `base.NewIdentifierGeneratorCounter[base.EventID]()`:
- withhold: `Promote` with a non-nil error publishes exactly one record with `Verdict: VerdictWithhold`, `ToStage: apply.StageSun`, `Component` equal to the path, a `Reason` containing the apply error text, an empty `EventID` and an empty `Delivery`; the gateway's `SendCallCount()` is 0.
- promote: `Promote` with a nil error publishes exactly one record with `Verdict: VerdictPromote`, `ToStage: apply.StageSun`, `Delivery: DeliveryAccepted`, a non-empty `EventID`; the gateway was called once with that path and that same identifier.
- gateway failure: `SendReturns(errors.Errorf(ctx, "gateway returned 500"))` → one record with `Delivery: DeliveryFailed` and a `Reason` containing `500`; `Promote` returns nil when the publish succeeded.
- publish failure: `PublishReturns(errors.Errorf(ctx, "kafka down"))` → `Promote` returns an error containing `kafka down`; the gateway was still called once on the promote path.
- non-promoting stages: `DescribeTable` over `apply.StageSun` and `apply.StageProd` asserting `PublishCallCount() == 0` and `SendCallCount() == 0` for both a nil and a non-nil apply error.
- record shape: marshal the captured `Record` and unmarshal into `map[string]any`; assert the keys `operation` and `success` are absent, and assert the keys `kind`, `component`, `to_stage`, `verdict` are present and carry the literal wire values a consumer reads: `"promotion"`, the component path, `"sun"`, and `"promote"` on the promote row / `"withhold"` on the withhold row. On the promote row also assert `delivery` is `"accepted"`; on the gateway-failure row assert it is `"failed"`. Comparing Go constants proves only what the test typed — these literals are what the boundary actually emits. Assert key absence, not a substring match, so a component path containing the word cannot pass.
- the generated identifier is not the component path: assert `record.EventID != base.EventID(componentPath)`.

6b. `pkg/promotion/record_publisher_test.go`: a test-local fake implementing `libkafka.SyncProducer` (`SendMessage`, `SendMessages`, `Close`) that captures the `*sarama.ProducerMessage`. Assert the topic is `develop-k8s-apply-v1-result` for `base.TopicPrefixFromBranch(apply.StageDev.Branch())`, the key is the record's event id, and the value unmarshals back into a `Record` with the same verdict and target stage. Add one case with an empty `EventID` asserting the key falls back to the component path. Comment the fake as a test-local double: `libkafka` ships no counterfeiter fake for this interface.

6c. `pkg/command/command-object-executor-apply_test.go`: update all three `command.NewCommandObjectExecutorApply(...)` call sites to pass a `*mocks.PromotionPromoter` (declare it in `BeforeEach`), then add:
- "records the promotion decision once per attempted apply": a successful apply → `promoter.PromoteCallCount()` is 1, and from `PromoteArgsForCall(0)` the path argument equals the resolved component's path and the error argument is nil.
- "records the promotion refusal with the apply error": `applier.ApplyReturns(errors.Errorf(ctx, "make exited 2"))` → `PromoteCallCount()` is 1, the error argument from `PromoteArgsForCall(0)` has a message containing `make exited 2`, and the executor still returns an error containing `make exited 2`.
- "does not decide a promotion when the component cannot be resolved": resolver error → `PromoteCallCount()` is 0.
- "does not decide a promotion when the approver refuses": approver error → `PromoteCallCount()` is 0.
- "publishes the apply result when the promotion cannot be recorded": `promoter.PromoteReturns(errors.Errorf(ctx, "kafka down"))` with a successful apply → the executor returns a non-nil event id equal to the component path and a nil error.

7. Before finishing, re-run the verification and walk every requirement above against the change.
</requirements>

<constraints>
- The shape and content of the existing Success and Failure result entries do not change. The rollback text that a failed health check produces stays where it is.
- The promotion record carries no `operation` field and no `success` field, and is never a `base.Result` and never embeds one.
- A command that never reaches an apply attempt (unparseable payload, failed validation, refused approval, unknown component) produces no promotion record — the existing Failure entry is its whole record.
- The promotion's own failure never changes the dev apply's result: the apply's error is returned as it is today, and a failed record publish is logged, not returned.
- Approval enforcement keeps its current order: before the component resolves, before any make invocation, and before any filesystem access.
- Stage semantics do not change: the branch passed to make, the topic branch consumed, the master-head guard and the approval requirement are all derived from the stage exactly as they are today.
- Topic names do not change: `develop-k8s-apply-v1-request`, `develop-k8s-apply-v1-result`, `sun-k8s-apply-v1-request`, `sun-k8s-apply-v1-result`.
- The applier's make invocation does not change. No new flag, environment variable or target is passed to make.
- The promotion command carries only the component path. It carries no approval, no commit and no free-form metadata.
- Do NOT add a promotion metric, dashboard or alert.
- Do NOT add an enable/disable switch for promotion, a configurable promotion target, or a configurable promotion chain.
- Do NOT add a second approval field or a second approver.
- Do NOT implement the HTTP gateway, the consumed-command log line, the startup configuration or the cross-verdict table test — later prompts own them. The noop gateway is the seam.
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass.
- Use `github.com/bborbe/errors` for wrapping and `github.com/golang/glog` for logging, as the surrounding files do.
</constraints>

<verification>
Run `make precommit` — must pass.

Then run `go test ./pkg/promotion/... ./pkg/command/... -count=1` and confirm both packages pass, and confirm `mocks/promotion-gateway.go`, `mocks/promotion-record-publisher.go` and `mocks/promotion-promoter.go` exist after the generate step.
</verification>
