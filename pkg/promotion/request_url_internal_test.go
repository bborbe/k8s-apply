// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package promotion

import (
	"strings"
	"testing"

	"github.com/bborbe/cqrs/base"
)

// The URL shape is asserted without a server so a change to the path is caught
// even if the HTTP tests are skipped.
func TestRequestURL(t *testing.T) {
	const identifier = base.EventID("11111111-1111-1111-1111-111111111111")

	withSlash := requestURL("https://gateway.example.com/", identifier)
	withoutSlash := requestURL("https://gateway.example.com", identifier)

	if withSlash != withoutSlash {
		t.Errorf("a trailing base slash changed the URL: %q vs %q", withSlash, withoutSlash)
	}

	want := "https://gateway.example.com/api/1.0/command/k8s-apply-v1/apply/" + identifier.String()
	if withSlash != want {
		t.Errorf("requestURL = %q, want %q", withSlash, want)
	}

	last := withSlash[strings.LastIndex(withSlash, "/")+1:]
	if last != identifier.String() {
		t.Errorf("identifier is not the last path segment: %q", last)
	}
}
