package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ZentienceLabs/sahayak-cli/core/agent"
	"github.com/ZentienceLabs/sahayak-cli/core/cartridge"
	"github.com/ZentienceLabs/sahayak-cli/core/config"
	"github.com/ZentienceLabs/sahayak-cli/core/curator"
	"github.com/ZentienceLabs/sahayak-cli/core/envfacts"
	"github.com/ZentienceLabs/sahayak-cli/core/knowledge"
	"github.com/ZentienceLabs/sahayak-cli/core/learn"
	"github.com/ZentienceLabs/sahayak-cli/core/llm"
	"github.com/ZentienceLabs/sahayak-cli/core/router"
	"github.com/ZentienceLabs/sahayak-cli/core/tui"
)

// setupAgent wires the foreground agent and starts the background curator for a
// session — shared by one-shot `ask` and the interactive shell, so both get the same
// gate/knowledge/memory/env-cache. Returns the agent and a cleanup func that stops the
// curator and flushes learned topology. The approver is chosen by the caller.
func setupAgent(ctx context.Context, cfg config.Config, approver agent.Approver) (*agent.Agent, func(), error) {
	provider := newProvider(cfg)
	if err := provider.Health(ctx); err != nil {
		return nil, nil, fmt.Errorf("%w\n  hint: start your backend (e.g. `ollama serve`) or set --endpoint / SAHAYAK_ENDPOINT", err)
	}

	gate := llm.NewGate()
	a := agent.New(gate.Foreground(provider), approver, os.Stdout)
	a.AutoRunReadOnly = cfg.AutoRunReadOnly

	// Load cartridges once (default routing); their curated KB is also fed to the
	// retriever so a tool's commands AND its knowledge ship together.
	var carts []cartridge.Cartridge
	useCartridges := os.Getenv("SAHAYAK_LEGACY") != "1"
	if useCartridges {
		if c, err := cartridge.LoadAll(); err == nil {
			carts = c
		}
	}

	var extraPacks []knowledge.Pack
	if p, ok := cartridgeKnowledgePack(ctx, cfg, carts); ok {
		extraPacks = append(extraPacks, p)
	}
	if retr, err := buildRetriever(cfg, "", extraPacks...); err == nil && retr != nil && !retr.Empty() {
		a.Retriever = groundingRetriever{r: retr}
	}
	a.Memory = memorizer{s: newMemoryStore(cfg)}

	envStore := envfacts.NewStore("")
	a.Env = envCache{s: envStore}

	// Self-learning: record deterministically-judged observations (routed outcomes,
	// ad-hoc commands, unmatched requests) for `sahayak learn suggest`. Observe-only.
	a.Learner = learnerAdapter{s: learn.NewStore("")}

	// Routing: the data-driven CARTRIDGE engine is the DEFAULT (tool-agnostic, peer
	// cartridges, commands as data). The legacy pipeline (regex playbooks + semantic
	// router + model classifier) is opt-in via SAHAYAK_LEGACY=1 for side-by-side
	// comparison only. Exactly one is active so behavior is unambiguous.
	if !useCartridges {
		if r, err := buildRouter(ctx, cfg); err != nil {
			a.UI.Note("semantic router disabled (" + err.Error() + ")")
		} else {
			a.Router = r
		}
	} else {
		if ix, err := cartridge.NewIndex(ctx, newEmbedder(cfg), carts); err != nil || ix.Empty() {
			a.UI.Note("cartridge engine disabled — falling back to legacy routing")
			if r, rerr := buildRouter(ctx, cfg); rerr == nil {
				a.Router = r
			}
		} else {
			a.Cartridges = ix
		}
	}

	curCtx, curStop := context.WithCancel(ctx)
	cur := curator.New(gate, gate.Background(provider), envStore, newMemoryStore(cfg))
	go cur.Run(curCtx)

	cleanup := func() {
		curStop()
		_ = envStore.Save()
	}
	return a, cleanup, nil
}

// shellSources populates the rich prompt's "@" entity picker — installed cartridges plus
// the namespaces/workloads the env cache has learned (read fresh from disk each prompt).
func shellSources(a *agent.Agent) tui.Sources {
	var src tui.Sources
	if a.Cartridges != nil {
		for _, c := range a.Cartridges.Cartridges() {
			src.Cartridges = append(src.Cartridges, c.Name)
		}
	}
	es := envfacts.NewStore("")
	for _, f := range es.Fresh(envfacts.KindNamespace, "") {
		src.Namespaces = append(src.Namespaces, f.Name)
	}
	for _, f := range es.Fresh(envfacts.KindDeployment, "") {
		src.Deployments = append(src.Deployments, f.Name)
	}
	return src
}

// buildRouter constructs the semantic intent router from the embedded default catalog
// plus an optional user catalog file (SAHAYAK_CATALOG), embedding every example with
// the configured embedder. With the default hash embedder this is instant and offline;
// with an Ollama embedding model it makes one embed call per example at startup.
func buildRouter(ctx context.Context, cfg config.Config) (*router.Router, error) {
	extra := ""
	if path := os.Getenv("SAHAYAK_CATALOG"); path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading SAHAYAK_CATALOG %s: %w", path, err)
		}
		extra = string(b)
	}
	return router.New(ctx, newEmbedder(cfg), extra)
}

// cartridgeKnowledgePack builds an in-memory knowledge Pack from the curated KB embedded
// in the loaded cartridges, embedded with the configured embedder. This is how a tool's
// shipped knowledge (how-to/scenarios) becomes searchable grounding alongside its
// commands — "commands + knowledge together." Returns ok=false when no cartridge has KB.
func cartridgeKnowledgePack(ctx context.Context, cfg config.Config, carts []cartridge.Cartridge) (knowledge.Pack, bool) {
	var chunks []knowledge.Chunk
	for _, c := range carts {
		for _, k := range c.Knowledge {
			chunks = append(chunks, knowledge.Chunk{Text: k, Command: c.Name, SourceDoc: c.Name + " cartridge KB"})
		}
	}
	if len(chunks) == 0 {
		return knowledge.Pack{}, false
	}
	pack, err := knowledge.Build(ctx, "cartridge-kb", "1", "cartridges", chunks, newEmbedder(cfg))
	if err != nil {
		return knowledge.Pack{}, false
	}
	return pack, true
}

// listModels asks the Ollama backend which model tags are installed.
func listModels(ctx context.Context, endpoint string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var body struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(body.Models))
	for _, m := range body.Models {
		names = append(names, m.Name)
	}
	sort.Strings(names)
	return names, nil
}

// runModels prints the installed models (for `sahayak models`).
func runModels(ctx context.Context, _ []string) error {
	cfg := config.Defaults()
	models, err := listModels(ctx, cfg.Endpoint)
	if err != nil {
		return fmt.Errorf("could not reach Ollama at %s: %w\n  hint: run `ollama serve`", cfg.Endpoint, err)
	}
	if len(models) == 0 {
		fmt.Println("no models installed — pull one, e.g. `ollama pull qwen3:4b-instruct`")
		return nil
	}
	fmt.Printf("Installed models (%s):\n", cfg.Endpoint)
	for _, m := range models {
		marker := "  "
		if m == cfg.Model {
			marker = "▸ "
		}
		fmt.Printf("  %s%s\n", marker, m)
	}
	fmt.Printf("\ndefault: %s  (override with --model or SAHAYAK_MODEL, or pick one in `sahayak shell`)\n", cfg.Model)
	return nil
}

// runShell is the interactive REPL: pick a model from the installed list, then type
// requests in a loop. One agent (and one background curator) lives for the whole
// session, so the curator keeps consolidating topology between your questions and the
// env-cache warms up — later questions in the session start already knowing names.
func runShell(ctx context.Context, args []string) error {
	cfg := config.Defaults()
	// Minimal flags; everything else can be changed live or via env.
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--endpoint":
			if i+1 < len(args) {
				cfg.Endpoint = args[i+1]
				i++
			}
		case "--model":
			if i+1 < len(args) {
				cfg.Model = args[i+1]
				i++
			}
		case "--engine":
			if i+1 < len(args) {
				cfg.Engine = config.Engine(args[i+1])
				i++
			}
		}
	}

	in := bufio.NewReader(os.Stdin)
	interactive := tui.IsInteractive()

	// Model selection: show the installed list and let the operator pick (Enter keeps
	// the default). Only when we have a real terminal and an Ollama backend.
	if interactive && cfg.Engine == config.EngineOllama {
		if models, err := listModels(ctx, cfg.Endpoint); err == nil && len(models) > 0 {
			cfg.Model = chooseModel(in, models, cfg.Model)
		}
	}

	// The shell is line-based, so use the line approver (not the full-screen TUI) and
	// share its reader with our prompt loop — one stdin, no contention.
	approver := agent.NewLineApprover(in, os.Stdout)

	a, cleanup, err := setupAgent(ctx, cfg, approver)
	if err != nil {
		return err
	}
	defer cleanup()
	a.MaxInvestigateSteps = 8

	// Rich prompt (slash palette + @ entity picker) on a real terminal; plain readline
	// otherwise or when SAHAYAK_PLAIN_PROMPT=1.
	rich := tui.IsInteractive() && os.Getenv("SAHAYAK_PLAIN_PROMPT") != "1"

	shellBanner(cfg.Model)
	for {
		if ctx.Err() != nil {
			return nil
		}

		var line string
		if rich {
			l, eof, perr := tui.Prompt("\033[36msahayak>\033[0m ", shellSources(a))
			if perr != nil {
				fmt.Fprintln(os.Stderr, "(rich prompt unavailable, falling back to plain input)")
				rich = false
				continue
			}
			if eof {
				fmt.Println()
				return nil
			}
			line = strings.TrimSpace(l)
		} else {
			fmt.Print("\n\033[36msahayak>\033[0m ")
			l, err := in.ReadString('\n')
			if err != nil { // EOF (Ctrl-D / closed pipe)
				fmt.Println()
				return nil
			}
			line = strings.TrimSpace(l)
		}

		// Slash commands (/help, /model, /cartridge …) — dispatched, never sent to the model.
		if strings.HasPrefix(line, "/") {
			fields := strings.Fields(line[1:])
			if len(fields) == 0 {
				continue
			}
			switch fields[0] {
			case "exit", "quit", "q":
				return nil
			case "help", "h":
				shellHelp()
			case "clear":
				fmt.Print("\033[2J\033[H")
			case "cartridge", "cart":
				if err := runCartridge(ctx, fields[1:]); err != nil {
					fmt.Fprintf(os.Stderr, "error: %v\n", err)
				}
			case "learn":
				if err := runLearn(ctx, fields[1:]); err != nil {
					fmt.Fprintf(os.Stderr, "error: %v\n", err)
				}
			case "knowledge", "kb":
				if err := runKnowledge(ctx, fields[1:]); err != nil {
					fmt.Fprintf(os.Stderr, "error: %v\n", err)
				}
			case "memory", "mem":
				if err := runMemory(ctx, fields[1:]); err != nil {
					fmt.Fprintf(os.Stderr, "error: %v\n", err)
				}
			case "legacy":
				mode := "cartridge engine (default)"
				if a.Cartridges == nil {
					mode = "legacy regex/router"
				}
				fmt.Printf("routing: %s — switch by restarting with SAHAYAK_LEGACY=1 (or unset it)\n", mode)
			case "model", "models":
				if models, err := listModels(ctx, cfg.Endpoint); err == nil {
					cfg.Model = chooseModel(in, models, cfg.Model)
					cleanup()
					if a, cleanup, err = setupAgent(ctx, cfg, approver); err != nil {
						return err
					}
					a.MaxInvestigateSteps = 8
					fmt.Printf("switched to %s\n", cfg.Model)
				}
			default:
				fmt.Printf("unknown command /%s (try /help)\n", fields[0])
			}
			continue
		}

		switch {
		case line == "":
			continue
		case line == "exit", line == "quit", line == ":q":
			return nil
		case line == "help", line == ":h", line == "?":
			shellHelp()
			continue
		case strings.HasPrefix(line, "!"):
			// Escape hatch: run a command yourself. Still risk-gated, so a mutating
			// command is confirmed before it runs.
			raw := strings.TrimSpace(line[1:])
			if raw == "" {
				continue
			}
			fields := strings.Fields(raw)
			if err := a.RunOnce(ctx, fields[0], fields[1:]); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
			}
			continue
		case line == "models", line == ":models":
			if models, err := listModels(ctx, cfg.Endpoint); err == nil {
				cfg.Model = chooseModel(in, models, cfg.Model)
				cleanup()
				if a, cleanup, err = setupAgent(ctx, cfg, approver); err != nil {
					return err
				}
				a.MaxInvestigateSteps = 8
				fmt.Printf("switched to %s\n", cfg.Model)
			}
			continue
		}
		if err := a.Investigate(ctx, line); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		}
	}
}

// chooseModel prints the installed models numbered and reads a selection; Enter (or
// any invalid input) keeps the current default.
func chooseModel(in *bufio.Reader, models []string, current string) string {
	fmt.Println("\nAvailable models:")
	for i, m := range models {
		marker := " "
		if m == current {
			marker = "▸"
		}
		fmt.Printf("  %s [%d] %s\n", marker, i+1, m)
	}
	fmt.Printf("Select [1-%d] (Enter = %s): ", len(models), current)
	line, err := in.ReadString('\n')
	if err != nil {
		return current
	}
	n, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || n < 1 || n > len(models) {
		return current
	}
	return models[n-1]
}

func shellBanner(model string) {
	fmt.Printf("\n⬡ Sahayak interactive shell  ·  model: %s\n", model)
	fmt.Println("  Plain language to act · type / for commands · @ to reference a tool/app · /help")
}

func shellHelp() {
	fmt.Print(`
Sahayak shell — type a request and press Enter. Examples:
  list configmaps for web-api
  how is web-api doing
  restart web-api               (will ask for approval)

Type-ahead (rich prompt on a terminal):
  /        command palette — Tab to accept, ↑↓ to move, Esc to close
  @        entity picker — cartridges, namespaces, and learned workloads

Slash commands:
  /help                 this help          /clear     clear the screen
  /model | /models      switch the model   /legacy    show routing mode
  /cartridge <args>     manage cartridges  /learn <args>     self-learning
  /knowledge <args>     knowledge packs    /memory <args>    long-term memory
  /exit                 leave the shell

Other:
  ! <cmd>    run a command yourself (risk-gated), e.g. ! kubectl get ns
  exit / quit / Ctrl-D   leave the shell   (set SAHAYAK_PLAIN_PROMPT=1 for a basic prompt)
`)
}
