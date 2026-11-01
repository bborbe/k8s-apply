// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apply

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bborbe/errors"
)

//counterfeiter:generate -o ../../mocks/approver.go --fake-name Approver . Approver

// approvalClockSkew bounds how far a command's approved_at may sit from the
// store's answered_at for the same item.
//
// Without it the approval window is self-asserted: the payload could claim any
// approved_at and any expires_at and the service would honour them, with the
// store never corroborating that the operator answered then. The store's
// answered_at is the one timestamp the producer cannot write.
const approvalClockSkew = 5 * time.Minute

// maxApprovalLifetime caps the window a payload may declare. A gate whose
// expiry is chosen by the party being gated is not an expiry.
const maxApprovalLifetime = 24 * time.Hour

// Approver decides whether a command may be applied on a given stage.
//
// It is a separate collaborator from the Applier on purpose: the Applier runs
// make, and the question "may this run at all" belongs before it, in a place
// that can be swapped per stage without touching the shell-out path.
type Approver interface {
	Approve(ctx context.Context, stage Stage, command Command) error
}

// NewNoopApprover returns an Approver for stages that need none.
//
// It is deliberately fail-closed: asked about a stage that requires an
// approval, it refuses rather than passing. Wiring the noop approver on a prod
// deployment therefore disables prod applies instead of silently disabling the
// gate, which is the only failure direction worth having.
func NewNoopApprover() Approver {
	return noopApprover{}
}

type noopApprover struct{}

func (noopApprover) Approve(ctx context.Context, stage Stage, _ Command) error {
	if err := stage.Validate(ctx); err != nil {
		return err
	}
	if stage.RequiresApproval() {
		return errors.Errorf(
			ctx,
			"stage '%s' requires a recorded operator approval but no approver is configured",
			string(stage),
		)
	}
	return nil
}

// AttentionStoreApprover verifies a command's approval against the attention
// store — the one surface in this fleet where an answer is attributed to the
// operator rather than to whoever published the command.
type AttentionStoreApprover struct {
	baseURL string
	token   string
	client  *http.Client
	now     func() time.Time
}

// NewAttentionStoreApprover returns an Approver that reads approval records
// from the attention store at baseURL.
//
// token is sent as a bearer header when non-empty. Point baseURL at the store's
// gated listener where one exists: the approval record is the authorisation, so
// reading it over an ungated port leaves the gate open to anything in the
// cluster that can reach the store.
func NewAttentionStoreApprover(baseURL, token string, now func() time.Time) Approver {
	return &AttentionStoreApprover{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		client:  &http.Client{Timeout: 10 * time.Second},
		now:     now,
	}
}

// attentionItem is the subset of the store's item this approver reads. The full
// record carries far more; only these fields bear on the decision.
type attentionItem struct {
	ItemID     string `json:"item_id"`
	DedupKey   string `json:"dedup_key"`
	State      string `json:"state"`
	AnsweredAt string `json:"answered_at"`
	Answer     *struct {
		Kind  string `json:"kind"`
		Value string `json:"value"`
	} `json:"answer"`
}

// Approve runs every binding check the approval-carrier decision names, in the
// order that produces the most specific refusal first.
func (a *AttentionStoreApprover) Approve(ctx context.Context, stage Stage, command Command) error {
	if err := stage.Validate(ctx); err != nil {
		return err
	}
	if !stage.RequiresApproval() {
		return nil
	}

	approval, expected, err := verifyPayload(ctx, stage, command)
	if err != nil {
		return err
	}
	approvedAt, expiresAt, err := a.verifyWindow(ctx, approval)
	if err != nil {
		return err
	}
	item, err := a.fetch(ctx, approval.ID)
	if err != nil {
		return err
	}
	return verifyItem(ctx, approval, item, expected, approvedAt, expiresAt)
}

// verifyPayload decides everything the command alone can settle, and returns
// the approval plus the action hash the record must agree with.
func verifyPayload(
	ctx context.Context,
	stage Stage,
	command Command,
) (*Approval, string, error) {
	approval, err := ParseApproval(ctx, command.Approval)
	if err != nil {
		return nil, "", err
	}
	if approval == nil {
		return nil, "", errors.Errorf(
			ctx,
			"no approval carried: stage '%s' applies only with one recorded operator approval",
			string(stage),
		)
	}
	if approval.Component != command.Path {
		return nil, "", errors.Errorf(
			ctx,
			"approval names component '%s' but the command applies '%s'",
			approval.Component,
			command.Path,
		)
	}
	expected := ActionHash(stage, command.Path, approval.Commit)
	if approval.ActionSHA256 != expected {
		return nil, "", errors.Errorf(
			ctx,
			"approval action hash does not match this change: it covers a different stage, component or commit",
		)
	}
	return approval, expected, nil
}

// verifyWindow checks the payload's declared window against the clock, and
// returns both bounds so the record check can compare them with the store.
func (a *AttentionStoreApprover) verifyWindow(
	ctx context.Context,
	approval *Approval,
) (time.Time, time.Time, error) {
	approvedAt, err := parseTime(ctx, approval.ApprovedAt, "approved_at")
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	expiresAt, err := parseTime(ctx, approval.ExpiresAt, "expires_at")
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	now := a.now()
	if now.Before(approvedAt) {
		return time.Time{}, time.Time{}, errors.Errorf(
			ctx,
			"approval is not yet valid: approved_at is in the future",
		)
	}
	if !now.Before(expiresAt) {
		return time.Time{}, time.Time{}, errors.Errorf(
			ctx,
			"approval expired at %s: an approval carried past its window is stale",
			approval.ExpiresAt,
		)
	}
	if lifetime := expiresAt.Sub(approvedAt); lifetime > maxApprovalLifetime {
		return time.Time{}, time.Time{}, errors.Errorf(
			ctx,
			"approval '%s' declares a %s window, above the %s maximum",
			approval.ID,
			lifetime,
			maxApprovalLifetime,
		)
	}
	return approvedAt, expiresAt, nil
}

// verifyItem checks the stored record against the payload.
func verifyItem(
	ctx context.Context,
	approval *Approval,
	item *attentionItem,
	expected string,
	approvedAt time.Time,
	_ time.Time,
) error {
	// The binding is the item's dedup_key, never its payload: payload is
	// operator-facing display text, and driving a decision from it is how a
	// gate ends up matching the sentence rather than the action.
	if item.DedupKey != expected {
		return errors.Errorf(
			ctx,
			"approval '%s' was recorded for a different action: its dedup key does not match this change",
			approval.ID,
		)
	}
	if item.Answer == nil || item.Answer.Kind != "option" ||
		item.Answer.Value != ApprovalOptionApprove {
		return errors.Errorf(
			ctx,
			"approval '%s' was not answered with '%s'",
			approval.ID,
			ApprovalOptionApprove,
		)
	}
	if item.AnsweredAt == "" {
		return errors.Errorf(ctx, "approval '%s' carries an answer but no answered_at", approval.ID)
	}
	// The store's answered_at is authoritative; the payload's approved_at only
	// has to agree with it. Otherwise the window is whatever the publisher
	// typed.
	answeredAt, err := parseTime(ctx, item.AnsweredAt, "answered_at")
	if err != nil {
		return err
	}
	if skew := approvedAt.Sub(answeredAt); skew > approvalClockSkew || skew < -approvalClockSkew {
		return errors.Errorf(
			ctx,
			"approval '%s' claims approved_at %s but the store answered it at %s: the window must match the record, not the payload",
			approval.ID,
			approval.ApprovedAt,
			item.AnsweredAt,
		)
	}
	return nil
}

func parseTime(ctx context.Context, value, field string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, errors.Wrapf(ctx, err, "approval %s is not RFC3339: '%s'", field, value)
	}
	return parsed, nil
}

func (a *AttentionStoreApprover) fetch(ctx context.Context, id string) (*attentionItem, error) {
	endpoint := a.baseURL + "/api/1.0/attention/" + url.PathEscape(id)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, errors.Wrapf(ctx, err, "build approval request for '%s' failed", id)
	}
	if a.token != "" {
		req.Header.Set("Authorization", "Bearer "+a.token)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, errors.Wrapf(ctx, err, "read approval '%s' from the attention store failed", id)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, errors.Errorf(ctx, "approval '%s' is not on record", id)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, errors.Errorf(
			ctx,
			"read approval '%s' failed: store returned %d: %s",
			id,
			resp.StatusCode,
			strings.TrimSpace(string(body)),
		)
	}
	var item attentionItem
	if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
		return nil, errors.Wrapf(ctx, err, "decode approval '%s' failed", id)
	}
	return &item, nil
}
