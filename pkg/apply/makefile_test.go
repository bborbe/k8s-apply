// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apply

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("extractRecipe", func() {
	It("returns nil and not-found when the target is missing", func() {
		recipe, found := extractRecipe("install:\n\tkubectl apply -f x.yaml\n")
		Expect(found).To(BeFalse())
		Expect(recipe).To(BeNil())
	})

	It("returns the recipe lines of the target", func() {
		content := "apply:\n\tkubectl apply -f a.yaml\n\tkubectl apply -f b.yaml\n"
		recipe, found := extractRecipe(content)
		Expect(found).To(BeTrue())
		Expect(recipe).To(Equal([]string{
			"kubectl apply -f a.yaml",
			"kubectl apply -f b.yaml",
		}))
	})

	It("stops at the first non-recipe line", func() {
		content := "apply:\n\tkubectl apply -f a.yaml\n\nother:\n\tdocker build .\n"
		recipe, found := extractRecipe(content)
		Expect(found).To(BeTrue())
		Expect(recipe).To(Equal([]string{"kubectl apply -f a.yaml"}))
	})

	It("joins backslash continuations into one logical line", func() {
		content := "apply:\n\tteamvault-cli config parse \\\n\t\t--file x --out y\n"
		recipe, found := extractRecipe(content)
		Expect(found).To(BeTrue())
		Expect(recipe).To(Equal([]string{
			"teamvault-cli config parse  \t--file x --out y",
		}))
	})

	It("does not match a target whose name merely starts with the target", func() {
		content := "apply-all:\n\tdocker build .\n"
		recipe, found := extractRecipe(content)
		Expect(found).To(BeFalse())
		Expect(recipe).To(BeNil())
	})

	It("strips make's command prefixes", func() {
		content := "apply:\n\t@echo hi\n\t-docker build .\n\t@-kubectl apply -f x.yaml\n\t+docker push x\n\t-@docker build .\n"
		recipe, found := extractRecipe(content)
		Expect(found).To(BeTrue())
		Expect(recipe).To(Equal([]string{
			"echo hi",
			"docker build .",
			"kubectl apply -f x.yaml",
			"docker push x",
			"docker build .",
		}))
	})

	It("keeps a leading dash on a continuation line", func() {
		// The `-` here is the shell flag for `docker build`, not make's
		// ignore-errors marker, so it must survive.
		content := "apply:\n\tdocker build \\\n\t-t x .\n"
		recipe, found := extractRecipe(content)
		Expect(found).To(BeTrue())
		Expect(recipe).To(Equal([]string{"docker build  -t x ."}))
	})

	It("reports a prerequisite-only target as found with an empty recipe", func() {
		content := "apply: dep\n\ndep:\n\ttouch sentinel\n"
		recipe, found := extractRecipe(content)
		Expect(found).To(BeTrue())
		Expect(recipe).To(BeEmpty())
	})

	It("reports a placeholder target with no prerequisites and no recipe as not found", func() {
		content := "apply:\n\nother:\n\techo hi\n"
		recipe, found := extractRecipe(content)
		Expect(found).To(BeFalse())
		Expect(recipe).To(BeNil())
	})

	It("reports a missing target as not found", func() {
		recipe, found := extractRecipe("install:\n\techo hi\n")
		Expect(found).To(BeFalse())
		Expect(recipe).To(BeNil())
	})

	It("reads a one-line recipe after the semicolon so docker is still refused", func() {
		recipe, found := extractRecipe("apply: ; docker build -t x .\n")
		Expect(found).To(BeTrue())
		Expect(recipe).To(Equal([]string{"docker build -t x ."}))
		Expect(runsDocker(recipe)).To(BeTrue())
	})

	It("reads a non-docker one-line recipe after the semicolon", func() {
		recipe, found := extractRecipe("apply: ; kubectl apply -f x.yaml\n")
		Expect(found).To(BeTrue())
		Expect(recipe).To(Equal([]string{"kubectl apply -f x.yaml"}))
	})
})

var _ = Describe("runsDocker", func() {
	DescribeTable("detects docker invocations",
		func(recipe []string, expected bool) {
			Expect(runsDocker(recipe)).To(Equal(expected))
		},
		Entry("plain docker", []string{"docker build -t x ."}, true),
		Entry("docker after a semicolon", []string{"make build; docker push x"}, true),
		Entry("docker in a pipeline", []string{"cat x | docker load"}, true),
		Entry("kubectl only", []string{"kubectl apply -f x.yaml"}, false),
		Entry("docker in a word", []string{"dockerize --help"}, false),
		Entry("docker as a variable value", []string{"IMAGE=docker.io/x"}, false),
		Entry("empty recipe", nil, false),
	)
})
