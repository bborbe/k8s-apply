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
