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
	"github.com/rashpile/pako-telegram/internal/msgstore"
	"github.com/rashpile/pako-telegram/internal/scheduler"
	"github.com/rashpile/pako-telegram/internal/status"
	pkgcmd "github.com/rashpile/pako-telegram/pkg/command"
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
	reloadCmd := builtin.NewReloadCommand(loader, registry)
	registry.Register(reloadCmd)
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

	// Set up message store for cleanup functionality
	var msgStore *msgstore.Store
	if cfg.MessageStorePath != "" {
		storePath := cfg.ExpandPath(configPath, cfg.MessageStorePath)
		msgStore, err = msgstore.New(storePath)
		if err != nil {
			return err
		}
		slog.Info("message store enabled", "path", storePath)
	}

	// Create bot with dependencies
	b, err := bot.New(bot.Config{
		Token:          cfg.Telegram.Token,
		Authorizer:     authorizer,
		Registry:       registry,
		Defaults:       cfg.Defaults,
		AllowedChatIDs: cfg.Telegram.AllowedChatIDs,
		MessageStore:   msgStore,
	})
	if err != nil {
		return err
	}
	_ = auditLogger // TODO: wire into bot for command logging

	// Create scheduler (always, even if no scheduled commands yet)
	sched := createScheduler(yamlCommands, cfg.Telegram.AllowedChatIDs, b)

	// Wire scheduler with bot and reload command
	b.SetScheduler(sched)
	reloadCmd.SetScheduler(&schedulerAdapter{sched: sched})

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

	// Start scheduler in background
	go func() {
		if err := sched.Run(ctx); err != nil && err != context.Canceled {
			slog.Error("scheduler error", "error", err)
		}
	}()

	// Notify users that bot has restarted
	b.NotifyStartup()

	slog.Info("starting bot")
	return b.Run(ctx)
}

// schedulerAdapter wraps a scheduler to implement builtin.SchedulerUpdater.
type schedulerAdapter struct {
	sched *scheduler.Scheduler
}

// UpdateScheduledCommands implements builtin.SchedulerUpdater.
func (a *schedulerAdapter) UpdateScheduledCommands(cmds []pkgcmd.Command) {
	scheduled := extractScheduledCommands(cmds)
	a.sched.UpdateCommands(scheduled)
	slog.Info("scheduler updated", "scheduled_commands", len(scheduled))
}

// createScheduler creates a scheduler and loads any scheduled commands.
// Always returns a scheduler (even if no commands are scheduled yet).
func createScheduler(cmds []pkgcmd.Command, chatIDs []int64, exec scheduler.CommandExecutor) *scheduler.Scheduler {
	scheduled := extractScheduledCommands(cmds)

	sched := scheduler.New(scheduler.Config{
		ChatIDs:  chatIDs,
		Executor: exec,
	})
	sched.UpdateCommands(scheduled)

	slog.Info("scheduler initialized", "scheduled_commands", len(scheduled))
	return sched
}

// extractScheduledCommands extracts commands with schedules or intervals from a list.
func extractScheduledCommands(cmds []pkgcmd.Command) []scheduler.ScheduledCommand {
	var scheduled []scheduler.ScheduledCommand

	for _, cmd := range cmds {
		// Only YAMLCommand supports scheduling
		yamlCmd, ok := cmd.(*command.YAMLCommand)
		if !ok {
			continue
		}

		schedTimes := yamlCmd.Schedule()
		interval := yamlCmd.Interval()

		// Skip if no scheduling configured
		if len(schedTimes) == 0 && interval == 0 {
			continue
		}

		sc := scheduler.ScheduledCommand{
			Name:          cmd.Name(),
			Interval:      interval,
			InitialPaused: yamlCmd.InitialPaused(),
			Command:       cmd,
		}

		// Parse time-of-day schedule if present
		if len(schedTimes) > 0 {
			times, err := scheduler.ParseTimes(schedTimes)
			if err != nil {
				// Should not happen - already validated during load
				slog.Warn("invalid schedule times", "command", cmd.Name(), "error", err)
				continue
			}
			sc.Times = times
		}

		scheduled = append(scheduled, sc)
	}

	return scheduled
}
