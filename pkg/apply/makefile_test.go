// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apply

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("extractRecipe", func() {
	It("returns nil when the target is missing", func() {
		Expect(extractRecipe("install:\n\tkubectl apply -f x.yaml\n", "apply")).To(BeNil())
	})

	It("returns the recipe lines of the target", func() {
		content := "apply:\n\tkubectl apply -f a.yaml\n\tkubectl apply -f b.yaml\n"
		Expect(extractRecipe(content, "apply")).To(Equal([]string{
			"kubectl apply -f a.yaml",
			"kubectl apply -f b.yaml",
		}))
	})

	It("stops at the first non-recipe line", func() {
		content := "apply:\n\tkubectl apply -f a.yaml\n\nother:\n\tdocker build .\n"
		Expect(extractRecipe(content, "apply")).To(Equal([]string{"kubectl apply -f a.yaml"}))
	})

	It("joins backslash continuations into one logical line", func() {
		content := "apply:\n\tteamvault-cli config parse \\\n\t\t--file x --out y\n"
		Expect(extractRecipe(content, "apply")).To(Equal([]string{
			"teamvault-cli config parse  \t--file x --out y",
		}))
	})

	It("does not match a target whose name merely starts with the target", func() {
		content := "apply-all:\n\tdocker build .\n"
		Expect(extractRecipe(content, "apply")).To(BeNil())
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
