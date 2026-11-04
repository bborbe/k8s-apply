// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apply

import (
	"context"
	"os"
	"path/filepath"
	"time"

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

	// The prod make branch is "master", not "prod": nuke's Makefile.env maps
	// both names onto prod.env, and every prod deploy is documented against
	// "master". Passing "prod" here would deploy the right stage from the wrong
	// branch name, which the deploy repo rejects.
	It("targets the prod branch as master", func() {
		component := &Component{Path: "egress", Dir: "/repo/egress"}

		Expect(makeArgs(component, StageProd)).To(Equal([]string{
			"-C", "/repo/egress", "BRANCH=master", "apply",
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
		applier := NewApplier(Stage("bogus"), log.DefaultSamplerFactory)
		component := &Component{Path: "egress", Dir: "/repo/egress"}

		err := applier.Apply(context.Background(), component)

		Expect(err).NotTo(BeNil())
		Expect(err.Error()).To(ContainSubstring("unknown stage 'bogus'"))
	})

	It("reports a missing component directory as a failure rather than a success", func() {
		applier := NewApplier(StageDev, log.DefaultSamplerFactory)
		component := &Component{Path: "absent", Dir: "/does/not/exist"}

		err := applier.Apply(context.Background(), component)

		Expect(err).NotTo(BeNil())
		Expect(err.Error()).To(ContainSubstring("apply 'absent' failed"))
	})

	It("applies a component whose apply target only lists prerequisites", func() {
		repoDir := GinkgoT().TempDir()
		componentDir := filepath.Join(repoDir, "agent")
		Expect(os.MkdirAll(componentDir, 0o750)).To(BeNil())
		makefile := "apply: dep\n\ndep:\n\ttouch sentinel\n"
		Expect(os.WriteFile(
			filepath.Join(componentDir, "Makefile"),
			[]byte(makefile),
			0o600,
		)).To(BeNil())

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		component, err := NewComponentResolver(repoDir).Resolve(ctx, "agent")
		Expect(err).To(BeNil())
		Expect(component).NotTo(BeNil())

		applier := NewApplier(StageDev, log.DefaultSamplerFactory)
		Expect(applier.Apply(ctx, component)).To(BeNil())

		_, statErr := os.Stat(filepath.Join(componentDir, "sentinel"))
		Expect(statErr).To(BeNil())
	})
})
