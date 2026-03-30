package gitignore

import (
	"strings"

	extgitignore "github.com/git-pkgs/gitignore"
)

// MatchResult defines outcomes of a match, no match, exclusion or inclusion.
type MatchResult int

const (
	// NoMatch defines the no match outcome of a match check
	NoMatch MatchResult = iota
	// Exclude defines an exclusion of a file as a result of a match check
	Exclude
	// Include defines an explicit inclusion of a file as a result of a match check
	Include
)

const (
	inclusionPrefix = "!"
	patternDirSep   = "/"
)

// Pattern defines a single gitignore pattern.
type Pattern interface {
	// Match matches the given path to the pattern.
	Match(path []string, isDir bool) MatchResult
}

type pattern struct {
	domain        []string
	patternText   string
	originalText  string
	isInclusion   bool
}

// ParsePattern parses a gitignore pattern string into the Pattern structure.
func ParsePattern(p string, domain []string) Pattern {
	// storing domain, copy it to ensure it isn't changed externally
	domain = append([]string(nil), domain...)

	originalText := p
	isInclusion := false

	if strings.HasPrefix(p, inclusionPrefix) {
		isInclusion = true
		p = p[1:]
	}

	if !strings.HasSuffix(p, "\\ ") {
		p = strings.TrimRight(p, " ")
	}

	return &pattern{
		domain:       domain,
		patternText:  p,
		originalText: originalText,
		isInclusion:  isInclusion,
	}
}

func (p *pattern) Match(path []string, isDir bool) MatchResult {
	// Check if path is within the domain
	if len(path) <= len(p.domain) {
		return NoMatch
	}
	for i, e := range p.domain {
		if path[i] != e {
			return NoMatch
		}
	}

	// Extract the path relative to the domain
	relativePath := path[len(p.domain):]

	// Convert path from []string to string with forward slashes
	pathStr := strings.Join(relativePath, patternDirSep)

	// Create a matcher with this single pattern
	// The domain serves as the directory context for the pattern
	matcher := extgitignore.New("")

	// Build the pattern line with domain prefix if needed
	// git-pkgs/gitignore expects patterns relative to the matcher's directory
	patternLine := p.patternText
	if p.isInclusion {
		patternLine = inclusionPrefix + patternLine
	}

	matcher.AddPatterns([]byte(patternLine), "")

	// Match using git-pkgs/gitignore
	// Use MatchPath for directory-awareness, MatchDetail for negation info
	matchPath := matcher.MatchPath(pathStr, isDir)

	// MatchDetail requires trailing slash to identify directories for patterns like !dir/**/
	detailPath := pathStr
	if isDir && !strings.HasSuffix(pathStr, patternDirSep) {
		detailPath = pathStr + patternDirSep
	}
	detail := matcher.MatchDetail(detailPath)

	// If MatchPath says no match, trust it (handles directory-specific patterns)
	// Exception: detail might show a negation match even when MatchPath is false
	if !matchPath && !detail.Matched {
		return NoMatch
	}

	// If detail shows a negation pattern matched, that's an Include
	if detail.Negate {
		return Include
	}

	// If MatchPath matched, it's an Exclude
	if matchPath {
		return Exclude
	}

	// Edge case: detail.Matched but !matchPath and !detail.Negate
	// This can happen with directory-specific patterns - trust MatchPath
	return NoMatch
}
