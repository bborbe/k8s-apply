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

// CreateCommandConsumer consumes apply commands from the branch-scoped request
// topic and publishes one result per command.
//
// ignoreUnsupported is false: an unknown operation on this schema must surface
// as a failure rather than be dropped silently, because the only caller is an
// operator waiting for a result.
func CreateCommandConsumer(
	saramaClientProvider libkafka.SaramaClientProvider,
	syncProducer libkafka.SyncProducer,
	db libkv.DB,
	stage apply.Stage,
	repoDir string,
) run.Func {
	logSamplerFactory := log.DefaultSamplerFactory
	return cdb.RunCommandConsumerTxDefault(
		saramaClientProvider,
		syncProducer,
		db,
		apply.SchemaID,
		base.TopicPrefixFromBranch(stage.Branch()),
		false,
		cdb.CommandObjectExecutorTxs{
			command.NewCommandObjectExecutorApply(
				apply.NewComponentResolver(repoDir),
				apply.NewApplier(stage, logSamplerFactory),
				logSamplerFactory,
			),
		},
	)
}
