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
// continuations joined into one logical line per entry. found reports whether
// the target exists AND declares something to run: its prerequisites, its
// recipe lines, or both.
//
// A prerequisite-only target (`apply: a b c`) is found with an empty recipe,
// because make runs its prerequisites. A placeholder target with neither
// prerequisites nor a recipe (`apply:`) is not found, so it is still refused:
// a target that would run nothing must not report a success.
func extractRecipe(content string) (recipe []string, found bool) {
	lines := strings.Split(content, "\n")
	start := -1
	targetLine := ""
	for i, line := range lines {
		if !strings.HasPrefix(line, applyTarget+":") {
			continue
		}
		start = i + 1
		targetLine = line
		break
	}
	if start == -1 {
		return nil, false
	}

	// The target line itself may declare prerequisites and a one-line recipe
	// (`apply: dep ; docker build -t x .`). The text after the first semicolon
	// is a recipe line even though it never appears in a tab-indented line, so
	// it must be visible to runsDocker.
	declaresPrerequisites := false
	rest := stripMakeComment(strings.TrimPrefix(targetLine, applyTarget+":"))
	prerequisites, inlineRecipe, hasInlineRecipe := strings.Cut(rest, ";")
	declaresPrerequisites = strings.TrimSpace(prerequisites) != ""
	if hasInlineRecipe {
		// make strips its command prefixes from a one-line recipe exactly as it
		// does from a tab-indented one. Without this, `apply: ; @docker build .`
		// reaches runsDocker still carrying the '@' — which is neither the start
		// of the string nor whitespace — and slips past the refusal.
		body := strings.TrimLeft(strings.TrimSpace(inlineRecipe), recipeCommandPrefixes)
		if body != "" {
			recipe = append(recipe, body)
		}
	}

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
	return recipe, declaresPrerequisites || len(recipe) > 0
}

// stripMakeComment returns s with everything from its first unescaped '#'
// removed. make treats that character as the start of a comment, so the text
// after it is never part of the target's prerequisites or recipe — `apply: ##
// help` declares nothing, and must not be read as a prerequisite list.
func stripMakeComment(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] != '#' {
			continue
		}
		if i > 0 && s[i-1] == '\\' {
			continue
		}
		return s[:i]
	}
	return s
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
