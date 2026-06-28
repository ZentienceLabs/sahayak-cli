package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/ZentienceLabs/sahayak-cli/core/cartridge"
	"github.com/ZentienceLabs/sahayak-cli/core/knowledge"
)

// runCartridge handles `sahayak cartridge {list|install|where}` — managing the tool
// cartridges (the data packs that teach Sahayak a tool). Installed cartridges live in
// ~/.sahayak/cartridges and are peers of the built-ins.
func runCartridge(_ context.Context, args []string) error {
	sub := "list"
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "list":
		return cartridgeList()
	case "install", "add":
		if len(args) < 2 {
			return fmt.Errorf("usage: sahayak cartridge install <name | file.json | https://url>")
		}
		return cartridgeInstall(args[1])
	case "search", "find":
		return cartridgeSearch(args[1:])
	case "registry":
		return cartridgeRegistry(args[1:])
	case "where":
		dir, err := cartridge.InstallDir()
		if err != nil {
			return err
		}
		fmt.Println(dir)
		return nil
	case "build":
		return cartridgeBuild(args[1:])
	case "remove", "rm", "uninstall":
		if len(args) < 2 {
			return fmt.Errorf("usage: sahayak cartridge remove <name>")
		}
		if err := cartridge.Remove(args[1]); err != nil {
			return err
		}
		fmt.Printf("removed cartridge %q\n", args[1])
		return nil
	case "trust":
		return cartridgeTrust(args[1:])
	case "keygen":
		pub, priv, err := cartridge.Keygen()
		if err != nil {
			return err
		}
		fmt.Printf("public key (share/publish):\n  %s\n\nprivate key (KEEP SECRET — save it):\n  %s\n", pub, priv)
		return nil
	case "sign":
		return cartridgeSign(args[1:])
	default:
		return fmt.Errorf("unknown cartridge subcommand %q (list|search|install|remove|registry|trust|keygen|sign|build|where)", sub)
	}
}

func cartridgeTrust(args []string) error {
	sub := "list"
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "add":
		if len(args) < 2 {
			return fmt.Errorf("usage: sahayak cartridge trust add <publisher-public-key>")
		}
		if err := cartridge.AddTrustedKey(args[1]); err != nil {
			return err
		}
		fmt.Println("trusted publisher key added.")
		return nil
	case "list":
		keys, err := cartridge.TrustedKeys()
		if err != nil {
			return err
		}
		if len(keys) == 0 {
			fmt.Println("no trusted keys — add a publisher's key: sahayak cartridge trust add <pubkey>")
			return nil
		}
		fmt.Printf("Trusted publisher keys (%d):\n", len(keys))
		for _, k := range keys {
			fmt.Printf("  %s\n", k)
		}
		return nil
	default:
		return fmt.Errorf("unknown trust subcommand %q (add | list)", sub)
	}
}

// cartridgeSign signs a cartridge file with a private key and prints the base64 signature
// to paste into the registry index entry's "signature" field. Arg order is flexible
// (the file and `--key <path>` may appear in any order).
func cartridgeSign(args []string) error {
	keyFile, file := "", ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--key", "-key":
			if i+1 < len(args) {
				keyFile = args[i+1]
				i++
			}
		default:
			if file == "" {
				file = args[i]
			}
		}
	}
	if file == "" || keyFile == "" {
		return fmt.Errorf("usage: sahayak cartridge sign <cartridge.json> --key <privkey-file>")
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	priv, err := os.ReadFile(keyFile)
	if err != nil {
		return err
	}
	sig, err := cartridge.Sign(data, strings.TrimSpace(string(priv)))
	if err != nil {
		return err
	}
	fmt.Printf("%s\n", sig)
	return nil
}

// cartridgeInstall installs from a registry NAME, or a local file / https URL. A bare
// name (no slash, no .json, not http) is resolved via the configured registries with
// checksum verification; otherwise the arg is treated as a direct path/URL.
func cartridgeInstall(arg string) error {
	isDirect := strings.ContainsAny(arg, "/\\") || strings.HasSuffix(arg, ".json") ||
		strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://")
	if isDirect {
		name, err := cartridge.Install(arg)
		if err != nil {
			return err
		}
		fmt.Printf("installed cartridge %q (from %s)\n", name, arg)
		return nil
	}
	c, note, err := cartridge.InstallByName(arg, os.Getenv("SAHAYAK_REQUIRE_SIGNED") == "1")
	if err != nil {
		return err
	}
	fmt.Printf("installed cartridge %q from the registry — %s\n", c.Name, note)
	reviewCartridge(c)
	return nil
}

// reviewCartridge prints what a freshly-installed cartridge can RUN, so the operator sees
// the commands/risk a downloaded cartridge introduces (it runs commands — this is the
// supply-chain review surface).
func reviewCartridge(c cartridge.Cartridge) {
	if len(c.Templates) == 0 {
		return
	}
	fmt.Println("  this cartridge can run:")
	for _, t := range c.Templates {
		risk := t.Risk
		if risk == "" {
			risk = "read-only"
		}
		fmt.Printf("    - %-10s %s (%s)\n", t.Intent, t.Command, risk)
	}
}

func cartridgeSearch(args []string) error {
	q := ""
	if len(args) > 0 {
		q = strings.Join(args, " ")
	}
	hits, errs := cartridge.Search(q)
	for _, e := range errs {
		fmt.Printf("  ⚠ %v\n", e)
	}
	if len(hits) == 0 {
		fmt.Println("no matches (add a registry: sahayak cartridge registry add <url>)")
		return nil
	}
	fmt.Printf("Found %d cartridge(s):\n", len(hits))
	for _, e := range hits {
		fmt.Printf("  %-12s %-8s %s\n", e.Name, e.Version, e.Description)
	}
	fmt.Println("\ninstall one with: sahayak cartridge install <name>")
	return nil
}

func cartridgeRegistry(args []string) error {
	sub := "list"
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "add":
		if len(args) < 2 {
			return fmt.Errorf("usage: sahayak cartridge registry add <url | path>")
		}
		if err := cartridge.AddRegistry(args[1]); err != nil {
			return err
		}
		fmt.Printf("added registry: %s\n", args[1])
		return nil
	case "list":
		srcs, err := cartridge.Registries()
		if err != nil {
			return err
		}
		if len(srcs) == 0 {
			fmt.Println("no registries configured — add one: sahayak cartridge registry add <url>")
			return nil
		}
		fmt.Printf("Registries (%d):\n", len(srcs))
		for _, s := range srcs {
			fmt.Printf("  %s\n", s)
		}
		return nil
	default:
		return fmt.Errorf("unknown registry subcommand %q (add | list)", sub)
	}
}

// cartridgeBuild authors a cartridge package from content: a how-to markdown doc becomes
// the cartridge's KB (chunked), and an optional templates JSON file supplies its commands.
// The result is one self-describing cartridge (commands + knowledge) ready to install or
// publish — `sahayak cartridge build --name redis --kb redis-howto.md --templates redis.json`.
func cartridgeBuild(args []string) error {
	fs := flag.NewFlagSet("cartridge build", flag.ContinueOnError)
	name := fs.String("name", "", "cartridge name (required)")
	kb := fs.String("kb", "", "markdown/text how-to doc to fold in as knowledge")
	tmpl := fs.String("templates", "", "JSON file: an array of command templates (optional)")
	probe := fs.String("command", "", "applicability probe, e.g. \"redis-cli ping\" (optional)")
	fromPack := fs.String("from-pack", "", "fold an installed knowledge pack's chunks into the KB")
	out := fs.String("o", "", "output cartridge .json (default <name>.json)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" {
		return fmt.Errorf("cartridge build: --name is required")
	}

	var md string
	if *kb != "" {
		b, err := os.ReadFile(*kb)
		if err != nil {
			return fmt.Errorf("reading --kb: %w", err)
		}
		md = string(b)
	}
	var templates []cartridge.Template
	if *tmpl != "" {
		b, err := os.ReadFile(*tmpl)
		if err != nil {
			return fmt.Errorf("reading --templates: %w", err)
		}
		if err := json.Unmarshal(b, &templates); err != nil {
			return fmt.Errorf("parsing --templates (want a JSON array of templates): %w", err)
		}
	}
	var packChunks []string
	if *fromPack != "" {
		packs, err := knowledge.NewStore("").LoadAll()
		if err != nil {
			return fmt.Errorf("loading packs: %w", err)
		}
		found := false
		for _, p := range packs {
			if p.Manifest.Name == *fromPack {
				found = true
				for _, ch := range p.Chunks {
					packChunks = append(packChunks, ch.Text)
				}
			}
		}
		if !found {
			return fmt.Errorf("no installed knowledge pack named %q (see `sahayak knowledge list`)", *fromPack)
		}
	}
	if md == "" && len(templates) == 0 && len(packChunks) == 0 {
		return fmt.Errorf("cartridge build: provide --kb, --templates, and/or --from-pack (a cartridge needs content)")
	}

	// Catalog phrasings come from each template's intent name as a starting point; the
	// author refines them. (A template with no catalog still routes via its own phrasings
	// once added.)
	var catalog []cartridge.CatalogEntry
	for _, t := range templates {
		catalog = append(catalog, cartridge.CatalogEntry{Intent: t.Intent, Phrases: []string{t.Intent + " X"}})
	}

	c := cartridge.Compose(*name, *probe, templates, catalog, md)
	c.Knowledge = append(c.Knowledge, packChunks...) // fold in an existing knowledge pack
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	path := *out
	if path == "" {
		path = *name + ".json"
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	fmt.Printf("built cartridge %q → %s  (%d templates, %d KB chunks)\n", c.Name, path, len(c.Templates), len(c.Knowledge))
	fmt.Println("next: review the catalog phrasings, then `sahayak cartridge install " + path + "`")
	return nil
}

func cartridgeList() error {
	carts, err := cartridge.LoadAll()
	if err != nil {
		return err
	}
	installed, errs := cartridge.LoadInstalled()
	installedNames := map[string]bool{}
	for _, c := range installed {
		installedNames[c.Name] = true
	}
	fmt.Printf("Cartridges (%d):\n", len(carts))
	for _, c := range carts {
		origin := "built-in"
		if installedNames[c.Name] {
			origin = "installed"
		}
		fmt.Printf("  %-12s %-10s %d templates, %d catalog intents\n", c.Name, origin, len(c.Templates), len(c.Catalog))
	}
	for _, e := range errs {
		fmt.Printf("  ⚠ skipped: %v\n", e)
	}
	return nil
}
