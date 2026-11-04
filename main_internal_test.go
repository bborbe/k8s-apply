// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/bborbe/cqrs/base"

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

// A dev instance that promotes without a gateway is a startup failure, not a
// runtime one: service.Main turns Run's error into a non-zero exit, so the pod
// crash-loops instead of accepting applies and silently dropping every
// promotion. The test asserts the message, not the process exit.
func TestCreateGateway(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		stage     apply.Stage
		url       string
		initiator string
		wantErr   string
	}{
		{
			name:    "dev without a gateway URL fails closed",
			stage:   apply.StageDev,
			wantErr: "SUN_GATEWAY_URL is empty",
		},
		{
			name:      "dev with https is accepted",
			stage:     apply.StageDev,
			url:       "https://gateway.example.com",
			initiator: "k8s-apply",
		},
		{
			name:      "dev with an in-cluster http service is accepted",
			stage:     apply.StageDev,
			url:       "http://command.sun.svc.cluster.local:8080",
			initiator: "k8s-apply",
		},
		{
			name:      "dev with plain http to a routable host is refused",
			stage:     apply.StageDev,
			url:       "http://gateway.example.com:8080",
			initiator: "k8s-apply",
			wantErr:   "in clear text",
		},
		{
			name:    "dev without an initiator fails closed",
			stage:   apply.StageDev,
			url:     "https://gateway.example.com",
			wantErr: "SUN_GATEWAY_INITIATOR is empty",
		},
		{
			name:  "sun needs no gateway",
			stage: apply.StageSun,
		},
		{
			name:  "prod needs no gateway",
			stage: apply.StageProd,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app := &application{
				Stage:               string(test.stage),
				SunGatewayURL:       test.url,
				SunGatewayInitiator: test.initiator,
			}

			gateway, err := app.createGateway(ctx, test.stage)

			if test.wantErr != "" {
				if err == nil {
					t.Fatalf("expected an error containing %q, got none", test.wantErr)
				}
				if !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("error %q does not contain %q", err.Error(), test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if gateway == nil {
				t.Fatal("expected a gateway, got nil")
			}
		})
	}
}

// A non-promoting stage is wired to the noop gateway, and that gateway must
// refuse. A future wiring mistake that put the real gateway on sun or prod
// would otherwise be invisible until a promotion leaked out of a stage that has
// no promotion target.
func TestCreateGatewayNoopRefuses(t *testing.T) {
	for _, stage := range []apply.Stage{apply.StageSun, apply.StageProd} {
		t.Run(string(stage), func(t *testing.T) {
			app := &application{Stage: string(stage)}

			gateway, err := app.createGateway(context.Background(), stage)
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			if err := gateway.Send(
				context.Background(),
				"a2a-worker-controller",
				base.EventID("some-id"),
			); err == nil {
				t.Fatalf("the %s gateway accepted a promotion", string(stage))
			}
		})
	}
}
