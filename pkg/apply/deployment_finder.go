// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apply

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bborbe/errors"
	"github.com/golang/glog"
	"go.yaml.in/yaml/v3"
)

//counterfeiter:generate -o ../../mocks/deployment-finder.go --fake-name DeploymentFinder . DeploymentFinder

// DeploymentFinder names the Deployments a component's manifests declare.
type DeploymentFinder interface {
	Find(ctx context.Context, component *Component) ([]string, error)
}

// NewDeploymentFinder returns a DeploymentFinder that reads the component's own
// top-level *.yaml files. It reads the manifests as written, before make renders
// them: only metadata.name is used, and a name that is itself a template cannot
// be resolved here, so it is refused rather than guessed.
func NewDeploymentFinder() DeploymentFinder {
	return &deploymentFinder{}
}

type deploymentFinder struct{}

type manifestHeader struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
}

func (d *deploymentFinder) Find(ctx context.Context, component *Component) ([]string, error) {
	paths, err := filepath.Glob(filepath.Join(component.Dir, "*.yaml"))
	if err != nil {
		return nil, errors.Wrapf(ctx, err, "list manifests of '%s' failed", component.Path)
	}
	sort.Strings(paths)
	var names []string
	for _, path := range paths {
		found, err := deploymentNames(ctx, path)
		if err != nil {
			return nil, errors.Wrapf(ctx, err, "find deployments of '%s' failed", component.Path)
		}
		names = append(names, found...)
	}
	return names, nil
}

func deploymentNames(ctx context.Context, path string) ([]string, error) {
	// #nosec G304 -- path comes from a glob inside the resolver-validated component dir.
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrapf(ctx, err, "read manifest '%s' failed", path)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	var names []string
	for {
		var header manifestHeader
		err := decoder.Decode(&header)
		if err == io.EOF {
			return names, nil
		}
		if err != nil {
			// A manifest the yaml parser cannot read (an unquoted template, say)
			// is not this check's to reject: make renders it, and refusing here
			// would block applies that work today.
			glog.V(1).Infof("skip manifest %s for deployment discovery: %v", path, err)
			return names, nil
		}
		if header.Kind != "Deployment" {
			continue
		}
		if header.Metadata.Name == "" || strings.Contains(header.Metadata.Name, "{{") {
			return nil, errors.Errorf(
				ctx,
				"manifest '%s' declares a Deployment whose name '%s' is not a literal: cannot capture its pre-apply image",
				path,
				header.Metadata.Name,
			)
		}
		names = append(names, header.Metadata.Name)
	}
}
