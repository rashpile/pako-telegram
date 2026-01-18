package builtin

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	pkgcmd "github.com/rashpile/pako-telegram/pkg/command"
)

// PodcastConfig holds configuration for podcast generation.
type PodcastConfig struct {
	PodcastgenPath string // Path to podcastgen directory
	ConfigPath     string // Path to TTS config.yml
	TempDir        string // Temp directory for files
}

// PodcastCommand generates audio from text using podcastgen.
type PodcastCommand struct {
	cfg          PodcastConfig
	fileResponse *pkgcmd.FileResponse
}

// NewPodcastCommand creates a podcast command.
func NewPodcastCommand(cfg PodcastConfig) *PodcastCommand {
	// Ensure temp dir exists
	if cfg.TempDir == "" {
		cfg.TempDir = os.TempDir()
	}
	os.MkdirAll(cfg.TempDir, 0755)

	return &PodcastCommand{cfg: cfg}
}

// Name returns "podcast".
func (p *PodcastCommand) Name() string {
	return "podcast"
}

// Description returns the podcast description.
func (p *PodcastCommand) Description() string {
	return "Generate audio from text (send multi-line text after command)"
}

// Execute generates audio from the provided text.
func (p *PodcastCommand) Execute(ctx context.Context, args []string, output io.Writer) error {
	// Reset file response
	p.fileResponse = nil

	// Validate input
	if len(args) == 0 || args[0] == "" {
		return fmt.Errorf("no text provided. Usage: /podcast followed by your text")
	}

	text := args[0]
	fmt.Fprintf(output, "Generating audio for %d characters...\n", len(text))

	// Create unique temp files
	timestamp := time.Now().UnixNano()
	inputPath := filepath.Join(p.cfg.TempDir, fmt.Sprintf("podcast_input_%d.txt", timestamp))
	outputPath := filepath.Join(p.cfg.TempDir, fmt.Sprintf("podcast_output_%d.mp3", timestamp))

	// Write input file
	if err := os.WriteFile(inputPath, []byte(text), 0644); err != nil {
		return fmt.Errorf("failed to create input file: %w", err)
	}
	defer os.Remove(inputPath) // Always cleanup input file

	fmt.Fprintln(output, "Input file created, starting TTS generation...")

	// Run podcastgen
	cmd := exec.CommandContext(ctx,
		"uv", "run", "python", "-m", "tts_gen.cli",
		"--input", inputPath,
		"--output", outputPath,
		"--config", p.cfg.ConfigPath,
	)
	cmd.Dir = p.cfg.PodcastgenPath
	cmd.Stdout = output
	cmd.Stderr = output

	if err := cmd.Run(); err != nil {
		// Cleanup output file on error
		os.Remove(outputPath)
		if ctx.Err() != nil {
			return fmt.Errorf("generation timed out or cancelled")
		}
		return fmt.Errorf("podcastgen failed: %w", err)
	}

	// Check if output file exists
	if _, err := os.Stat(outputPath); err != nil {
		return fmt.Errorf("output file not created")
	}

	fmt.Fprintln(output, "Audio generated successfully!")

	// Set file response for bot to send
	p.fileResponse = &pkgcmd.FileResponse{
		Path:    outputPath,
		Caption: "Generated audio",
		Cleanup: true,
	}

	return nil
}

// Metadata returns command configuration.
func (p *PodcastCommand) Metadata() pkgcmd.Metadata {
	return pkgcmd.Metadata{
		Timeout:        10 * time.Minute, // TTS can take a while
		MaxOutput:      10000,
		RequireConfirm: false,
	}
}

// FileResponse returns the file to send after execution.
func (p *PodcastCommand) FileResponse() *pkgcmd.FileResponse {
	return p.fileResponse
}
