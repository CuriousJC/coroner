package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/curiousjc/coroner/internal/embed"
)

// Starter renders the config that `coroner initconfig` writes.
//
// coroner.example.yaml at the repo root is a committed copy of this output, and
// TestExampleConfigMatchesGenerator is the only thing keeping the two in sync.
// Change this and regenerate the example, or that test fails with the command to
// do it.
func Starter() string {
	var b strings.Builder

	b.WriteString("# coroner config\n")
	b.WriteString("#\n")
	b.WriteString("# Everything here is optional, and every value can be overridden by typing\n")
	b.WriteString("# the equivalent flag. Precedence is: explicit flag, then this file, then\n")
	b.WriteString("# the built-in default.\n")
	b.WriteString("\n")
	b.WriteString("defaults:\n")
	b.WriteString("  # Where your source directories live. Each one holds an export and a\n")
	b.WriteString("  # corpus.yaml describing it. `coroner digest` with no -source digests\n")
	b.WriteString("  # every corpus found under here.\n")
	b.WriteString("  source: source\n")
	b.WriteString("\n")
	b.WriteString("  # Where the searchable corpus is written. One shared directory: every\n")
	b.WriteString("  # source digests into it and search reads across all of them at once.\n")
	b.WriteString("  digested: digested\n")
	b.WriteString("\n")
	b.WriteString("  # How many results a search returns.\n")
	b.WriteString("  # hits: 10\n")
	b.WriteString("\n")
	b.WriteString("  # How many files are parsed concurrently while digesting.\n")
	b.WriteString("  # workers: 8\n")
	b.WriteString("\n")
	b.WriteString("  # verbose: false\n")
	b.WriteString("\n")
	b.WriteString("embed:\n")
	b.WriteString("  # Coroner embeds locally, through ollama, so your writing never leaves\n")
	b.WriteString("  # the machine. There is no hosted fallback and no keyword-only fallback:\n")
	b.WriteString("  # if ollama is not answering, digest and search both stop and say so.\n")
	b.WriteString("  #\n")
	b.WriteString("  #   ollama serve\n")
	fmt.Fprintf(&b, "  #   ollama pull %s\n", embed.DefaultModel)
	b.WriteString("  #\n")
	fmt.Fprintf(&b, "  # base_url: %s\n", embed.DefaultBaseURL)
	b.WriteString("\n")
	b.WriteString("  # The embedding model. Changing this invalidates every vector already\n")
	b.WriteString("  # stored: embeddings from two different models are not comparable, so\n")
	b.WriteString("  # coroner refuses to search a corpus built with a different one rather\n")
	b.WriteString("  # than quietly return nonsense. Changing it means re-digesting\n")
	b.WriteString("  # everything.\n")
	fmt.Fprintf(&b, "  # model: %s\n", embed.DefaultModel)

	return b.String()
}

// WriteStarter writes a starter config, refusing to overwrite an existing one.
func WriteStarter(path string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists; delete it first if you meant to start over", path)
	}
	return os.WriteFile(path, []byte(Starter()), 0644)
}
