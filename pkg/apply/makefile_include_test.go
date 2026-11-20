// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apply

import (
	"context"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("readMakefileWithIncludes", func() {
	var (
		ctx     context.Context
		dir     string
		writeFn func(rel string, body string)
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		dir, err = os.MkdirTemp("", "k8s-apply-include-*")
		Expect(err).To(BeNil())
		writeFn = func(rel string, body string) {
			path := filepath.Join(dir, rel)
			Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
			Expect(os.WriteFile(path, []byte(body), 0o644)).To(Succeed())
		}
	})

	AfterEach(func() {
		Expect(os.RemoveAll(dir)).To(Succeed())
	})

	It("resolves an apply target that lives only in an included file", func() {
		// The shape data/cdb-config uses: the component Makefile is nothing but
		// includes, and the recipe is two directories up.
		writeFn(
			"schemas/cdb/Makefile",
			"include ../../Makefile.variables\ninclude ../../Makefile.env\ninclude ../../Makefile.k8s\n",
		)
		writeFn("Makefile.variables", "ROOTDIR ?= $(shell pwd)\n")
		writeFn("Makefile.env", "BRANCH ?= dev\n")
		writeFn("Makefile.k8s", "apply:\n\tfind . -name \"*.yaml\" | kubectl apply -f -\n")

		content, err := readMakefileWithIncludes(ctx, filepath.Join(dir, "schemas/cdb"))
		Expect(err).To(BeNil())

		recipe, found := extractRecipe(content)
		Expect(found).To(BeTrue())
		Expect(recipe).To(Equal([]string{"find . -name \"*.yaml\" | kubectl apply -f -"}))
	})

	It("follows includes transitively", func() {
		writeFn("Makefile", "include a.mk\n")
		writeFn("a.mk", "include b.mk\n")
		writeFn("b.mk", "apply:\n\tkubectl apply -f x.yaml\n")

		content, err := readMakefileWithIncludes(ctx, dir)
		Expect(err).To(BeNil())

		recipe, found := extractRecipe(content)
		Expect(found).To(BeTrue())
		Expect(recipe).To(Equal([]string{"kubectl apply -f x.yaml"}))
	})

	It("still reports not-found when nothing in the chain declares apply", func() {
		writeFn("Makefile", "include vars.mk\n")
		writeFn("vars.mk", "IMAGE ?= x\n")

		content, err := readMakefileWithIncludes(ctx, dir)
		Expect(err).To(BeNil())

		_, found := extractRecipe(content)
		Expect(found).To(BeFalse())
	})

	It("ignores a missing optional include", func() {
		writeFn("Makefile", "-include missing.mk\napply:\n\tkubectl apply -f x.yaml\n")

		content, err := readMakefileWithIncludes(ctx, dir)
		Expect(err).To(BeNil())

		recipe, found := extractRecipe(content)
		Expect(found).To(BeTrue())
		Expect(recipe).To(Equal([]string{"kubectl apply -f x.yaml"}))
	})

	It("fails when a required include is missing", func() {
		writeFn("Makefile", "include missing.mk\n")

		_, err := readMakefileWithIncludes(ctx, dir)
		Expect(err).NotTo(BeNil())
	})

	It("does not loop forever on an include cycle", func() {
		writeFn("Makefile", "include a.mk\napply:\n\tkubectl apply -f x.yaml\n")
		writeFn("a.mk", "include Makefile\n")

		content, err := readMakefileWithIncludes(ctx, dir)
		Expect(err).To(BeNil())

		recipe, found := extractRecipe(content)
		Expect(found).To(BeTrue())
		Expect(recipe).To(Equal([]string{"kubectl apply -f x.yaml"}))
	})

	It("leaves a variable-expanded include path unresolved rather than guessing", func() {
		writeFn("Makefile", "include $(SOMEDIR)/x.mk\napply:\n\tkubectl apply -f x.yaml\n")

		content, err := readMakefileWithIncludes(ctx, dir)
		Expect(err).To(BeNil())

		recipe, found := extractRecipe(content)
		Expect(found).To(BeTrue())
		Expect(recipe).To(Equal([]string{"kubectl apply -f x.yaml"}))
	})
})
