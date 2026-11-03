// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apply_test

import (
	"context"
	stderrors "errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/k8s-apply/mocks"
	"github.com/bborbe/k8s-apply/pkg/apply"
)

var _ = Describe("HealthCheckedApplier", func() {
	var (
		ctx       context.Context
		inner     *mocks.Applier
		finder    *mocks.DeploymentFinder
		client    *mocks.DeploymentClient
		applier   apply.Applier
		component *apply.Component
		healthy   *apply.DeploymentStatus
		broken    *apply.DeploymentStatus
		oldImages apply.ContainerImages
		newImages apply.ContainerImages
	)

	BeforeEach(func() {
		ctx = context.Background()
		inner = &mocks.Applier{}
		finder = &mocks.DeploymentFinder{}
		client = &mocks.DeploymentClient{}
		applier = apply.NewHealthCheckedApplier(
			inner,
			finder,
			client,
			50*time.Millisecond,
			5*time.Millisecond,
		)
		component = &apply.Component{Path: "sentry-proxy", Dir: "/repo/sentry-proxy"}
		healthy = &apply.DeploymentStatus{
			Generation: 3, ObservedGeneration: 3, Replicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1,
		}
		broken = &apply.DeploymentStatus{
			Generation: 3, ObservedGeneration: 3, Replicas: 1, UpdatedReplicas: 1, AvailableReplicas: 0, UnavailableReplicas: 1,
		}
		oldImages = apply.ContainerImages{"service": "registry/sentry-proxy:v0.5.1"}
		newImages = apply.ContainerImages{"service": "registry/sentry-proxy:v0.5.2"}
		finder.FindReturns([]string{"sentry-proxy"}, nil)
	})

	It("leaves a healthy apply in place", func() {
		client.ImagesReturnsOnCall(0, oldImages, nil)
		client.StatusReturns(healthy, nil)

		Expect(applier.Apply(ctx, component)).To(Succeed())
		Expect(inner.ApplyCallCount()).To(Equal(1))
		Expect(client.SetImagesCallCount()).To(Equal(0))
	})

	It("restores the pre-apply image when the stage fails its check", func() {
		client.ImagesReturnsOnCall(0, oldImages, nil)
		client.ImagesReturnsOnCall(1, newImages, nil)
		client.StatusReturns(broken, nil)

		err := applier.Apply(ctx, component)

		Expect(err).To(HaveOccurred())
		Expect(
			err.Error(),
		).To(ContainSubstring("post-apply health check failed for 'sentry-proxy'"))
		Expect(err.Error()).To(ContainSubstring("unavailable 1"))
		Expect(err.Error()).To(ContainSubstring(
			"restored deployment 'sentry-proxy' image service=registry/sentry-proxy:v0.5.2 -> service=registry/sentry-proxy:v0.5.1",
		))
		Expect(client.SetImagesCallCount()).To(Equal(1))
		_, name, images := client.SetImagesArgsForCall(0)
		Expect(name).To(Equal("sentry-proxy"))
		Expect(images).To(Equal(oldImages))
	})

	It("recovers when the rollout becomes healthy before the timeout", func() {
		client.ImagesReturnsOnCall(0, oldImages, nil)
		client.StatusReturnsOnCall(0, broken, nil)
		client.StatusReturnsOnCall(1, healthy, nil)

		Expect(applier.Apply(ctx, component)).To(Succeed())
		Expect(client.SetImagesCallCount()).To(Equal(0))
	})

	It("reports a failed check with nothing to restore when the image did not change", func() {
		client.ImagesReturns(oldImages, nil)
		client.StatusReturns(broken, nil)

		err := applier.Apply(ctx, component)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("image unchanged"))
		Expect(err.Error()).To(ContainSubstring("nothing to restore"))
		Expect(client.SetImagesCallCount()).To(Equal(0))
	})

	It("reports a failed check with nothing to restore for a new deployment", func() {
		client.ImagesReturnsOnCall(0, nil, nil)
		client.StatusReturns(broken, nil)

		err := applier.Apply(ctx, component)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("did not exist before the apply"))
		Expect(client.SetImagesCallCount()).To(Equal(0))
	})

	It("names a failed restore in the result", func() {
		client.ImagesReturnsOnCall(0, oldImages, nil)
		client.ImagesReturnsOnCall(1, newImages, nil)
		client.StatusReturns(broken, nil)
		client.SetImagesReturns(stderrors.New("forbidden"))

		err := applier.Apply(ctx, component)

		Expect(err).To(HaveOccurred())
		Expect(
			err.Error(),
		).To(ContainSubstring("restore of image service=registry/sentry-proxy:v0.5.1 failed: forbidden"))
	})

	It("returns the apply error without checking health", func() {
		client.ImagesReturns(oldImages, nil)
		inner.ApplyReturns(stderrors.New("make failed"))

		err := applier.Apply(ctx, component)

		Expect(err).To(MatchError("make failed"))
		Expect(client.StatusCallCount()).To(Equal(0))
		Expect(client.SetImagesCallCount()).To(Equal(0))
	})

	It("refuses to apply when the pre-apply image cannot be captured", func() {
		client.ImagesReturns(nil, stderrors.New("unauthorized"))

		err := applier.Apply(ctx, component)

		Expect(err).To(HaveOccurred())
		Expect(
			err.Error(),
		).To(ContainSubstring("capture pre-apply images of 'sentry-proxy' failed"))
		Expect(inner.ApplyCallCount()).To(Equal(0))
	})

	It("refuses to apply when the deployments cannot be named", func() {
		finder.FindReturns(nil, stderrors.New("templated name"))

		err := applier.Apply(ctx, component)

		Expect(err).To(HaveOccurred())
		Expect(inner.ApplyCallCount()).To(Equal(0))
	})
})

var _ = Describe("DeploymentStatus", func() {
	It("is healthy when every replica is updated and available", func() {
		Expect(apply.DeploymentStatus{
			Generation: 2, ObservedGeneration: 2, Replicas: 2, UpdatedReplicas: 2, AvailableReplicas: 2,
		}.Healthy()).To(BeTrue())
	})

	// A surviving old pod keeps availableReplicas up while the new one fails.
	It("is not healthy while an old replica still serves", func() {
		Expect(apply.DeploymentStatus{
			Generation: 2, ObservedGeneration: 2, Replicas: 2, UpdatedReplicas: 1, AvailableReplicas: 2, UnavailableReplicas: 1,
		}.Healthy()).To(BeFalse())
	})

	It("is not healthy before the controller observed the new generation", func() {
		Expect(apply.DeploymentStatus{
			Generation: 3, ObservedGeneration: 2, Replicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1,
		}.Healthy()).To(BeFalse())
	})
})
