// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apply_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/k8s-apply/pkg/apply"
)

var _ = Describe("Command", func() {
	DescribeTable("Validate",
		func(path string, valid bool) {
			err := apply.Command{Path: path}.Validate(context.Background())
			if valid {
				Expect(err).To(BeNil())
				return
			}
			Expect(err).NotTo(BeNil())
		},
		Entry("simple component", "egress-proxy", true),
		Entry("nested component", "trading/core/symbol", true),
		Entry("leading and trailing space is rejected, not trimmed", " egress-proxy ", false),
		Entry("empty path", "", false),
		Entry("whitespace only", "   ", false),
		Entry("absolute path", "/etc/passwd", false),
		Entry("parent traversal", "../nuke-prod", false),
		Entry("parent traversal in the middle", "a/../../b", false),
		Entry("not clean", "a//b", false),
		Entry("current directory", ".", false),
	)
})
