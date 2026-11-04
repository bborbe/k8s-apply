---
status: completed
summary: Implemented the HTTP gateway that carries a promote verdict to the sun command gateway as one POST whose path ends in the service-chosen identifier, with the delivery outcome reported in the record
spec: [001-health-gated-promotion]
created: "2026-11-03T22:30:50Z"
queued: "2026-11-03T23:05:51Z"
---

# feat: carry a promote verdict across to the sun command gateway

<summary>
- A promote verdict now issues one real HTTP apply command to the sun command gateway.
- The service chooses the identifier before the request leaves, and that identifier is the last segment of the gateway path.
- The request body carries only the component path.
- An accepted request is recorded as an accepted delivery; a rejected or unreachable gateway is recorded as a failed delivery that still keeps the identifier.
- A failed delivery never turns a successful dev apply into a failed one.
- The gateway credential is sent in a header, never in the URL, the record or a log line.
- The gateway client gives up after ten seconds, so a promotion can delay the dev result by at most that.
- The sun gateway itself is not modified; only the caller side is built here.
</summary>

<objective>
Implement the Gateway the promotion decision already calls into, so a promote verdict crosses from dev to sun over the sun command gateway as one POST carrying a service-chosen identifier, and so the delivery outcome is reported in the record instead of assumed.
</objective>

<context>
Read `docs/dod.md` for project conventions.

The approved spec is `specs/in-progress/001-health-gated-promotion.md`. Read Desired Behaviors 4 and 6, the Failure Modes rows for 5xx / 429 / 401 / 403, and the Security section before writing. Prompt 1 of this series created `pkg/promotion` with `Record`, `Verdict`, `Delivery`, `Gateway`, `RecordPublisher`, `Promoter` and `NewNoopGateway`; this prompt adds the real `Gateway` and does not change the decision.

Read before writing:
- `pkg/promotion/promotion.go` and `pkg/promotion/promoter.go` — the interfaces and the promote path this implements.
- `pkg/apply/approver.go` — `NewAttentionStoreApprover`: the `*http.Client` with `Timeout: 10 * time.Second`, the `strings.TrimRight(baseURL, "/")` normalization, the bounded `io.LimitReader(resp.Body, 512)` error-body read, and the `Authorization` header set only when the token is non-empty. Mirror that shape.
- `pkg/apply/apply_command.go` — `apply.Command` marshals to exactly `{"path":"..."}` because `Approval` is `omitempty`. Reuse the type for the request body instead of defining a second payload.
- `pkg/command/command-object-executor-apply.go` — where the decision is invoked; the executor tests live beside it.
- `pkg/command/command-object-executor-apply_test.go` — the ginkgo style for the new end-to-end promote tests.

Verified external facts (the gateway lives in another repository and is NOT a modification target here):
- The command gateway route is `POST /api/1.0/command/{schemaID}/{operation}[/{eventID}]` (registered in the gateway's `frontend/command` router as `router.Path("/api/1.0/command/{schemaID:.+}/{operation:.+}/{eventID:.+}").Methods(http.MethodPost)`), and the handler builds the command with the parsed `{eventID}` as the command's own event identifier, so the identifier in the path is what the sun side consumes.
- `apply.SchemaID.String()` is `k8s-apply-v1`, so the path is `/api/1.0/command/k8s-apply-v1/apply/<eventID>`.
- The gateway's command handler resolves its initiator with `req.Header.Get(login.LoginHeaderName)` and `login.LoginHeaderName` is `"X-Gateway-Initiator"`. Use that header name as an exported constant so a change is one line.

Open question for the reviewer (do not resolve by guessing): whether the sun gateway's auth middleware overwrites `X-Gateway-Initiator` on a service-to-service call. The header name is a named constant precisely so the answer changes one line; the request shape test asserts the constant, not a literal.
</context>

<requirements>
1. `pkg/promotion/gateway.go` — the HTTP gateway:

```go
// InitiatorHeader is the header the command gateway reads the caller's identity
// from. The gateway's command handler resolves its initiator with
// req.Header.Get(login.LoginHeaderName), and login.LoginHeaderName is
// "X-Gateway-Initiator".
const InitiatorHeader = "X-Gateway-Initiator"

func NewGateway(baseURL string, initiator iam.Initiator, client *http.Client) Gateway
```

(`iam` is `github.com/bborbe/cqrs/iam`.)

2. `Send(ctx context.Context, componentPath string, eventID base.EventID) error`:

- Endpoint: `strings.TrimRight(baseURL, "/") + "/api/1.0/command/" + apply.SchemaID.String() + "/" + string(ApplyOperation) + "/" + url.PathEscape(eventID.String())`. Put this in a named helper `requestURL(baseURL string, eventID base.EventID) string` so the shape is assertable without a server.
- Body: `json.Marshal(apply.Command{Path: componentPath})`. Do not hand-roll the JSON and do not add fields.
- Headers: `Content-Type: application/json` and `InitiatorHeader: initiator.String()`.
- `http.NewRequestWithContext(ctx, http.MethodPost, requestURL(...), bytes.NewReader(body))`; a request-construction failure is wrapped as `"build promotion request for '%s' failed"`.
- On `client.Do` error: return `errors.Wrapf(ctx, err, "promote '%s' to the sun gateway failed", componentPath)` — a transport failure, including a timeout, is a failed delivery.
- On a status outside 200–299: read at most 512 bytes of the body and return `errors.Errorf(ctx, "promote '%s' to the sun gateway failed: gateway returned %d: %s", componentPath, resp.StatusCode, strings.TrimSpace(string(body)))`. The status must appear in the text (the operator needs to tell 429 from 401); the initiator value must not.
- On 200–299: return nil. Close the body on every path.
- The initiator value must never appear in an error, a log line, the request URL or the record. Do not log the request headers.

3. `pkg/promotion/gateway_test.go`, using `httptest.NewServer` and a `t.Cleanup` that closes it:
- request shape: assert the method is `POST`, the URL path is exactly `/api/1.0/command/k8s-apply-v1/apply/<eventID>`, `Content-Type` is `application/json`, the request carries the literal header name `X-Gateway-Initiator` (read it with the literal, not with the constant, and separately assert `InitiatorHeader == "X-Gateway-Initiator"`) whose value is the configured initiator, and the decoded body unmarshals into `map[string]any` with exactly one key `path` whose value is the component path. Decoding into a map and asserting `len(map) == 1` is what pins "the promotion command carries only the component path".
- `200` → `Send` returns nil.
- `500` → the error names `500`; the initiator value does not appear anywhere in the error text.
- `401` → the error names `401`; the initiator value does not appear in the error text.
- unreachable server (close it before the call) → `Send` returns an error naming the component.
- `requestURL` unit test: a base URL with a trailing slash and one without produce the identical path, and the identifier is the last segment.

4. End-to-end promote tests in `pkg/command/command-object-executor-apply_test.go`, using a real `promotion.NewPromoter(apply.StageDev, gateway, publisher, generator)` with `*mocks.PromotionGateway` and `*mocks.PromotionRecordPublisher` (no mocked promoter):
- "publishes a promote record carrying an identifier the command did not choose": build the command object with `base.Command{ID: base.EventID("operator-1"), ...}`; a successful apply → exactly one published record with `Verdict: VerdictPromote`, `ToStage: apply.StageSun`, `Delivery: DeliveryAccepted`, and `record.EventID` non-empty and not equal to `operator-1`; the gateway's `SendArgsForCall(0)` carries that same identifier.
- "keeps the apply's success entry when the gateway returns 500": `gateway.SendReturns(errors.Errorf(ctx, "gateway returned 500"))` with a successful apply → the executor returns the component path as the event id and a nil error (that is the Success entry the result sender publishes), and the published record has `Delivery: DeliveryFailed` with a `Reason` containing `500` and a non-empty `EventID`.
- "publishes no promote record when the health check fails": `applier.ApplyReturns(errors.Errorf(ctx, "post-apply health check failed for 'x': deployment 'y': not healthy after 5m"))` → one record with `Verdict: VerdictWithhold` and a `Reason` containing `post-apply health check failed`, `SendCallCount() == 0`, and the executor still returns an error containing the same text.
- "publishes no promote record when the make step fails": `applier.ApplyReturns(errors.Errorf(ctx, "apply 'x' failed: make exited 2"))` → one record with `Verdict: VerdictWithhold` and a `Reason` containing `make exited 2`, `SendCallCount() == 0`.

5. Do not wire `NewGateway` into `main.go` or `pkg/factory` in this prompt — a later prompt adds the configuration and replaces the noop gateway. `NewGateway` is exported and tested here, unused at the wiring layer for now.

6. Before finishing, re-run the verification and walk every requirement above against the change.
</requirements>

<constraints>
- The shape and content of the existing Success and Failure result entries do not change. The rollback text that a failed health check produces stays where it is.
- The promotion record carries no `operation` field and no `success` field.
- One attempt per promotion: no retry, no backoff, no queueing. A failed delivery is recorded and the dev result is published regardless.
- A failed delivery never retroactively fails a dev apply that succeeded.
- The identifier stays in the record on a failed delivery, so an operator can check the sun side for a command that may have arrived.
- The gateway URL and the initiator are configuration in a later prompt; this prompt takes them as constructor arguments and never derives either from a command payload.
- The initiator value is never written to a record, a log line or a command payload, and never appears in an error text.
- The gateway's own handler lives in another repository and is not a modification target here.
- The applier's make invocation does not change. No new flag, environment variable or target is passed to make.
- Do NOT add a promotion metric, dashboard or alert.
- Do NOT add an enable/disable switch for promotion, a configurable promotion target, or a configurable promotion chain.
- Do NOT add a second approval field or a second approver.
- Do NOT add the startup configuration, the consumed-command log line, the prod invariance tests or the cross-verdict table test — the next prompt owns them.
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass.
- Use `github.com/bborbe/errors` for wrapping and `github.com/golang/glog` for logging, as the surrounding files do.
</constraints>

<verification>
Run `make precommit` — must pass.

Then run `go test ./pkg/promotion/... ./pkg/command/... -count=1` and confirm both packages pass.
</verification>
