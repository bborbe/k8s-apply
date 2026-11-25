// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package factory

import (
	"time"

	"github.com/bborbe/cqrs/cdb"
	libkafka "github.com/bborbe/kafka"
	libkv "github.com/bborbe/kv"
	"github.com/bborbe/log"
	"github.com/bborbe/run"

	"github.com/bborbe/k8s-apply/pkg/apply"
	"github.com/bborbe/k8s-apply/pkg/command"
)

// CreateCommandConsumer consumes apply commands from the branch-scoped request
// topic and publishes one result per command.
//
// ignoreUnsupported is false: an unknown operation on this schema must surface
// as a failure rather than be dropped silently, because the only caller is an
// operator waiting for a result.
//
// The applier is health-checked: an apply that lands but leaves a Deployment
// unhealthy within healthCheckTimeout is rolled back to its pre-apply images.
func CreateCommandConsumer(
	saramaClientProvider libkafka.SaramaClientProvider,
	syncProducer libkafka.SyncProducer,
	db libkv.DB,
	stage apply.Stage,
	config apply.StageConfig,
	repoDir string,
	deploymentClient apply.DeploymentClient,
	healthCheckTimeout time.Duration,
) run.Func {
	logSamplerFactory := log.DefaultSamplerFactory
	applier := apply.NewHealthCheckedApplier(
		apply.NewApplier(stage, config, repoDir, logSamplerFactory),
		apply.NewDeploymentFinder(),
		deploymentClient,
		healthCheckTimeout,
		5*time.Second,
	)
	return cdb.RunCommandConsumerTxDefault(
		saramaClientProvider,
		syncProducer,
		db,
		apply.SchemaID,
		config.TopicPrefix,
		false,
		cdb.CommandObjectExecutorTxs{
			command.NewCommandObjectExecutorApply(
				stage,
				apply.NewComponentResolver(repoDir),
				applier,
				logSamplerFactory,
			),
		},
	)
}
