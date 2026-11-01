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
// stage is one deploy-repo checkout: every branch, guard and approval decision
// is derived from this value rather than hard-coded.
type Stage string

const (
	// StageDev deploys the nuke dev cluster from the dev branch.
	StageDev Stage = "dev"
	// StageSun deploys the single-stage sun cluster from master.
	StageSun Stage = "sun"
)

// ApplyBranch is the branch passed to make as BRANCH. An empty string means
// the stage passes no BRANCH argument at all.
func (s Stage) ApplyBranch() string {
	switch s {
	case StageDev:
		return "dev"
	case StageSun:
		return ""
	default:
		return ""
	}
}

// RequiresBranchArgument reports whether ApplyBranch is passed to make.
func (s Stage) RequiresBranchArgument() bool {
	return s == StageDev
}

// RequiresApproval reports whether the stage gates on a recorded operator
// approval. Neither stage defined here does; the prod stage added by a sibling
// branch returns true.
func (s Stage) RequiresApproval() bool {
	return false
}

// Branch is the CQRS topic branch this instance consumes. TopicPrefixFromBranch
// maps dev to "develop" and passes sun through as "sun".
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
	default:
		return "", errors.Errorf(ctx, "unknown stage '%s': accepted are 'dev', 'sun'", name)
	}
}
