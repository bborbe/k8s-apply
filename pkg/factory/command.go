// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package factory

import (
	"time"

	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/cqrs/cdb"
	libkafka "github.com/bborbe/kafka"
	libkv "github.com/bborbe/kv"
	"github.com/bborbe/log"
	"github.com/bborbe/run"

	"github.com/bborbe/k8s-apply/pkg/apply"
	"github.com/bborbe/k8s-apply/pkg/command"
	"github.com/bborbe/k8s-apply/pkg/promotion"
)

// CQRSBranch is the branch whose topic prefix this stage consumes.
//
// It is deliberately NOT the make branch. On prod the make branch is "master"
// while the cqrs branch is "prod", and TopicPrefixFromBranch maps those two
// different inputs onto the same prefix — which is exactly why they must stay
// separate values: the day one of them changes, a single shared variable would
// silently consume a topic nothing writes.
func CQRSBranch(stage apply.Stage) base.Branch {
	if stage == apply.StageProd {
		return base.Branch("prod")
	}
	return base.Branch(string(stage))
}

// CreateCommandConsumer consumes apply commands from the branch-scoped request
// topic and publishes one result per command.
//
// ignoreUnsupported is false: an unknown operation on this schema must surface
// as a failure rather than be dropped silently, because the only caller is an
// operator waiting for a result.
//
// The approver is consulted inside the executor, before any make invocation, so
// a stage that requires an approval cannot reach a shell without one.
//
// The applier is health-checked: an apply that lands but leaves a Deployment
// unhealthy within healthCheckTimeout is rolled back to its pre-apply images.
func CreateCommandConsumer(
	saramaClientProvider libkafka.SaramaClientProvider,
	syncProducer libkafka.SyncProducer,
	db libkv.DB,
	stage apply.Stage,
	repoDir string,
	approver apply.Approver,
	deploymentClient apply.DeploymentClient,
	healthCheckTimeout time.Duration,
	gateway promotion.Gateway,
) run.Func {
	logSamplerFactory := log.DefaultSamplerFactory
	applier := apply.NewHealthCheckedApplier(
		apply.NewApplier(stage, logSamplerFactory),
		apply.NewDeploymentFinder(),
		deploymentClient,
		healthCheckTimeout,
		5*time.Second,
	)
	// The prefix is the stage's own branch prefix, so on dev the record lands on
	// develop-k8s-apply-v1-result — the same topic the existing result entries
	// use.
	promoter := promotion.NewPromoter(
		stage,
		gateway,
		promotion.NewRecordPublisher(
			syncProducer,
			base.TopicPrefixFromBranch(stage.Branch()),
		),
		base.NewIdentifierGeneratorUUID[base.EventID](),
	)
	return cdb.RunCommandConsumerTxDefault(
		saramaClientProvider,
		syncProducer,
		db,
		apply.SchemaID,
		base.TopicPrefixFromBranch(stage.Branch()),
		false,
		cdb.CommandObjectExecutorTxs{
			command.NewCommandObjectExecutorApply(
				stage,
				apply.NewComponentResolver(repoDir),
				applier,
				approver,
				apply.NewGitRevision(repoDir),
				promoter,
				logSamplerFactory,
			),
		},
	)
}
