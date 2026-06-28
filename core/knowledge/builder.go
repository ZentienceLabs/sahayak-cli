package knowledge

import (
	"context"

	"github.com/ZentienceLabs/sahayak-cli/core/embed"
)

// Build embeds the given chunks with the embedder and assembles a Pack with a
// model-pinned manifest. Used by `sahayak knowledge build` (pack authoring) and by
// tests. Authoring is where embedding cost is paid — installs are then free.
func Build(ctx context.Context, name, version, source string, chunks []Chunk, e embed.Embedder) (Pack, error) {
	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = embedText(c)
	}
	vecs, err := e.EmbedBatch(ctx, texts)
	if err != nil {
		return Pack{}, err
	}
	return Pack{
		Manifest: Manifest{
			Name:         name,
			Version:      version,
			Source:       source,
			EmbedModelID: e.ID(),
			EmbedDim:     e.Dim(),
		},
		Chunks:  chunks,
		Vectors: vecs,
	}, nil
}

// embedText is the text actually embedded for a chunk: the command and section
// prefixed so lexical signal from those fields is captured too.
func embedText(c Chunk) string {
	prefix := ""
	if c.Command != "" {
		prefix += c.Command + " "
	}
	if c.Section != "" {
		prefix += c.Section + ": "
	}
	return prefix + c.Text
}
