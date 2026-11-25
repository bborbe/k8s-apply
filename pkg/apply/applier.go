// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apply

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/bborbe/errors"
	"github.com/bborbe/log"
	"github.com/golang/glog"
)

//counterfeiter:generate -o ../../mocks/applier.go --fake-name Applier . Applier

// Applier runs one component's own apply target.
type Applier interface {
	Apply(ctx context.Context, component *Component) error
}

// NewApplier returns an Applier that shells out to the component's own make
// target for the given stage and config. It never rewrites the recipe: whatever
// the component's Makefile says `apply` does is what runs.
//
// repoDir is the deploy repo checkout the component paths are resolved against.
// It is the same directory the resolver is rooted at, and it is what a config
// with RefreshCheckout moves to the tip of its branch before an apply.
//
// The stage is kept for the log line that names which instance ran an apply;
// every behavioural decision reads config, never the name.
func NewApplier(
	stage Stage,
	config StageConfig,
	repoDir string,
	logSamplerFactory log.SamplerFactory,
) Applier {
	return &applier{
		stage:      stage,
		config:     config,
		repoDir:    repoDir,
		logSampler: logSamplerFactory.Sampler(),
	}
}

type applier struct {
	stage      Stage
	config     StageConfig
	repoDir    string
	logSampler log.Sampler
}

// makeArgs is the exact argv the applier hands to make. It is a named function
// so the invocation is assertable in a test without running make.
//
// A config that gates by a precondition inside its own recipes passes no BRANCH
// at all — passing one there would assert a gate that stage does not use.
func makeArgs(component *Component, config StageConfig) []string {
	args := []string{"-C", component.Dir}
	if config.RequiresBranchArgument() {
		args = append(args, "BRANCH="+config.ApplyBranch)
	}
	return append(args, applyTarget)
}

func (a *applier) Apply(ctx context.Context, component *Component) error {
	if a.config.RefreshCheckout {
		head, err := refreshCheckout(ctx, a.repoDir)
		if err != nil {
			return errors.Wrapf(
				ctx,
				err,
				"refresh checkout for component '%s'",
				component.Path,
			)
		}
		// The line SC1 and SC3 read: it names the revision the apply below runs
		// from, so a stale checkout is visible in the log instead of only in the
		// cluster. It is emitted only when the checkout was refreshed — on the
		// stages that do not refresh, HEAD is the pod-start clone and a separate
		// git invocation per apply would buy nothing.
		glog.Infof("apply %s: checkout HEAD %s", component.Path, head)
	}
	if a.config.RequiresMasterHead {
		if err := guardMaster(ctx, component); err != nil {
			return errors.Wrapf(
				ctx,
				err,
				"refusing to apply component '%s'",
				component.Path,
			)
		}
	}
	args := makeArgs(component, a.config)
	// #nosec G204 -- the component path was validated by the resolver and make
	// is the documented entry point of every component in the deploy repo.
	cmd := exec.CommandContext(ctx, "make", args...)
	output, err := cmd.CombinedOutput()
	if a.logSampler.IsSample() {
		glog.V(2).Infof(
			"apply %s: make %s exited with %v (sample)",
			component.Path,
			strings.Join(args, " "),
			err,
		)
	}
	if err != nil {
		return errors.Wrapf(
			ctx,
			err,
			"apply '%s' failed: %s",
			component.Path,
			strings.TrimSpace(string(output)),
		)
	}
	return nil
}

// guardMaster refuses a component whose deploy checkout does not contain
// origin/master in HEAD. The fetch is best-effort: the checkout is mounted
// read-only in the pod, so the guard cannot write and the ancestor check reads
// the ref the shallow clone already created.
func guardMaster(ctx context.Context, component *Component) error {
	fetchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	fetchArgs := []string{"-C", component.Dir, "fetch", "-q", "origin"}
	// #nosec G204 -- component.Dir was validated by the resolver; git is the
	// documented tool for this checkout.
	fetchCmd := exec.CommandContext(fetchCtx, "git", fetchArgs...)
	fetchCmd.Env = append(
		os.Environ(),
		"GIT_SSH_COMMAND=ssh -o BatchMode=yes -o ConnectTimeout=5",
	)
	if _, err := fetchCmd.CombinedOutput(); err != nil {
		glog.V(2).Infof(
			"guard master %s: best-effort fetch failed, continuing: %v",
			component.Path,
			err,
		)
	}

	checkArgs := []string{
		"-C", component.Dir,
		"merge-base", "--is-ancestor", "origin/master", "HEAD",
	}
	// #nosec G204 -- component.Dir was validated by the resolver; git is the
	// documented tool for this checkout.
	checkCmd := exec.CommandContext(ctx, "git", checkArgs...)
	output, err := checkCmd.CombinedOutput()
	if err != nil {
		return errors.Wrapf(
			ctx,
			err,
			"checkout '%s' does not contain origin/master in HEAD; fetch and merge origin/master before applying: %s",
			component.Path,
			strings.TrimSpace(string(output)),
		)
	}
	return nil
}

// checkoutRefreshTimeout bounds the network half of a refresh. It is longer
// than guardMaster's 10s because that fetch is best-effort and this one decides
// whether the apply runs at all.
const checkoutRefreshTimeout = 60 * time.Second

// refreshCheckout moves the deploy repo checkout to the tip of the branch it is
// checked out on and returns the commit the apply will run from.
//
// It follows the CHECKOUT's branch, never ApplyBranch. ApplyBranch is the value
// handed to make as BRANCH, and it selects a stage's env file inside the tree:
// nuke's dev instance clones `master` and passes BRANCH=dev. Refreshing to
// origin/dev would ask a master clone for a ref it does not have —
// `fatal: ambiguous argument 'origin/dev'` — and, because a refresh failure is
// fatal, that fails every apply on the stage.
//
// Every failure is returned, never swallowed. guardMaster's best-effort fetch
// is the shape that made this defect invisible: it logs a failed fetch at V(2)
// and then applies whatever the checkout already held, so the apply reports
// success against a revision nobody chose. A refresh that failed must fail the
// apply instead — the result topic then carries the fetch failure and nothing is
// applied.
func refreshCheckout(ctx context.Context, repoDir string) (string, error) {
	branch, err := checkoutBranch(ctx, repoDir)
	if err != nil {
		return "", err
	}
	fetchCtx, cancel := context.WithTimeout(ctx, checkoutRefreshTimeout)
	defer cancel()

	if output, err := runGit(fetchCtx, repoDir, "fetch", "-q", "origin", branch); err != nil {
		return "", errors.Wrapf(
			ctx,
			err,
			"fetch origin %s: %s",
			branch,
			strings.TrimSpace(output),
		)
	}
	// reset --hard, not merge: the checkout is a disposable clone of a deploy
	// repo, so anything local is meaningless and a merge would leave conflict
	// markers for make to render.
	if output, err := runGit(ctx, repoDir, "reset", "--hard", "-q", "origin/"+branch); err != nil {
		return "", errors.Wrapf(
			ctx,
			err,
			"reset to origin/%s: %s",
			branch,
			strings.TrimSpace(output),
		)
	}
	return checkoutHead(ctx, repoDir)
}

// checkoutBranch returns the branch the deploy checkout is on. A detached HEAD
// is refused by name rather than refreshed to whatever FETCH_HEAD happens to
// hold — a deploy that silently follows the wrong revision is the failure mode
// this whole path exists to remove.
func checkoutBranch(ctx context.Context, repoDir string) (string, error) {
	output, err := runGit(ctx, repoDir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", errors.Wrapf(
			ctx,
			err,
			"resolve checkout branch: %s",
			strings.TrimSpace(output),
		)
	}
	branch := strings.TrimSpace(output)
	if branch == "" || branch == "HEAD" {
		return "", errors.Errorf(
			ctx,
			"deploy checkout is on a detached HEAD; refresh needs a branch to follow",
		)
	}
	return branch, nil
}

// checkoutHead returns the commit the deploy checkout sits on.
func checkoutHead(ctx context.Context, repoDir string) (string, error) {
	output, err := runGit(ctx, repoDir, "rev-parse", "HEAD")
	if err != nil {
		return "", errors.Wrapf(ctx, err, "rev-parse HEAD: %s", strings.TrimSpace(output))
	}
	return strings.TrimSpace(output), nil
}

// runGit runs one git command in repoDir and returns its combined output.
//
// GIT_SSH_COMMAND is inherited when the deployment sets it, because the deploy
// key lives on a read-only mount whose path only the manifest knows. BatchMode
// and a connect timeout are added either way: a fetch that waits on a
// passphrase prompt or an unroutable host would hold the apply open until the
// context deadline instead of failing by name.
func runGit(ctx context.Context, repoDir string, args ...string) (string, error) {
	gitArgs := append([]string{"-C", repoDir}, args...)
	// #nosec G204 -- repoDir is REPO_DIR, validated by the resolver before any
	// component is applied, and git is the documented tool for this checkout.
	cmd := exec.CommandContext(ctx, "git", gitArgs...)
	cmd.Env = append(os.Environ(), gitSSHEnvironment()...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

// gitSSHEnvironment returns the GIT_SSH_COMMAND override runGit applies, or nil
// when the deployment's own value already forbids interaction.
func gitSSHEnvironment() []string {
	const batchMode = "-o BatchMode=yes -o ConnectTimeout=5"
	command := os.Getenv("GIT_SSH_COMMAND")
	if command == "" {
		return []string{"GIT_SSH_COMMAND=ssh " + batchMode}
	}
	if strings.Contains(command, "BatchMode") {
		return nil
	}
	return []string{"GIT_SSH_COMMAND=" + command + " " + batchMode}
}
