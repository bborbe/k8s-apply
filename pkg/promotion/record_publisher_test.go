// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package promotion_test

import (
	"context"
	"encoding/json"

	"github.com/IBM/sarama"
	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/errors"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/k8s-apply/pkg/apply"
	"github.com/bborbe/k8s-apply/pkg/promotion"
)

// captureProducer is a test-local double for libkafka.SyncProducer: libkafka
// ships no counterfeiter fake for this interface.
type captureProducer struct {
	messages []*sarama.ProducerMessage
	err      error
}

func (p *captureProducer) SendMessage(
	_ context.Context,
	msg *sarama.ProducerMessage,
) (int32, int64, error) {
	if p.err != nil {
		return 0, 0, p.err
	}
	p.messages = append(p.messages, msg)
	return 0, int64(len(p.messages)), nil
}

func (p *captureProducer) SendMessages(_ context.Context, _ []*sarama.ProducerMessage) error {
	return nil
}

func (p *captureProducer) Close() error {
	return nil
}

var _ = Describe("RecordPublisher", func() {
	var ctx context.Context
	var producer *captureProducer
	var publisher promotion.RecordPublisher

	BeforeEach(func() {
		ctx = context.Background()
		producer = &captureProducer{}
		publisher = promotion.NewRecordPublisher(
			producer,
			base.TopicPrefixFromBranch(apply.StageDev.Branch()),
		)
	})

	single := func() *sarama.ProducerMessage {
		Expect(producer.messages).To(HaveLen(1))
		return producer.messages[0]
	}

	It("writes to the dev result topic", func() {
		Expect(publisher.Publish(ctx, promotion.Record{
			Kind:      promotion.KindPromotion,
			Component: "a2a-worker-controller",
			ToStage:   apply.StageSun,
			Verdict:   promotion.VerdictPromote,
			EventID:   base.EventID("11111111-1111-1111-1111-111111111111"),
			Delivery:  promotion.DeliveryAccepted,
		})).To(Succeed())

		Expect(single().Topic).To(Equal("develop-k8s-apply-v1-result"))
	})

	It("keys a promote by its identifier and round-trips the record", func() {
		want := promotion.Record{
			Kind:      promotion.KindPromotion,
			Component: "a2a-worker-controller",
			ToStage:   apply.StageSun,
			Verdict:   promotion.VerdictPromote,
			EventID:   base.EventID("11111111-1111-1111-1111-111111111111"),
			Delivery:  promotion.DeliveryAccepted,
		}
		Expect(publisher.Publish(ctx, want)).To(Succeed())

		msg := single()
		key, err := msg.Key.Encode()
		Expect(err).To(BeNil())
		Expect(string(key)).To(Equal(want.EventID.String()))

		value, err := msg.Value.Encode()
		Expect(err).To(BeNil())
		var decoded promotion.Record
		Expect(json.Unmarshal(value, &decoded)).To(Succeed())
		Expect(decoded.Verdict).To(Equal(want.Verdict))
		Expect(decoded.ToStage).To(Equal(want.ToStage))
		Expect(decoded.EventID).To(Equal(want.EventID))
	})

	// A withhold has no identifier, so an empty key would scatter every refusal
	// across partitions. The component is the stable key that is available.
	It("keys a withhold by the component when the record has no identifier", func() {
		Expect(publisher.Publish(ctx, promotion.Record{
			Kind:      promotion.KindPromotion,
			Component: "a2a-worker-controller",
			ToStage:   apply.StageSun,
			Verdict:   promotion.VerdictWithhold,
			Reason:    "make exited 2",
		})).To(Succeed())

		key, err := single().Key.Encode()
		Expect(err).To(BeNil())
		Expect(string(key)).To(Equal("a2a-worker-controller"))
	})

	It("surfaces a send failure naming the topic", func() {
		producer.err = errors.Errorf(ctx, "broker unavailable")

		err := publisher.Publish(ctx, promotion.Record{
			Kind:      promotion.KindPromotion,
			Component: "a2a-worker-controller",
			ToStage:   apply.StageSun,
			Verdict:   promotion.VerdictWithhold,
		})

		Expect(err).NotTo(BeNil())
		Expect(err.Error()).To(ContainSubstring("develop-k8s-apply-v1-result"))
		Expect(err.Error()).To(ContainSubstring("broker unavailable"))
	})
})
