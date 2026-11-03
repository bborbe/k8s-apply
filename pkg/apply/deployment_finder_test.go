// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apply_test

import (
	"context"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/k8s-apply/pkg/apply"
)

var _ = Describe("DeploymentFinder", func() {
	var (
		dir       string
		component *apply.Component
		write     func(name string, content string)
	)

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
		component = &apply.Component{Path: "sentry-proxy", Dir: dir}
		write = func(name string, content string) {
			Expect(os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600)).To(Succeed())
		}
	})

	It("names the Deployments the manifests declare, skipping other kinds", func() {
		write("sentry-proxy-deploy.yaml", `apiVersion: apps/v1
kind: Deployment
metadata:
  name: sentry-proxy
  namespace: '{{ "NAMESPACE" | env }}'
---
apiVersion: v1
kind: Service
metadata:
  name: sentry-proxy
`)
		write("sentry-proxy-secret.yaml", "kind: Secret\nmetadata:\n  name: sentry-proxy\n")

		Expect(apply.NewDeploymentFinder().Find(context.Background(), component)).
			To(Equal([]string{"sentry-proxy"}))
	})

	It("returns no names for a component without a Deployment", func() {
		write("alert.yaml", "kind: Alert\nmetadata:\n  name: x\n")

		Expect(apply.NewDeploymentFinder().Find(context.Background(), component)).To(BeEmpty())
	})

	It("refuses a Deployment whose name is a template", func() {
		write("deploy.yaml", "kind: Deployment\nmetadata:\n  name: '{{ \"NAME\" | env }}'\n")

		_, err := apply.NewDeploymentFinder().Find(context.Background(), component)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("is not a literal"))
	})

	It("skips a manifest the yaml parser cannot read", func() {
		write("broken.yaml", "kind: Deployment\nmetadata: {{ unquoted }}\n")

		Expect(apply.NewDeploymentFinder().Find(context.Background(), component)).To(BeEmpty())
	})
})
