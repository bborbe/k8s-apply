// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package promotion_test

import (
	"context"
	"encoding/json"

	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/errors"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/k8s-apply/mocks"
	"github.com/bborbe/k8s-apply/pkg/apply"
	"github.com/bborbe/k8s-apply/pkg/promotion"
)

var _ = Describe("Promoter", func() {
	const componentPath = "a2a-worker-controller"

	var ctx context.Context
	var gateway *mocks.PromotionGateway
	var publisher *mocks.PromotionRecordPublisher
	var promoter promotion.Promoter

	BeforeEach(func() {
		ctx = context.Background()
		gateway = &mocks.PromotionGateway{}
		publisher = &mocks.PromotionRecordPublisher{}
		promoter = promotion.NewPromoter(
			apply.StageDev,
			gateway,
			publisher,
			base.NewIdentifierGeneratorCounter[base.EventID](),
		)
	})

	// The single published record. Asserting the call count first means a
	// second publish fails here rather than being silently ignored by
	// PublishArgsForCall(0).
	singleRecord := func() promotion.Record {
		Expect(publisher.PublishCallCount()).To(Equal(1))
		_, published := publisher.PublishArgsForCall(0)
		return published
	}

	// The record as a consumer sees it: the serialized map, not the Go value.
	// Comparing Go constants proves only what this test typed; these literals
	// are what the boundary actually emits.
	wireRecord := func() map[string]any {
		raw, err := json.Marshal(singleRecord())
		Expect(err).To(BeNil())
		var decoded map[string]any
		Expect(json.Unmarshal(raw, &decoded)).To(Succeed())
		return decoded
	}

	Describe("withholding on a failed apply", func() {
		It("publishes one refusal naming sun as withheld and never calls the gateway", func() {
			Expect(promoter.Promote(ctx, componentPath, errors.Errorf(ctx, "make exited 2"))).
				To(Succeed())

			record := singleRecord()
			Expect(record.Verdict).To(Equal(promotion.VerdictWithhold))
			Expect(record.ToStage).To(Equal(apply.StageSun))
			Expect(record.Component).To(Equal(componentPath))
			Expect(record.Reason).To(ContainSubstring("make exited 2"))
			Expect(record.EventID).To(BeEmpty())
			Expect(record.Delivery).To(BeEmpty())
			Expect(gateway.SendCallCount()).To(Equal(0))
		})

		It("serializes the wire values a consumer reads", func() {
			Expect(promoter.Promote(ctx, componentPath, errors.Errorf(ctx, "make exited 2"))).
				To(Succeed())

			decoded := wireRecord()
			Expect(decoded).NotTo(HaveKey("operation"))
			Expect(decoded).NotTo(HaveKey("success"))
			Expect(decoded).To(HaveKeyWithValue("kind", "promotion"))
			Expect(decoded).To(HaveKeyWithValue("component", componentPath))
			Expect(decoded).To(HaveKeyWithValue("to_stage", "sun"))
			Expect(decoded).To(HaveKeyWithValue("verdict", "withhold"))
		})
	})

	Describe("promoting a healthy apply", func() {
		It("issues one identifier the command did not choose and calls the gateway once", func() {
			Expect(promoter.Promote(ctx, componentPath, nil)).To(Succeed())

			record := singleRecord()
			Expect(record.Verdict).To(Equal(promotion.VerdictPromote))
			Expect(record.ToStage).To(Equal(apply.StageSun))
			Expect(record.Delivery).To(Equal(promotion.DeliveryAccepted))
			Expect(record.EventID).NotTo(BeEmpty())

			Expect(gateway.SendCallCount()).To(Equal(1))
			_, sentPath, sentID := gateway.SendArgsForCall(0)
			Expect(sentPath).To(Equal(componentPath))
			Expect(sentID).To(Equal(record.EventID))
		})

		It("serializes the wire values a consumer reads", func() {
			Expect(promoter.Promote(ctx, componentPath, nil)).To(Succeed())

			decoded := wireRecord()
			Expect(decoded).NotTo(HaveKey("operation"))
			Expect(decoded).NotTo(HaveKey("success"))
			Expect(decoded).To(HaveKeyWithValue("kind", "promotion"))
			Expect(decoded).To(HaveKeyWithValue("component", componentPath))
			Expect(decoded).To(HaveKeyWithValue("to_stage", "sun"))
			Expect(decoded).To(HaveKeyWithValue("verdict", "promote"))
			Expect(decoded).To(HaveKeyWithValue("delivery", "accepted"))
		})

		// The identifier must never be the component path: the path is the same
		// value the dev command already carries, so reusing it would make the
		// sun side's event id indistinguishable from the dev side's.
		It("never uses the component path as the identifier", func() {
			Expect(promoter.Promote(ctx, componentPath, nil)).To(Succeed())

			Expect(singleRecord().EventID).NotTo(Equal(base.EventID(componentPath)))
		})
	})

	Describe("when the gateway refuses the promotion", func() {
		It("records a failed delivery that keeps the identifier and still succeeds", func() {
			gateway.SendReturns(errors.Errorf(ctx, "gateway returned 500"))

			Expect(promoter.Promote(ctx, componentPath, nil)).To(Succeed())

			record := singleRecord()
			Expect(record.Delivery).To(Equal(promotion.DeliveryFailed))
			Expect(record.Reason).To(ContainSubstring("500"))
			Expect(record.EventID).NotTo(BeEmpty())
			Expect(gateway.SendCallCount()).To(Equal(1))
		})

		It("serializes the failed delivery", func() {
			gateway.SendReturns(errors.Errorf(ctx, "gateway returned 500"))

			Expect(promoter.Promote(ctx, componentPath, nil)).To(Succeed())

			Expect(wireRecord()).To(HaveKeyWithValue("delivery", "failed"))
		})
	})

	Describe("when the record cannot be published", func() {
		It("returns the publish error and does not hide that the gateway was called", func() {
			publisher.PublishReturns(errors.Errorf(ctx, "kafka down"))

			err := promoter.Promote(ctx, componentPath, nil)

			Expect(err).NotTo(BeNil())
			Expect(err.Error()).To(ContainSubstring("kafka down"))
			Expect(gateway.SendCallCount()).To(Equal(1))
		})
	})

	// Sun and prod are non-promoting by construction, not by which promoter the
	// factory happened to wire. Both an apply failure and a success must stay
	// silent on those stages.
	DescribeTable(
		"records nothing on a stage that does not promote",
		func(stage apply.Stage, applyErr error) {
			promoter := promotion.NewPromoter(
				stage,
				gateway,
				publisher,
				base.NewIdentifierGeneratorCounter[base.EventID](),
			)

			Expect(promoter.Promote(ctx, componentPath, applyErr)).To(Succeed())

			Expect(publisher.PublishCallCount()).To(Equal(0))
			Expect(gateway.SendCallCount()).To(Equal(0))
		},
		Entry("sun on a successful apply", apply.StageSun, nil),
		Entry(
			"sun on a failed apply",
			apply.StageSun,
			errors.Errorf(context.Background(), "make exited 2"),
		),
		Entry("prod on a successful apply", apply.StageProd, nil),
		Entry(
			"prod on a failed apply",
			apply.StageProd,
			errors.Errorf(context.Background(), "make exited 2"),
		),
	)

	// The evidence that ONE entry point produces both verdicts. A stub wired
	// into the existing failure path could satisfy the refusal alone; a single
	// table over both outcomes cannot.
	//
	// The Entry arguments use context.Background(), never the suite's ctx:
	// Entry parameters are evaluated eagerly when the Ginkgo tree is built,
	// before any BeforeEach runs, so ctx is still nil there and errors.Errorf
	// would panic on it.
	DescribeTable("decides one verdict per attempted apply",
		func(applyErr error, wantVerdict promotion.Verdict, wantTarget apply.Stage) {
			Expect(promoter.Promote(ctx, componentPath, applyErr)).To(Succeed())

			record := singleRecord()
			Expect(record.Verdict).To(Equal(wantVerdict))
			Expect(record.ToStage).To(Equal(wantTarget))
		},
		Entry("promote on a healthy apply", nil, promotion.VerdictPromote, apply.StageSun),
		Entry(
			"withhold on a failed apply",
			errors.Errorf(context.Background(), "post-apply health check failed"),
			promotion.VerdictWithhold,
			apply.StageSun,
		),
	)
})
