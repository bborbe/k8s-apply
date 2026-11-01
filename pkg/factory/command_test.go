// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package factory_test

import (
	"github.com/bborbe/cqrs/base"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/nuke-apply/pkg/apply"
	"github.com/bborbe/nuke-apply/pkg/factory"
)

var _ = Describe("CQRSBranch", func() {
	// The cqrs branch is NOT the make branch. On prod they are "prod" and
	// "master" respectively, and only the cqrs one is what
	// TopicPrefixFromBranch expects. Handing it "master" would still yield the
	// right prefix today, which is exactly why a wrong mapping here would stay
	// hidden until either value changed.
	It("maps prod to the cqrs branch 'prod', whose topic prefix is master", func() {
		Expect(factory.CQRSBranch(apply.StageProd)).To(Equal(base.Branch("prod")))
		Expect(base.TopicPrefixFromBranch(factory.CQRSBranch(apply.StageProd))).
			To(Equal(base.TopicPrefix("master")))
	})

	It("maps dev to the cqrs branch 'dev', whose topic prefix is develop", func() {
		Expect(factory.CQRSBranch(apply.StageDev)).To(Equal(base.Branch("dev")))
		Expect(base.TopicPrefixFromBranch(factory.CQRSBranch(apply.StageDev))).
			To(Equal(base.TopicPrefix("develop")))
	})

	// The make branch and the cqrs branch differ on prod; asserting it here is
	// what stops someone collapsing them back into one variable.
	It("differs from the stage's make branch on prod", func() {
		Expect(
			string(factory.CQRSBranch(apply.StageProd)),
		).NotTo(Equal(apply.StageProd.ApplyBranch()))
		Expect(apply.StageProd.ApplyBranch()).To(Equal("master"))
	})
})
