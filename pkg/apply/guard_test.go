// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apply

import (
	"context"
	"os/exec"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("guardMaster", func() {
	var ctx context.Context
	var dir string

	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(
			cmd.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@example.com",
		)
		output, err := cmd.CombinedOutput()
		Expect(err).To(BeNil(), string(output))
	}

	BeforeEach(func() {
		ctx = context.Background()
		dir = GinkgoT().TempDir()
		git("init", "-q")
		git("commit", "--allow-empty", "-q", "-m", "first")
	})

	It("passes when HEAD contains origin/master", func() {
		git("update-ref", "refs/remotes/origin/master", "HEAD")

		err := guardMaster(ctx, &Component{Path: "agent", Dir: dir})

		Expect(err).To(BeNil())
	})

	It("refuses when origin/master is not an ancestor of HEAD", func() {
		git("update-ref", "refs/remotes/origin/master", "HEAD")
		git("checkout", "-q", "--orphan", "other")
		git("commit", "--allow-empty", "-q", "-m", "unrelated")

		err := guardMaster(ctx, &Component{Path: "agent", Dir: dir})

		Expect(err).NotTo(BeNil())
		Expect(err.Error()).To(ContainSubstring("does not contain origin/master"))
	})
})
