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
	It("passes BRANCH for the dev stage", func() {
		component := &Component{Path: "egress", Dir: "/repo/egress"}

		Expect(makeArgs(component, StageDev)).To(Equal([]string{
			"-C", "/repo/egress", "BRANCH=dev", "apply",
		}))
	})

	It("passes no BRANCH for the sun stage", func() {
		component := &Component{Path: "agent", Dir: "/repo/agent"}

		Expect(makeArgs(component, StageSun)).To(Equal([]string{
			"-C", "/repo/agent", "apply",
		}))
	})
})

var _ = Describe("Applier", func() {
	It("refuses an unknown stage", func() {
		applier := NewApplier(Stage("prod"), log.DefaultSamplerFactory)
		component := &Component{Path: "egress", Dir: "/repo/egress"}

		err := applier.Apply(context.Background(), component)

		Expect(err).NotTo(BeNil())
		Expect(err.Error()).To(ContainSubstring("unknown stage 'prod'"))
	})

	It("reports a missing component directory as a failure rather than a success", func() {
		applier := NewApplier(StageDev, log.DefaultSamplerFactory)
		component := &Component{Path: "absent", Dir: "/does/not/exist"}

		err := applier.Apply(context.Background(), component)

		Expect(err).NotTo(BeNil())
		Expect(err.Error()).To(ContainSubstring("apply 'absent' failed"))
	})
})
