package filewatch

import (
	"math"
	"testing"

	"github.com/fsnotify/fsnotify"
)

func TestLooksWrong(t *testing.T) {
	cases := []struct {
		name string
		path string
		head []byte
		want bool
	}{
		{"png with png magic", "/data/a.png", []byte("\x89PNG\r\n\x1a\n...."), false},
		{"docx with zip magic", "/data/a.docx", []byte("PK\x03\x04rest"), false},
		{"pdf with pdf magic", "/data/a.pdf", []byte("%PDF-1.7"), false},
		{"exe with MZ magic", "/data/a.exe", []byte("MZ\x90\x00"), false},

		// The encryption case: content no longer matches the extension.
		{"docx with random bytes", "/data/a.docx", []byte("\x91\xab\xcd\xef\x01\x23"), true},
		{"png with random bytes", "/data/a.png", []byte("\x91\xab\xcd\xef\x01\x23"), true},
		{"pdf with random bytes", "/data/a.pdf", []byte("\x91\xab\xcd\xef\x01\x23"), true},

		// Text that stops being text.
		{"txt with control bytes", "/data/a.txt", []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07}, true},
		{"txt with plain text", "/data/a.txt", []byte("hello world\n"), false},
		{"json with plain text", "/data/a.json", []byte(`{"a": 1}`), false},

		// No claim to check against.
		{"unknown extension", "/data/a.locked", []byte("\x91\xab\xcd"), false},
		{"empty sample", "/data/a.docx", nil, false},
		{"no extension", "/data/README", []byte("\x91\xab"), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := looksWrong(tc.path, tc.head); got != tc.want {
				t.Fatalf("looksWrong(%q, %v) = %v, want %v", tc.path, tc.head, got, tc.want)
			}
		})
	}
}

func TestShannon(t *testing.T) {
	t.Run("empty input is zero", func(t *testing.T) {
		if got := shannon(nil); got != 0 {
			t.Fatalf("entropy = %v, want 0", got)
		}
	})

	t.Run("single repeated byte is zero", func(t *testing.T) {
		if got := shannon([]byte("aaaaaaaa")); got != 0 {
			t.Fatalf("entropy = %v, want 0", got)
		}
	})

	t.Run("uniform bytes approach eight bits", func(t *testing.T) {
		all := make([]byte, 256)
		for i := range all {
			all[i] = byte(i)
		}
		got := shannon(all)
		if math.Abs(got-8) > 0.001 {
			t.Fatalf("entropy = %v, want ~8", got)
		}
	})

	t.Run("samples are combined", func(t *testing.T) {
		head := []byte("aaaaaaaa")
		tail := []byte("bbbbbbbb")
		got := shannon(head, tail)
		if math.Abs(got-1) > 0.001 {
			t.Fatalf("entropy = %v, want ~1 for two equally likely bytes", got)
		}
	})
}

func TestKindFor(t *testing.T) {
	cases := []struct {
		name string
		op   uint32
		want string
		ok   bool
	}{
		{"create", 0x01, "file_create", true},
		{"write", 0x02, "file_write", true},
		{"remove", 0x04, "file_delete", true},
		{"rename", 0x08, "file_rename", true},
		{"chmod only", 0x10, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kind, ok := kindFor(fsnotify.Op(tc.op))
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if ok && kind.String() != tc.want {
				t.Fatalf("kind = %q, want %q", kind, tc.want)
			}
		})
	}
}
