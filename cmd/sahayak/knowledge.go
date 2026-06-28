package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/ZentienceLabs/sahayak-cli/core/agent"
	"github.com/ZentienceLabs/sahayak-cli/core/config"
	"github.com/ZentienceLabs/sahayak-cli/core/embed"
	"github.com/ZentienceLabs/sahayak-cli/core/knowledge"
)

// runKnowledge dispatches the `sahayak knowledge` subcommands.
func runKnowledge(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: sahayak knowledge <install|list|search|build|remove> …")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "install":
		return knowledgeInstall(rest)
	case "list":
		return knowledgeList()
	case "search":
		return knowledgeSearch(ctx, rest)
	case "build":
		return knowledgeBuild(ctx, rest)
	case "remove", "rm":
		return knowledgeRemove(rest)
	default:
		return fmt.Errorf("unknown knowledge subcommand %q", sub)
	}
}

func knowledgeInstall(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: sahayak knowledge install <file.sahayakpack>")
	}
	cfg := config.Defaults()
	store := knowledge.NewStore("")
	m, err := store.Install(args[0], newEmbedder(cfg))
	if err != nil {
		return err
	}
	fmt.Printf("installed %s v%s (%d chunks, embed %s) → %s\n",
		m.Name, m.Version, m.ChunkCount, m.EmbedModelID, store.Dir)
	return nil
}

func knowledgeList() error {
	store := knowledge.NewStore("")
	manifests, err := store.List()
	if err != nil {
		return err
	}
	if len(manifests) == 0 {
		fmt.Printf("no knowledge packs installed (dir: %s)\n", store.Dir)
		return nil
	}
	fmt.Printf("Installed knowledge packs (%s):\n", store.Dir)
	for _, m := range manifests {
		fmt.Printf("  %-16s v%-8s %5d chunks  embed=%s  src=%s\n",
			m.Name, orDash(m.Version), m.ChunkCount, m.EmbedModelID, orDash(m.Source))
	}
	return nil
}

func knowledgeSearch(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	pack := fs.String("pack", "", "restrict to one pack by name")
	k := fs.Int("k", 5, "number of results")
	if err := fs.Parse(args); err != nil {
		return err
	}
	query := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if query == "" {
		return fmt.Errorf("usage: sahayak knowledge search [--pack NAME] [-k N] <query>")
	}

	cfg := config.Defaults()
	retr, err := buildRetriever(cfg, *pack)
	if err != nil {
		return err
	}
	if retr == nil || retr.Empty() {
		fmt.Println("no installed packs to search — try `sahayak knowledge install <pack>`")
		return nil
	}
	hits, err := retr.Search(ctx, query, *k)
	if err != nil {
		return err
	}
	if len(hits) == 0 {
		fmt.Println("no matches")
		return nil
	}
	for i, h := range hits {
		fmt.Printf("%d. [%s] (score %.4f)\n   %s\n", i+1, h.Pack, h.Score, collapse(h.Chunk.Text))
	}
	return nil
}

func knowledgeRemove(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: sahayak knowledge remove <name>")
	}
	if err := knowledge.NewStore("").Remove(args[0]); err != nil {
		return err
	}
	fmt.Printf("removed pack %q\n", args[0])
	return nil
}

// knowledgeBuild authors a .sahayakpack from a source file (.jsonl of chunks, or a
// .txt/.md chunked by blank lines). Embedding cost is paid here, once.
func knowledgeBuild(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	name := fs.String("name", "", "pack name (required)")
	ver := fs.String("version", "", "pack version")
	src := fs.String("source", "", "human-readable source description")
	from := fs.String("from", "", "input file: .jsonl chunks or .txt/.md (required)")
	out := fs.String("o", "", "output .sahayakpack path (required)")
	command := fs.String("command", "", "CLI this pack documents, e.g. kubectl (for .txt/.md)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" || *from == "" || *out == "" {
		return fmt.Errorf("usage: sahayak knowledge build --name N --from FILE -o OUT.sahayakpack [--version V --source S --command kubectl]")
	}

	chunks, err := loadChunks(*from, *command)
	if err != nil {
		return err
	}
	if len(chunks) == 0 {
		return fmt.Errorf("no chunks found in %s", *from)
	}

	cfg := config.Defaults()
	pack, err := knowledge.Build(ctx, *name, *ver, *src, chunks, newEmbedder(cfg))
	if err != nil {
		return err
	}
	f, err := os.Create(*out)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := knowledge.WritePack(f, pack); err != nil {
		return err
	}
	fmt.Printf("built %s (%d chunks, embed %s) → %s\n", *name, len(chunks), pack.Manifest.EmbedModelID, *out)
	return nil
}

// loadChunks parses a source file into chunks.
func loadChunks(path, command string) ([]knowledge.Chunk, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	if strings.HasSuffix(path, ".jsonl") {
		var chunks []knowledge.Chunk
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			var c knowledge.Chunk
			if err := json.Unmarshal([]byte(line), &c); err != nil {
				return nil, fmt.Errorf("bad jsonl line: %w", err)
			}
			if c.Command == "" {
				c.Command = command
			}
			chunks = append(chunks, c)
		}
		return chunks, sc.Err()
	}

	// Plain text / markdown: chunk on blank lines.
	var chunks []knowledge.Chunk
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var para []string
	flush := func() {
		text := strings.TrimSpace(strings.Join(para, " "))
		if text != "" {
			chunks = append(chunks, knowledge.Chunk{Text: text, Command: command, Kind: knowledge.KindProse})
		}
		para = para[:0]
	}
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			flush()
			continue
		}
		para = append(para, line)
	}
	flush()
	return chunks, sc.Err()
}

// newEmbedder resolves the configured embedder spec to an implementation.
func newEmbedder(cfg config.Config) embed.Embedder {
	spec := cfg.Embedder
	switch {
	case strings.HasPrefix(spec, "ollama:"):
		return embed.NewOllamaEmbedder(cfg.Endpoint, strings.TrimPrefix(spec, "ollama:"))
	case strings.HasPrefix(spec, "hash:"):
		dim, _ := strconv.Atoi(strings.TrimPrefix(spec, "hash:"))
		return embed.NewHashEmbedder(dim)
	default:
		return embed.NewHashEmbedder(256)
	}
}

// buildRetriever loads installed packs (optionally one) into a knowledge Retriever.
func buildRetriever(cfg config.Config, onlyPack string, extra ...knowledge.Pack) (*knowledge.Retriever, error) {
	store := knowledge.NewStore("")
	packs, err := store.LoadAll()
	if err != nil {
		return nil, err
	}
	if onlyPack != "" {
		filtered := packs[:0]
		for _, p := range packs {
			if p.Manifest.Name == onlyPack {
				filtered = append(filtered, p)
			}
		}
		packs = filtered
	}
	packs = append(packs, extra...) // in-memory packs, e.g. cartridge KB
	e := newEmbedder(cfg)
	r := knowledge.NewRetriever(e, packs)
	// Second-stage reranking (MMR over the existing embeddings) — diversifies the
	// candidate set so near-duplicate runbook paragraphs don't crowd out a second
	// relevant fact. Uses the SAME embedder the packs were queried with.
	r.Reranker = knowledge.NewMMRReranker(e)
	// Surface model-pin mismatches: a pack built with a different embedder can't be
	// searched semantically with this one, so the operator should rebuild it.
	for _, w := range r.Warnings() {
		fmt.Fprintln(os.Stderr, "⚠ knowledge: "+w)
	}
	return r, nil
}

// groundingRetriever adapts knowledge.Retriever to the narrow agent.Retriever
// interface, decoupling the agent from the knowledge package's concrete types.
type groundingRetriever struct{ r *knowledge.Retriever }

func (g groundingRetriever) Empty() bool { return g.r == nil || g.r.Empty() }

func (g groundingRetriever) Search(ctx context.Context, query string, k int) ([]agent.Grounding, error) {
	hits, err := g.r.Search(ctx, query, k)
	if err != nil {
		return nil, err
	}
	out := make([]agent.Grounding, 0, len(hits))
	for _, h := range hits {
		src := h.Pack
		if h.Chunk.Command != "" {
			src = h.Chunk.Command + "/" + h.Pack
		}
		out = append(out, agent.Grounding{Text: h.Chunk.Text, Source: src})
	}
	return out, nil
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func collapse(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 160 {
		s = s[:160] + "…"
	}
	return s
}
