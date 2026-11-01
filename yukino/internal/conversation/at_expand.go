package conversation

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	MaxInlineBytes      = 100_000
	MaxInlineTotalBytes = 300_000
	MaxAtRefs           = 32
	// Files larger than this are never read, even for a narrow line range.
	MaxRangeFileBytes = 10_000_000
)

// An @ref may carry a #L3 or #L3-10 line-range suffix (inserted via the IDE
// integration).
var atRefLineRangeRe = regexp.MustCompile(`^(.+)#L(\d+)(?:-L?(\d+))?$`)

// Clipboard images and paths containing spaces use quoted mentions.
var atRefRe = regexp.MustCompile("(?:^|[\\s(（])(?:'@([^']+)'|\"@([^\"]+)\"|@\"([^\"]+)\"|@'([^']+)'|@([^\\s#,;，；。！？、()（）<>\"'`]+(?:#L\\d+(?:-L?\\d+)?)?))")

var hanRunRe = regexp.MustCompile(`\p{Han}+`)

func collectAtRefs(text, cwd string) []string {
	matches := atRefRe.FindAllStringSubmatchIndex(text, -1)
	if len(matches) > MaxAtRefs {
		matches = matches[:MaxAtRefs]
	}
	var refs []string
	for _, match := range matches {
		group := func(i int) (string, bool) {
			start, end := match[2*i], match[2*i+1]
			if start < 0 {
				return "", false
			}
			return text[start:end], true
		}
		ref := ""
		unquoted := false
		for i := 1; i <= 5; i++ {
			if value, ok := group(i); ok {
				ref = value
				unquoted = i == 5
				break
			}
		}
		// Prose can glue CJK text onto an unquoted mention; trim back to the
		// longest existing prefix before each Han run.
		if unquoted && !pathExists(ref, cwd) {
			runs := hanRunRe.FindAllStringIndex(ref, -1)
			for i := len(runs) - 1; i >= 0; i-- {
				prefix := ref[:runs[i][0]]
				if prefix != "" && pathExists(prefix, cwd) {
					ref = prefix
					break
				}
			}
		}
		refs = append(refs, ref)
	}
	return refs
}

func pathExists(ref, cwd string) bool {
	p := ref
	if !filepath.IsAbs(p) {
		p = filepath.Join(cwd, p)
	}
	_, err := os.Stat(p)
	return err == nil
}

func parseRef(ref string) (path string, lineStart, lineEnd int, hasRange bool) {
	match := atRefLineRangeRe.FindStringSubmatch(ref)
	if match == nil {
		return ref, 0, 0, false
	}
	start, err := strconv.Atoi(match[2])
	if err != nil {
		return ref, 0, 0, false
	}
	end := start
	if match[3] != "" {
		if parsed, err := strconv.Atoi(match[3]); err == nil {
			end = parsed
		}
	}
	return match[1], start, end, true
}

func sliceLines(content string, lineStart, lineEnd int) string {
	all := strings.Split(content, "\n")
	from := lineStart
	if from < 1 {
		from = 1
	}
	to := len(all)
	if upper := lineEnd; upper < to {
		to = upper
	}
	if to < from {
		to = from
	}
	if from > len(all) {
		return ""
	}
	return strings.Join(all[from-1:to], "\n")
}

// ExpandAtRefs expands @path references in a user message by appending the
// referenced files' contents (resolved relative to cwd), or just the selected
// line range when the ref carries a #L suffix. Tokens that don't resolve to a
// small readable file are left untouched.
func ExpandAtRefs(text, cwd string) string {
	refs := collectAtRefs(text, cwd)
	if len(refs) == 0 {
		return text
	}
	var appendix strings.Builder
	totalBytes := 0
	appendBlock := func(block string) {
		if totalBytes+len(block) > MaxInlineTotalBytes {
			return
		}
		appendix.WriteString(block)
		totalBytes += len(block)
	}
	seen := map[string]bool{}
	for _, ref := range refs {
		if seen[ref] {
			continue
		}
		seen[ref] = true
		refPath, lineStart, lineEnd, hasRange := parseRef(ref)
		p := refPath
		if !filepath.IsAbs(p) {
			p = filepath.Join(cwd, p)
		}
		info, err := os.Stat(p)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if hasRange {
			if info.Size() > MaxRangeFileBytes {
				continue
			}
			data, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			snippet := sliceLines(string(data), lineStart, lineEnd)
			if utf8.RuneCountInString(snippet) <= MaxInlineBytes {
				appendBlock(fmt.Sprintf("\n\n<file path=%q lines=\"%d-%d\">\n%s\n</file>", refPath, lineStart, lineEnd, snippet))
			}
			continue
		}
		if info.Size() > MaxInlineBytes {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		appendBlock(fmt.Sprintf("\n\n<file path=%q>\n%s\n</file>", ref, string(data)))
	}
	if appendix.Len() == 0 {
		return text
	}
	return text + appendix.String()
}
