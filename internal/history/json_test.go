package history

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestHistoryLoadsExistingFormatsAndSavesWithoutVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	store, err := NewJSON[string](path)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Save([]string{"saved"}); err != nil {
		t.Fatal(err)
	}
	records, err := store.Load()
	if err != nil || len(records) != 1 || records[0] != "saved" {
		t.Fatalf("records=%v err=%v", records, err)
	}
	saved, _ := os.ReadFile(path)
	if bytes.Contains(saved, []byte("version")) {
		t.Fatal("saved history must not have a version")
	}
	for _, existing := range []string{`["old"]`, `{"version":1,"tasks":["old"]}`, `{"tasks":["old"]}`} {
		if err = os.WriteFile(path, []byte(existing), 0600); err != nil {
			t.Fatal(err)
		}
		loaded, loadErr := store.Load()
		if loadErr != nil || len(loaded) != 1 || loaded[0] != "old" {
			t.Fatalf("existing history %s not loaded: records=%v err=%v", existing, loaded, loadErr)
		}
		after, _ := os.ReadFile(path)
		if !bytes.Equal(after, []byte(existing)) {
			t.Fatal("loading history modified the file")
		}
	}
}
