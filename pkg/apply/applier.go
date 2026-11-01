// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apply

import (
	"context"
	"os/exec"
	"strings"

	"github.com/bborbe/errors"
	"github.com/bborbe/log"
	"github.com/golang/glog"
)

// DevBranch is the only branch this service may deploy. The service is
// dev-scoped by construction: a prod deploy stays an operator action.
const DevBranch = "dev"

//counterfeiter:generate -o ../../mocks/applier.go --fake-name Applier . Applier

// Applier runs one component's own apply target.
type Applier interface {
	Apply(ctx context.Context, component *Component) error
}

// NewApplier returns an Applier that shells out to the component's own make
// target. It never rewrites the recipe: whatever the component's Makefile says
// `apply` does is what runs.
func NewApplier(branch string, logSamplerFactory log.SamplerFactory) Applier {
	return &applier{
		branch:     branch,
		logSampler: logSamplerFactory.Sampler(),
	}
}

type applier struct {
	branch     string
	logSampler log.Sampler
}

func (a *applier) Apply(ctx context.Context, component *Component) error {
	if a.branch != DevBranch {
		return errors.Errorf(
			ctx,
			"refusing to apply on branch '%s': this service only deploys '%s'",
			a.branch,
			DevBranch,
		)
	}
	args := []string{"-C", component.Dir, "BRANCH=" + a.branch, applyTarget}
	// #nosec G204 -- the component path was validated by the resolver and make
	// is the documented entry point of every component in the deploy repo.
	cmd := exec.CommandContext(ctx, "make", args...)
	output, err := cmd.CombinedOutput()
	if a.logSampler.IsSample() {
		glog.V(2).Infof(
			"apply %s: make %s exited with %v (sample)",
			component.Path,
			strings.Join(args, " "),
			err,
		)
	}
	if err != nil {
		return errors.Wrapf(
			ctx,
			err,
			"apply '%s' failed: %s",
			component.Path,
			strings.TrimSpace(string(output)),
		)
	}
	return nil
}
