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

// enforceApproval refuses a command the stage will not accept: no recorded
// approval where one is required, or a checkout that is not the approved
// revision.
//
// It runs before the component is resolved, so a refused command never reaches
// the filesystem, let alone a shell.
func enforceApproval(
	ctx context.Context,
	stage apply.Stage,
	command apply.Command,
	approver apply.Approver,
	revision apply.Revision,
) error {
	if err := approver.Approve(ctx, stage, command); err != nil {
		return errors.Wrapf(
			ctx,
			err,
			"apply '%s' refused on stage '%s'",
			command.Path,
			string(stage),
		)
	}
	// An approval names a commit; make runs against whatever the pod cloned.
	// Without this check the binding is paperwork — the hash can say commit A
	// while the checkout sits at commit B.
	if !stage.RequiresApproval() {
		return nil
	}
	approval, err := apply.ParseApproval(ctx, command.Approval)
	if err != nil {
		return err
	}
	if approval == nil {
		return errors.Errorf(
			ctx,
			"apply '%s' reached the revision check with no approval",
			command.Path,
		)
	}
	if err := revision.Verify(ctx, approval.Commit); err != nil {
		return errors.Wrapf(
			ctx,
			err,
			"apply '%s' refused on stage '%s'",
			command.Path,
			string(stage),
		)
	}
	return nil
}

// NewCommandObjectExecutorApply returns the executor for the apply operation.
//
// Every failure path returns a real error on purpose: the result sender turns a
// non-nil error into a Failure entry on the result topic, which is what an
// operator needs to see. Returning cdb.ErrCommandObjectSkipped would publish
// nothing at all, and returning nil,nil,nil would publish a Success that never
// happened.
//
// The approver runs BEFORE the component is resolved and before any make
// invocation: on a stage that requires one, a command without a valid approval
// must not reach the filesystem, let alone a shell.
//
// The promotion decision runs once per attempted apply and never changes the
// apply's result: a promotion that cannot be recorded must not turn a successful
// dev apply into a failed one.
func NewCommandObjectExecutorApply(
	stage apply.Stage,
	componentResolver apply.ComponentResolver,
	applier apply.Applier,
	approver apply.Approver,
	revision apply.Revision,
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
			if err := enforceApproval(ctx, stage, command, approver, revision); err != nil {
				return nil, nil, err
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
