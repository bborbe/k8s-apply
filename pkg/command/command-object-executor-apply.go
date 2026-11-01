// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package command

import (
	"context"

	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/cqrs/cdb"
	"github.com/bborbe/errors"
	libkv "github.com/bborbe/kv"
	"github.com/bborbe/log"
	"github.com/golang/glog"

	"github.com/bborbe/nuke-apply/pkg/apply"
)

// Operation is the wire name of the apply command. It is the last path segment
// of the gateway URL: POST /api/1.0/command/nuke-apply-v1/apply.
const Operation base.CommandOperation = "apply"

// NewCommandObjectExecutorApply returns the executor for the apply operation.
//
// Every failure path returns a real error on purpose: the result sender turns a
// non-nil error into a Failure entry on the result topic, which is what an
// operator needs to see. Returning cdb.ErrCommandObjectSkipped would publish
// nothing at all, and returning nil,nil,nil would publish a Success that never
// happened.
func NewCommandObjectExecutorApply(
	componentResolver apply.ComponentResolver,
	applier apply.Applier,
	logSamplerFactory log.SamplerFactory,
) cdb.CommandObjectExecutorTx {
	logSampler := logSamplerFactory.Sampler()
	return cdb.CommandObjectExecutorTxFunc(
		Operation,
		true,
		func(
			ctx context.Context,
			_ libkv.Tx,
			commandObject cdb.CommandObject,
		) (*base.EventID, base.Event, error) {
			var command apply.Command
			if err := commandObject.Command.Data.MarshalInto(ctx, &command); err != nil {
				return nil, nil, errors.Wrapf(ctx, err, "unmarshal apply command failed")
			}
			if err := command.Validate(ctx); err != nil {
				return nil, nil, errors.Wrapf(ctx, err, "validate apply command failed")
			}
			component, err := componentResolver.Resolve(ctx, command.Path)
			if err != nil {
				return nil, nil, errors.Wrapf(
					ctx,
					err,
					"resolve component '%s' failed",
					command.Path,
				)
			}
			if err := applier.Apply(ctx, component); err != nil {
				return nil, nil, errors.Wrapf(ctx, err, "apply component '%s' failed", command.Path)
			}
			if logSampler.IsSample() {
				glog.V(2).Infof("apply component %s completed (sample)", component.Path)
			}
			return base.EventID(component.Path).Ptr(), commandObject.Command.Data, nil
		},
	)
}
