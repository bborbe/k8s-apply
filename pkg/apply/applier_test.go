// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apply

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/bborbe/log"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func boolPtr(value bool) *bool { return &value }

var _ = Describe("makeArgs", func() {
	It("passes BRANCH for the dev stage", func() {
		component := &Component{Path: "egress", Dir: "/repo/egress"}

		Expect(
			makeArgs(component, ResolveStageConfig(StageDev, StageOverrides{})),
		).To(Equal([]string{
			"-C", "/repo/egress", "BRANCH=dev", "apply",
		}))
	})

	// The prod make branch is "master", not "prod": nuke's Makefile.env maps
	// both names onto prod.env, and every prod deploy is documented against
	// "master". Passing "prod" here would deploy the right stage from the wrong
	// branch name, which the deploy repo rejects.
	It("targets the prod branch as master", func() {
		component := &Component{Path: "egress", Dir: "/repo/egress"}

		Expect(
			makeArgs(component, ResolveStageConfig(StageProd, StageOverrides{})),
		).To(Equal([]string{
			"-C", "/repo/egress", "BRANCH=master", "apply",
		}))
	})

	It("passes no BRANCH for the sun stage", func() {
		component := &Component{Path: "agent", Dir: "/repo/agent"}

		Expect(
			makeArgs(component, ResolveStageConfig(StageSun, StageOverrides{})),
		).To(Equal([]string{
			"-C", "/repo/agent", "apply",
		}))
	})
})

var _ = Describe("Applier", func() {
	// A stage name is a label, not a gate: an unknown name is accepted and runs
	// with the neutral config. This is the behaviour the closed enum used to
	// refuse.
	It("accepts an unknown stage and runs it with the neutral config", func() {
		component := &Component{Path: "absent", Dir: "/does/not/exist"}
		applier := NewApplier(
			Stage("octopus-eu"),
			ResolveStageConfig(Stage("octopus-eu"), StageOverrides{}),
			"/does/not/exist",
			log.DefaultSamplerFactory,
		)

		err := applier.Apply(context.Background(), component)

		// It fails on the missing directory, not on the stage name.
		Expect(err).NotTo(BeNil())
		Expect(err.Error()).To(ContainSubstring("apply 'absent' failed"))
		Expect(err.Error()).NotTo(ContainSubstring("unknown stage"))
	})

	It("reports a missing component directory as a failure rather than a success", func() {
		applier := NewApplier(
			StageDev,
			// Refresh off: this test is about the make failure, and a refresh
			// against a directory that is not a git checkout would fail first.
			ResolveStageConfig(StageDev, StageOverrides{RefreshCheckout: boolPtr(false)}),
			"/does/not/exist",
			log.DefaultSamplerFactory,
		)
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

		applier := NewApplier(
			StageDev,
			// Refresh off: this test exercises make, and the temp dir is not a
			// git checkout.
			ResolveStageConfig(StageDev, StageOverrides{RefreshCheckout: boolPtr(false)}),
			repoDir,
			log.DefaultSamplerFactory,
		)
		Expect(applier.Apply(ctx, component)).To(BeNil())

		_, statErr := os.Stat(filepath.Join(componentDir, "sentinel"))
		Expect(statErr).To(BeNil())
	})

	// The defect this covers: the deploy checkout is cloned once by the pod's
	// init container, so a commit pushed after pod start is invisible to every
	// apply until the pod restarts — and the apply then reports success against
	// the stale revision.
	It("moves a stale checkout to the origin branch tip before applying", func() {
		_, upstream, repoDir := seedClonedDeployRepo()

		Expect(os.WriteFile(
			filepath.Join(upstream, "agent", "new-image.txt"),
			[]byte("v2"),
			0o600,
		)).To(BeNil())
		commitAll(upstream, "new image")
		gitRun(upstream, "push", "origin", "dev")

		want := gitOutput(upstream, "rev-parse", "HEAD")
		// The clone the pod holds is the pre-push commit.
		Expect(gitOutput(repoDir, "rev-parse", "HEAD")).NotTo(Equal(want))

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		component, err := NewComponentResolver(repoDir).Resolve(ctx, "agent")
		Expect(err).To(BeNil())

		applier := NewApplier(
			StageDev,
			ResolveStageConfig(StageDev, StageOverrides{}),
			repoDir,
			log.DefaultSamplerFactory,
		)
		Expect(applier.Apply(ctx, component)).To(BeNil())

		Expect(gitOutput(repoDir, "rev-parse", "HEAD")).To(Equal(want))
		Expect(gitOutput(repoDir, "show", "HEAD:agent/new-image.txt")).To(Equal("v2"))
	})

	// A best-effort fetch is what made the defect invisible: the apply ran on
	// whatever the checkout held and reported success. The fetch failure must
	// fail the apply instead, so the result topic carries it and nothing is
	// applied.
	It("fails the apply when the checkout fetch fails instead of applying the stale tree", func() {
		repoDir := GinkgoT().TempDir()
		gitRun(repoDir, "init", "--initial-branch=dev")
		gitRun(repoDir, "remote", "add", "origin", filepath.Join(repoDir, "no-such-origin.git"))
		writeApplyMakefile(repoDir)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		component, err := NewComponentResolver(repoDir).Resolve(ctx, "agent")
		Expect(err).To(BeNil())

		applier := NewApplier(
			StageDev,
			ResolveStageConfig(StageDev, StageOverrides{}),
			repoDir,
			log.DefaultSamplerFactory,
		)
		err = applier.Apply(ctx, component)

		Expect(err).NotTo(BeNil())
		Expect(err.Error()).To(ContainSubstring("fetch origin dev"))
		_, statErr := os.Stat(filepath.Join(repoDir, "agent", "sentinel"))
		Expect(os.IsNotExist(statErr)).To(BeTrue())
	})
})

// seedClonedDeployRepo builds a bare origin carrying an applyable "agent"
// component, clones it the way the pod's init container does (shallow, one
// branch), and returns the origin path, the upstream working clone and the
// pod-style clone.
func seedClonedDeployRepo() (string, string, string) {
	GinkgoHelper()
	origin := filepath.Join(GinkgoT().TempDir(), "origin.git")
	upstream := GinkgoT().TempDir()
	gitRun(upstream, "init", "--initial-branch=dev")
	writeApplyMakefile(upstream)
	commitAll(upstream, "initial")
	gitRun(upstream, "init", "--bare", origin)
	gitRun(upstream, "remote", "add", "origin", origin)
	gitRun(upstream, "push", "origin", "dev")

	repoDir := GinkgoT().TempDir()
	gitRun(upstream, "clone", "--depth", "1", "--branch", "dev", "file://"+origin, repoDir)
	return origin, upstream, repoDir
}

// writeApplyMakefile creates a component whose apply target touches a sentinel,
// so the test can tell "make ran" from "make did not run".
func writeApplyMakefile(repoDir string) {
	GinkgoHelper()
	componentDir := filepath.Join(repoDir, "agent")
	Expect(os.MkdirAll(componentDir, 0o750)).To(BeNil())
	Expect(os.WriteFile(
		filepath.Join(componentDir, "Makefile"),
		[]byte("apply: dep\n\ndep:\n\ttouch sentinel\n"),
		0o600,
	)).To(BeNil())
}

func commitAll(dir string, message string) {
	GinkgoHelper()
	gitRun(dir, "add", "-A")
	gitRun(dir, "commit", "-q", "-m", message)
}

// gitRun runs a git command that must succeed. The global and system configs
// are dropped so a host-level commit signing setup cannot fail the test.
func gitRun(dir string, args ...string) {
	GinkgoHelper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)
	output, err := cmd.CombinedOutput()
	Expect(err).To(BeNil(), string(output))
}

func gitOutput(dir string, args ...string) string {
	GinkgoHelper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(
		os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)
	output, err := cmd.Output()
	Expect(err).To(BeNil())
	return strings.TrimSpace(string(output))
}
