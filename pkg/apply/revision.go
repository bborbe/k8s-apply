// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apply

import (
	"context"
	"os/exec"
	"strings"

	"github.com/bborbe/errors"
)

//counterfeiter:generate -o ../../mocks/revision.go --fake-name Revision . Revision

// Revision verifies that the deploy-repo checkout the applier is about to run
// make in is the revision the approval covers.
//
// It exists because an approval's commit binding is otherwise paperwork: the
// hash can say "commit A" while the pod's init container cloned a branch whose
// tip is commit B, and make then applies B under an approval for A. Verifying
// the checkout is what turns the binding into a control.
type Revision interface {
	Verify(ctx context.Context, commit string) error
}

// NewGitRevision returns a Revision that reads HEAD from the deploy-repo
// checkout at repoDir.
func NewGitRevision(repoDir string) Revision {
	return &gitRevision{repoDir: repoDir}
}

type gitRevision struct {
	repoDir string
}

func (g *gitRevision) Verify(ctx context.Context, commit string) error {
	if commit == "" {
		return errors.Errorf(ctx, "approval carries no commit to verify the checkout against")
	}
	// #nosec G204 -- the binary is the literal "git" and the only variable is
	// repoDir, which comes from the service's own configuration. No command
	// text is taken from the command payload; the commit is a comparison
	// operand, never an argument.
	cmd := exec.CommandContext(ctx, "git", "-C", g.repoDir, "rev-parse", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return errors.Wrapf(ctx, err, "read HEAD of the deploy-repo checkout failed")
	}
	head := strings.TrimSpace(string(output))
	if head != commit {
		return errors.Errorf(
			ctx,
			"deploy-repo checkout is at %s but the approval covers %s: refusing to apply a revision nobody approved",
			head,
			commit,
		)
	}
	return nil
}
