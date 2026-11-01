package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func fileFixture(t *testing.T, content string) (context.Context, string, *ReadFileTool, *EditFileTool, *WriteFileTool) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte(content), 0o751); err != nil {
		t.Fatal(err)
	}
	ctx := WithExecutionContext(context.Background(), ExecutionContext{WorkDir: dir})
	cache := NewFileStateCache()
	read := &ReadFileTool{cache}
	if result := read.Execute(ctx, map[string]any{"file_path": "file.txt"}); result.IsError {
		t.Fatal(result.Output)
	}
	return ctx, path, read, &EditFileTool{FileStateCache: cache}, &WriteFileTool{FileStateCache: cache}
}
func editArgs(path string, edits ...map[string]any) map[string]any {
	raw := make([]any, len(edits))
	for i, edit := range edits {
		raw[i] = edit
	}
	return map[string]any{"file_path": path, "edits": raw}
}
func fileText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
func TestBatchEditOriginalMatchesAndAtomicFailure(t *testing.T) {
	for _, invalid := range []map[string]any{
		{"old_string": "missing", "new_string": "x"}, {"old_string": "beta"}, {"old_string": "alpha\nbeta", "new_string": "overlap"}, {"old_string": "alpha", "new_string": "duplicate"},
	} {
		ctx, path, _, edit, _ := fileFixture(t, "alpha\nbeta")
		result := edit.Execute(ctx, editArgs(path, map[string]any{"old_string": "alpha", "new_string": "new"}, invalid))
		if !result.IsError || fileText(t, path) != "alpha\nbeta" {
			t.Fatalf("invalid edit changed file: %+v", result)
		}
	}
	ctx, path, _, edit, _ := fileFixture(t, "alpha\nbeta\ngamma")
	result := edit.Execute(ctx, editArgs("file.txt", map[string]any{"old_string": "beta", "new_string": "alpha"}, map[string]any{"old_string": "alpha", "new_string": "beta"}))
	if result.IsError || fileText(t, path) != "beta\nalpha\ngamma" || !strings.Contains(result.Output, "2 replacements") {
		t.Fatal(result)
	}
	if result := edit.Execute(ctx, map[string]any{"file_path": path, "old_string": "beta", "new_string": "new"}); !result.IsError {
		t.Fatal("obsolete edit shape accepted")
	}
}
func TestEditLineEndingFidelity(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n"} {
		for _, snippetEnding := range []string{"\n", "\r\n"} {
			original := "\uFEFFheader" + ending + "first" + ending + "second" + ending + "unchanged  " + ending
			ctx, path, _, edit, _ := fileFixture(t, original)
			result := edit.Execute(ctx, editArgs(path, map[string]any{"old_string": "fi\rrst" + snippetEnding + "sec\rond", "new_string": "O\rNE" + snippetEnding + "T\rWO"}))
			want := "\uFEFFheader" + ending + "ONE" + ending + "TWO" + ending + "unchanged  " + ending
			if result.IsError || fileText(t, path) != want {
				t.Fatalf("%q/%q: %+v\n%q", ending, snippetEnding, result, fileText(t, path))
			}
		}
	}
	ctx, path, _, edit, _ := fileFixture(t, "first\r\nsecond\r\n")
	result := edit.Execute(ctx, editArgs(path, map[string]any{"old_string": "\nsec\rond", "new_string": "\nTH\rIRD"}))
	if result.IsError || fileText(t, path) != "first\r\nTHIRD\r\n" {
		t.Fatal(result)
	}
	for _, old := range []string{"first\n\nTHIRD", "first\nTHIRD"} {
		result := edit.Execute(ctx, editArgs(path, map[string]any{"old_string": old, "new_string": "first\nTHIRD"}))
		if !result.IsError {
			t.Fatal("ineffective or invalid edit succeeded")
		}
	}
}
func TestFileReadLimitsAndValidation(t *testing.T) {
	ctx, path, read, _, write := fileFixture(t, strings.Repeat("あああ\n", 10000))
	result := read.Execute(ctx, map[string]any{"file_path": path, "limit": 10000})
	body := strings.Split(result.Output, "\n[")[0]
	if result.IsError || len(body) > MaxReadBytes || !strings.Contains(result.Output, "Use offset=") {
		t.Fatal("unbounded read", len(result.Output))
	}
	for _, args := range []map[string]any{{"file_path": path, "offset": -1}, {"file_path": path, "limit": 0}, {"file_path": path, "offset": 10002}} {
		if result := read.Execute(ctx, args); !result.IsError {
			t.Fatalf("invalid read: %+v", result)
		}
	}
	if result := write.Execute(ctx, map[string]any{"file_path": path}); !result.IsError {
		t.Fatal("missing content accepted")
	}
	if result := write.Execute(ctx, map[string]any{"file_path": path, "content": ""}); result.IsError {
		t.Fatal(result)
	}
	if fileText(t, path) != "" {
		t.Fatal("empty write failed")
	}
}
func TestFileStateDeletedBackwardsAndAliases(t *testing.T) {
	ctx, path, _, edit, write := fileFixture(t, "original")
	alias := filepath.Join(filepath.Dir(path), "alias.txt")
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	if result := edit.Execute(ctx, editArgs(alias, map[string]any{"old_string": "original", "new_string": "updated"})); result.IsError {
		t.Fatal(result)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o751 {
		t.Fatal("mode lost")
	}
	link, _ := os.Lstat(alias)
	if link.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink replaced")
	}
	earlier := info.ModTime().Add(-10 * time.Second)
	if err := os.Chtimes(path, earlier, earlier); err != nil {
		t.Fatal(err)
	}
	if ok, _ := edit.FileStateCache.Check(path); ok {
		t.Fatal("backward timestamp accepted")
	}
	edit.FileStateCache.Update(path)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if result := write.Execute(ctx, map[string]any{"file_path": path, "content": "recreated"}); !result.IsError || !strings.Contains(result.Output, "deleted") {
		t.Fatal(result)
	}
}
func TestConcurrentEditsAndCancellation(t *testing.T) {
	ctx, path, _, edit, _ := fileFixture(t, "first\nsecond")
	var wg sync.WaitGroup
	for _, pair := range [][2]string{{"first", "FIRST"}, {"second", "SECOND"}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if result := edit.Execute(ctx, editArgs(path, map[string]any{"old_string": pair[0], "new_string": pair[1]})); result.IsError {
				t.Error(result)
			}
		}()
	}
	wg.Wait()
	if fileText(t, path) != "FIRST\nSECOND" {
		t.Fatal("lost concurrent edit")
	}
	release, err := AcquireWorkspaceMutation(ctx, filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	result := edit.Execute(cancelled, editArgs(path, map[string]any{"old_string": "FIRST", "new_string": "changed"}))
	release()
	if !result.IsError || fileText(t, path) != "FIRST\nSECOND" {
		t.Fatal(result)
	}
}
func TestDistantEditDiffAndDiagnostics(t *testing.T) {
	lines := make([]string, 1000)
	for i := range lines {
		lines[i] = fmt.Sprintf("line-%d", i)
	}
	ctx, path, _, edit, _ := fileFixture(t, strings.Join(lines, "\n"))
	result := edit.Execute(ctx, editArgs(path, map[string]any{"old_string": "line-10\n", "new_string": "new-10\ninserted\n"}, map[string]any{"old_string": "line-900\n", "new_string": "new-900\n"}))
	for _, want := range []string{"3 additions and 2 removals", "+  902  new-900"} {
		if result.IsError || !strings.Contains(result.Output, want) {
			t.Fatal(result)
		}
	}
	if strings.Contains(result.Output, "line-500") || strings.Contains(result.Output, "truncated") {
		t.Fatal(result)
	}
	diagnostic := describeEditMismatch("header\r\na\u00A0🐱\r\n", "a 🐱")
	for _, want := range []string{"U+0020", "U+00A0", "line 2, column 2", "No edits were written."} {
		if !strings.Contains(diagnostic, want) {
			t.Fatalf("missing %s: %s", want, diagnostic)
		}
	}
	diagnostic = describeEditMismatch("a🐱", "a🐶")
	if !strings.Contains(diagnostic, "U+1F436") || !strings.Contains(diagnostic, "U+1F431") {
		t.Fatal(diagnostic)
	}
}
