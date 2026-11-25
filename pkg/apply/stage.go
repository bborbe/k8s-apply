// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apply

import (
	"context"

	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/errors"
)

// Stage names the cluster a service instance deploys. It is a label, not a
// gate: any non-empty name is accepted, and the behaviour an instance runs with
// comes from StageConfig rather than from a comparison on the name.
type Stage string

const (
	// StageDev deploys the nuke dev cluster from the dev branch.
	StageDev Stage = "dev"
	// StageSun deploys the single-stage sun cluster from master.
	StageSun Stage = "sun"
	// StageProd deploys the nuke prod cluster from the master branch.
	StageProd Stage = "prod"
)

// StageConfig is the behaviour one instance runs with. Every field is
// configuration; no value is selected by comparing the stage name.
type StageConfig struct {
	// ApplyBranch is the branch passed to make as BRANCH. An empty string
	// means the stage passes no BRANCH argument at all.
	//
	// Prod's branch is "master", not "prod": nuke's Makefile.env maps both
	// names onto prod.env, and every prod deploy is documented against
	// "master". Passing "prod" would deploy the right stage from a branch name
	// the deploy repo does not know.
	ApplyBranch string
	// TopicPrefix is the prefix prepended to the schema's topic names.
	//
	// The Seibert stages consume the UNPREFIXED names their Schema CR declares
	// — maintainer-kubeapply-v1-request, not develop-maintainer-kubeapply-v1-request
	// — because each stage has its own cluster and its own Kafka, so the branch
	// prefix is not what separates them. data/cdb-config's topics/maintainer.yaml
	// declares every maintainer topic unprefixed; a prefixed consumer would
	// subscribe to a topic nothing writes.
	//
	// `sun` keeps the branch-derived prefix it has always used.
	TopicPrefix base.TopicPrefix
	// RequiresMasterHead reports whether the deploy checkout's HEAD must
	// contain origin/master. This is the one behaviour the dev|prod shape does
	// not have; it is how sun's checkout guard is expressed, because a sun
	// apply must never run from a feature worktree.
	RequiresMasterHead bool
	// RefreshCheckout reports whether every apply first moves the deploy
	// checkout to the tip of ApplyBranch.
	//
	// The checkout is cloned once by the pod's init container, so without a
	// refresh a commit pushed after pod start is invisible to every apply until
	// the pod is restarted — and the apply then reports success on the stale
	// revision. That is the silent-wrong-success shape this removes on dev.
	//
	// It is off by default, and on for dev only: prod keeps its current
	// behaviour deliberately (a separate change), and sun already guards its
	// checkout through RequiresMasterHead. A refresh needs a writable REPO_DIR
	// and git credentials, so an instance that turns it on must mount both.
	RefreshCheckout bool
}

// RequiresBranchArgument reports whether ApplyBranch is passed to make. It is
// derived from ApplyBranch, so any config with a non-empty branch passes it to
// make without editing this method.
func (c StageConfig) RequiresBranchArgument() bool {
	return c.ApplyBranch != ""
}

// StageOverrides are the values a deploy sets explicitly. A nil field means
// "use the built-in default"; a set field wins over it. Pointers are what make
// an explicit empty value distinguishable from an unset one, which matters for
// sun: its make branch is the empty string, not "unset".
type StageOverrides struct {
	ApplyBranch        *string
	TopicPrefix        *base.TopicPrefix
	RequiresMasterHead *bool
	RefreshCheckout    *bool
}

// defaultStageConfigs returns the built-in behaviour for the three stages that
// existed before a stage name became a label. It is a function, not a package
// variable, so no caller can mutate the table for another.
//
// An unknown stage gets the neutral StageConfig: no BRANCH argument, no topic
// prefix and no master-head guard. An unconfigured unknown stage therefore
// consumes the same unprefixed topic as dev, so two such instances must not
// share a cluster — a second consumer on the same request topic splits
// partitions instead of deduping, and the same command is applied twice.
func defaultStageConfigs() map[Stage]StageConfig {
	return map[Stage]StageConfig{
		StageDev: {
			ApplyBranch:        "dev",
			TopicPrefix:        base.TopicPrefix(""),
			RequiresMasterHead: false,
			RefreshCheckout:    true,
		},
		StageSun: {
			ApplyBranch:        "",
			TopicPrefix:        base.TopicPrefix("sun"),
			RequiresMasterHead: true,
		},
		StageProd: {
			ApplyBranch:        "master",
			TopicPrefix:        base.TopicPrefix(""),
			RequiresMasterHead: false,
		},
	}
}

// ResolveStageConfig returns the behaviour for stage: its built-in default,
// with any override the deploy set applied on top.
func ResolveStageConfig(stage Stage, overrides StageOverrides) StageConfig {
	config := StageConfig{}
	if builtin, ok := defaultStageConfigs()[stage]; ok {
		config = builtin
	}
	if overrides.ApplyBranch != nil {
		config.ApplyBranch = *overrides.ApplyBranch
	}
	if overrides.TopicPrefix != nil {
		config.TopicPrefix = *overrides.TopicPrefix
	}
	if overrides.RequiresMasterHead != nil {
		config.RequiresMasterHead = *overrides.RequiresMasterHead
	}
	if overrides.RefreshCheckout != nil {
		config.RefreshCheckout = *overrides.RefreshCheckout
	}
	return config
}

// ParseStage returns the stage named by name. Any non-empty name is accepted:
// the name is a label, and the behaviour comes from ResolveStageConfig. An
// empty name is still refused, so a mis-set environment fails loudly instead of
// starting an unnamed instance.
func ParseStage(ctx context.Context, name string) (Stage, error) {
	if name == "" {
		return "", errors.Errorf(ctx, "stage must not be empty")
	}
	return Stage(name), nil
}
