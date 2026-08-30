package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/extensions/store"
)

// cmdInsights handles the `omega insights` subcommand.
// It aggregates session data over the last N days and prints a report.
func cmdInsights(configPath string, args []string) error {
	fs := flag.NewFlagSet("insights", flag.ContinueOnError)
	days := fs.Int("days", 30, "number of days to analyze")
	if err := fs.Parse(stripConfigFlag(stripTrustArgs(args))); err != nil {
		return err
	}

	cfg, err := LoadConfig(resolveConfigPath(configPath))
	if err != nil {
		return err
	}
	if err := resolveHomePaths(&cfg); err != nil {
		return err
	}
	storeDB, err := store.Open(cfg.Store.DBPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer storeDB.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stats, err := storeDB.ComputeInsights(ctx, *days)
	if err != nil {
		return fmt.Errorf("compute insights: %w", err)
	}

	if stats.Sessions == 0 {
		fmt.Printf("No sessions in the last %d days.\n", *days)
		return nil
	}

	fmt.Print(agent.FormatInsights(stats))
	return nil
}
