// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apply

import (
	"context"

	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/errors"
)

// Stage names the cluster a service instance deploys. One instance is one
// stage is one deploy-repo checkout: every branch and guard decision is derived
// from this value rather than hard-coded.
type Stage string

const (
	// StageDev deploys the nuke dev cluster from the dev branch.
	StageDev Stage = "dev"
	// StageSun deploys the single-stage sun cluster from master.
	StageSun Stage = "sun"
	// StageProd deploys the nuke prod cluster from the master branch.
	StageProd Stage = "prod"
)

// ApplyBranch is the branch passed to make as BRANCH. An empty string means
// the stage passes no BRANCH argument at all.
//
// Prod's branch is "master", not "prod": nuke's Makefile.env maps both names
// onto prod.env, and every prod deploy is documented against "master". Passing
// "prod" would deploy the right stage from a branch name the deploy repo does
// not know.
func (s Stage) ApplyBranch() string {
	switch s {
	case StageDev:
		return "dev"
	case StageProd:
		return "master"
	case StageSun:
		return ""
	default:
		return ""
	}
}

// RequiresBranchArgument reports whether ApplyBranch is passed to make. It is
// derived from ApplyBranch, so any stage that returns a non-empty branch passes
// it to make without editing this method.
func (s Stage) RequiresBranchArgument() bool {
	return s.ApplyBranch() != ""
}

// Promotes reports whether a successful, healthy apply on this stage promotes
// the component to the next stage. Only dev does: sun and prod are never
// promotion sources.
func (s Stage) Promotes() bool {
	return s == StageDev
}

// Branch is the CQRS topic branch this instance consumes. TopicPrefixFromBranch
// maps dev to "develop", prod to "master", and passes sun through as "sun".
//
// It is deliberately NOT ApplyBranch: on prod those are "prod" and "master".
// Both yield the prefix "master" today, which is exactly why collapsing them
// into one value would stay hidden until either changed.
func (s Stage) Branch() base.Branch {
	return base.Branch(s)
}

// RequiresMasterHead reports whether the deploy checkout's HEAD must contain
// origin/master. This is the one method the dev|prod shape does not have; it is
// how sun's checkout guard is expressed, because a sun apply must never run
// from a feature worktree.
func (s Stage) RequiresMasterHead() bool {
	return s == StageSun
}

// ParseStage returns the stage named by name. It refuses anything that is not
// an accepted stage, including the empty string, so a mis-set environment fails
// loudly instead of silently deploying the wrong tree.
func ParseStage(ctx context.Context, name string) (Stage, error) {
	switch name {
	case string(StageDev):
		return StageDev, nil
	case string(StageSun):
		return StageSun, nil
	case string(StageProd):
		return StageProd, nil
	default:
		return "", errors.Errorf(
			ctx,
			"unknown stage '%s': accepted are 'dev', 'sun', 'prod'",
			name,
		)
	}
}
