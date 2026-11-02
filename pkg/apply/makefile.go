// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apply

import (
	"regexp"
	"strings"
)

// applyTarget is the make target every applicable component must define.
const applyTarget = "apply"

// recipeLinePrefix is the byte every make recipe line starts with. A line
// starting with anything else ends the recipe.
const recipeLinePrefix = "\t"

// recipeCommandPrefixes are the characters make strips from the front of a
// recipe line before handing it to the shell: `@` suppresses echoing the
// command, `-` ignores a non-zero exit, and `+` forces the line to run even
// under `-n`. They may be combined in any order (`@-`, `-@`).
//
// They matter to the docker refusal because a prefixed line runs exactly the
// command it prefixes: `@docker manifest inspect` needs a docker binary just as
// plainly as `docker manifest inspect` does. Reading only the unprefixed form
// let such a component through as if it were an ordinary kubectl apply.
const recipeCommandPrefixes = "@-+"

// dockerInvocation matches a docker command as a whole word inside a recipe,
// so a component that merely mentions docker in a comment or a variable name
// is not refused.
var dockerInvocation = regexp.MustCompile(`(^|[\s;&|(])docker(\s|$)`)

// extractRecipe returns the recipe lines of the apply target, with backslash
// continuations joined into one logical line per entry. It returns nil when the
// target is not defined, which callers must treat as "this component cannot be
// applied" rather than "empty recipe".
func extractRecipe(content string) []string {
	lines := strings.Split(content, "\n")
	start := -1
	for i, line := range lines {
		if !strings.HasPrefix(line, applyTarget+":") {
			continue
		}
		start = i + 1
		break
	}
	if start == -1 {
		return nil
	}

	var recipe []string
	var pending strings.Builder
	for _, line := range lines[start:] {
		if !strings.HasPrefix(line, recipeLinePrefix) {
			if pending.Len() > 0 {
				recipe = append(recipe, pending.String())
			}
			break
		}
		body := strings.TrimPrefix(line, recipeLinePrefix)
		// Prefixes are recognised only at the start of a recipe line, never on
		// a backslash-continued one. In
		//     docker build \
		//     -t x .
		// the second line's `-` is the shell flag for `docker build`, not make's
		// ignore-errors marker; stripping it there would silently corrupt the
		// command this function reports.
		if pending.Len() == 0 {
			body = strings.TrimLeft(body, recipeCommandPrefixes)
		}
		if strings.HasSuffix(body, `\`) {
			pending.WriteString(strings.TrimSuffix(body, `\`))
			pending.WriteString(" ")
			continue
		}
		pending.WriteString(body)
		recipe = append(recipe, pending.String())
		pending.Reset()
	}
	if pending.Len() > 0 {
		recipe = append(recipe, pending.String())
	}
	return recipe
}

// runsDocker reports whether any recipe line invokes docker.
func runsDocker(recipe []string) bool {
	for _, line := range recipe {
		if dockerInvocation.MatchString(line) {
			return true
		}
	}
	return false
}
