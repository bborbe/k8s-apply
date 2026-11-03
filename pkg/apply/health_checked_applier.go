// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apply

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bborbe/errors"
	"github.com/golang/glog"
)

// NewHealthCheckedApplier wraps an Applier so that an apply which lands but
// leaves the stage unhealthy is rolled back to the images it ran before.
//
// make exits 0 the moment the manifests are accepted; it never observes the
// rollout. This applier records every Deployment's container images before the
// apply, waits for each rollout to finish after it, and on a failed check
// writes the recorded images back. The returned error names the failed check
// and the images restored, and the command executor publishes it as the
// Failure entry on the result topic — that entry is the rollback record.
func NewHealthCheckedApplier(
	inner Applier,
	finder DeploymentFinder,
	client DeploymentClient,
	timeout time.Duration,
	pollInterval time.Duration,
) Applier {
	return &healthCheckedApplier{
		inner:        inner,
		finder:       finder,
		client:       client,
		timeout:      timeout,
		pollInterval: pollInterval,
	}
}

type healthCheckedApplier struct {
	inner        Applier
	finder       DeploymentFinder
	client       DeploymentClient
	timeout      time.Duration
	pollInterval time.Duration
}

func (h *healthCheckedApplier) Apply(ctx context.Context, component *Component) error {
	names, err := h.finder.Find(ctx, component)
	if err != nil {
		return errors.Wrapf(ctx, err, "refusing to apply component '%s'", component.Path)
	}
	before := make(map[string]ContainerImages, len(names))
	for _, name := range names {
		images, err := h.client.Images(ctx, name)
		if err != nil {
			return errors.Wrapf(ctx, err, "capture pre-apply images of '%s' failed", component.Path)
		}
		before[name] = images
	}

	// An apply that fails leaves nothing new to observe: the health check and
	// the restore only follow an apply that landed.
	if err := h.inner.Apply(ctx, component); err != nil {
		return errors.Wrapf(
			ctx,
			err,
			"apply component '%s' failed before the health check",
			component.Path,
		)
	}

	var failures []string
	for _, name := range names {
		if reason := h.waitHealthy(ctx, name); reason != "" {
			failures = append(failures, fmt.Sprintf("deployment '%s': %s", name, reason))
		}
	}
	if len(failures) == 0 {
		return nil
	}
	restored := h.restore(ctx, names, before)
	return errors.Errorf(
		ctx,
		"post-apply health check failed for '%s': %s; %s",
		component.Path,
		strings.Join(failures, "; "),
		strings.Join(restored, "; "),
	)
}

// waitHealthy polls until the rollout is healthy or the timeout passes, and
// returns an empty string on success or the reason it failed.
func (h *healthCheckedApplier) waitHealthy(ctx context.Context, name string) string {
	ctx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()
	ticker := time.NewTicker(h.pollInterval)
	defer ticker.Stop()

	var last *DeploymentStatus
	var lastErr error
	for {
		status, err := h.client.Status(ctx, name)
		if err == nil && status.Healthy() {
			return ""
		}
		if err == nil {
			last = status
		}
		lastErr = err
		select {
		case <-ctx.Done():
			if last != nil {
				return fmt.Sprintf(
					"not healthy after %s (generation %d observed %d, replicas %d updated %d available %d unavailable %d)",
					h.timeout,
					last.Generation,
					last.ObservedGeneration,
					last.Replicas,
					last.UpdatedReplicas,
					last.AvailableReplicas,
					last.UnavailableReplicas,
				)
			}
			return fmt.Sprintf("status unreadable after %s: %v", h.timeout, lastErr)
		case <-ticker.C:
		}
	}
}

// restore writes the pre-apply images back to every Deployment whose images
// changed, and describes what it did for the result entry.
func (h *healthCheckedApplier) restore(
	ctx context.Context,
	names []string,
	before map[string]ContainerImages,
) []string {
	var restored []string
	for _, name := range names {
		previous := before[name]
		if previous == nil {
			restored = append(
				restored,
				fmt.Sprintf(
					"deployment '%s' did not exist before the apply: nothing to restore",
					name,
				),
			)
			continue
		}
		current, err := h.client.Images(ctx, name)
		if err != nil {
			restored = append(
				restored,
				fmt.Sprintf("deployment '%s' restore skipped, read images failed: %v", name, err),
			)
			continue
		}
		if current.Equal(previous) {
			restored = append(
				restored,
				fmt.Sprintf(
					"deployment '%s' image unchanged (%s): nothing to restore",
					name,
					previous,
				),
			)
			continue
		}
		if err := h.client.SetImages(ctx, name, previous); err != nil {
			restored = append(
				restored,
				fmt.Sprintf("deployment '%s' restore of image %s failed: %v", name, previous, err),
			)
			continue
		}
		glog.V(1).Infof("restored deployment %s image %s -> %s", name, current, previous)
		restored = append(
			restored,
			fmt.Sprintf("restored deployment '%s' image %s -> %s", name, current, previous),
		)
	}
	return restored
}
