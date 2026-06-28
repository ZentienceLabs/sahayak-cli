// Command sahayak is the Sahayak CLI entrypoint. Phase 1 wires three commands —
// ask, doctor, version — over a hand-rolled dispatcher (stdlib flag), keeping the
// walking skeleton dependency-free. Cobra-powered completions/man pages come later.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/ZentienceLabs/sahayak-cli/core/agent"
	"github.com/ZentienceLabs/sahayak-cli/core/config"
	"github.com/ZentienceLabs/sahayak-cli/core/knowledge"
	"github.com/ZentienceLabs/sahayak-cli/core/llm"
	"github.com/ZentienceLabs/sahayak-cli/core/tui"
	"github.com/ZentienceLabs/sahayak-cli/core/version"
)

func main() {
	// Ctrl-C cancels the in-flight model call / command cleanly.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// No subcommand: drop into the interactive shell on a real terminal (so you can
	// just run the exe), otherwise print usage.
	if len(os.Args) < 2 {
		if tui.IsInteractive() {
			if err := runShell(ctx, nil); err != nil {
				fmt.Fprintf(os.Stderr, "\nerror: %v\n", err)
				os.Exit(1)
			}
			return
		}
		usage()
		os.Exit(2)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	var err error
	switch cmd {
	case "ask":
		err = runAsk(ctx, args)
	case "shell", "repl", "interactive":
		err = runShell(ctx, args)
	case "models":
		err = runModels(ctx, args)
	case "knowledge", "kb":
		err = runKnowledge(ctx, args)
	case "cartridge", "cart":
		err = runCartridge(ctx, args)
	case "learn":
		err = runLearn(ctx, args)
	case "memory", "mem":
		err = runMemory(ctx, args)
	case "doctor":
		err = runDoctor(ctx, args)
	case "version", "--version", "-v":
		fmt.Printf("sahayak %s (commit %s, built %s)\n", version.Version, version.Commit, version.Date)
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		usage()
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "\nerror: %v\n", err)
		os.Exit(1)
	}
}

// runAsk turns a natural-language request into an approved, executed plan.
func runAsk(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("ask", flag.ContinueOnError)
	cfg := config.Defaults()
	engine := fs.String("engine", string(cfg.Engine), "brain: ollama (dev) | embedded (appliance, Phase 6)")
	fs.StringVar(&cfg.Endpoint, "endpoint", cfg.Endpoint, "inference endpoint (Ollama)")
	fs.StringVar(&cfg.Model, "model", cfg.Model, "model tag")
	yolo := fs.Bool("approve-all-readonly", cfg.AutoRunReadOnly, "auto-run read-only steps without prompting")
	noTUI := fs.Bool("no-tui", false, "use the plain line-mode approval gate instead of the rich TUI")
	investigate := fs.Bool("investigate", false, "force iterative mode: discover step-by-step (list→match→drill in)")
	plan := fs.Bool("plan", false, "force one-shot plan mode (don't auto-route diagnostic questions to investigate)")
	maxSteps := fs.Int("max-steps", 8, "max steps for investigate mode")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg.Engine = config.Engine(*engine)
	cfg.AutoRunReadOnly = *yolo

	request := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if request == "" {
		return fmt.Errorf("nothing to do — try: sahayak ask \"reload nginx after my config edit\"")
	}

	// Rich TUI on a real terminal; line-mode over SSH pipes / CI / cron, or when
	// the operator forces it. Both satisfy agent.Approver, so the loop is identical.
	var approver agent.Approver
	if !*noTUI && tui.IsInteractive() {
		approver = tui.NewApprover()
	} else {
		approver = agent.NewLineApprover(os.Stdin, os.Stdout)
	}

	a, cleanup, err := setupAgent(ctx, cfg, approver)
	if err != nil {
		return err
	}
	defer cleanup()
	a.MaxInvestigateSteps = *maxSteps

	// Default to the adaptive loop (reacts to each step's output). --plan forces
	// the classic one-shot plan for direct, known actions. --investigate is the
	// explicit name for the default and stays for clarity.
	if *plan {
		return a.Handle(ctx, request)
	}
	_ = *investigate // default path is already the investigate loop
	return a.Investigate(ctx, request)
}

// runDoctor checks the environment: backend reachability and basic config.
func runDoctor(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	cfg := config.Defaults()
	engine := fs.String("engine", string(cfg.Engine), "brain: ollama (dev) | embedded (appliance, Phase 6)")
	fs.StringVar(&cfg.Endpoint, "endpoint", cfg.Endpoint, "inference endpoint (Ollama)")
	fs.StringVar(&cfg.Model, "model", cfg.Model, "model tag")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg.Engine = config.Engine(*engine)

	fmt.Printf("Sahayak doctor\n")
	fmt.Printf("  engine:   %s\n", cfg.Engine)
	fmt.Printf("  endpoint: %s\n", cfg.Endpoint)
	fmt.Printf("  model:    %s\n", cfg.Model)
	fmt.Printf("  embedder: %s\n", cfg.Embedder)

	// Knowledge + memory subsystem status (these work offline).
	if packs, err := knowledge.NewStore("").List(); err == nil {
		fmt.Printf("  packs:    %d installed\n", len(packs))
	}
	if mems, err := newMemoryStore(cfg).All(); err == nil {
		fmt.Printf("  memory:   %d entries\n", len(mems))
	}

	provider := newProvider(cfg)
	if err := provider.Health(ctx); err != nil {
		fmt.Printf("  backend:  ✗ %v\n", err)
		switch cfg.Engine {
		case config.EngineOllama:
			fmt.Printf("\nNot ready. Start Ollama (`ollama serve`) and pull a model (`ollama pull %s`).\n", cfg.Model)
		case config.EngineCloud:
			fmt.Printf("\nNot ready. The cloud engine (%s) calls a hosted API — set ANTHROPIC_API_KEY and SAHAYAK_MODEL=claude-opus-4-8.\nNote: this engine is NOT sovereign — requests leave the host. Use ollama/embedded for the air-gapped appliance.\n", cfg.CloudProvider)
		default:
			fmt.Printf("\nNot ready. Bundle the embedded engine, or set SAHAYAK_LLAMA_SERVER + SAHAYAK_MODEL_PATH for dev.\n")
		}
		return nil
	}
	fmt.Printf("  backend:  ✓ reachable\n\nReady. Try: sahayak ask \"show me disk usage under /var\"\n")
	return nil
}

// newProvider builds the configured brain. Both engines satisfy llm.Provider, so
// everything above this line is engine-agnostic. Ollama is the Phase-1 dev brain;
// the embedded llama-server appliance (Phase 6) plugs in here with no other change.
func newProvider(cfg config.Config) llm.Provider {
	switch cfg.Engine {
	case config.EngineEmbedded:
		return llm.NewEmbedded(cfg.Model)
	case config.EngineCloud:
		return newCloudProvider(cfg)
	default:
		return llm.NewOllama(cfg.Endpoint, cfg.Model)
	}
}

// newCloudProvider picks the hosted backend for the (non-sovereign) cloud engine.
// Today only Anthropic/Claude is wired; the switch is where other hosted providers
// plug in, selected by SAHAYAK_CLOUD_PROVIDER.
func newCloudProvider(cfg config.Config) llm.Provider {
	switch cfg.CloudProvider {
	case "", "anthropic", "claude":
		return llm.NewAnthropic(cfg.Model)
	default:
		// Unknown selector: fall back to Anthropic so the cloud lane still works;
		// doctor/health will surface any misconfiguration clearly.
		return llm.NewAnthropic(cfg.Model)
	}
}

func usage() {
	fmt.Printf(`Sahayak — sovereign AI command-line assistant for DevOps & sysadmins

Usage:
  sahayak                               start the interactive shell (pick a model, then type)
  sahayak shell                         same, explicit
  sahayak ask "<what you want to do>"   one-shot: propose, explain, approve & run
  sahayak models                        list installed models
  sahayak knowledge <cmd>               manage offline knowledge packs (RAG)
  sahayak memory <cmd>                  add/list/search/forget long-term memory
  sahayak doctor                        check backend connectivity & config
  sahayak version                       print build info
  sahayak help                          show this help

Knowledge:
  sahayak knowledge install <file.sahayakpack>
  sahayak knowledge list
  sahayak knowledge search [--pack N] [-k 5] "<query>"
  sahayak knowledge build --name N --from FILE -o OUT.sahayakpack [--command kubectl]
  sahayak knowledge remove <name>

Flags (ask/doctor):
  --engine <name>    ollama (dev) | embedded   (env SAHAYAK_ENGINE)
  --endpoint <url>   inference endpoint        (env SAHAYAK_ENDPOINT)
  --model <tag>      model to use              (env SAHAYAK_MODEL)
  --approve-all-readonly=false   prompt for read-only steps too
  --no-tui           use the plain line-mode approval gate (auto on non-TTY)
  --investigate      force iterative discovery (auto-on for diagnostic questions)
  --plan             force one-shot plan mode
  --max-steps <n>    step budget for investigate mode (default 8)

Examples:
  sahayak ask "reload nginx after editing the server block"
  sahayak ask "find files larger than 100MB under /var/log"
  SAHAYAK_MODEL=qwen2.5-coder:7b sahayak ask "why did my pod crash?"
`)
}
