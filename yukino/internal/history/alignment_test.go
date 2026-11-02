package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/yukino-js/yukino/yukino/internal/storage"
)

func TestStrictActivityShapesAndIntegerNumbers(t *testing.T) {
	valid := testInput(t, 1, nil)
	for _, field := range []string{"timestamp", "cwd", "sessionId", "commandName", "source"} {
		var record map[string]json.RawMessage
		if err := json.Unmarshal(valid, &record); err != nil {
			t.Fatal(err)
		}
		delete(record, field)
		raw, _ := json.Marshal(record)
		if _, err := applyRecord(emptyState(), raw); err == nil {
			t.Fatalf("accepted missing %s", field)
		}
	}
	for _, suffix := range []string{`,"extra":true}`, `,"extra":null}`} {
		raw := append(append([]byte(nil), valid[:len(valid)-1]...), suffix...)
		if _, err := applyRecord(emptyState(), raw); err == nil {
			t.Fatal("accepted unknown input property")
		}
	}
	for _, value := range []string{"null", "1.5", "-1", "9007199254740992", `"1"`} {
		raw := strings.Replace(string(valid), `"timestamp":1`, `"timestamp":`+value, 1)
		if _, err := applyRecord(emptyState(), []byte(raw)); err == nil {
			t.Fatalf("accepted invalid timestamp: %s", value)
		}
	}
	for _, value := range []string{"1.0", "1e0"} {
		raw := strings.Replace(string(valid), `"timestamp":1`, `"timestamp":`+value, 1)
		state, err := applyRecord(emptyState(), []byte(raw))
		if err != nil || len(state.Inputs) != 1 || state.Inputs[0].Timestamp != 1 {
			t.Fatalf("integer JSON number %s: %+v %v", value, state, err)
		}
	}
}

func TestCheckpointValidationIsAtomic(t *testing.T) {
	state, err := applyRecord(emptyState(), testInput(t, 1, nil))
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`{"type":"checkpoint","projects":{},"inputs":[{}],"commandUsage":{}}`,
		`{"type":"checkpoint","projects":{},"inputs":[],"commandUsage":{"help":{"usageCount":-1,"lastUsedAt":0}}}`,
		`{"type":"checkpoint","projects":{},"inputs":[],"commandUsage":{"help":{"usageCount":1}}}`,
		`{"type":"checkpoint","projects":null,"inputs":[],"commandUsage":{}}`,
	} {
		next, err := applyRecord(state, []byte(raw))
		if err == nil || len(next.Inputs) != 1 {
			t.Fatalf("invalid checkpoint changed state: %+v %v", next, err)
		}
	}
	if _, err := applyRecord(state, []byte(`{"type":"checkpoint","projects":{},"inputs":[],"commandUsage":{},"ignored":null}`)); err != nil {
		t.Fatalf("checkpoint unknown field was not stripped: %v", err)
	}
}

func TestHistoryQueryReportsIOError(t *testing.T) {
	setupHome(t)
	path := storage.YukinoPath(ActivityFileName)
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadInputs(); err == nil {
		t.Fatal("read failure became empty history")
	}
	if _, err := RecordInput(InputOptions{Cwd: canonicalCwd(t), Source: "cli", Text: "input"}); err == nil {
		t.Fatal("write failure was hidden")
	}
	cacheMu.Lock()
	_, cached := cachedActivity[path]
	cacheMu.Unlock()
	if cached {
		t.Fatal("failed update cached invalid state")
	}
}

func TestHistoryCopiesInputValues(t *testing.T) {
	setupHome(t)
	sessionID, commandName := "session", "help"
	if _, err := RecordInput(InputOptions{Cwd: canonicalCwd(t), Source: "cli", Text: "input", SessionID: &sessionID, CommandName: &commandName}); err != nil {
		t.Fatal(err)
	}
	inputs := mustHistory(LoadInputs())
	*inputs[0].SessionID, *inputs[0].CommandName = "changed", "changed"
	unchanged := mustHistory(LoadInputs())
	if *unchanged[0].SessionID != "session" || *unchanged[0].CommandName != "help" {
		t.Fatal("query exposed cached pointers")
	}
}

func TestCommandUsageOrderSurvivesCompaction(t *testing.T) {
	setupHome(t)
	path := storage.YukinoPath(ActivityFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	raw := `{"type":"checkpoint","projects":{},"inputs":[],"commandUsage":{"z":{"usageCount":1,"lastUsedAt":0},"2":{"usageCount":1,"lastUsedAt":0},"a":{"usageCount":1,"lastUsedAt":0},"1":{"usageCount":1,"lastUsedAt":0}}}` + "\n"
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	want := []string{"1", "2", "z", "a"}
	if got := mustHistory(GetCommandUsage()).Order; !reflect.DeepEqual(got, want) {
		t.Fatalf("order: %v", got)
	}
	if err := CompactActivity(); err != nil {
		t.Fatal(err)
	}
	cacheMu.Lock()
	delete(cachedActivity, path)
	cacheMu.Unlock()
	if got := mustHistory(GetCommandUsage()).Order; !reflect.DeepEqual(got, want) {
		t.Fatalf("compacted order: %v", got)
	}
}
