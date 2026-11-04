// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"reflect"
	"testing"
)

func TestApplicationStageTags(t *testing.T) {
	field, ok := reflect.TypeOf(application{}).FieldByName("Stage")
	if !ok {
		t.Fatal("application has no Stage field")
	}
	if got := field.Tag.Get("arg"); got != "stage" {
		t.Errorf("Stage arg tag = %q, want %q", got, "stage")
	}
	if got := field.Tag.Get("env"); got != "STAGE" {
		t.Errorf("Stage env tag = %q, want %q", got, "STAGE")
	}
}

// TestCreateGateway sets the struct fields directly and never exercises the
// arg/env parser, so a mistyped tag would ship undetected and a dev pod would
// crash-loop on an env var nobody set.
func TestApplicationSunGatewayTags(t *testing.T) {
	tests := []struct {
		field string
		arg   string
		env   string
	}{
		{field: "SunGatewayURL", arg: "sun-gateway-url", env: "SUN_GATEWAY_URL"},
		{field: "SunGatewayInitiator", arg: "sun-gateway-initiator", env: "SUN_GATEWAY_INITIATOR"},
		{field: "SunGatewayUser", arg: "sun-gateway-user", env: "SUN_GATEWAY_USER"},
		{field: "SunGatewayPassword", arg: "sun-gateway-password", env: "SUN_GATEWAY_PASSWORD"},
	}

	for _, test := range tests {
		t.Run(test.field, func(t *testing.T) {
			field, ok := reflect.TypeOf(application{}).FieldByName(test.field)
			if !ok {
				t.Fatalf("application has no %s field", test.field)
			}
			if got := field.Tag.Get("arg"); got != test.arg {
				t.Errorf("%s arg tag = %q, want %q", test.field, got, test.arg)
			}
			if got := field.Tag.Get("env"); got != test.env {
				t.Errorf("%s env tag = %q, want %q", test.field, got, test.env)
			}
		})
	}
}
