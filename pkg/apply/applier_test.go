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
	It("targets the component directory on the dev branch", func() {
		component := &Component{Path: "egress", Dir: "/repo/egress"}

		Expect(makeArgs(component, StageDev)).To(Equal([]string{
			"-C", "/repo/egress", "BRANCH=dev", "apply",
		}))
	})

	// The prod make branch is "master", not "prod": nuke's Makefile.env maps
	// both names onto prod.env, and every prod deploy is documented against
	// "master". Passing "prod" here would deploy the right stage from the
	// wrong branch name, which the deploy repo rejects.
	It("targets the prod branch as master", func() {
		component := &Component{Path: "egress", Dir: "/repo/egress"}

		Expect(makeArgs(component, StageProd)).To(Equal([]string{
			"-C", "/repo/egress", "BRANCH=master", "apply",
		}))
	})

	// A stage that gates inside its own recipes passes no BRANCH at all, so the
	// invocation must not grow one for it.
	It("passes no BRANCH for a stage that needs no branch argument", func() {
		component := &Component{Path: "egress", Dir: "/repo/egress"}

		Expect(makeArgs(component, Stage("guard-master"))).To(Equal([]string{
			"-C", "/repo/egress", "apply",
		}))
	})
})

var _ = Describe("Applier", func() {
	It("refuses an unknown stage rather than defaulting to no branch argument", func() {
		applier := NewApplier(Stage("staging"), log.DefaultSamplerFactory)
		component := &Component{Path: "egress", Dir: "/repo/egress"}

		err := applier.Apply(context.Background(), component)

		Expect(err).NotTo(BeNil())
		Expect(err.Error()).To(ContainSubstring("unknown stage 'staging'"))
	})

	It("reports a missing component directory as a failure rather than a success", func() {
		applier := NewApplier(StageDev, log.DefaultSamplerFactory)
		component := &Component{Path: "absent", Dir: "/does/not/exist"}

		err := applier.Apply(context.Background(), component)

		Expect(err).NotTo(BeNil())
		Expect(err.Error()).To(ContainSubstring("apply 'absent' failed"))
	})
})
