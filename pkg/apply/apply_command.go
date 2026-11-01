// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apply

import (
	"context"
	"encoding/json"
	"path"
	"strings"

	"github.com/bborbe/errors"
)

// Command is the payload of an apply command published through the CQRS
// command gateway. Path names one component of the nuke deploy repo, relative
// to the repo root — for example "egress-proxy" or "trading/core/symbol".
type Command struct {
	Path string `json:"path"`
	// Approval is the operator approval this command carries. It is left raw
	// rather than typed so that a payload carrying a *list* of approvals is
	// refused by name ("more than one approval carried") instead of failing as
	// a generic unmarshal error that names no cause. ParseApproval reads it.
	//
	// Absent on stages that need none, and on a stage that requires one it is
	// what the Approver refuses — never something Validate rejects, because a
	// missing approval is a policy refusal with a reason, not a malformed
	// command.
	Approval json.RawMessage `json:"approval,omitempty"`
}

// Validate rejects a command whose path could escape the repo root or is
// otherwise unusable. It deliberately does not touch the filesystem: an
// unknown component is a resolve failure, not a malformed command.
//
// Every check is on the value that is later used, never on a normalized copy:
// a path this accepts is joined verbatim against the repo root by the resolver,
// so " egress " is rejected here rather than accepted and then looked up under
// a different name.
func (c Command) Validate(ctx context.Context) error {
	if c.Path == "" {
		return errors.Errorf(ctx, "path missing")
	}
	if strings.TrimSpace(c.Path) != c.Path {
		return errors.Errorf(ctx, "path '%s' has leading or trailing whitespace", c.Path)
	}
	if strings.HasPrefix(c.Path, "/") {
		return errors.Errorf(ctx, "path '%s' must be relative to the repo root", c.Path)
	}
	if path.Clean(c.Path) != c.Path {
		return errors.Errorf(ctx, "path '%s' is not a clean relative path", c.Path)
	}
	for _, part := range strings.Split(c.Path, "/") {
		if part == ".." {
			return errors.Errorf(ctx, "path '%s' must not escape the repo root", c.Path)
		}
		if part == "." {
			return errors.Errorf(ctx, "path '%s' does not name a component", c.Path)
		}
	}
	return nil
}
