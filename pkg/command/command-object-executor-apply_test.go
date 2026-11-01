// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package command_test

import (
	"context"

	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/cqrs/cdb"
	"github.com/bborbe/cqrs/iam"
	"github.com/bborbe/errors"
	"github.com/bborbe/log"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/nuke-apply/mocks"
	"github.com/bborbe/nuke-apply/pkg/apply"
	"github.com/bborbe/nuke-apply/pkg/command"
)

var _ = Describe("CommandObjectExecutorApply", func() {
	var ctx context.Context
	var componentResolver *mocks.ComponentResolver
	var applier *mocks.Applier
	var executor cdb.CommandObjectExecutorTx

	BeforeEach(func() {
		ctx = context.Background()
		componentResolver = &mocks.ComponentResolver{}
		applier = &mocks.Applier{}
		executor = command.NewCommandObjectExecutorApply(
			componentResolver,
			applier,
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
