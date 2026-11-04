// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package promotion

import (
	"context"

	"github.com/bborbe/cqrs/base"

	"github.com/bborbe/k8s-apply/pkg/apply"
)

// KindPromotion is the "kind" every promotion record carries. A result consumer
// skips the record by finding this value where it expects an operation.
const KindPromotion = "promotion"

// ApplyOperation is the command operation a promotion issues, and therefore the
// last path segment of the gateway URL.
//
// It mirrors command.Operation in pkg/command and cannot import it back: that
// package imports this one. The drift test in pkg/command is what keeps the two
// values from separating.
const ApplyOperation base.CommandOperation = "apply"

// Verdict is the promotion decision taken for one attempted apply.
type Verdict string

const (
	// VerdictPromote means the apply was healthy, so the component is handed to
	// the next stage.
	VerdictPromote Verdict = "promote"
	// VerdictWithhold means the apply did not succeed, so the next stage keeps
	// the revision it already runs.
	VerdictWithhold Verdict = "withhold"
)

// Delivery is the outcome of carrying a promote verdict across to the target
// stage's command gateway.
type Delivery string

const (
	// DeliveryAccepted means the gateway accepted the command.
	DeliveryAccepted Delivery = "accepted"
	// DeliveryFailed means the gateway refused it or could not be reached. The
	// identifier stays in the record so the target stage can still be checked
	// for a command that may have arrived.
	DeliveryFailed Delivery = "failed"
)

// Record is the promotion entry on this instance's result topic. It is its own
// type, never base.Result and never embedding it: base.Result always serializes
// "operation" and "success", and a consumer would read this record as a failed
// apply. Consumers skip it by finding no operation on it.
type Record struct {
	Kind      string       `json:"kind"`
	Component string       `json:"component"`
	ToStage   apply.Stage  `json:"to_stage"`
	Verdict   Verdict      `json:"verdict"`
	EventID   base.EventID `json:"event_id,omitempty"`
	Delivery  Delivery     `json:"delivery,omitempty"`
	Reason    string       `json:"reason,omitempty"`
}

//counterfeiter:generate -o ../../mocks/promotion-record-publisher.go --fake-name PromotionRecordPublisher . RecordPublisher

// RecordPublisher writes one promotion record to this instance's result topic.
type RecordPublisher interface {
	Publish(ctx context.Context, record Record) error
}

//counterfeiter:generate -o ../../mocks/promotion-gateway.go --fake-name PromotionGateway . Gateway

// Gateway carries a promote verdict across to the next stage's command gateway.
//
// It is a seam rather than a direct call so a non-promoting stage can be wired
// to a gateway that refuses: a promotion reaching it is then recorded as a
// failed delivery instead of being silently dropped.
type Gateway interface {
	Send(ctx context.Context, componentPath string, eventID base.EventID) error
}

//counterfeiter:generate -o ../../mocks/promotion-promoter.go --fake-name PromotionPromoter . Promoter

// Promoter decides one promotion verdict per attempted apply and records it.
type Promoter interface {
	Promote(ctx context.Context, componentPath string, applyErr error) error
}
