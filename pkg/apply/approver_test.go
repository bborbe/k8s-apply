// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apply

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	testCommit    = "b786ef4aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testAnswered  = "2026-11-01T20:05:00Z"
	testExpiresAt = "2026-11-01T22:00:00Z"
)

func approvalFor(stage Stage, component, commit string) *Approval {
	return &Approval{
		ID:           "0123456789abcdef",
		Component:    component,
		Commit:       commit,
		ApprovedBy:   "operator",
		ApprovedAt:   testAnswered,
		ExpiresAt:    testExpiresAt,
		ActionSHA256: ActionHash(stage, component, commit),
	}
}

func commandWith(t *Approval) Command {
	raw, err := json.Marshal(t)
	Expect(err).To(BeNil())
	return Command{Path: t.Component, Approval: raw}
}

func storeServing(item map[string]any) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if item == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(item)
	}))
}

func answeredItem(dedupKey, kind, value string) map[string]any {
	return map[string]any{
		"item_id":     "0123456789abcdef",
		"dedup_key":   dedupKey,
		"state":       "answered",
		"answered_at": testAnswered,
		"answer":      map[string]any{"kind": kind, "value": value},
	}
}

var _ = Describe("ActionHash", func() {
	It("is stable for the same action", func() {
		Expect(ActionHash(StageProd, "alertmanager", testCommit)).
			To(Equal(ActionHash(StageProd, "alertmanager", testCommit)))
	})

	// The stage is in the digest on purpose: a dev approval replayed against
	// prod is the stage-crossing bug this task exists to prevent.
	It("differs by stage", func() {
		Expect(ActionHash(StageDev, "alertmanager", testCommit)).
			NotTo(Equal(ActionHash(StageProd, "alertmanager", testCommit)))
	})

	It("differs by component", func() {
		Expect(ActionHash(StageProd, "alertmanager", testCommit)).
			NotTo(Equal(ActionHash(StageProd, "grafana", testCommit)))
	})

	It("differs by commit", func() {
		Expect(ActionHash(StageProd, "alertmanager", testCommit)).
			NotTo(Equal(ActionHash(StageProd, "alertmanager", "0000000")))
	})

	// Field boundaries must not be shiftable: ("ab","c") and ("a","bc") are
	// different actions and must not collide.
	It("does not collide when a field boundary moves", func() {
		Expect(ActionHash(StageProd, "ab", "c")).NotTo(Equal(ActionHash(StageProd, "a", "bc")))
	})
})

var _ = Describe("ParseApproval", func() {
	var ctx context.Context
	BeforeEach(func() { ctx = context.Background() })

	It("returns nil for an absent approval", func() {
		approval, err := ParseApproval(ctx, nil)
		Expect(err).To(BeNil())
		Expect(approval).To(BeNil())
	})

	It("returns nil for an explicit null", func() {
		approval, err := ParseApproval(ctx, json.RawMessage("null"))
		Expect(err).To(BeNil())
		Expect(approval).To(BeNil())
	})

	It("parses a single object", func() {
		approval, err := ParseApproval(ctx, json.RawMessage(
			`{"id":"abc","component":"x","commit":"c","action_sha256":"h"}`,
		))
		Expect(err).To(BeNil())
		Expect(approval.ID).To(Equal("abc"))
	})

	// Two approvals must be refused by name rather than by a generic unmarshal
	// error that names no cause on the result topic.
	It("refuses two approvals by name", func() {
		_, err := ParseApproval(ctx, json.RawMessage(
			`[{"id":"a","action_sha256":"h"},{"id":"b","action_sha256":"h"}]`,
		))
		Expect(err).NotTo(BeNil())
		Expect(err.Error()).To(ContainSubstring("more than one approval carried"))
	})

	// Zero and many are different mistakes; an operator reading the result topic
	// must be able to tell which one they made.
	It("names an empty list as no approval, not as too many", func() {
		_, err := ParseApproval(ctx, json.RawMessage(`[]`))
		Expect(err).NotTo(BeNil())
		Expect(err.Error()).To(ContainSubstring("no approval carried"))
		Expect(err.Error()).NotTo(ContainSubstring("more than one"))
	})

	It("accepts a single-element array", func() {
		approval, err := ParseApproval(ctx, json.RawMessage(
			`[{"id":"a","component":"x","commit":"c","action_sha256":"h"}]`,
		))
		Expect(err).To(BeNil())
		Expect(approval.ID).To(Equal("a"))
	})

	// A missing component or commit must be refused by name; left to
	// ActionHash it would surface as a hash mismatch and name the wrong cause.
	It("refuses an approval with no component", func() {
		_, err := ParseApproval(ctx, json.RawMessage(`{"id":"a","commit":"c","action_sha256":"h"}`))
		Expect(err).NotTo(BeNil())
		Expect(err.Error()).To(ContainSubstring("carries no component"))
	})

	It("refuses an approval with no commit", func() {
		_, err := ParseApproval(
			ctx,
			json.RawMessage(`{"id":"a","component":"x","action_sha256":"h"}`),
		)
		Expect(err).NotTo(BeNil())
		Expect(err.Error()).To(ContainSubstring("carries no commit"))
	})

	It("refuses an approval with no id", func() {
		_, err := ParseApproval(ctx, json.RawMessage(`{"component":"x"}`))
		Expect(err).NotTo(BeNil())
		Expect(err.Error()).To(ContainSubstring("carries no id"))
	})
})

var _ = Describe("NoopApprover", func() {
	var ctx context.Context
	BeforeEach(func() { ctx = context.Background() })

	It("passes a stage that needs no approval", func() {
		Expect(NewNoopApprover().Approve(ctx, StageDev, Command{Path: "x"})).To(BeNil())
	})

	// Fail-closed: forgetting to wire a real approver on prod must disable prod
	// applies, never silently disable the gate.
	It("refuses a stage that requires an approval", func() {
		err := NewNoopApprover().Approve(ctx, StageProd, Command{Path: "x"})
		Expect(err).NotTo(BeNil())
		Expect(err.Error()).To(ContainSubstring("no approver is configured"))
	})
})

var _ = Describe("AttentionStoreApprover", func() {
	var ctx context.Context
	var now time.Time

	BeforeEach(func() {
		ctx = context.Background()
		now = time.Date(2026, 11, 1, 21, 0, 0, 0, time.UTC)
	})

	approverFor := func(server *httptest.Server) Approver {
		return NewAttentionStoreApprover(server.URL, "", func() time.Time { return now })
	}

	It("passes a command carrying one recorded approval for this action", func() {
		approval := approvalFor(StageProd, "alertmanager", testCommit)
		server := storeServing(answeredItem(approval.ActionSHA256, "option", ApprovalOptionApprove))
		defer server.Close()

		Expect(approverFor(server).Approve(ctx, StageProd, commandWith(approval))).To(BeNil())
	})

	It("passes without contacting the store on a stage that needs no approval", func() {
		server := storeServing(nil)
		defer server.Close()

		Expect(approverFor(server).Approve(ctx, StageDev, Command{Path: "x"})).To(BeNil())
	})

	It("refuses a command carrying no approval", func() {
		server := storeServing(nil)
		defer server.Close()

		err := approverFor(server).Approve(ctx, StageProd, Command{Path: "alertmanager"})
		Expect(err).NotTo(BeNil())
		Expect(err.Error()).To(ContainSubstring("no approval carried"))
	})

	It("refuses an approval naming a different component", func() {
		approval := approvalFor(StageProd, "grafana", testCommit)
		server := storeServing(answeredItem(approval.ActionSHA256, "option", ApprovalOptionApprove))
		defer server.Close()

		command := commandWith(approval)
		command.Path = "alertmanager"

		err := approverFor(server).Approve(ctx, StageProd, command)
		Expect(err).NotTo(BeNil())
		Expect(err.Error()).To(ContainSubstring("approval names component 'grafana'"))
	})

	// The stage is what separates a dev approval from a prod one; a field-level
	// comparison of component and commit alone would let this through.
	It("refuses a dev approval replayed against prod", func() {
		approval := approvalFor(StageDev, "alertmanager", testCommit)
		server := storeServing(answeredItem(approval.ActionSHA256, "option", ApprovalOptionApprove))
		defer server.Close()

		err := approverFor(server).Approve(ctx, StageProd, commandWith(approval))
		Expect(err).NotTo(BeNil())
		Expect(err.Error()).To(ContainSubstring("action hash does not match"))
	})

	It("refuses a tampered action hash", func() {
		approval := approvalFor(StageProd, "alertmanager", testCommit)
		approval.ActionSHA256 = "deadbeef"
		server := storeServing(answeredItem(approval.ActionSHA256, "option", ApprovalOptionApprove))
		defer server.Close()

		err := approverFor(server).Approve(ctx, StageProd, commandWith(approval))
		Expect(err).NotTo(BeNil())
		Expect(err.Error()).To(ContainSubstring("action hash does not match"))
	})

	It("refuses an approval that is not on record", func() {
		approval := approvalFor(StageProd, "alertmanager", testCommit)
		server := storeServing(nil)
		defer server.Close()

		err := approverFor(server).Approve(ctx, StageProd, commandWith(approval))
		Expect(err).NotTo(BeNil())
		Expect(err.Error()).To(ContainSubstring("is not on record"))
	})

	// The binding is dedup_key, never the operator-facing payload.
	It("refuses an item whose dedup key names a different action", func() {
		approval := approvalFor(StageProd, "alertmanager", testCommit)
		server := storeServing(answeredItem("a-different-action", "option", ApprovalOptionApprove))
		defer server.Close()

		err := approverFor(server).Approve(ctx, StageProd, commandWith(approval))
		Expect(err).NotTo(BeNil())
		Expect(err.Error()).To(ContainSubstring("recorded for a different action"))
	})

	It("refuses a card answered with a decline", func() {
		approval := approvalFor(StageProd, "alertmanager", testCommit)
		server := storeServing(answeredItem(approval.ActionSHA256, "option", "decline"))
		defer server.Close()

		err := approverFor(server).Approve(ctx, StageProd, commandWith(approval))
		Expect(err).NotTo(BeNil())
		Expect(err.Error()).To(ContainSubstring("was not answered with 'approve'"))
	})

	It("refuses a skipped card", func() {
		approval := approvalFor(StageProd, "alertmanager", testCommit)
		server := storeServing(answeredItem(approval.ActionSHA256, "skip", ""))
		defer server.Close()

		err := approverFor(server).Approve(ctx, StageProd, commandWith(approval))
		Expect(err).NotTo(BeNil())
		Expect(err.Error()).To(ContainSubstring("was not answered with 'approve'"))
	})

	It("refuses an expired approval", func() {
		approval := approvalFor(StageProd, "alertmanager", testCommit)
		server := storeServing(answeredItem(approval.ActionSHA256, "option", ApprovalOptionApprove))
		defer server.Close()

		now = time.Date(2026, 11, 1, 23, 0, 0, 0, time.UTC)

		err := approverFor(server).Approve(ctx, StageProd, commandWith(approval))
		Expect(err).NotTo(BeNil())
		Expect(err.Error()).To(ContainSubstring("expired"))
	})

	It("refuses an approval whose window has not opened", func() {
		approval := approvalFor(StageProd, "alertmanager", testCommit)
		server := storeServing(answeredItem(approval.ActionSHA256, "option", ApprovalOptionApprove))
		defer server.Close()

		now = time.Date(2026, 11, 1, 19, 0, 0, 0, time.UTC)

		err := approverFor(server).Approve(ctx, StageProd, commandWith(approval))
		Expect(err).NotTo(BeNil())
		Expect(err.Error()).To(ContainSubstring("not yet valid"))
	})

	// The window must come from the store, not from the payload: otherwise the
	// party being gated chooses when its own approval was given.
	It("refuses an approved_at that disagrees with the store's answered_at", func() {
		approval := approvalFor(StageProd, "alertmanager", testCommit)
		approval.ApprovedAt = "2026-11-01T12:00:00Z"
		approval.ExpiresAt = "2026-11-01T14:00:00Z"
		server := storeServing(answeredItem(approval.ActionSHA256, "option", ApprovalOptionApprove))
		defer server.Close()

		now = time.Date(2026, 11, 1, 13, 0, 0, 0, time.UTC)

		err := approverFor(server).Approve(ctx, StageProd, commandWith(approval))
		Expect(err).NotTo(BeNil())
		Expect(err.Error()).To(ContainSubstring("must match the record"))
	})

	It("refuses a declared window longer than the maximum lifetime", func() {
		approval := approvalFor(StageProd, "alertmanager", testCommit)
		approval.ExpiresAt = "2026-11-09T20:05:00Z"
		server := storeServing(answeredItem(approval.ActionSHA256, "option", ApprovalOptionApprove))
		defer server.Close()

		err := approverFor(server).Approve(ctx, StageProd, commandWith(approval))
		Expect(err).NotTo(BeNil())
		Expect(err.Error()).To(ContainSubstring("above the"))
	})

	It("reports an unreachable store rather than treating it as no approval", func() {
		server := storeServing(nil)
		url := server.URL
		server.Close()

		approval := approvalFor(StageProd, "alertmanager", testCommit)
		err := NewAttentionStoreApprover(url, "", func() time.Time { return now }).
			Approve(ctx, StageProd, commandWith(approval))
		Expect(err).NotTo(BeNil())
		Expect(err.Error()).To(ContainSubstring("read approval"))
		Expect(fmt.Sprint(err)).NotTo(ContainSubstring("is not on record"))
	})
})
