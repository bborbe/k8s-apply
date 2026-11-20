// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apply

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bborbe/errors"
)

// maxIncludeDepth bounds include recursion. A legitimate component nests two or
// three levels (component Makefile -> Makefile.variables/env/k8s); the bound
// exists so a pathological chain fails with a named error instead of hanging.
const maxIncludeDepth = 10

// includeDirective matches make's `include` and `-include` directives. The
// leading `-` marks the include optional: make continues when the file is
// absent.
var includeDirective = regexp.MustCompile(`^\s*(-?)include\s+(.+)$`)

// readMakefileWithIncludes returns the content of dir/Makefile with every
// include directive replaced by the content of the file it names, recursively.
//
// This exists because a component Makefile may be nothing but includes. In
// data/cdb-config both schemas/cdb/Makefile and schemas/raw/Makefile contain
// only
//
//	include ../../Makefile.variables
//	include ../../Makefile.env
//	include ../../Makefile.k8s
//
// and the `apply:` target lives in Makefile.k8s. extractRecipe reads a single
// string, so without expansion it finds no target and Resolve refuses a
// component that make itself would happily apply.
//
// Paths are resolved relative to the including file's directory, which is what
// make does for `make -C <dir>`. An include path containing a make variable
// reference is left unresolved rather than guessed at: extractRecipe then
// simply sees no target, which is a refusal rather than a wrong answer.
func readMakefileWithIncludes(ctx context.Context, dir string) (string, error) {
	return readFileWithIncludes(ctx, filepath.Join(dir, "Makefile"), 0, map[string]struct{}{})
}

func readFileWithIncludes(
	ctx context.Context,
	path string,
	depth int,
	visited map[string]struct{},
) (string, error) {
	if depth > maxIncludeDepth {
		return "", errors.Errorf(ctx, "include depth exceeded at '%s'", path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", errors.Wrapf(ctx, err, "resolve '%s' failed", path)
	}
	// A file already expanded contributes nothing the second time. This is what
	// makes an include cycle terminate instead of recursing to the depth bound.
	if _, ok := visited[abs]; ok {
		return "", nil
	}
	visited[abs] = struct{}{}

	// #nosec G703 -- the path is a make include directive read out of the
	// deploy-repo checkout, which is the same trust domain as the Makefile that
	// names it: make itself resolves that path and reads that file. The component
	// directory was confined to repoDir by the resolver before this runs, and an
	// include reaching outside it is the checkout's own content, not caller input.
	content, err := os.ReadFile(abs)
	if err != nil {
		return "", errors.Wrapf(ctx, err, "read '%s' failed", abs)
	}

	baseDir := filepath.Dir(abs)
	var out strings.Builder
	for _, line := range strings.Split(string(content), "\n") {
		match := includeDirective.FindStringSubmatch(line)
		if match == nil {
			out.WriteString(line)
			out.WriteString("\n")
			continue
		}
		optional := match[1] == "-"
		target := strings.TrimSpace(stripMakeComment(match[2]))
		if target == "" || strings.Contains(target, "$") {
			// Nothing to resolve, or make would expand a variable first. Keep the
			// directive verbatim so the text still reads as the file it came from.
			out.WriteString(line)
			out.WriteString("\n")
			continue
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(baseDir, target)
		}
		expanded, err := readFileWithIncludes(ctx, target, depth+1, visited)
		if err != nil {
			if optional {
				continue
			}
			return "", err
		}
		out.WriteString(expanded)
	}
	return out.String(), nil
}
