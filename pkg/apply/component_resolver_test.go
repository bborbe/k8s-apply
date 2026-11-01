// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apply_test

import (
	"context"
	"os"
	"path/filepath"

	"github.com/bborbe/errors"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/nuke-apply/pkg/apply"
)

var _ = Describe("ComponentResolver", func() {
	var ctx context.Context
	var repoDir string
	var resolver apply.ComponentResolver

	BeforeEach(func() {
		ctx = context.Background()
		repoDir = GinkgoT().TempDir()
		resolver = apply.NewComponentResolver(repoDir)
	})

	writeMakefile := func(path string, content string) {
		dir := filepath.Join(repoDir, path)
		Expect(os.MkdirAll(dir, 0o750)).To(BeNil())
		Expect(os.WriteFile(filepath.Join(dir, "Makefile"), []byte(content), 0o600)).To(BeNil())
	}

	It("resolves a component that has an apply target", func() {
		writeMakefile("egress", "apply:\n\tkubectl apply -f x.yaml\n")

		component, err := resolver.Resolve(ctx, "egress")

		Expect(err).To(BeNil())
		Expect(component).NotTo(BeNil())
		Expect(component.Path).To(Equal("egress"))
		Expect(component.Dir).To(Equal(filepath.Join(repoDir, "egress")))
		Expect(component.Recipe).To(Equal([]string{"kubectl apply -f x.yaml"}))
	})

	It("rejects a path that escapes the repo root", func() {
		_, err := resolver.Resolve(ctx, "../nuke-prod")
		Expect(err).NotTo(BeNil())
	})

	It("rejects an unknown component", func() {
		_, err := resolver.Resolve(ctx, "does-not-exist")
		Expect(err).NotTo(BeNil())
	})

	It("rejects a directory without a Makefile", func() {
		Expect(os.MkdirAll(filepath.Join(repoDir, "empty"), 0o750)).To(BeNil())

		_, err := resolver.Resolve(ctx, "empty")
		Expect(err).NotTo(BeNil())
		Expect(errors.Is(err, os.ErrNotExist)).To(BeTrue())
	})

	It("rejects a Makefile without an apply target", func() {
		writeMakefile("noapply", "install:\n\tkubectl apply -f x.yaml\n")

		_, err := resolver.Resolve(ctx, "noapply")
		Expect(err).NotTo(BeNil())
		Expect(errors.Is(err, apply.ErrNotApplicable)).To(BeTrue())
	})

	It("refuses a component whose apply target runs docker, naming the reason", func() {
		writeMakefile("egress-proxy", "apply:\n\tdocker build -t x .\n")

		_, err := resolver.Resolve(ctx, "egress-proxy")

		Expect(err).NotTo(BeNil())
		Expect(errors.Is(err, apply.ErrDockerComponent)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("docker component refused"))
		Expect(err.Error()).To(ContainSubstring("egress-proxy"))
	})

	It("refuses an excluded component by name even when its apply target is valid", func() {
		writeMakefile("teamvault", "apply:\n\tenvsubst < x.yaml | kubectl apply -f -\n")

		_, err := resolver.Resolve(ctx, "teamvault")

		Expect(err).NotTo(BeNil())
		Expect(errors.Is(err, apply.ErrExcludedComponent)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("excluded by design"))
		Expect(err.Error()).To(ContainSubstring("teamvault"))
	})

	It("refuses an excluded component that is absent from the checkout", func() {
		// The refusal must not depend on what the checkout happens to contain —
		// otherwise a repo that dropped the directory would silently un-exclude it.
		_, err := resolver.Resolve(ctx, "teamvault")

		Expect(errors.Is(err, apply.ErrExcludedComponent)).To(BeTrue())
	})
})
