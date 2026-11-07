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

	"github.com/bborbe/k8s-apply/pkg/apply"
	"github.com/bborbe/k8s-apply/pkg/promotion"
)

// Operation is the wire name of the apply command. It is the last path segment
// of the gateway URL: POST /api/1.0/command/k8s-apply-v1/apply.
const Operation base.CommandOperation = "apply"

// NewCommandObjectExecutorApply returns the executor for the apply operation.
//
// Every failure path returns a real error on purpose: the result sender turns a
// non-nil error into a Failure entry on the result topic, which is what an
// operator needs to see. Returning cdb.ErrCommandObjectSkipped would publish
// nothing at all, and returning nil,nil,nil would publish a Success that never
// happened.
//
// There is no approval gate here. The merge to the branch the apply pod cloned
// is the approval, so a command that arrives on this stage's topic is one the
// branch already carries — and Validate is the only refusal before the
// component is resolved and make runs.
//
// The promotion decision runs once per attempted apply and never changes the
// apply's result: a promotion that cannot be recorded must not turn a successful
// dev apply into a failed one.
func NewCommandObjectExecutorApply(
	stage apply.Stage,
	componentResolver apply.ComponentResolver,
	applier apply.Applier,
	promoter promotion.Promoter,
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
			// Logged before Validate, so a command that is later refused is still
			// logged. This is the line that proves a promotion arrived on sun —
			// the sun instance consumes the promotion's identifier as its own
			// event id — and the line that shows which operator command started
			// the chain on dev.
			glog.Infof(
				"consumed apply command '%s' for component '%s' on stage '%s'",
				commandObject.Command.ID,
				command.Path,
				string(stage),
			)
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
			applyErr := applier.Apply(ctx, component)

			// The promotion decision runs once per attempted apply, and its
			// failure is never allowed to change the apply's own result: a
			// promotion that cannot be recorded must not turn a successful dev
			// apply into a failed one.
			if promoErr := promoter.Promote(ctx, component.Path, applyErr); promoErr != nil {
				glog.V(2).Infof(
					"record promotion for component %s failed: %v",
					component.Path,
					promoErr,
				)
			}

			if applyErr != nil {
				return nil, nil, errors.Wrapf(
					ctx,
					applyErr,
					"apply component '%s' failed",
					command.Path,
				)
			}
			if logSampler.IsSample() {
				glog.V(2).Infof("apply component %s completed (sample)", component.Path)
			}
			return base.EventID(component.Path).Ptr(), commandObject.Command.Data, nil
		},
	)
}
