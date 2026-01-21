// Package fileref handles parsing and processing of file references in command output.
// Commands can include [file:/path/to/file] patterns in their output, which will be
// extracted and sent as Telegram media groups.
package fileref

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// FileType represents Telegram media type for file uploads.
type FileType int

const (
	FileTypeDocument FileType = iota
	FileTypePhoto
	FileTypeVideo
	FileTypeAudio
)

// FileRef represents a parsed file reference from command output.
type FileRef struct {
	Path string
	Type FileType
}

// ParseResult contains the parsed command output.
type ParseResult struct {
	Text   string    // Cleaned text without file references
	Files  []FileRef // Extracted file references (validated to exist)
	Errors []string  // Error messages for missing files
}

// fileRefPattern matches [file:/path/to/file] patterns.
var fileRefPattern = regexp.MustCompile(`\[file:([^\]]+)\]`)

// photoExtensions maps extensions to photo type.
var photoExtensions = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true,
}

// videoExtensions maps extensions to video type.
var videoExtensions = map[string]bool{
	".mp4": true, ".mov": true, ".avi": true, ".mkv": true, ".webm": true,
}

// audioExtensions maps extensions to audio type.
var audioExtensions = map[string]bool{
	".mp3": true, ".ogg": true, ".wav": true, ".m4a": true, ".flac": true,
}

// ParseOutput extracts file references from command output.
// Returns cleaned text, valid files, and error messages for missing files.
// If workdir is provided, relative paths are resolved against it.
func ParseOutput(output string, workdir string) ParseResult {
	var result ParseResult
	var files []FileRef
	var errors []string

	// Find all matches
	matches := fileRefPattern.FindAllStringSubmatchIndex(output, -1)
	if len(matches) == 0 {
		result.Text = output
		return result
	}

	// Build cleaned text and collect file references
	var cleaned strings.Builder
	lastEnd := 0

	for _, match := range matches {
		// match[0]:match[1] is the full match [file:path]
		// match[2]:match[3] is the captured group (path)
		fullStart, fullEnd := match[0], match[1]
		pathStart, pathEnd := match[2], match[3]

		// Append text before this match
		cleaned.WriteString(output[lastEnd:fullStart])
		lastEnd = fullEnd

		// Extract and validate path
		path := strings.TrimSpace(output[pathStart:pathEnd])
		if path == "" {
			continue
		}

		// Resolve relative paths against workdir
		fullPath := path
		if workdir != "" && !filepath.IsAbs(path) {
			fullPath = filepath.Join(workdir, path)
		}

		// Check if file exists
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			errors = append(errors, "File not found: "+fullPath)
			continue
		}

		files = append(files, FileRef{
			Path: fullPath,
			Type: DetectType(fullPath),
		})
	}

	// Append remaining text
	cleaned.WriteString(output[lastEnd:])

	// Clean up extra whitespace from removed references
	result.Text = cleanWhitespace(cleaned.String())
	result.Files = files
	result.Errors = errors

	return result
}

// DetectType determines Telegram media type from file extension.
func DetectType(path string) FileType {
	ext := strings.ToLower(filepath.Ext(path))

	if photoExtensions[ext] {
		return FileTypePhoto
	}
	if videoExtensions[ext] {
		return FileTypeVideo
	}
	if audioExtensions[ext] {
		return FileTypeAudio
	}
	return FileTypeDocument
}

// GroupFiles splits files into groups respecting the max limit.
func GroupFiles(files []FileRef, maxPerGroup int) [][]FileRef {
	if len(files) == 0 {
		return nil
	}
	if maxPerGroup <= 0 {
		maxPerGroup = 10
	}

	var groups [][]FileRef
	for i := 0; i < len(files); i += maxPerGroup {
		end := min(i+maxPerGroup, len(files))
		// Create a new slice to avoid sharing underlying array
		group := make([]FileRef, end-i)
		copy(group, files[i:end])
		groups = append(groups, group)
	}
	return groups
}

// HasFiles returns true if output contains file references.
func HasFiles(output string) bool {
	return fileRefPattern.MatchString(output)
}

// cleanWhitespace removes excessive whitespace left by removed file references.
func cleanWhitespace(s string) string {
	// Replace multiple consecutive newlines with double newline
	multiNewline := regexp.MustCompile(`\n{3,}`)
	s = multiNewline.ReplaceAllString(s, "\n\n")

	// Trim leading/trailing whitespace from each line
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	s = strings.Join(lines, "\n")

	return strings.TrimSpace(s)
}