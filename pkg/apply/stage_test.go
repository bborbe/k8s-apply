// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apply_test

import (
	"context"

	"github.com/bborbe/cqrs/base"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/nuke-apply/pkg/apply"
)

var _ = Describe("Stage", func() {
	DescribeTable("stage properties",
		func(
			stage apply.Stage,
			applyBranch string,
			requiresBranchArgument bool,
			requiresApproval bool,
			requiresMasterHead bool,
			branch base.Branch,
		) {
			Expect(stage.ApplyBranch()).To(Equal(applyBranch))
			Expect(stage.RequiresBranchArgument()).To(Equal(requiresBranchArgument))
			Expect(stage.RequiresApproval()).To(Equal(requiresApproval))
			Expect(stage.RequiresMasterHead()).To(Equal(requiresMasterHead))
			Expect(stage.Branch()).To(Equal(branch))
		},
		Entry("dev", apply.StageDev, "dev", true, false, false, base.Branch("dev")),
		Entry("sun", apply.StageSun, "", false, false, true, base.Branch("sun")),
	)

	DescribeTable("the consumed Kafka topic prefix",
		func(stage apply.Stage, prefix string) {
			Expect(base.TopicPrefixFromBranch(stage.Branch()).String()).To(Equal(prefix))
		},
		Entry("dev consumes the develop topics", apply.StageDev, "develop"),
		Entry("sun consumes the sun topics", apply.StageSun, "sun"),
	)
})

var _ = Describe("ParseStage", func() {
	It("accepts dev", func() {
		stage, err := apply.ParseStage(context.Background(), "dev")
		Expect(err).To(BeNil())
		Expect(stage).To(Equal(apply.StageDev))
	})

	It("accepts sun", func() {
		stage, err := apply.ParseStage(context.Background(), "sun")
		Expect(err).To(BeNil())
		Expect(stage).To(Equal(apply.StageSun))
	})

	DescribeTable("refuses an unknown stage",
		func(name string) {
			_, err := apply.ParseStage(context.Background(), name)
			Expect(err).NotTo(BeNil())
			Expect(err.Error()).To(ContainSubstring("unknown stage '" + name + "'"))
		},
		Entry("empty", ""),
		Entry("prod", "prod"),
		Entry("arbitrary", "staging"),
	)
})
