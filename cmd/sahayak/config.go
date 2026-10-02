package main

import (
	"context"
	"fmt"
	"github.com/ZentienceLabs/sahayak-cli/core/config"
	"github.com/charmbracelet/huh"
)

func runConfig(_ context.Context, args []string) error {
	cfg := config.Defaults()

	if len(args) == 0 {
		return runInteractiveConfig(cfg)
	}

	sub := args[0]
	switch sub {
	case "view":
		fmt.Printf("Current Configuration:\n")
		fmt.Printf("  mode:     %s\n", cfg.Mode)
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
		case "mode":
			cfg.Mode = config.Mode(val)
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

func runInteractiveConfig(cfg config.Config) error {
	var modeStr string = string(cfg.Mode)
	
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Operating Mode").
				Description("Select the trust boundary for LLM inference").
				Options(
					huh.NewOption("Sovereign (Local Air-gapped)", string(config.ModeSovereign)),
					huh.NewOption("Hybrid (Local Routing + Cloud Reasoning)", string(config.ModeHybrid)),
					huh.NewOption("Cloud (Hosted APIs)", string(config.ModeCloud)),
				).
				Value(&modeStr),
				
			huh.NewInput().
				Title("LLM Endpoint").
				Description("The URL of the inference server (e.g. http://127.0.0.1:11434)").
				Value(&cfg.Endpoint),
				
			huh.NewInput().
				Title("Default Model").
				Description("The primary reasoning model").
				Value(&cfg.Model),
				
			huh.NewInput().
				Title("Embedder").
				Description("The embedding model (e.g. hash:256 or ollama:nomic-embed-text)").
				Value(&cfg.Embedder),
		),
	)

	err := form.Run()
	if err != nil {
		return err // likely cancelled
	}

	cfg.Mode = config.Mode(modeStr)
	
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	
	fmt.Println("- Configuration saved successfully!")
	return nil
}
