// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package factory_test

import (
	"github.com/bborbe/cqrs/base"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/k8s-apply/pkg/apply"
)

var _ = Describe("Stage config", func() {
	// The make branch and the topic prefix are separate config values, and the
	// configured prefix is deliberately NOT the branch-derived one: the Seibert
	// stages consume the unprefixed topic their Schema CR declares, because each
	// stage has its own cluster and its own Kafka. Collapsing the two would stay
	// hidden until one of them changed — so assert they differ.
	It("keeps the make branch and the topic prefix separate on prod", func() {
		config := apply.ResolveStageConfig(apply.StageProd, apply.StageOverrides{})

		Expect(config.ApplyBranch).To(Equal("master"))
		Expect(string(config.TopicPrefix)).To(BeEmpty())
		Expect(base.TopicPrefixFromBranch(base.Branch(config.ApplyBranch))).
			To(Equal(base.TopicPrefix("master")),
				"a branch-derived prefix would be 'master' — the configured one is empty")
	})

	It("keeps the make branch and the topic prefix separate on dev", func() {
		config := apply.ResolveStageConfig(apply.StageDev, apply.StageOverrides{})

		Expect(config.ApplyBranch).To(Equal("dev"))
		Expect(string(config.TopicPrefix)).To(BeEmpty())
		Expect(base.TopicPrefixFromBranch(base.Branch(config.ApplyBranch))).
			To(Equal(base.TopicPrefix("develop")),
				"a branch-derived prefix would be 'develop' — the configured one is empty")
	})

	// sun is the stage that does keep a branch-derived prefix; it is now an
	// explicit config value rather than a fall-through.
	It("keeps sun on its branch-derived prefix", func() {
		config := apply.ResolveStageConfig(apply.StageSun, apply.StageOverrides{})

		Expect(config.ApplyBranch).To(BeEmpty())
		Expect(string(config.TopicPrefix)).To(Equal("sun"))
	})
})
