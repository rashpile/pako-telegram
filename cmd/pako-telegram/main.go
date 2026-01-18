// pako-telegram is an internal Telegram bot for executing ops tasks.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/rashpile/pako-telegram/internal/audit"
	"github.com/rashpile/pako-telegram/internal/auth"
	"github.com/rashpile/pako-telegram/internal/bot"
	"github.com/rashpile/pako-telegram/internal/command"
	"github.com/rashpile/pako-telegram/internal/command/builtin"
	"github.com/rashpile/pako-telegram/internal/config"
	"github.com/rashpile/pako-telegram/internal/executor"
	"github.com/rashpile/pako-telegram/internal/status"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to configuration file")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	if err := run(*configPath); err != nil {
		slog.Error("fatal error", "error", err)
		os.Exit(1)
	}
}

func run(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	// Resolve paths relative to config file
	commandsDir := cfg.ExpandPath(configPath, cfg.CommandsDir)
	dbPath := cfg.ExpandPath(configPath, cfg.Database.Path)

	slog.Info("configuration loaded",
		"commands_dir", commandsDir,
		"database", dbPath,
	)

	// Set up audit logger
	auditLogger, err := audit.NewSQLiteLogger(dbPath)
	if err != nil {
		return err
	}
	defer auditLogger.Close()

	// Set up authorization
	authorizer := auth.NewAllowlist(cfg.Telegram.AllowedChatIDs)

	// Set up executor
	exec := executor.NewShellExecutor()

	// Set up command registry
	registry := command.NewRegistry()

	// Set up YAML loader
	loader := command.NewLoader(commandsDir, cfg.Defaults, exec)

	// Load YAML commands
	yamlCommands, err := loader.Load()
	if err != nil {
		slog.Warn("failed to load yaml commands", "error", err)
	} else {
		for _, cmd := range yamlCommands {
			registry.Register(cmd)
		}
		slog.Info("loaded yaml commands", "count", len(yamlCommands))
	}

	// Register built-in commands
	registry.Register(builtin.NewHelpCommand(registry))
	registry.Register(builtin.NewStatusCommand(status.NewGopsutilCollector()))
	registry.Register(builtin.NewReloadCommand(loader, registry))
	registry.Register(builtin.NewVersionCommand())

	// Register podcast command if configured
	if cfg.Podcast.PodcastgenPath != "" {
		podcastCfg := builtin.PodcastConfig{
			PodcastgenPath: cfg.ExpandPath(configPath, cfg.Podcast.PodcastgenPath),
			ConfigPath:     cfg.ExpandPath(configPath, cfg.Podcast.ConfigPath),
			TempDir:        cfg.Podcast.TempDir,
		}
		registry.Register(builtin.NewPodcastCommand(podcastCfg))
		slog.Info("podcast command enabled", "path", podcastCfg.PodcastgenPath)
	}

	// Create bot with dependencies
	b, err := bot.New(bot.Config{
		Token:          cfg.Telegram.Token,
		Authorizer:     authorizer,
		Registry:       registry,
		Defaults:       cfg.Defaults,
		AllowedChatIDs: cfg.Telegram.AllowedChatIDs,
	})
	if err != nil {
		return err
	}
	_ = auditLogger // TODO: wire into bot for command logging

	// Set up graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		slog.Info("received signal, shutting down", "signal", sig)
		cancel()
	}()

	// Notify users that bot has restarted
	b.NotifyStartup()

	slog.Info("starting bot")
	return b.Run(ctx)
}
