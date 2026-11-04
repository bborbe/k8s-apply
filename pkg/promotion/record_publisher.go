// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package promotion

import (
	"context"
	"encoding/json"

	"github.com/IBM/sarama"
	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/errors"
	libkafka "github.com/bborbe/kafka"

	"github.com/bborbe/k8s-apply/pkg/apply"
)

// NewRecordPublisher returns a RecordPublisher that writes to the result topic
// this instance's own branch prefix names — the same topic the existing Success
// and Failure entries land on.
func NewRecordPublisher(
	syncProducer libkafka.SyncProducer,
	prefix base.TopicPrefix,
) RecordPublisher {
	return &recordPublisher{
		syncProducer: syncProducer,
		prefix:       prefix,
	}
}

type recordPublisher struct {
	syncProducer libkafka.SyncProducer
	prefix       base.TopicPrefix
}

// Publish sends one record to the result topic.
//
// The message is keyed by the promotion's own identifier when it has one, so a
// promote and its refusal stay in one partition, and by the component otherwise:
// a withhold has no identifier and would otherwise land on a random partition
// from an empty key.
func (p *recordPublisher) Publish(ctx context.Context, record Record) error {
	body, err := json.Marshal(record)
	if err != nil {
		return errors.Wrap(ctx, err, "serialize promotion record failed")
	}

	key := record.Component
	if record.EventID != "" {
		key = record.EventID.String()
	}

	topic := apply.SchemaID.ResultTopic(p.prefix)
	if _, _, err := p.syncProducer.SendMessage(ctx, &sarama.ProducerMessage{
		Topic: topic.String(),
		Key:   sarama.StringEncoder(key),
		Value: sarama.ByteEncoder(body),
	}); err != nil {
		return errors.Wrapf(ctx, err, "send promotion record to topic %s failed", topic)
	}
	return nil
}
