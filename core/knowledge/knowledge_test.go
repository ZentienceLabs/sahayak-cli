package knowledge

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/ZentienceLabs/sahayak-cli/core/embed"
)

func sampleChunks() []Chunk {
	return []Chunk{
		{Text: "kubectl get pods lists pods; use --field-selector status.phase=Running to filter running pods", Command: "kubectl", Kind: KindFlag},
		{Text: "kubectl rollout restart deployment/<name> restarts a deployment by rolling its pods", Command: "kubectl", Kind: KindExample},
		{Text: "ImagePullBackOff means the kubelet could not pull the container image; check the image name and registry credentials", Command: "kubectl", Kind: KindError},
		{Text: "az aks get-credentials --resource-group RG --name CLUSTER merges cluster credentials into kubeconfig", Command: "az", Kind: KindExample},
	}
}

func TestPackRoundTrip(t *testing.T) {
	e := embed.NewHashEmbedder(128)
	pack, err := Build(context.Background(), "k8s", "1.31", "test docs", sampleChunks(), e)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := WritePack(&buf, pack); err != nil {
		t.Fatal(err)
	}
	got, err := ReadPack(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if got.Manifest.Name != "k8s" || got.Manifest.ChunkCount != 4 || got.Manifest.EmbedModelID != "hash-v1:128" {
		t.Fatalf("manifest wrong: %+v", got.Manifest)
	}
	if len(got.Vectors) != 4 || len(got.Vectors[0]) != 128 {
		t.Fatalf("vectors wrong: %d x %d", len(got.Vectors), len(got.Vectors[0]))
	}
}

func TestInstallModelMismatch(t *testing.T) {
	dir := t.TempDir()
	// Build with one embedder…
	pack, _ := Build(context.Background(), "k8s", "1", "", sampleChunks(), embed.NewHashEmbedder(256))
	path := dir + "/k8s.sahayakpack"
	writeToFile(t, path, pack)

	store := NewStore(dir + "/store")
	// …install with a DIFFERENT embedder → must fail.
	if _, err := store.Install(path, embed.NewHashEmbedder(128)); err == nil {
		t.Fatal("expected embed-model mismatch error, got nil")
	}
	// Matching embedder → succeeds.
	if _, err := store.Install(path, embed.NewHashEmbedder(256)); err != nil {
		t.Fatalf("matching install failed: %v", err)
	}
}

func TestHybridRetrieval(t *testing.T) {
	e := embed.NewHashEmbedder(256)
	pack, _ := Build(context.Background(), "k8s", "1", "", sampleChunks(), e)
	r := NewRetriever(e, []Pack{pack})
	if r.Empty() {
		t.Fatal("retriever empty")
	}
	// Literal token "ImagePullBackOff" must surface the error chunk via the keyword arm.
	hits, err := r.Search(context.Background(), "why does my pod show ImagePullBackOff", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0].Chunk.Kind != KindError {
		t.Fatalf("expected error chunk first, got %+v", topKinds(hits))
	}
}

func writeToFile(t *testing.T, path string, p Pack) {
	t.Helper()
	var buf bytes.Buffer
	if err := WritePack(&buf, p); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func topKinds(hits []Result) []Kind {
	var k []Kind
	for _, h := range hits {
		k = append(k, h.Chunk.Kind)
	}
	return k
}
