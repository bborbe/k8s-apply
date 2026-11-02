// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package command_test

import (
	"context"
	"encoding/json"

	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/cqrs/cdb"
	"github.com/bborbe/cqrs/iam"
	"github.com/bborbe/errors"
	"github.com/bborbe/log"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/k8s-apply/mocks"
	"github.com/bborbe/k8s-apply/pkg/apply"
	"github.com/bborbe/k8s-apply/pkg/command"
)

var _ = Describe("CommandObjectExecutorApply", func() {
	var ctx context.Context
	var componentResolver *mocks.ComponentResolver
	var applier *mocks.Applier
	var approver *mocks.Approver
	var revision *mocks.Revision
	var executor cdb.CommandObjectExecutorTx

	BeforeEach(func() {
		ctx = context.Background()
		componentResolver = &mocks.ComponentResolver{}
		applier = &mocks.Applier{}
		approver = &mocks.Approver{}
		revision = &mocks.Revision{}
		executor = command.NewCommandObjectExecutorApply(
			apply.StageDev,
			componentResolver,
			applier,
			approver,
			revision,
			log.DefaultSamplerFactory,
		)
	})

	commandObject := func(data base.Event) cdb.CommandObject {
		return cdb.CommandObject{
			SchemaID: apply.SchemaID,
			Command: base.Command{
				RequestID: base.NewRequestID(),
				Initiator: iam.Initiator("test"),
				Operation: command.Operation,
				Data:      data,
			},
		}
	}

	It("declares the apply operation and publishes a result", func() {
		Expect(executor.CommandOperation()).To(Equal(base.CommandOperation("apply")))
		Expect(executor.SendResultEnabled()).To(BeTrue())
	})

	It("applies the named component and returns its path as event id", func() {
		component := &apply.Component{Path: "trading/core/symbol"}
		componentResolver.ResolveReturns(component, nil)

		eventID, event, err := executor.HandleCommand(
			ctx,
			nil,
			commandObject(base.Event{"path": "trading/core/symbol"}),
		)

		Expect(err).To(BeNil())
		Expect(eventID).NotTo(BeNil())
		Expect(eventID.String()).To(Equal("trading/core/symbol"))
		Expect(event).NotTo(BeNil())
		_, resolvedPath := componentResolver.ResolveArgsForCall(0)
		Expect(resolvedPath).To(Equal("trading/core/symbol"))
		_, appliedComponent := applier.ApplyArgsForCall(0)
		Expect(appliedComponent).To(Equal(component))
	})

	// The whole point of the prod gate is that a refused command never reaches
	// the filesystem, let alone a shell. Asserting only "an error came back"
	// would pass for a service that resolved, resolved-and-failed, or applied
	// and then reported a failure.
	It("consults the approver before resolving, and applies nothing when it refuses", func() {
		approver.ApproveReturns(errors.Errorf(ctx, "no approval carried"))

		_, _, err := executor.HandleCommand(
			ctx,
			nil,
			commandObject(base.Event{"path": "trading/core/symbol"}),
		)

		Expect(err).NotTo(BeNil())
		Expect(err.Error()).To(ContainSubstring("no approval carried"))
		Expect(approver.ApproveCallCount()).To(Equal(1))
		Expect(componentResolver.ResolveCallCount()).To(Equal(0))
		Expect(applier.ApplyCallCount()).To(Equal(0))
	})

	It("passes the stage and the command's path to the approver", func() {
		componentResolver.ResolveReturns(&apply.Component{Path: "alertmanager"}, nil)

		_, _, err := executor.HandleCommand(
			ctx,
			nil,
			commandObject(base.Event{"path": "alertmanager"}),
		)
		Expect(err).To(BeNil())

		_, stage, approved := approver.ApproveArgsForCall(0)
		Expect(stage).To(Equal(apply.StageDev))
		Expect(approved.Path).To(Equal("alertmanager"))
	})

	// Fail-closed wiring: a prod deployment that forgot to configure an
	// approver must refuse every apply, not silently skip the gate.
	It("refuses a prod apply when only the noop approver is wired", func() {
		prodExecutor := command.NewCommandObjectExecutorApply(
			apply.StageProd,
			componentResolver,
			applier,
			apply.NewNoopApprover(),
			revision,
			log.DefaultSamplerFactory,
		)

		_, _, err := prodExecutor.HandleCommand(
			ctx,
			nil,
			commandObject(base.Event{"path": "alertmanager"}),
		)

		Expect(err).NotTo(BeNil())
		Expect(err.Error()).To(ContainSubstring("no approver is configured"))
		Expect(applier.ApplyCallCount()).To(Equal(0))
	})

	// The approval names a commit; make runs against whatever the pod cloned.
	// Without this check the binding is paperwork — the hash can say commit A
	// while the checkout sits at commit B.
	It("refuses when the checkout is not the approved commit", func() {
		approver.ApproveReturns(nil)
		revision.VerifyReturns(errors.Errorf(ctx, "checkout is at b, the approval covers a"))

		prodExecutor := command.NewCommandObjectExecutorApply(
			apply.StageProd,
			componentResolver,
			applier,
			approver,
			revision,
			log.DefaultSamplerFactory,
		)
		raw, err := json.Marshal(map[string]any{
			"id":            "abc",
			"component":     "alertmanager",
			"commit":        "aaa",
			"action_sha256": apply.ActionHash(apply.StageProd, "alertmanager", "aaa"),
		})
		Expect(err).To(BeNil())

		_, _, err = prodExecutor.HandleCommand(
			ctx,
			nil,
			commandObject(base.Event{"path": "alertmanager", "approval": json.RawMessage(raw)}),
		)

		Expect(err).NotTo(BeNil())
		Expect(err.Error()).To(ContainSubstring("checkout is at b"))
		Expect(revision.VerifyCallCount()).To(Equal(1))
		Expect(componentResolver.ResolveCallCount()).To(Equal(0))
		Expect(applier.ApplyCallCount()).To(Equal(0))
	})

	It("does not consult the revision on a stage that needs no approval", func() {
		componentResolver.ResolveReturns(&apply.Component{Path: "alertmanager"}, nil)

		_, _, err := executor.HandleCommand(
			ctx,
			nil,
			commandObject(base.Event{"path": "alertmanager"}),
		)
		Expect(err).To(BeNil())
		Expect(revision.VerifyCallCount()).To(Equal(0))
	})

	It("fails without resolving when the command carries no path", func() {
		_, _, err := executor.HandleCommand(ctx, nil, commandObject(base.Event{}))

		Expect(err).NotTo(BeNil())
		Expect(componentResolver.ResolveCallCount()).To(Equal(0))
		Expect(applier.ApplyCallCount()).To(Equal(0))
	})

	It("surfaces a docker refusal as an error so a Failure result is published", func() {
		componentResolver.ResolveReturns(nil, errors.Wrapf(
			ctx,
			apply.ErrDockerComponent,
			"component '%s' builds a container image in its apply target",
			"egress-proxy",
		))

		_, _, err := executor.HandleCommand(
			ctx,
			nil,
			commandObject(base.Event{"path": "egress-proxy"}),
		)

		Expect(err).NotTo(BeNil())
		Expect(errors.Is(err, apply.ErrDockerComponent)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("docker component refused"))
		Expect(applier.ApplyCallCount()).To(Equal(0))
	})

	It("surfaces an excluded-component refusal and never calls the applier", func() {
		componentResolver.ResolveReturns(nil, errors.Wrapf(
			ctx,
			apply.ErrExcludedComponent,
			"component '%s' is excluded by design",
			"teamvault",
		))

		_, _, err := executor.HandleCommand(
			ctx,
			nil,
			commandObject(base.Event{"path": "teamvault"}),
		)

		Expect(err).NotTo(BeNil())
		Expect(errors.Is(err, apply.ErrExcludedComponent)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("excluded by design"))
		Expect(applier.ApplyCallCount()).To(Equal(0))
	})

	It("surfaces an apply failure as an error", func() {
		componentResolver.ResolveReturns(&apply.Component{Path: "x"}, nil)
		applier.ApplyReturns(errors.Errorf(ctx, "make exited 2"))

		_, _, err := executor.HandleCommand(ctx, nil, commandObject(base.Event{"path": "x"}))

		Expect(err).NotTo(BeNil())
		Expect(err.Error()).To(ContainSubstring("make exited 2"))
	})
})
