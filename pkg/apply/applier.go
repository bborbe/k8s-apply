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
// target for the given stage. It never rewrites the recipe: whatever the
// component's Makefile says `apply` does is what runs.
//
// The stage, not a bare branch string, is the parameter: the branch handed to
// make and the branch whose topics this instance consumes are different facts,
// and only a stage knows both.
func NewApplier(stage Stage, logSamplerFactory log.SamplerFactory) Applier {
	return &applier{
		stage:      stage,
		logSampler: logSamplerFactory.Sampler(),
	}
}

type applier struct {
	stage      Stage
	logSampler log.Sampler
}

// makeArgs is the exact argv the applier hands to make. It is a named function
// so the invocation is assertable in a test without running make.
//
// A stage that gates by a precondition inside its own recipes passes no BRANCH
// at all — passing one there would assert a gate that stage does not use.
func makeArgs(component *Component, stage Stage) []string {
	args := []string{"-C", component.Dir}
	if stage.RequiresBranchArgument() {
		args = append(args, "BRANCH="+stage.ApplyBranch())
	}
	return append(args, applyTarget)
}

func (a *applier) Apply(ctx context.Context, component *Component) error {
	if _, err := ParseStage(ctx, string(a.stage)); err != nil {
		return errors.Wrapf(ctx, err, "refusing to apply component '%s'", component.Path)
	}
	if a.stage.RequiresMasterHead() {
		if err := guardMaster(ctx, component); err != nil {
			return errors.Wrapf(
				ctx,
				err,
				"refusing to apply component '%s'",
				component.Path,
			)
		}
	}
	args := makeArgs(component, a.stage)
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
