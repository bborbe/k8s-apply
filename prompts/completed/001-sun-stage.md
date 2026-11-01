---
status: completed
summary: Generalised the apply service around a Stage type (dev/sun), deriving branch arguments, the sun master-HEAD checkout guard and topic prefix from the configured stage, with tests and docs updated
execution_id: nuke-apply-sun-stage-exec-001-sun-stage
dark-factory-version: v0.198.7
created: "2026-11-01T19:07:47Z"
queued: "2026-11-01T19:07:47Z"
started: "2026-11-01T19:09:19Z"
completed: "2026-11-01T19:12:44Z"
---

# feat: apply from the sun stage as well as dev

<summary>
- The apply service can deploy the sun-k3s cluster, not only the nuke dev cluster
- Which cluster a service instance deploys is named by a stage, not by a bare branch
- The stage decides whether a BRANCH argument is passed to each component's make target
- A stage with no BRANCH argument is guarded by the deploy checkout itself
- The sun stage refuses to apply from a checkout that does not contain origin/master
- An unknown stage is refused instead of silently falling back to a deployable one
- Existing dev behaviour is unchanged: the dev stage still passes BRANCH=dev
- The service's own configuration is the stage name; the branch is derived from it
- Example configuration and the dev manifest name the stage instead of the branch
- Unit tests cover every stage's branch, master-HEAD guard and approval predicate
</summary>

<objective>
Generalise the apply service so one instance deploys one named stage — dev today, sun now — deriving every branch, guard and approval decision from that stage, so a second cluster can be served without a second copy of the apply logic and without a misconfiguration silently deploying the wrong tree.
</objective>

<context>
Read `docs/dod.md` (this project's definition of done) and `README.md` (service behaviour and configuration) for project conventions.

No spec governs this change; the requirements below are the sole definition of scope.

Read these files before writing:

- `pkg/apply/applier.go` — the hard-coded `DevBranch` guard at the top of `Apply`, and `makeArgs`.
- `pkg/apply/component_resolver.go` — `Component{Dir}` is `filepath.Join(repoDir, path)`; the resolver is already rooted at one repo dir, so it needs no change.
- `pkg/apply/makefile.go` — `applyTarget` and the recipe parsing.
- `pkg/factory/command.go` — `CreateCommandConsumer` currently takes `branch base.Branch` and `repoDir string`.
- `main.go` — the `application` struct's `Branch` field and the `Run` wiring.
- `pkg/apply/applier_test.go` — the existing `makeArgs` and guard tests.
- `pkg/apply/apply_suite_test.go` — the suite bootstrap and counterfeiter directive.

The shared `Stage` contract. A sibling branch (`feat/prod-mode`, not yet pushed) introduces the same type; implement against exactly this shape rather than inventing a different one:

- `func (s Stage) ApplyBranch() string` — the branch passed to make; an empty string means the stage passes no BRANCH at all.
- `func (s Stage) RequiresBranchArgument() bool` — whether `ApplyBranch` is passed to make.
- `func (s Stage) RequiresApproval() bool` — whether the stage gates on a recorded operator approval.
- dev: `ApplyBranch` `dev`, `RequiresBranchArgument` true, `RequiresApproval` false.
- prod: `ApplyBranch` `master`, `RequiresBranchArgument` true, `RequiresApproval` true — NOT implemented here.
- sun: `ApplyBranch` ``, `RequiresBranchArgument` false, `RequiresApproval` false.

`RequiresApproval` is part of the shared contract even though both stages defined here return false: the sibling branch adds `StageProd` (which returns true) without changing the type. Do not call it from the applier in this change. `AvailableStages`/`Validate` from the coding plugin's enum guide are deliberately omitted for the same reason — `ParseStage` is this change's membership check.

Facts about the sun repo (`bborbe/sun`) the design must respect:

- sun is single-stage: `Makefile.env` includes `sun.env` and `common.env` unconditionally and carries no stage gate, so a `BRANCH=` argument selects nothing.
- Every sun component's `apply` must run from a checkout whose HEAD contains `origin/master` (the `guard-master` pattern in `sun/agent/Makefile`), so a feature worktree must never be the deploy source.
- sun components that carry an apply target are resolved by path exactly like nuke's. Do not encode a component count anywhere: this service resolves paths, it does not enumerate components.

The deploy checkout is mounted read-only in the pod, so a `git fetch` in the guard cannot write and must be best-effort; the ancestor check reads the `origin/master` ref the shallow clone already created.
</context>

<requirements>
1. Create `pkg/apply/stage.go`:
   - `type Stage string` with `StageDev Stage = "dev"` and `StageSun Stage = "sun"`. Do NOT define `StageProd` — a sibling branch owns prod.
   - Implement `ApplyBranch`, `RequiresBranchArgument` and `RequiresApproval` per the contract above. `RequiresApproval` returns false for both stages defined here; the prod approval gate is the sibling branch's.
   - Add `func (s Stage) Branch() base.Branch` returning `base.Branch(s)` — the CQRS topic branch the instance consumes (`dev` maps to prefix `develop`, `sun` passes through as `sun` via `base.TopicPrefixFromBranch`).
   - Add `func (s Stage) RequiresMasterHead() bool` returning true only for `StageSun`. This is the one method the dev|prod shape does not have; it is how sun's checkout guard is expressed. Name it in a doc comment that says so.
   - Add `func ParseStage(ctx context.Context, name string) (Stage, error)` accepting exactly `dev` and `sun`, refusing anything else (including the empty string) with the message `unknown stage '<name>': accepted are 'dev', 'sun'`. Use `github.com/bborbe/errors`.

2. `pkg/apply/applier.go`:
   - Replace the `branch string` field and the `DevBranch` constant with a `stage Stage` field: `NewApplier(stage Stage, logSamplerFactory log.SamplerFactory) Applier`.
   - `makeArgs(component *Component, stage Stage) []string`: always `-C <component.Dir>`; append `BRANCH=<ApplyBranch()>` only when `RequiresBranchArgument()`; always end with `applyTarget`.
   - `Apply` first refuses a stage that `ParseStage` does not accept (this preserves the old hard-refusal property, now stage-aware). Then, when `RequiresMasterHead()`, run the checkout guard before make and return a wrapped error when it fails.
   - The guard (`guardMaster`): run `git -C <component.Dir> fetch -q origin` best-effort (log at V(2) on failure and continue — mirror the sun Makefile's `|| true`), then `git -C <component.Dir> merge-base --is-ancestor origin/master HEAD`; a non-zero exit is a refusal naming the component and telling the operator to fetch and merge origin/master. Run the fetch under `context.WithTimeout(ctx, 10*time.Second)` with `cmd.Env` carrying `GIT_SSH_COMMAND=ssh -o BatchMode=yes -o ConnectTimeout=5`, so an unreachable remote cannot stall the command consumer; a timeout is the tolerated failure and the ancestor check still runs. Declare it as a package-level `func guardMaster(ctx context.Context, component *Component) error` (not a method on `applier`) so the test can call it directly without constructing an applier or running make.
   - Keep the existing `#nosec G204` justification style for the exec calls, the log sampling, and the error-wrapping shape.

3. `pkg/factory/command.go`: change `CreateCommandConsumer` to take `stage apply.Stage` instead of `branch base.Branch`; pass `base.TopicPrefixFromBranch(stage.Branch())` as the topic prefix and `apply.NewApplier(stage, logSamplerFactory)`. `repoDir` stays a parameter.

4. `main.go`: replace the `Branch base.Branch` field with `Stage string` (`arg:"stage"`, `env:"STAGE"`, default `dev`, required). Parse it with `apply.ParseStage` at the top of `Run` and pass the resulting stage to the factory. Remove the `base` import if nothing else uses it.

5. Configuration and manifests:
   - `example.env`: replace `export BRANCH=dev` with `export STAGE=dev`.
   - `k8s/nuke-apply-deploy.yaml`: change the service container's `BRANCH` env entry to `STAGE` with value `dev`. Leave the init container's `--branch dev` clone and both `{{"BRANCH" | env}}` image tags (init container and service container) alone — those are the deploy repo's own BRANCH, a different value.
   - `README.md`: update the one-line description so it no longer claims the service is nuke-only, and fix the three passages that still document the old flag: the Runtime Configuration table row `| \`BRANCH\` | ... |` becomes a `STAGE` row (stage whose apply commands this instance consumes, default `dev`, accepted `dev` and `sun`); the `make -C <repo>/<path> BRANCH=dev apply` example must describe the stage-dependent argument (dev passes `BRANCH=dev`, sun passes none); and the paragraph beginning "Only `BRANCH=dev` is accepted." must name the accepted stages and the two stage-dependent behaviours (branch argument, master-HEAD guard).

6. Tests (Ginkgo/Gomega; `pkg/apply/stage_test.go` in `package apply_test`, `pkg/apply/applier_test.go` stays in `package apply` because it asserts the unexported `makeArgs`):
   - New `pkg/apply/stage_test.go`: a `DescribeTable` over dev and sun asserting `ApplyBranch`, `RequiresBranchArgument`, `RequiresApproval`, `RequiresMasterHead` and `Branch`; plus `ParseStage` accepting `dev` and `sun` and refusing `""`, `prod` and an arbitrary name. Also assert the Kafka topic-prefix contract for each stage — `base.TopicPrefixFromBranch(StageDev.Branch()).String()` is `develop` and `base.TopicPrefixFromBranch(StageSun.Branch()).String()` is `sun` — because `Branch()` alone is an accessor test that does not prove which topic the instance consumes.
   - `pkg/apply/applier_test.go`: update `makeArgs` for dev (`BRANCH=dev` present) and add sun (no `BRANCH` argument); replace the "refuses any branch but dev" test with a refusal for an unknown stage; replace the `NewApplier(DevBranch, ...)` argument with `NewApplier(StageDev, ...)` and keep the missing-directory failure test on the dev stage.
   - Add guard tests in `pkg/apply/guard_test.go` (`package apply`, because `guardMaster` is unexported) against a real temporary git repository (git is available): one checkout whose HEAD contains `refs/remotes/origin/master` passes, and one whose `refs/remotes/origin/master` points at a commit HEAD does not contain is refused. Call `guardMaster(ctx, component)` directly. Create the ref with `git update-ref`; do not require a network remote, and tolerate the guard's best-effort fetch failing in the temp repo.
   - Add a `package main` test file (e.g. `application_test.go`) that reflects over the `application.Stage` field and asserts its tags are `arg:"stage"` and `env:"STAGE"` — the binary-compile test alone cannot catch a wrong tag, and the tag is the only thing binding the container's env var to the field.
   - Do not change `pkg/command` tests: the `Applier` and `ComponentResolver` interfaces are unchanged.

7. `CHANGELOG.md`: add bullets under `## Unreleased` describing the stage generalisation and the sun stage.

8. Before finishing, re-run `ROOTDIR=/workspace make precommit` and confirm it passes; then walk each requirement above against the change and confirm it is implemented.
</requirements>

<constraints>
- Do NOT implement `StageProd` or any prod approval gate; a sibling branch owns them. The `Stage` type must nonetheless carry `RequiresApproval` so that branch can add `StageProd` without changing the type.
- Do NOT change `Component`, `ComponentResolver`, or the `Applier` interface — the counterfeiter fakes must stay valid.
- Do NOT change the command payload (`Command{Path}`), the schema id, or the docker-refusal logic.
- Do NOT add a sun Deployment manifest — this change makes the service stage-aware; the sun instance's manifest is a separate change in the sun repo.
- Do NOT change the resolver's repo-dir rooting: one instance is one stage is one repo checkout.
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass.
- Use `github.com/bborbe/errors` for wrapping; no `fmt.Errorf`, no `github.com/pkg/errors`.
- Follow the existing file header, import grouping and naming conventions.
</constraints>

<verification>
Run `ROOTDIR=/workspace make precommit` — must pass. `.git` is masked in this run, so `ROOTDIR` is passed explicitly to point the build at the workspace root. It runs go mod tidy, format, generate, test, lint, vet, vulncheck, osv-scanner, trivy and addlicense.
</verification>
