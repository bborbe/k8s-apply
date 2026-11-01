// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package factory

import (
	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/cqrs/cdb"
	libkafka "github.com/bborbe/kafka"
	libkv "github.com/bborbe/kv"
	"github.com/bborbe/log"
	"github.com/bborbe/run"

	"github.com/bborbe/nuke-apply/pkg/apply"
	"github.com/bborbe/nuke-apply/pkg/command"
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
func CreateCommandConsumer(
	saramaClientProvider libkafka.SaramaClientProvider,
	syncProducer libkafka.SyncProducer,
	db libkv.DB,
	stage apply.Stage,
	repoDir string,
	approver apply.Approver,
) run.Func {
	logSamplerFactory := log.DefaultSamplerFactory
	return cdb.RunCommandConsumerTxDefault(
		saramaClientProvider,
		syncProducer,
		db,
		apply.SchemaID,
		base.TopicPrefixFromBranch(CQRSBranch(stage)),
		false,
		cdb.CommandObjectExecutorTxs{
			command.NewCommandObjectExecutorApply(
				stage,
				apply.NewComponentResolver(repoDir),
				apply.NewApplier(stage, logSamplerFactory),
				approver,
				logSamplerFactory,
			),
		},
	)
}
