// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apply

import (
	"context"
	"path"
	"strings"

	"github.com/bborbe/errors"
)

// Command is the payload of an apply command published through the CQRS
// command gateway. Path names one component of the nuke deploy repo, relative
// to the repo root — for example "egress-proxy" or "trading/core/symbol".
type Command struct {
	Path string `json:"path"`
}

// Validate rejects a command whose path could escape the repo root or is
// otherwise unusable. It deliberately does not touch the filesystem: an
// unknown component is a resolve failure, not a malformed command.
func (c Command) Validate(ctx context.Context) error {
	cleaned := strings.TrimSpace(c.Path)
	if cleaned == "" {
		return errors.Errorf(ctx, "path missing")
	}
	if strings.HasPrefix(cleaned, "/") {
		return errors.Errorf(ctx, "path '%s' must be relative to the repo root", cleaned)
	}
	if path.Clean(cleaned) != cleaned {
		return errors.Errorf(ctx, "path '%s' is not a clean relative path", cleaned)
	}
	for _, part := range strings.Split(cleaned, "/") {
		if part == ".." {
			return errors.Errorf(ctx, "path '%s' must not escape the repo root", cleaned)
		}
		if part == "." {
			return errors.Errorf(ctx, "path '%s' does not name a component", cleaned)
		}
	}
	return nil
}
