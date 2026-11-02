// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/bborbe/k8s-apply/pkg/apply"
)

// The suite in main_test.go is an external package and only compiles the
// binary, so it cannot reach createApprover. This file covers the fail-closed
// startup path, which is the one place a regression would silently disable the
// prod gate rather than fail loudly.
func TestCreateApprover(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		stage   apply.Stage
		url     string
		wantErr string
	}{
		{
			name:  "dev needs no approver",
			stage: apply.StageDev,
		},
		{
			name:    "prod without an attention URL fails closed",
			stage:   apply.StageProd,
			wantErr: "ATTENTION_URL is empty",
		},
		{
			name:  "prod with https is accepted",
			stage: apply.StageProd,
			url:   "https://attention.example.com",
		},
		{
			name:  "prod with an in-cluster http service is accepted",
			stage: apply.StageProd,
			url:   "http://attention-controller.agent.svc.cluster.local:18080",
		},
		{
			name:    "prod with plain http to a routable host is refused",
			stage:   apply.StageProd,
			url:     "http://attention.example.com:18080",
			wantErr: "in clear text",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app := &application{Stage: string(test.stage), AttentionURL: test.url}

			approver, err := app.createApprover(ctx, test.stage)

			if test.wantErr != "" {
				if err == nil {
					t.Fatalf("expected an error containing %q, got nil", test.wantErr)
				}
				if !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("expected an error containing %q, got %q", test.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if approver == nil {
				t.Fatal("expected an approver, got nil")
			}
		})
	}
}

// A stage that requires an approval must never end up with the noop approver:
// that wiring accepts commands and refuses all of them for a reason nobody sees
// until the first real deploy.
func TestCreateApproverNeverGivesProdTheNoop(t *testing.T) {
	app := &application{
		Stage:        string(apply.StageProd),
		AttentionURL: "https://attention.example.com",
	}

	approver, err := app.createApprover(context.Background(), apply.StageProd)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if err := approver.Approve(
		context.Background(),
		apply.StageProd,
		apply.Command{Path: "alertmanager"},
	); err == nil {
		t.Fatal("prod approver accepted a command carrying no approval")
	}
}
