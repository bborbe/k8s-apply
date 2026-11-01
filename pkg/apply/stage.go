// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apply

import (
	"context"

	"github.com/bborbe/errors"
)

// Stage is one deploy target of the nuke deploy repo. It exists because a
// single branch value cannot describe every stage: the cqrs topic prefix and
// the branch handed to make are different facts that only coincide on dev.
//
// The cqrs side maps a stage to a topic prefix through
// base.TopicPrefixFromBranch — dev becomes "develop" and prod becomes
// "master" — while the make side needs the branch name the deploy repo's own
// Makefile.env understands. Conflating the two is a stage-crossing bug that
// reads as correct, because on dev both values are "dev".
type Stage string

const (
	// StageDev deploys nuke dev. Its make branch is "dev".
	StageDev Stage = "dev"
	// StageProd deploys nuke prod. Its make branch is "master" — nuke's
	// Makefile.env maps both "master" and "prod" onto prod.env, and every
	// prod deploy is documented against "master".
	StageProd Stage = "prod"
)

// ApplyBranch returns the branch handed to each component's make target.
//
// An empty string means the stage passes no BRANCH argument at all. That is a
// real case, not a degenerate one: a stage whose components guard themselves
// (a `guard-master` precondition inside the apply recipe) selects nothing by
// argument, so passing BRANCH there would assert a gate that stage does not
// use. Callers must consult RequiresBranchArgument rather than comparing
// against "" themselves.
func (s Stage) ApplyBranch() string {
	switch s {
	case StageDev:
		return "dev"
	case StageProd:
		return "master"
	}
	return ""
}

// RequiresBranchArgument reports whether ApplyBranch is passed to make. A
// stage that gates by a precondition inside its recipes returns false.
func (s Stage) RequiresBranchArgument() bool {
	return s.ApplyBranch() != ""
}

// RequiresApproval reports whether a command must carry one recorded operator
// approval before anything is applied.
//
// It is true only for prod, because prod is the stage where a wrong apply is
// not recoverable by re-applying. Dev and sun promote on their own.
func (s Stage) RequiresApproval() bool {
	return s == StageProd
}

// Validate rejects a stage this build does not know.
//
// It matters more than it looks: an unknown stage yields an empty ApplyBranch,
// which RequiresBranchArgument reads as "pass no BRANCH" rather than as an
// error. Every caller that builds an invocation must therefore Validate first,
// and the applier does.
func (s Stage) Validate(ctx context.Context) error {
	switch s {
	case StageDev, StageProd:
		return nil
	}
	return errors.Errorf(
		ctx,
		"unknown stage '%s': expected '%s' or '%s'",
		string(s),
		StageDev,
		StageProd,
	)
}
