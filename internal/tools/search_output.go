package tools

import (
	"path"
	"strconv"
	"strings"
	"unicode/utf8"
)

type searchOutput struct {
	lines      []string
	bytes, max int
	limit      string
}

func (s *searchOutput) append(line string) bool {
	if len(s.lines) >= s.max {
		s.limit = "matches"
		return false
	}
	size := len(line)
	if len(s.lines) > 0 {
		size++
	}
	if s.bytes+size > MaxSearchOutputBytes {
		s.limit = "bytes"
		return false
	}
	s.lines = append(s.lines, line)
	s.bytes += size
	return true
}
func takeUTF8Prefix(text string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(text) <= n {
		return text
	}
	for n > 0 && !utf8.RuneStart(text[n]) {
		n--
	}
	return text[:n]
}

func expandBraces(pattern string) []string {
	start := -1
	depth := 0
	for i, r := range pattern {
		if r == '{' {
			if start < 0 {
				start = i
			}
			depth++
		}
		if r == '}' && start >= 0 {
			depth--
			if depth != 0 {
				continue
			}
			body := pattern[start+1 : i]
			var options []string
			cursor, nested := 0, 0
			for j, c := range body {
				if c == '{' {
					nested++
				}
				if c == '}' {
					nested--
				}
				if c == ',' && nested == 0 {
					options = append(options, body[cursor:j])
					cursor = j + 1
				}
			}
			if len(options) > 0 {
				options = append(options, body[cursor:])
			} else {
				numbers := strings.Split(body, "..")
				if len(numbers) == 2 || len(numbers) == 3 {
					from, e1 := strconv.Atoi(numbers[0])
					to, e2 := strconv.Atoi(numbers[1])
					step := 1
					if len(numbers) == 3 {
						step, _ = strconv.Atoi(numbers[2])
						if step < 0 {
							step = -step
						}
					}
					if e1 == nil && e2 == nil && step > 0 && max(from, to)-min(from, to) <= 10000 {
						if from > to {
							step = -step
						}
						for n := from; step > 0 && n <= to || step < 0 && n >= to; n += step {
							options = append(options, strconv.Itoa(n))
						}
					}
				}
			}
			if len(options) == 0 {
				return []string{pattern}
			}
			var out []string
			for _, option := range options {
				out = append(out, expandBraces(pattern[:start]+option+pattern[i+1:])...)
			}
			return out
		}
	}
	return []string{pattern}
}

func matchGlob(pattern, name string) bool {
	for _, expanded := range expandBraces(pattern) {
		candidate := name
		if !strings.Contains(expanded, "/") {
			candidate = path.Base(name)
		}
		patterns, segments := strings.Split(expanded, "/"), strings.Split(candidate, "/")
		type state struct{ p, s int }
		seen := map[state]bool{}
		var match func(int, int) bool
		match = func(p, s int) bool {
			key := state{p, s}
			if seen[key] {
				return false
			}
			seen[key] = true
			if p == len(patterns) {
				return s == len(segments)
			}
			if patterns[p] == "**" {
				return match(p+1, s) || s < len(segments) && match(p, s+1)
			}
			if s >= len(segments) {
				return false
			}
			segment := patterns[p]
			if strings.HasPrefix(segment, "[") && strings.HasPrefix(segment, "[!") {
				segment = "[^" + segment[2:]
			}
			ok, _ := path.Match(segment, segments[s])
			return ok && match(p+1, s+1)
		}
		if match(0, 0) {
			return true
		}
	}
	return false
}

func searchAllowed(ctx ExecutionContext, path string, traverse bool) bool {
	if traverse && ctx.CanTraverseSearchPath != nil {
		return ctx.CanTraverseSearchPath(path)
	}
	if ctx.CanReadSearchPath != nil {
		return ctx.CanReadSearchPath(path)
	}
	return true
}
