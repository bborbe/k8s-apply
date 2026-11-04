// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package promotion

import (
	"context"

	"github.com/bborbe/cqrs/base"
	"github.com/golang/glog"

	"github.com/bborbe/k8s-apply/pkg/apply"
)

// NewPromoter returns the single promotion decision entry point.
//
// Every verdict an instance can take comes out of Promote, so a table test that
// reads one record per verdict proves the whole decision surface rather than
// two independent stubs.
func NewPromoter(
	stage apply.Stage,
	gateway Gateway,
	publisher RecordPublisher,
	generator base.IdentifierGenerator[base.EventID],
) Promoter {
	return &promoter{
		stage:     stage,
		gateway:   gateway,
		publisher: publisher,
		generator: generator,
	}
}

type promoter struct {
	stage     apply.Stage
	gateway   Gateway
	publisher RecordPublisher
	generator base.IdentifierGenerator[base.EventID]
}

// Promote decides one verdict for one attempted apply and records it.
//
// A stage that does not promote returns before any record exists, so sun and
// prod are non-promoting by construction rather than by which promoter the
// factory happened to wire.
func (p *promoter) Promote(
	ctx context.Context,
	componentPath string,
	applyErr error,
) error {
	if !p.stage.Promotes() {
		return nil
	}

	record := Record{
		Kind:      KindPromotion,
		Component: componentPath,
		ToStage:   apply.StageSun,
	}

	// The refusal carries the apply's own error text verbatim: it is the same
	// sentence the Failure entry already publishes, and composing a second
	// wording here is how an operator ends up with two stories for one event.
	if applyErr != nil {
		record.Verdict = VerdictWithhold
		record.Reason = applyErr.Error()
		return p.publisher.Publish(ctx, record)
	}

	record.Verdict = VerdictPromote
	record.EventID = p.generator.NewIdentifier()
	glog.Infof(
		"promotion of component '%s' to stage '%s' issued with identifier '%s'",
		componentPath,
		apply.StageSun,
		record.EventID,
	)

	if err := p.gateway.Send(ctx, componentPath, record.EventID); err != nil {
		record.Delivery = DeliveryFailed
		record.Reason = err.Error()
	} else {
		record.Delivery = DeliveryAccepted
	}

	return p.publisher.Publish(ctx, record)
}
