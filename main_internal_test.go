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

// A dev instance that promotes without a gateway is a startup failure, not a
// runtime one: service.Main turns Run's error into a non-zero exit, so the pod
// crash-loops instead of accepting applies and silently dropping every
// promotion. The test asserts the message, not the process exit.
// createGatewayTests is kept out of TestCreateGateway so the case list does not
// push the function past the funlen limit.
var createGatewayTests = []struct {
	name      string
	stage     apply.Stage
	url       string
	initiator string
	user      string
	password  string
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
		user:      "api",
		password:  "secret",
	},
	{
		name:      "dev with an in-cluster http service is accepted",
		stage:     apply.StageDev,
		url:       "http://command.sun.svc.cluster.local:8080",
		initiator: "k8s-apply",
		user:      "api",
		password:  "secret",
	},
	{
		name:      "dev with plain http to a routable host is refused",
		stage:     apply.StageDev,
		url:       "http://gateway.example.com:8080",
		initiator: "k8s-apply",
		user:      "api",
		password:  "secret",
		wantErr:   "in clear text",
	},
	{
		name:    "dev without an initiator fails closed",
		stage:   apply.StageDev,
		url:     "https://gateway.example.com",
		wantErr: "SUN_GATEWAY_INITIATOR is empty",
	},
	{
		name:      "dev without a credential user fails closed",
		stage:     apply.StageDev,
		url:       "https://gateway.example.com",
		initiator: "k8s-apply",
		password:  "secret",
		wantErr:   "SUN_GATEWAY_USER is empty",
	},
	{
		name:      "dev without a credential password fails closed",
		stage:     apply.StageDev,
		url:       "https://gateway.example.com",
		initiator: "k8s-apply",
		user:      "api",
		wantErr:   "SUN_GATEWAY_PASSWORD is empty",
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

func TestCreateGateway(t *testing.T) {
	ctx := context.Background()

	for _, test := range createGatewayTests {
		t.Run(test.name, func(t *testing.T) {
			app := &application{
				Stage:               string(test.stage),
				SunGatewayURL:       test.url,
				SunGatewayInitiator: test.initiator,
				SunGatewayUser:      test.user,
				SunGatewayPassword:  test.password,
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
