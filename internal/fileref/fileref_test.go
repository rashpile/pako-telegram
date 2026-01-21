package fileref

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseOutput(t *testing.T) {
	// Create temp files for testing
	tmpDir := t.TempDir()
	existingFile := filepath.Join(tmpDir, "test.pdf")
	existingPhoto := filepath.Join(tmpDir, "image.jpg")
	if err := os.WriteFile(existingFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existingPhoto, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		input      string
		wantText   string
		wantFiles  int
		wantErrors int
	}{
		{
			name:       "no file references",
			input:      "Hello world",
			wantText:   "Hello world",
			wantFiles:  0,
			wantErrors: 0,
		},
		{
			name:       "single existing file",
			input:      "Found file: [file:" + existingFile + "]",
			wantText:   "Found file:",
			wantFiles:  1,
			wantErrors: 0,
		},
		{
			name:       "multiple existing files",
			input:      "Files:\n[file:" + existingFile + "]\n[file:" + existingPhoto + "]",
			wantText:   "Files:",
			wantFiles:  2,
			wantErrors: 0,
		},
		{
			name:       "missing file",
			input:      "File: [file:/nonexistent/path.pdf]",
			wantText:   "File:",
			wantFiles:  0,
			wantErrors: 1,
		},
		{
			name:       "mixed existing and missing",
			input:      "Results:\n[file:" + existingFile + "]\n[file:/missing.pdf]",
			wantText:   "Results:",
			wantFiles:  1,
			wantErrors: 1,
		},
		{
			name:       "empty file reference ignored",
			input:      "Empty: [file:]",
			wantText:   "Empty: [file:]", // Empty refs are not matched
			wantFiles:  0,
			wantErrors: 0,
		},
		{
			name:       "text before and after",
			input:      "Before [file:" + existingFile + "] After",
			wantText:   "Before  After",
			wantFiles:  1,
			wantErrors: 0,
		},
		{
			name:       "only file references",
			input:      "[file:" + existingFile + "]\n[file:" + existingPhoto + "]",
			wantText:   "",
			wantFiles:  2,
			wantErrors: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseOutput(tt.input, "")

			if result.Text != tt.wantText {
				t.Errorf("Text = %q, want %q", result.Text, tt.wantText)
			}
			if len(result.Files) != tt.wantFiles {
				t.Errorf("Files count = %d, want %d", len(result.Files), tt.wantFiles)
			}
			if len(result.Errors) != tt.wantErrors {
				t.Errorf("Errors count = %d, want %d", len(result.Errors), tt.wantErrors)
			}
		})
	}
}

func TestParseOutputWithWorkdir(t *testing.T) {
	// Create temp directory with files
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "report.pdf")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		input      string
		workdir    string
		wantFiles  int
		wantErrors int
	}{
		{
			name:       "relative path with workdir",
			input:      "[file:report.pdf]",
			workdir:    tmpDir,
			wantFiles:  1,
			wantErrors: 0,
		},
		{
			name:       "relative path without workdir fails",
			input:      "[file:report.pdf]",
			workdir:    "",
			wantFiles:  0,
			wantErrors: 1,
		},
		{
			name:       "absolute path ignores workdir",
			input:      "[file:" + testFile + "]",
			workdir:    "/some/other/dir",
			wantFiles:  1,
			wantErrors: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseOutput(tt.input, tt.workdir)
			if len(result.Files) != tt.wantFiles {
				t.Errorf("Files count = %d, want %d", len(result.Files), tt.wantFiles)
			}
			if len(result.Errors) != tt.wantErrors {
				t.Errorf("Errors count = %d, want %d", len(result.Errors), tt.wantErrors)
			}
		})
	}
}

func TestDetectType(t *testing.T) {
	tests := []struct {
		path string
		want FileType
	}{
		// Photos
		{"/path/image.jpg", FileTypePhoto},
		{"/path/image.JPEG", FileTypePhoto},
		{"/path/image.png", FileTypePhoto},
		{"/path/image.gif", FileTypePhoto},
		{"/path/image.webp", FileTypePhoto},

		// Videos
		{"/path/video.mp4", FileTypeVideo},
		{"/path/video.MOV", FileTypeVideo},
		{"/path/video.avi", FileTypeVideo},
		{"/path/video.mkv", FileTypeVideo},
		{"/path/video.webm", FileTypeVideo},

		// Audio
		{"/path/audio.mp3", FileTypeAudio},
		{"/path/audio.OGG", FileTypeAudio},
		{"/path/audio.wav", FileTypeAudio},
		{"/path/audio.m4a", FileTypeAudio},
		{"/path/audio.flac", FileTypeAudio},

		// Documents (default)
		{"/path/doc.pdf", FileTypeDocument},
		{"/path/doc.txt", FileTypeDocument},
		{"/path/doc.docx", FileTypeDocument},
		{"/path/unknown", FileTypeDocument},
		{"/path/no.extension.here", FileTypeDocument},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := DetectType(tt.path)
			if got != tt.want {
				t.Errorf("DetectType(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestGroupFiles(t *testing.T) {
	makeFiles := func(n int) []FileRef {
		files := make([]FileRef, n)
		for i := range files {
			files[i] = FileRef{Path: "/test", Type: FileTypeDocument}
		}
		return files
	}

	tests := []struct {
		name        string
		files       []FileRef
		maxPerGroup int
		wantGroups  int
		wantSizes   []int
	}{
		{
			name:        "empty input",
			files:       nil,
			maxPerGroup: 10,
			wantGroups:  0,
			wantSizes:   nil,
		},
		{
			name:        "under limit",
			files:       makeFiles(5),
			maxPerGroup: 10,
			wantGroups:  1,
			wantSizes:   []int{5},
		},
		{
			name:        "exact limit",
			files:       makeFiles(10),
			maxPerGroup: 10,
			wantGroups:  1,
			wantSizes:   []int{10},
		},
		{
			name:        "over limit",
			files:       makeFiles(15),
			maxPerGroup: 10,
			wantGroups:  2,
			wantSizes:   []int{10, 5},
		},
		{
			name:        "multiple full groups",
			files:       makeFiles(30),
			maxPerGroup: 10,
			wantGroups:  3,
			wantSizes:   []int{10, 10, 10},
		},
		{
			name:        "custom limit",
			files:       makeFiles(7),
			maxPerGroup: 3,
			wantGroups:  3,
			wantSizes:   []int{3, 3, 1},
		},
		{
			name:        "zero max uses default",
			files:       makeFiles(5),
			maxPerGroup: 0,
			wantGroups:  1,
			wantSizes:   []int{5},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			groups := GroupFiles(tt.files, tt.maxPerGroup)

			if len(groups) != tt.wantGroups {
				t.Errorf("GroupFiles() returned %d groups, want %d", len(groups), tt.wantGroups)
				return
			}

			for i, group := range groups {
				if len(group) != tt.wantSizes[i] {
					t.Errorf("Group %d has %d files, want %d", i, len(group), tt.wantSizes[i])
				}
			}
		})
	}
}

func TestHasFiles(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"no files here", false},
		{"[file:/path/to/file.pdf]", true},
		{"text [file:/path] more text", true},
		{"[file:]", false}, // Empty path doesn't match
		{"[FILE:/path]", false}, // Case sensitive
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := HasFiles(tt.input)
			if got != tt.want {
				t.Errorf("HasFiles(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestGroupFilesImmutability(t *testing.T) {
	// Ensure GroupFiles doesn't share underlying arrays
	original := []FileRef{
		{Path: "/a", Type: FileTypeDocument},
		{Path: "/b", Type: FileTypeDocument},
		{Path: "/c", Type: FileTypeDocument},
	}

	groups := GroupFiles(original, 2)

	// Modify the returned group
	groups[0][0].Path = "/modified"

	// Original should be unchanged
	if original[0].Path != "/a" {
		t.Error("GroupFiles modified the original slice")
	}
}