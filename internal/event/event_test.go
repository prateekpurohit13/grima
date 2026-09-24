package event

import "testing"

func TestExtension(t *testing.T) {
	cases := map[string]string{
		"/data/report.docx":    ".docx",
		"/data/REPORT.DOCX":    ".docx",
		`C:\data\report.docx`:  ".docx",
		"/data/archive.tar.gz": ".gz",
		"/data/noext":          "",
		"/data/.bashrc":        "",
		"/data/trailing.":      "",
		"":                     "",
	}
	for path, want := range cases {
		if got := Extension(path); got != want {
			t.Fatalf("Extension(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestParentDir(t *testing.T) {
	cases := map[string]string{
		"/data/sub/report.docx": "/data/sub",
		`C:\data\report.docx`:   `C:\data`,
		"/report.docx":          "",
		"report.docx":           "",
	}
	for path, want := range cases {
		if got := ParentDir(path); got != want {
			t.Fatalf("ParentDir(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestKindStringIsStable(t *testing.T) {
	cases := map[Kind]string{
		KindProcessStart:       "process_start",
		KindProcessExit:        "process_exit",
		KindFileCreate:         "file_create",
		KindFileWrite:          "file_write",
		KindFileRename:         "file_rename",
		KindFileDelete:         "file_delete",
		KindPersistenceInstall: "persistence_install",
		KindDecoyTouch:         "decoy_touch",
		KindUnknown:            "unknown",
	}
	for kind, want := range cases {
		if got := kind.String(); got != want {
			t.Fatalf("Kind(%d).String() = %q, want %q", kind, got, want)
		}
	}

	if got := Kind(200).String(); got != "kind(200)" {
		t.Fatalf("out-of-range kind = %q", got)
	}
}

func TestIsFile(t *testing.T) {
	for _, kind := range []Kind{KindFileCreate, KindFileWrite, KindFileRename, KindFileDelete} {
		if !kind.IsFile() {
			t.Fatalf("%v should be a file kind", kind)
		}
	}
	for _, kind := range []Kind{KindProcessStart, KindProcessExit, KindPersistenceInstall, KindDecoyTouch, KindUnknown} {
		if kind.IsFile() {
			t.Fatalf("%v should not be a file kind", kind)
		}
	}
}

func TestEventExtensionUsesPath(t *testing.T) {
	ev := Event{Path: "/data/report.DOCX"}
	if got := ev.Extension(); got != ".docx" {
		t.Fatalf("Event.Extension() = %q, want .docx", got)
	}
}
