// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apply

import (
	"context"
	stderrors "errors"
	"os"
	"path/filepath"

	"github.com/bborbe/errors"
)

// ErrDockerComponent is returned when a component's apply target builds or
// pushes a container image. v1 of this service refuses those by name instead
// of running them, because an in-cluster build has no docker daemon and the
// image build belongs to the release pipeline, not to a deploy command.
var ErrDockerComponent = stderrors.New("docker component refused")

// ErrNotApplicable is returned when a component has no apply target at all.
var ErrNotApplicable = stderrors.New("component has no apply target")

// Component is one resolvable unit of the nuke deploy repo.
type Component struct {
	// Path is the component path relative to the repo root, exactly as the
	// command named it.
	Path string
	// Dir is the absolute directory the apply target runs in.
	Dir string
	// Recipe is the resolved `apply` recipe, used for the refusal decision and
	// for the log line.
	Recipe []string
}

//counterfeiter:generate -o ../../mocks/component-resolver.go --fake-name ComponentResolver . ComponentResolver

// ComponentResolver turns a command path into a Component, refusing the cases
// that must never reach a shell.
type ComponentResolver interface {
	Resolve(ctx context.Context, path string) (*Component, error)
}

// NewComponentResolver returns a resolver rooted at the deploy repo checkout.
func NewComponentResolver(repoDir string) ComponentResolver {
	return &componentResolver{repoDir: repoDir}
}

type componentResolver struct {
	repoDir string
}

func (c *componentResolver) Resolve(ctx context.Context, path string) (*Component, error) {
	if err := (Command{Path: path}).Validate(ctx); err != nil {
		return nil, errors.Wrap(ctx, err, "validate path failed")
	}
	dir := filepath.Join(c.repoDir, path)
	info, err := os.Stat(dir)
	if err != nil {
		return nil, errors.Wrapf(ctx, err, "component '%s' not found in repo", path)
	}
	if !info.IsDir() {
		return nil, errors.Errorf(ctx, "component '%s' is not a directory", path)
	}
	content, err := os.ReadFile(filepath.Join(dir, "Makefile"))
	if err != nil {
		return nil, errors.Wrapf(ctx, err, "component '%s' has no Makefile", path)
	}
	recipe := extractRecipe(string(content))
	if len(recipe) == 0 {
		return nil, errors.Wrapf(ctx, ErrNotApplicable, "component '%s'", path)
	}
	if runsDocker(recipe) {
		return nil, errors.Wrapf(
			ctx,
			ErrDockerComponent,
			"component '%s' builds a container image in its apply target",
			path,
		)
	}
	return &Component{Path: path, Dir: dir, Recipe: recipe}, nil
}
