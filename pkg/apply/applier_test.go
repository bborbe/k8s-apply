// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apply

import (
	"context"

	"github.com/bborbe/log"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("makeArgs", func() {
	It("targets the component directory on the given branch", func() {
		component := &Component{Path: "egress", Dir: "/repo/egress"}

		Expect(makeArgs(component, "dev")).To(Equal([]string{
			"-C", "/repo/egress", "BRANCH=dev", "apply",
		}))
	})
})

var _ = Describe("Applier", func() {
	It("refuses to run on any branch but dev", func() {
		applier := NewApplier("prod", log.DefaultSamplerFactory)
		component := &Component{Path: "egress", Dir: "/repo/egress"}

		err := applier.Apply(context.Background(), component)

		Expect(err).NotTo(BeNil())
		Expect(err.Error()).To(ContainSubstring("only deploys 'dev'"))
	})

	It("reports a missing component directory as a failure rather than a success", func() {
		applier := NewApplier(DevBranch, log.DefaultSamplerFactory)
		component := &Component{Path: "absent", Dir: "/does/not/exist"}

		err := applier.Apply(context.Background(), component)

		Expect(err).NotTo(BeNil())
		Expect(err.Error()).To(ContainSubstring("apply 'absent' failed"))
	})
})
