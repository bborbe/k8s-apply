---
status: completed
summary: Derived RequiresBranchArgument from ApplyBranch, de-collided unknown-stage tests to 'bogus', added the branch-argument invariant table, and updated docs/changelog
execution_id: nuke-apply-sun-stage-exec-002-stage-branch-derivation
dark-factory-version: v0.198.7
created: "2026-11-01T19:16:01Z"
queued: "2026-11-01T19:16:01Z"
started: "2026-11-01T19:16:27Z"
completed: "2026-11-01T19:18:49Z"
---

# refactor: derive the stage branch-argument decision and de-collide the stage tests

<summary>
- A stage that returns a branch passes it to make without the applier being edited
- The current dev-only comparison would silently withhold BRANCH from a future prod stage
- The unknown-stage tests name a value no future stage can claim
- The repo-path description no longer claims the checkout is always nuke
- The branch-argument invariant is asserted for every defined stage
- No behaviour changes for the existing dev and sun stages
</summary>

<objective>
Make the branch-argument decision a consequence of `ApplyBranch` rather than a dev-only comparison, so a sibling branch that adds a prod stage passes `BRANCH=master` without touching the applier, and de-collide the unknown-stage test so it cannot start passing as a known stage.
</objective>

<context>
Read `docs/dod.md` and `README.md` for project conventions.

No spec governs this change; the requirements below are the sole definition of scope.

The `Stage` type in `pkg/apply/stage.go` is shared with a sibling branch (`feat/prod-mode`, not yet pushed) that adds a `StageProd` returning `ApplyBranch() "master"` and `RequiresBranchArgument() true`. The current `RequiresBranchArgument` implementation compares against `StageDev` only, so it would return false for that prod stage and silently pass no BRANCH. Fix the derivation, do not add prod.

Read before writing:
- `pkg/apply/stage.go` — the `Stage` type, `ApplyBranch`, `RequiresBranchArgument`.
- `pkg/apply/applier_test.go` — the "refuses an unknown stage" test.
- `pkg/apply/stage_test.go` — the `DescribeTable` over stage properties.
- `main.go` — the `RepoDir` field usage string.
- `README.md` — the `NUKE_REPO_DIR` row in the Runtime Configuration table.
</context>

<requirements>
1. `pkg/apply/stage.go`: change `RequiresBranchArgument` to `return s.ApplyBranch() != ""`, and update its doc comment to say it is derived from `ApplyBranch`, so any stage that returns a non-empty branch passes it to make without editing this method.

2. De-collide both unknown-stage tests with a value no future stage can claim — `bogus`, not `prod` and not `staging` (`staging` is a plausible future stage name and is already used in `pkg/apply/stage_test.go`):
   - `pkg/apply/applier_test.go`: in the `Describe("Applier")` "refuses an unknown stage" test, replace `NewApplier(Stage("prod"), ...)` with `NewApplier(Stage("bogus"), ...)` and the asserted substring with `unknown stage 'bogus'`.
   - `pkg/apply/stage_test.go`: in the `Describe("ParseStage")` "refuses an unknown stage" `DescribeTable`, remove `Entry("prod", "prod")` and change `Entry("arbitrary", "staging")` to `Entry("arbitrary", "bogus")`, so no entry becomes a known stage when the sibling branch adds `prod`.

3. `pkg/apply/stage_test.go`: add a `DescribeTable("branch-argument derivation", ...)` over every stage asserting `stage.RequiresBranchArgument()` equals `stage.ApplyBranch() != ""`, with a comment above the entries: "every stage must be added here — this invariant is what stops a new stage that returns a branch from silently passing no BRANCH". Note in a test comment that this is a forward-looking guard: for the current dev/sun set the assertion also holds on the pre-change code, so it does not by itself verify requirement 1.

4. `main.go`: change the `RepoDir` usage string from `path to the nuke deploy repo checkout` to `path to the deploy repo checkout this stage applies from`.

5. `README.md`: change the `NUKE_REPO_DIR` row description from `Path to the \`bborbe/nuke\` checkout the apply targets run in` to `Path to the deploy repo checkout the apply targets run in`.

6. `CHANGELOG.md`: add one bullet under `## Unreleased`: `refactor: derive the stage's branch-argument decision from ApplyBranch so a future stage passes BRANCH without editing the applier`.

7. Before finishing, re-run `ROOTDIR=/workspace make precommit` and confirm it passes; then walk each requirement above against the change and confirm it is implemented.
</requirements>

<constraints>
- Do NOT implement `StageProd` or any prod approval gate; a sibling branch owns them.
- Do NOT change `Component`, `ComponentResolver`, or the `Applier` interface.
- Do NOT change the dev or sun stage behaviour: dev still passes `BRANCH=dev`, sun still passes none and still guards on origin/master.
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass.
- Use `github.com/bborbe/errors` for wrapping.
</constraints>

<verification>
Run `ROOTDIR=/workspace make precommit` — must pass.
</verification>
