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
	"github.com/bborbe/k8s-apply/pkg/promotion"
)

var _ = Describe("CommandObjectExecutorApply", func() {
	var ctx context.Context
	var componentResolver *mocks.ComponentResolver
	var applier *mocks.Applier
	var approver *mocks.Approver
	var revision *mocks.Revision
	var promoter *mocks.PromotionPromoter
	var executor cdb.CommandObjectExecutorTx

	BeforeEach(func() {
		ctx = context.Background()
		componentResolver = &mocks.ComponentResolver{}
		applier = &mocks.Applier{}
		approver = &mocks.Approver{}
		revision = &mocks.Revision{}
		promoter = &mocks.PromotionPromoter{}
		executor = command.NewCommandObjectExecutorApply(
			apply.StageDev,
			componentResolver,
			applier,
			approver,
			revision,
			promoter,
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
			promoter,
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
			promoter,
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

	Describe("the promotion decision", func() {
		It("records the decision once per attempted apply", func() {
			component := &apply.Component{Path: "a2a-worker-controller"}
			componentResolver.ResolveReturns(component, nil)

			_, _, err := executor.HandleCommand(
				ctx,
				nil,
				commandObject(base.Event{"path": "a2a-worker-controller"}),
			)

			Expect(err).To(BeNil())
			Expect(promoter.PromoteCallCount()).To(Equal(1))
			_, decidedPath, decidedErr := promoter.PromoteArgsForCall(0)
			Expect(decidedPath).To(Equal(component.Path))
			Expect(decidedErr).To(BeNil())
		})

		It("records the refusal with the apply error", func() {
			componentResolver.ResolveReturns(&apply.Component{Path: "x"}, nil)
			applier.ApplyReturns(errors.Errorf(ctx, "make exited 2"))

			_, _, err := executor.HandleCommand(
				ctx,
				nil,
				commandObject(base.Event{"path": "x"}),
			)

			Expect(promoter.PromoteCallCount()).To(Equal(1))
			_, _, decidedErr := promoter.PromoteArgsForCall(0)
			Expect(decidedErr).NotTo(BeNil())
			Expect(decidedErr.Error()).To(ContainSubstring("make exited 2"))
			Expect(err).NotTo(BeNil())
			Expect(err.Error()).To(ContainSubstring("make exited 2"))
		})

		// A command that never reached an apply attempt has no verdict to take.
		It("decides nothing when the component cannot be resolved", func() {
			componentResolver.ResolveReturns(nil, errors.Errorf(ctx, "unknown component"))

			_, _, err := executor.HandleCommand(
				ctx,
				nil,
				commandObject(base.Event{"path": "nope"}),
			)

			Expect(err).NotTo(BeNil())
			Expect(promoter.PromoteCallCount()).To(Equal(0))
		})

		It("decides nothing when the approver refuses", func() {
			approver.ApproveReturns(errors.Errorf(ctx, "no approval carried"))

			_, _, err := executor.HandleCommand(
				ctx,
				nil,
				commandObject(base.Event{"path": "a2a-worker-controller"}),
			)

			Expect(err).NotTo(BeNil())
			Expect(promoter.PromoteCallCount()).To(Equal(0))
		})

		// The apply's own result must survive a promotion that could not be
		// recorded: a healthy dev apply that failed to record a promotion is
		// still a healthy dev apply.
		It("publishes the apply result when the promotion cannot be recorded", func() {
			componentResolver.ResolveReturns(&apply.Component{Path: "x"}, nil)
			promoter.PromoteReturns(errors.Errorf(ctx, "kafka down"))

			eventID, _, err := executor.HandleCommand(
				ctx,
				nil,
				commandObject(base.Event{"path": "x"}),
			)

			Expect(err).To(BeNil())
			Expect(eventID).NotTo(BeNil())
			Expect(eventID.String()).To(Equal("x"))
		})
	})

	Describe("end to end through the real promoter", func() {
		var gateway *mocks.PromotionGateway
		var publisher *mocks.PromotionRecordPublisher

		BeforeEach(func() {
			gateway = &mocks.PromotionGateway{}
			publisher = &mocks.PromotionRecordPublisher{}
		})

		// Builds an executor whose promoter is the real one, so the assertions
		// read the record the decision actually produced rather than a mock's
		// echo of what the test typed.
		newDevExecutor := func() cdb.CommandObjectExecutorTx {
			return command.NewCommandObjectExecutorApply(
				apply.StageDev,
				componentResolver,
				applier,
				approver,
				revision,
				promotion.NewPromoter(
					apply.StageDev,
					gateway,
					publisher,
					base.NewIdentifierGeneratorCounter[base.EventID](),
				),
				log.DefaultSamplerFactory,
			)
		}

		// The initiator is a credential. It may travel in the gateway header and
		// nowhere else, so every test that reads a record's contents also asserts
		// the initiator is absent from it.
		It("publishes a promote record carrying an identifier the command did not choose", func() {
			const initiator = "initiator-must-not-leak"

			componentResolver.ResolveReturns(&apply.Component{Path: "a2a-worker-controller"}, nil)

			commandWithID := cdb.CommandObject{
				SchemaID: apply.SchemaID,
				Command: base.Command{
					RequestID: base.NewRequestID(),
					Initiator: iam.Initiator(initiator),
					Operation: command.Operation,
					ID:        base.EventID("operator-1"),
					Data:      base.Event{"path": "a2a-worker-controller"},
				},
			}

			_, _, err := newDevExecutor().HandleCommand(ctx, nil, commandWithID)

			Expect(err).To(BeNil())
			Expect(publisher.PublishCallCount()).To(Equal(1))
			_, record := publisher.PublishArgsForCall(0)
			Expect(record.Verdict).To(Equal(promotion.VerdictPromote))
			Expect(record.ToStage).To(Equal(apply.StageSun))
			Expect(record.Delivery).To(Equal(promotion.DeliveryAccepted))
			Expect(record.EventID).NotTo(BeEmpty())
			Expect(record.EventID).NotTo(Equal(base.EventID("operator-1")))

			// Assert the absence of the value, not of a key named "initiator":
			// a field added under any other name would still leak the credential.
			serialized, err := json.Marshal(record)
			Expect(err).To(BeNil())
			Expect(string(serialized)).NotTo(ContainSubstring(initiator))

			Expect(gateway.SendCallCount()).To(Equal(1))
			_, sentPath, sentID := gateway.SendArgsForCall(0)
			Expect(sentPath).To(Equal("a2a-worker-controller"))
			Expect(sentID).To(Equal(record.EventID))
		})

		It("keeps the apply's success entry when the gateway returns 500", func() {
			componentResolver.ResolveReturns(&apply.Component{Path: "a2a-worker-controller"}, nil)
			gateway.SendReturns(errors.Errorf(ctx, "gateway returned 500"))

			eventID, _, err := newDevExecutor().HandleCommand(
				ctx,
				nil,
				commandObject(base.Event{"path": "a2a-worker-controller"}),
			)

			Expect(err).To(BeNil())
			Expect(eventID).NotTo(BeNil())
			Expect(eventID.String()).To(Equal("a2a-worker-controller"))

			Expect(publisher.PublishCallCount()).To(Equal(1))
			_, record := publisher.PublishArgsForCall(0)
			Expect(record.Delivery).To(Equal(promotion.DeliveryFailed))
			Expect(record.Reason).To(ContainSubstring("500"))
			Expect(record.EventID).NotTo(BeEmpty())
		})

		It("publishes a withhold record and calls no gateway when the health check fails", func() {
			componentResolver.ResolveReturns(&apply.Component{Path: "a2a-worker-controller"}, nil)
			applier.ApplyReturns(errors.Errorf(
				ctx,
				"post-apply health check failed for 'x': deployment 'y': not healthy after 5m",
			))

			_, _, err := newDevExecutor().HandleCommand(
				ctx,
				nil,
				commandObject(base.Event{"path": "a2a-worker-controller"}),
			)

			Expect(err).NotTo(BeNil())
			Expect(err.Error()).To(ContainSubstring("post-apply health check failed"))
			Expect(publisher.PublishCallCount()).To(Equal(1))
			_, record := publisher.PublishArgsForCall(0)
			Expect(record.Verdict).To(Equal(promotion.VerdictWithhold))
			Expect(record.Reason).To(ContainSubstring("post-apply health check failed"))
			Expect(gateway.SendCallCount()).To(Equal(0))
		})

		It("publishes a withhold record when the make step fails", func() {
			componentResolver.ResolveReturns(&apply.Component{Path: "x"}, nil)
			applier.ApplyReturns(errors.Errorf(ctx, "apply 'x' failed: make exited 2"))

			_, _, err := newDevExecutor().HandleCommand(
				ctx,
				nil,
				commandObject(base.Event{"path": "x"}),
			)

			Expect(err).NotTo(BeNil())
			Expect(publisher.PublishCallCount()).To(Equal(1))
			_, record := publisher.PublishArgsForCall(0)
			Expect(record.Verdict).To(Equal(promotion.VerdictWithhold))
			Expect(record.Reason).To(ContainSubstring("make exited 2"))
			Expect(gateway.SendCallCount()).To(Equal(0))
		})
	})

	// Prod must not move: it publishes no promotion entry for any apply, and the
	// approval gate keeps its order and its text. These tests fail if either
	// changes.
	Describe("prod invariance", func() {
		var gateway *mocks.PromotionGateway
		var publisher *mocks.PromotionRecordPublisher

		BeforeEach(func() {
			gateway = &mocks.PromotionGateway{}
			publisher = &mocks.PromotionRecordPublisher{}
			approver.ApproveReturns(nil)
			revision.VerifyReturns(nil)
			componentResolver.ResolveReturns(&apply.Component{Path: "alertmanager"}, nil)
		})

		approvedCommand := func() cdb.CommandObject {
			raw, err := json.Marshal(map[string]any{
				"id":            "abc",
				"component":     "alertmanager",
				"commit":        "aaa",
				"action_sha256": apply.ActionHash(apply.StageProd, "alertmanager", "aaa"),
			})
			Expect(err).To(BeNil())
			return commandObject(
				base.Event{"path": "alertmanager", "approval": json.RawMessage(raw)},
			)
		}

		newProdExecutor := func() cdb.CommandObjectExecutorTx {
			return command.NewCommandObjectExecutorApply(
				apply.StageProd,
				componentResolver,
				applier,
				approver,
				revision,
				promotion.NewPromoter(
					apply.StageProd,
					gateway,
					publisher,
					base.NewIdentifierGeneratorCounter[base.EventID](),
				),
				log.DefaultSamplerFactory,
			)
		}

		It("applies an approved prod command and records no promotion", func() {
			eventID, _, err := newProdExecutor().HandleCommand(ctx, nil, approvedCommand())

			Expect(err).To(BeNil())
			Expect(eventID.String()).To(Equal("alertmanager"))
			Expect(publisher.PublishCallCount()).To(Equal(0))
			Expect(gateway.SendCallCount()).To(Equal(0))
		})

		It("records no promotion when a prod apply fails", func() {
			applier.ApplyReturns(errors.Errorf(ctx, "make exited 2"))

			_, _, err := newProdExecutor().HandleCommand(ctx, nil, approvedCommand())

			Expect(err).NotTo(BeNil())
			Expect(publisher.PublishCallCount()).To(Equal(0))
			Expect(gateway.SendCallCount()).To(Equal(0))
		})
	})

	// The gateway's path segment and the consumed operation are the same wire
	// value, and pkg/promotion cannot import pkg/command to share it (this
	// package imports that one). This is the only place the two can be compared.
	It("keeps the gateway operation in step with the consumed operation", func() {
		Expect(string(command.Operation)).To(Equal(string(promotion.ApplyOperation)))
	})
})
