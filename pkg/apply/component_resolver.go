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

// ErrDockerComponent is returned when a component's apply target invokes
// docker at all — a build, a push, or a registry read such as
// `docker manifest inspect`. v1 of this service refuses those by name instead
// of running them, because the pod has no docker daemon and no docker config:
// the image work belongs to the release pipeline, not to a deploy command, and
// a target that assumes a daemon would fail in a way that names nothing useful.
//
// The check is textual, and deliberately so: it sees a docker command written
// literally in the recipe, behind make's `@`/`-`/`+` prefixes. It does not see
// one reached through a variable (`$(DOCKER) build`), through `podman`, through
// `docker-compose`, or through `sh -c 'docker …'`. Treat it as a guard against
// the shape that actually appears in this repo, not as a sandbox.
var ErrDockerComponent = stderrors.New("docker component refused")

// ErrNotApplicable is returned when a component has no apply target at all.
var ErrNotApplicable = stderrors.New("component has no apply target")

// ErrExcludedComponent is returned for a component this service refuses by name
// even though its apply target would succeed.
//
// `teamvault` is the reason this exists. Its `apply:` target only runs envsubst
// and `kubectl apply` over its own manifests — the TeamVault reads live in its
// separate operator-only `secrets:` target. So applying it would redeploy the
// component that holds every secret in the cluster without touching a single
// TeamVault grant, which is why neither the docker refusal nor a grant list
// stops that path. This refusal is the only thing that does.
var ErrExcludedComponent = stderrors.New("component excluded by design")

// excludedComponents are refused by name, before the component is even looked
// for on disk, so the refusal does not depend on what a checkout happens to
// contain.
var excludedComponents = map[string]struct{}{
	"teamvault": {},
}

// isExcluded reports whether path names a component this service refuses by
// design.
func isExcluded(path string) bool {
	_, ok := excludedComponents[path]
	return ok
}

// Component is one resolvable unit of the nuke deploy repo.
type Component struct {
	// Path is the component path relative to the repo root, exactly as the
	// command named it.
	Path string
	// Dir is the absolute directory the apply target runs in.
	Dir string
	// Recipe records the resolved `apply` recipe lines for diagnostics. It is
	// empty when the target exists but declares no recipe lines — for example a
	// prerequisite-only target, which make still runs through its prerequisites.
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
	if isExcluded(path) {
		return nil, errors.Wrapf(
			ctx,
			ErrExcludedComponent,
			"component '%s' is excluded by design",
			path,
		)
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
	recipe, found := extractRecipe(string(content))
	if !found {
		return nil, errors.Wrapf(ctx, ErrNotApplicable, "component '%s'", path)
	}
	if runsDocker(recipe) {
		return nil, errors.Wrapf(
			ctx,
			ErrDockerComponent,
			"component '%s' invokes docker in its apply target",
			path,
		)
	}
	return &Component{Path: path, Dir: dir, Recipe: recipe}, nil
}
