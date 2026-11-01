// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apply

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/bborbe/errors"
)

// ApprovalOptionApprove is the label the operator's answer must carry for it to
// count as an approval. The card is posted with exactly two options so the
// distinction is a value comparison rather than a guess at free text.
const ApprovalOptionApprove = "approve"

// Approval is the operator approval a command carries on a stage that requires
// one. Every field is machine-checkable: nothing here is a rendered sentence,
// because a gate that reads display text is a gate that can be talked past.
type Approval struct {
	// ID is the attention store item the approval was collected on.
	ID string `json:"id"`
	// Component is the component the approval was given for.
	Component string `json:"component"`
	// Commit is the deploy-repo commit the approval covers.
	Commit string `json:"commit"`
	// ApprovedBy names the approver, as recorded when the approval was taken.
	ApprovedBy string `json:"approved_by"`
	// ApprovedAt and ExpiresAt are RFC3339. An approval without a window
	// cannot be told from a stale one, so both are required.
	ApprovedAt string `json:"approved_at"`
	ExpiresAt  string `json:"expires_at"`
	// ActionSHA256 is the digest of the canonical action. It is compared
	// against a value recomputed from the command, never trusted as given.
	ActionSHA256 string `json:"action_sha256"`
}

// ActionHash is the canonical digest of one apply action.
//
// It binds the stage, the component and the commit into a single value, so an
// approval taken for one change cannot be replayed against another — including
// the case where only the stage differs, which is the stage-crossing bug this
// whole task exists to prevent.
//
// The input is newline-joined rather than concatenated so that no choice of
// component or commit can shift the field boundaries and collide with a
// different action.
func ActionHash(stage Stage, component, commit string) string {
	sum := sha256.Sum256([]byte("apply\n" + string(stage) + "\n" + component + "\n" + commit))
	return hex.EncodeToString(sum[:])
}

// ParseApproval reads the command's approval field.
//
// It returns (nil, nil) when the field is absent — a command carrying no
// approval is a legitimate payload that the stage's policy then refuses, not a
// malformed one. Every other shape is an error, and the two refusal cases the
// task names get their own message so the result topic can carry a reason an
// operator can act on rather than "invalid json".
func ParseApproval(ctx context.Context, raw json.RawMessage) (*Approval, error) {
	trimmed := trimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil, nil
	}
	if trimmed[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(trimmed, &items); err != nil {
			return nil, errors.Wrap(ctx, err, "parse approval array failed")
		}
		if len(items) != 1 {
			return nil, errors.Errorf(
				ctx,
				"more than one approval carried: %d approvals, exactly one is required",
				len(items),
			)
		}
		trimmed = trimSpace(items[0])
	}
	var approval Approval
	if err := json.Unmarshal(trimmed, &approval); err != nil {
		return nil, errors.Wrap(ctx, err, "parse approval object failed")
	}
	if approval.ID == "" {
		return nil, errors.Errorf(ctx, "approval carries no id")
	}
	return &approval, nil
}

func trimSpace(raw []byte) []byte {
	return bytes.TrimSpace(raw)
}
