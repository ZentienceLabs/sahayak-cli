package main

import (
	"context"
	"fmt"
	"github.com/ZentienceLabs/sahayak-cli/core/config"
)

func runConfig(_ context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: sahayak config [view | set <key> <value>]")
	}

	sub := args[0]
	cfg := config.Defaults()

	switch sub {
	case "view":
		fmt.Printf("Current Configuration:\n")
		fmt.Printf("  engine:   %s\n", cfg.Mode)
		fmt.Printf("  endpoint: %s\n", cfg.Endpoint)
		fmt.Printf("  model:    %s\n", cfg.Model)
		fmt.Printf("  embedder: %s\n", cfg.Embedder)
		return nil
	case "set":
		if len(args) < 3 {
			return fmt.Errorf("usage: sahayak config set <key> <value>")
		}
		key := args[1]
		val := args[2]

		switch key {
		case "engine":
			cfg.Mode = config.Engine(val)
		case "endpoint":
			cfg.Endpoint = val
		case "model":
			cfg.Model = val
		case "embedder":
			cfg.Embedder = val
		default:
			return fmt.Errorf("unknown config key %q (supported: mode, endpoint, model, embedder)", key)
		}

		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}
		fmt.Printf("saved %s = %s\n", key, val)
		return nil
	default:
		return fmt.Errorf("unknown config subcommand %q (view | set)", sub)
	}
}
