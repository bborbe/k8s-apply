// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package promotion

import (
	"context"

	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/errors"
)

// NewNoopGateway returns the Gateway a stage that does not promote gets.
//
// It refuses instead of silently accepting, mirroring NewNoopApprover: a
// promotion wired to it is recorded as a failed delivery with a reason, never
// as accepted. That is the only failure direction worth having — a silently
// accepting noop would report a promotion that never left the pod.
func NewNoopGateway() Gateway {
	return noopGateway{}
}

type noopGateway struct{}

func (noopGateway) Send(ctx context.Context, _ string, _ base.EventID) error {
	return errors.Errorf(ctx, "no promotion gateway is configured")
}
