package filewatch

import "github.com/prateekpurohit13/grima/internal/event"

// magicByExt is the leading byte sequence a file of each extension must have.
// A file whose content no longer matches its extension has been rewritten by
// something other than the application that owns that format.
var magicByExt = map[string][][]byte{
	".png":    {[]byte("\x89PNG\r\n\x1a\n")},
	".jpg":    {[]byte("\xff\xd8\xff")},
	".jpeg":   {[]byte("\xff\xd8\xff")},
	".gif":    {[]byte("GIF8")},
	".bmp":    {[]byte("BM")},
	".pdf":    {[]byte("%PDF")},
	".zip":    {[]byte("PK\x03\x04")},
	".docx":   {[]byte("PK\x03\x04")},
	".xlsx":   {[]byte("PK\x03\x04")},
	".pptx":   {[]byte("PK\x03\x04")},
	".jar":    {[]byte("PK\x03\x04")},
	".gz":     {[]byte("\x1f\x8b")},
	".bz2":    {[]byte("BZh")},
	".xz":     {[]byte("\xfd7zXZ")},
	".7z":     {[]byte("7z\xbc\xaf")},
	".rar":    {[]byte("Rar!")},
	".exe":    {[]byte("MZ")},
	".dll":    {[]byte("MZ")},
	".sys":    {[]byte("MZ")},
	".class":  {[]byte("\xca\xfe\xba\xbe")},
	".mp3":    {[]byte("ID3")},
	".mp4":    {[]byte("\x00\x00\x00")},
	".sqlite": {[]byte("SQLite format 3\x00")},
}

// textExts are formats that should stay mostly printable.
var textExts = map[string]bool{
	".txt": true, ".md": true, ".csv": true, ".tsv": true, ".json": true,
	".xml": true, ".yaml": true, ".yml": true, ".toml": true, ".ini": true,
	".conf": true, ".cfg": true, ".log": true, ".html": true, ".css": true,
	".js": true, ".ts": true, ".go": true, ".py": true, ".rb": true,
	".c": true, ".h": true, ".cpp": true, ".hpp": true, ".java": true,
	".rs": true, ".sh": true, ".sql": true, ".tex": true, ".rst": true,
}

// looksWrong reports whether a file's leading bytes contradict its extension.
func looksWrong(path string, head []byte) bool {
	if len(head) == 0 {
		return false
	}

	ext := event.Extension(path)

	if wanted, ok := magicByExt[ext]; ok {
		for _, want := range wanted {
			if len(head) >= len(want) && string(head[:len(want)]) == string(want) {
				return false
			}
		}
		return true
	}

	if textExts[ext] {
		// Encrypted bytes are ~12% control characters; plain text is near zero.
		return controlByteRatio(head) > 0.05
	}
	return false
}

// controlByteRatio is the share of bytes that plain text should not contain.
func controlByteRatio(b []byte) float64 {
	if len(b) == 0 {
		return 0
	}
	bad := 0
	for _, c := range b {
		if c == '\t' || c == '\n' || c == '\r' {
			continue
		}
		if c < 0x20 || c == 0x7f {
			bad++
		}
	}
	return float64(bad) / float64(len(b))
}
