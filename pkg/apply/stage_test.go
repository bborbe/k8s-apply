// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apply_test

import (
	"context"

	"github.com/bborbe/cqrs/base"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/k8s-apply/pkg/apply"
)

func ptrString(value string) *string { return &value }

func ptrBool(value bool) *bool { return &value }

var _ = Describe("Stage", func() {
	DescribeTable("built-in stage properties",
		func(
			stage apply.Stage,
			applyBranch string,
			requiresBranchArgument bool,
			requiresMasterHead bool,
			topicPrefix base.TopicPrefix,
		) {
			config := apply.ResolveStageConfig(stage, apply.StageOverrides{})
			Expect(config.ApplyBranch).To(Equal(applyBranch))
			Expect(config.RequiresBranchArgument()).To(Equal(requiresBranchArgument))
			Expect(config.RequiresMasterHead).To(Equal(requiresMasterHead))
			Expect(config.TopicPrefix).To(Equal(topicPrefix))
		},
		Entry("dev", apply.StageDev, "dev", true, false, base.TopicPrefix("")),
		Entry("sun", apply.StageSun, "", false, true, base.TopicPrefix("sun")),
		// prod applies master while consuming the unprefixed topic. ApplyBranch
		// is what make receives; TopicPrefix is what selects the topic. They are
		// separate config values, and collapsing them would stay hidden until one
		// of them changed.
		Entry("prod", apply.StageProd, "master", true, false, base.TopicPrefix("")),
	)

	// A stage name outside the three built-ins is accepted and resolves to the
	// neutral config. This is the criterion the closed enum used to fail: a
	// cluster that does not exist yet could not be named at all.
	DescribeTable("an unconfigured unknown stage resolves to neutral values",
		func(name string) {
			config := apply.ResolveStageConfig(apply.Stage(name), apply.StageOverrides{})
			Expect(config.ApplyBranch).To(BeEmpty())
			Expect(config.RequiresBranchArgument()).To(BeFalse())
			Expect(config.RequiresMasterHead).To(BeFalse())
			Expect(string(config.TopicPrefix)).To(BeEmpty())
		},
		Entry("octopus-eu", "octopus-eu"),
		Entry("staging", "staging"),
	)

	DescribeTable("an override beats the built-in default",
		func(stage apply.Stage, overrides apply.StageOverrides, expected apply.StageConfig) {
			Expect(apply.ResolveStageConfig(stage, overrides)).To(Equal(expected))
		},
		Entry("dev with a non-default branch",
			apply.StageDev,
			apply.StageOverrides{ApplyBranch: ptrString("feature-x")},
			apply.StageConfig{ApplyBranch: "feature-x", TopicPrefix: base.TopicPrefix("")},
		),
		Entry("sun with a master-head guard switched off",
			apply.StageSun,
			apply.StageOverrides{RequiresMasterHead: ptrBool(false)},
			apply.StageConfig{ApplyBranch: "", TopicPrefix: base.TopicPrefix("sun")},
		),
		Entry("an unknown stage with all three set",
			apply.Stage("octopus-eu"),
			apply.StageOverrides{
				ApplyBranch:        ptrString("master"),
				TopicPrefix:        ptrTopicPrefix("octopus"),
				RequiresMasterHead: ptrBool(true),
			},
			apply.StageConfig{
				ApplyBranch:        "master",
				TopicPrefix:        base.TopicPrefix("octopus"),
				RequiresMasterHead: true,
			},
		),
	)

	It("keeps an explicit empty override distinct from an unset one", func() {
		// sun's make branch is the empty string, not "unset" — a pointer is what
		// lets a deploy say so.
		config := apply.ResolveStageConfig(
			apply.StageDev,
			apply.StageOverrides{ApplyBranch: ptrString("")},
		)
		Expect(config.ApplyBranch).To(BeEmpty())
		Expect(config.RequiresBranchArgument()).To(BeFalse())
	})
})

func ptrTopicPrefix(value string) *base.TopicPrefix {
	prefix := base.TopicPrefix(value)
	return &prefix
}

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

	// The point of the change: a name outside the three built-ins is a label,
	// not an error.
	DescribeTable("accepts a stage name outside dev/sun/prod",
		func(name string) {
			stage, err := apply.ParseStage(context.Background(), name)
			Expect(err).To(BeNil())
			Expect(string(stage)).To(Equal(name))
		},
		Entry("octopus-eu", "octopus-eu"),
		Entry("staging", "staging"),
		Entry("bogus", "bogus"),
	)

	It("refuses an empty stage name", func() {
		_, err := apply.ParseStage(context.Background(), "")
		Expect(err).NotTo(BeNil())
		Expect(err.Error()).To(ContainSubstring("stage must not be empty"))
	})
})
