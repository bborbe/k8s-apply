---
status: completed
created: "2026-11-04T08:01:35Z"
queued: "2026-11-04T08:01:35Z"
completed: "2026-11-04T08:05:06Z"
pr-url: https://github.com/bborbe/k8s-apply/pull/12
---

# fix: run a component whose apply target is prerequisite-only

<summary>
- A component whose apply target only lists prerequisites becomes applicable
- Today such a component is refused as having no apply target at all
- A component with no apply target at all is still refused
- So is a placeholder target that neither declares prerequisites nor has a recipe
- The cases become distinguishable instead of both reading as absent
- A one-line `;` recipe stays visible to the docker refusal, which it is not today
- The docker refusal keeps its meaning: it reads the apply target's own recipe only
- Components that apply today resolve and run exactly as before
- Tests cover a prerequisite-only target, a placeholder target, a missing target, and a `;` recipe
</summary>

<objective>
Let the in-cluster apply service resolve a component whose apply target is prerequisite-only — the shape the nuke agent component uses — so the service runs that target's prerequisites instead of refusing the component outright, while a placeholder target that would run nothing stays refused.
</objective>

<context>
Read `docs/dod.md` (this project's definition of done) and `README.md` (service behaviour) for project conventions. `README.md` documents the three refusals; its "a component with no `apply` target at all" wording stays accurate.

No spec governs this change; the requirements below are the sole definition of scope.

Read these files before writing:

- `pkg/apply/makefile.go` — `extractRecipe` and its doc comment. The comment states it returns nil when the target is not defined. The defect is that it also returns nil when the target IS defined and simply has no tab-indented recipe lines, so the two cases are indistinguishable to the caller.
- `pkg/apply/component_resolver.go` — the `len(recipe) == 0` branch that returns `ErrNotApplicable`, the `Component.Recipe` field comment, and `runsDocker`.
- `pkg/apply/makefile_test.go` — the existing `Describe("extractRecipe")` cases.
- `pkg/apply/component_resolver_test.go` — the existing resolver cases, and its `GinkgoT().TempDir()` / `errors.Is` conventions.
- `pkg/apply/applier.go` — `makeArgs`, `Apply` and `guardMaster`. The applier shells out to `make -C <dir> [BRANCH=<branch>] apply`, so it never needs the recipe text. Note that the docker refusal reads the local `recipe` variable inside `Resolve`, not `Component.Recipe`.
- `pkg/apply/stage.go` — `RequiresMasterHead`. Only the sun stage guards the checkout, which matters for the applier test below.
- `CHANGELOG.md` — the release-notes file, and `prompts/completed/002-stage-branch-derivation.md` for the house convention of adding an `## Unreleased` bullet.
</context>

<requirements>
1. Change `extractRecipe` to `func extractRecipe(content string) (recipe []string, found bool)`. `found` reports whether the `apply:` target exists AND declares something to run — its prerequisites, its recipe lines, or both. Update the doc comment to state this contract.
   - A target defined with recipe lines reports found, with those lines.
   - A prerequisite-only target (`apply: a b c`, followed by a non-recipe line) reports found, with an empty recipe.
   - A placeholder target with neither prerequisites nor a recipe (`apply:` followed by a non-recipe line) reports **not found**, so it stays refused. A target that would run nothing must not publish a Success.
   - A file with no `apply:` target reports not found.
   - A target whose name merely starts with `apply` (e.g. `apply-all:`) is still not a match.

2. In `component_resolver.go`, return `ErrNotApplicable` only when the target is not found. A found-but-empty recipe must resolve to a `Component` carrying an empty `Recipe`. Replace the `Component.Recipe` field comment: it currently claims the field is "used for the refusal decision and for the log line", but `Resolve` decides on its local `recipe` variable and no production code reads the field. Say instead that it records the resolved recipe lines for diagnostics and is empty when the target exists but declares no recipe lines. The `ErrNotApplicable` comment already reads "no apply target at all" — leave it unchanged.

3. Leave the docker refusal unchanged: `runsDocker` reads the apply target's own recipe lines only. Do not extend it to prerequisites. The refusal means "this recipe invokes docker", never "something this target depends on invokes docker".

4. Close the one-line-recipe hole this change would otherwise open. A Makefile may write `apply: ; docker build -t x .` — the recipe lives after the semicolon on the target line, never in a tab-indented line. Before this change such a file was refused as `ErrNotApplicable` (no tab lines); after it, `found` would be true, `recipe` empty, and `runsDocker` would never see the docker command. Extend `extractRecipe` so the text after the first `;` on the `apply:` line is `strings.TrimSpace`d and appended as one recipe line, so `runsDocker` still judges it. `apply: dep ; docker build -t x .` must yield recipe `[]string{"docker build -t x ."}` with `found` true.

5. Update all five existing `extractRecipe` cases in `makefile_test.go` to the new two-value form — `recipe, found := extractRecipe(...)` — because they currently call it as a single return value and will not compile otherwise.

6. Add to `makefile_test.go`:
   - a prerequisite-only target reports found with an empty recipe;
   - a placeholder target with neither prerequisites nor a recipe reports not found;
   - a missing target reports not found;
   - `apply: ; docker build -t x .` reports found with a recipe that `runsDocker` rejects;
   - `apply: ; kubectl apply -f x.yaml` reports found with that recipe line.

7. Extend `component_resolver_test.go`: a component whose Makefile has a prerequisite-only `apply:` must resolve to a `Component` with an empty `Recipe`, not return `ErrNotApplicable`; and a component whose Makefile is `apply: ; docker build -t x .` must still return `ErrDockerComponent`.

8. Add an applier-level test to `pkg/apply/applier_test.go` proving the behaviour the objective promises: write a temporary directory holding a Makefile with `apply: dep` and a tab-indented `dep:` recipe that touches a sentinel file, resolve it through `NewComponentResolver(tmpDir).Resolve`, then run the real `applier.Apply` on the returned component. Use `StageDev` with `log.DefaultSamplerFactory`, as the existing `Applier` tests do — `StageSun` makes `Apply` run `guardMaster`, whose `git merge-base --is-ancestor origin/master HEAD` fails in a temp dir that is not a git checkout. Give the test a `context.WithTimeout(ctx, 10*time.Second)` so a wedged `make` fails instead of hanging, and assert `Apply` returns nil and the sentinel exists. `make` is already a documented dependency of the image and of `Apply`, so this costs nothing in the container.

9. Add one bullet under `## Unreleased` in `CHANGELOG.md` (create the section at the top of the file if it is absent): `fix: run a component whose apply target is prerequisite-only`. Also add one sentence to the refusal list in `README.md`: the docker refusal reads the apply target's own recipe lines only, so a prerequisite that invokes docker is not refused — it fails at apply time instead.

10. Before finishing, re-run every command in the `<verification>` block and confirm each output matches; walk each requirement above against the change.
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass.
- `extractRecipe`'s docker detection, command-prefix stripping and backslash-joining behaviour must not change.
- `Component.Recipe` stays in place.
- No new Go dependency for this change.
- Repo-relative paths only.
</constraints>

<verification>
Run `make precommit` -- must pass.

`grep -c 'func extractRecipe(content string) (recipe []string, found bool)' pkg/apply/makefile.go` prints 1.

`grep -c 'if !found' pkg/apply/component_resolver.go` prints 1.

`! grep -q 'len(recipe) == 0' pkg/apply/component_resolver.go` -- the refusal now keys on `found`, not on the recipe length.

`grep -c 'apply: ; docker build' pkg/apply/makefile_test.go` prints 1 -- the one-line-recipe case has a test.

`grep -c 'fix: run a component whose apply target is prerequisite-only' CHANGELOG.md` prints 1.
</verification>
