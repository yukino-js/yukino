package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSearchPathGlobsUnicodeAndBinary(t *testing.T) {
	dir := t.TempDir()
	ctx := WithExecutionContext(context.Background(), ExecutionContext{WorkDir: dir})
	files := map[string]string{
		"main.js": "console.log('entry');\n", "src/js/promise.js": "class PromiseV2 {}\nconst PENDING = 'pending';\n", "src/js/curry.js": "function curry(fn) {}\n", "src/md/notes.md": "function notes() {}\n", "node_modules/pkg/index.js": "function hidden() {}\n", ".github/task.yaml": "hidden NEEDLE\n", "unicode.txt": "日本語注釈\nemoji 😁 line\n全角数字１２３\nmixed 変数名abc end\nplain ascii only\n", "legacy.txt": "match foo{ here\n", "bin.dat": "BINARY_NEEDLE\x00binary junk", "links/target.txt": "NEEDLE_LINK in real file\n",
	}
	for name, text := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(dir, "links/target.txt"), filepath.Join(dir, "links/alias.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dir, filepath.Join(dir, "links/loop")); err != nil {
		t.Fatal(err)
	}
	grep := &GrepTool{}
	for _, tc := range []struct{ pattern, include, want string }{
		{"CLASS PROMISEV2", "src/js/*.js", "src/js/promise.js:1:class PromiseV2 {}"},
		{"const", "src/js/promise.js", "src/js/promise.js:2:const PENDING"},
		{`^\p{Script=Han}+$`, "*.txt", "unicode.txt:1:日本語注釈"},
		{"[😀-😜]", "*.txt", "unicode.txt:2:emoji 😁 line"},
		{`^mixed \w+ end$`, "*.txt", "unicode.txt:4:mixed 変数名abc end"},
		{`\b変数名`, "*.txt", "unicode.txt:4:"},
		{`数字\d{3}`, "*.txt", "unicode.txt:3:"},
		{`[\x{4e00}-\x{9fff}]`, "*.txt", "unicode.txt:1:"},
		{`\x{110000}`, "*.txt", "No matches found."},
		{"foo{", "*.txt", "legacy.txt:1:match foo{ here"},
		{"BINARY_NEEDLE", "", "No matches found."},
		{"hidden NEEDLE", "*.yaml", ".github/task.yaml:1:"},
	} {
		t.Run(tc.pattern, func(t *testing.T) {
			result := grep.Execute(ctx, map[string]any{"pattern": tc.pattern, "include": tc.include})
			if result.IsError || !strings.Contains(result.Output, tc.want) {
				t.Fatal(result)
			}
		})
	}
	result := grep.Execute(ctx, map[string]any{"pattern": "NEEDLE_LINK"})
	if result.IsError || !strings.Contains(result.Output, "links/target.txt:1:") || !strings.Contains(result.Output, "links/alias.txt:1:") || strings.Contains(result.Output, "loop/") {
		t.Fatal(result)
	}
	result = grep.Execute(ctx, map[string]any{"pattern": "PENDING", "path": "src/js/promise.js", "include": "*.md"})
	if result.IsError || !strings.Contains(result.Output, "src/js/promise.js:2:") {
		t.Fatal(result)
	}
	result = grep.Execute(ctx, map[string]any{"pattern": "(unclosed"})
	if !result.IsError {
		t.Fatal("invalid regex accepted")
	}
	glob := &GlobTool{}
	for _, pattern := range []string{"src/js/*.js", "**/*.js", "*.{js,md}"} {
		result := glob.Execute(ctx, map[string]any{"pattern": pattern})
		if result.IsError || !strings.Contains(result.Output, "src/js/curry.js") || strings.Contains(result.Output, "node_modules") {
			t.Fatal(result)
		}
	}
	limited := &GrepTool{TraversalLimits: &GrepTraversalLimits{MaxEntries: 1, MaxDepth: 25}}
	result = limited.Execute(ctx, map[string]any{"pattern": "never"})
	if result.IsError || !strings.Contains(result.Output, "visiting 1 entries") {
		t.Fatal(result)
	}
}

func TestSearchBoundsAndCatastrophicRegex(t *testing.T) {
	dir := t.TempDir()
	ctx := WithExecutionContext(context.Background(), ExecutionContext{WorkDir: dir})
	path := filepath.Join(dir, "many.txt")
	if err := os.WriteFile(path, []byte(strings.Repeat("a\n", 501)+strings.Repeat("a", 32)+"!\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := (&GrepTool{}).Execute(ctx, map[string]any{"pattern": "(a+)+$"})
	if result.IsError || !strings.Contains(result.Output, "truncated at 500 matches") {
		t.Fatal(result)
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("a", 32)+"!\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	result = (&GrepTool{}).Execute(ctx, map[string]any{"pattern": "(a+)+$"})
	if !result.IsError || !strings.Contains(result.Output, "timed out") || time.Since(start) > 8*time.Second {
		t.Fatal(result)
	}
}
