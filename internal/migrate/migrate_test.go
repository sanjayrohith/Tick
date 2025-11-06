package migrate

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestLoadEmbeddedMigrations(t *testing.T) {
	migrations, err := Load()
	if err != nil {
		t.Fatalf("Load() = %v, want success", err)
	}
	if len(migrations) == 0 {
		t.Fatal("no migrations were embedded; the //go:embed directive is not matching")
	}

	// Versions must be strictly increasing so application order is total.
	for i := 1; i < len(migrations); i++ {
		if migrations[i].Version <= migrations[i-1].Version {
			t.Errorf("migrations are not strictly ordered: %s then %s",
				migrations[i-1], migrations[i])
		}
	}
	for _, m := range migrations {
		if strings.TrimSpace(m.SQL) == "" {
			t.Errorf("migration %s has empty SQL", m)
		}
	}
}

func TestLoadFromRejectsBadFilenames(t *testing.T) {
	for _, tc := range []struct {
		name     string
		filename string
	}{
		{"no version prefix", "sql/tasks.sql"},
		{"version too short", "sql/001_tasks.sql"},
		{"uppercase in name", "sql/0001_Tasks.sql"},
		{"hyphen in name", "sql/0001_add-index.sql"},
		{"missing description", "sql/0001_.sql"},
	} {
		fsys := fstest.MapFS{
			tc.filename: {Data: []byte("SELECT 1;")},
		}
		if _, err := loadFrom(fsys, "sql"); err == nil {
			t.Errorf("%s: loadFrom accepted %q, want a rejection", tc.name, tc.filename)
		}
	}
}

func TestLoadFromRejectsDuplicateVersions(t *testing.T) {
	fsys := fstest.MapFS{
		"sql/0001_tasks.sql":     {Data: []byte("SELECT 1;")},
		"sql/0001_schedules.sql": {Data: []byte("SELECT 2;")},
	}
	_, err := loadFrom(fsys, "sql")
	if err == nil {
		t.Fatal("two migrations sharing a version must be rejected")
	}
	if !strings.Contains(err.Error(), "share version") {
		t.Errorf("error should name the collision, got: %v", err)
	}
}

func TestLoadFromRejectsEmptyMigration(t *testing.T) {
	fsys := fstest.MapFS{
		"sql/0001_tasks.sql": {Data: []byte("   \n\t\n")},
	}
	if _, err := loadFrom(fsys, "sql"); err == nil {
		t.Fatal("an empty migration is almost certainly a mistake and must be rejected")
	}
}

func TestLoadFromSortsByVersionNotFilename(t *testing.T) {
	fsys := fstest.MapFS{
		"sql/0010_tenth.sql":  {Data: []byte("SELECT 10;")},
		"sql/0002_second.sql": {Data: []byte("SELECT 2;")},
		"sql/0001_first.sql":  {Data: []byte("SELECT 1;")},
	}
	migrations, err := loadFrom(fsys, "sql")
	if err != nil {
		t.Fatalf("loadFrom() = %v, want success", err)
	}

	want := []int{1, 2, 10}
	if len(migrations) != len(want) {
		t.Fatalf("loaded %d migrations, want %d", len(migrations), len(want))
	}
	for i, v := range want {
		if migrations[i].Version != v {
			t.Errorf("position %d has version %d, want %d", i, migrations[i].Version, v)
		}
	}
}

func TestLoadFromIgnoresNonSQLFiles(t *testing.T) {
	fsys := fstest.MapFS{
		"sql/0001_tasks.sql": {Data: []byte("SELECT 1;")},
		"sql/README.md":      {Data: []byte("# notes")},
		"sql/.gitkeep":       {Data: []byte("")},
	}
	migrations, err := loadFrom(fsys, "sql")
	if err != nil {
		t.Fatalf("loadFrom() = %v, want success", err)
	}
	if len(migrations) != 1 {
		t.Errorf("loaded %d migrations, want 1 (non-SQL files must be skipped)", len(migrations))
	}
}

func TestMigrationString(t *testing.T) {
	m := Migration{Version: 7, Name: "add_traceparent"}
	if got, want := m.String(), "0007_add_traceparent"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}
