// Package embed turns text into vectors, via a local ollama.
//
// Local and not hosted, deliberately: the corpora coroner is built for are
// personal writing, and a design that ships every paragraph of it to a third
// party to be embedded is the wrong default even when the vendor is reputable.
// Ollama runs on the same machine, so the text never leaves it.
//
// There is no fallback. If ollama is not answering, digesting and searching both
// stop with an error that says how to fix it. A silent degrade to keyword-only
// search would be worse than useless here: the same query would return different
// results depending on whether a daemon happened to be running, which is exactly
// the class of quiet wrongness the rest of this tool is built to avoid.
package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultBaseURL is where ollama listens unless told otherwise.
const DefaultBaseURL = "http://localhost:11434"

// DefaultModel is the embedding model used when the config names none.
//
// nomic-embed-text: 768 dimensions, an 8k context that comfortably swallows any
// chunk this tool produces, and small enough to run on a laptop without a GPU.
const DefaultModel = "nomic-embed-text"

// BatchSize is how many texts go to ollama in one request.
//
// A constant rather than something derived from how much work is left, and that
// is a determinism decision rather than a performance one. A forward pass over a
// batch can accumulate floating point differently at different batch sizes, so
// batching that varies with the size of the job would mean the same chunk
// embedded slightly differently depending on how many other chunks happened to
// be digested alongside it. Fixed size plus the sorted chunk order upstream
// means the batches are a function of the corpus and nothing else.
const BatchSize = 16

// Client talks to ollama.
type Client struct {
	BaseURL string
	Model   string
	HTTP    *http.Client
}

// New builds a client, filling in defaults for empty values.
func New(baseURL, model string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if model == "" {
		model = DefaultModel
	}

	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Model:   model,
		// Generous: a cold model has to load off disk before the first
		// embedding returns, which on a large model is tens of seconds.
		HTTP: &http.Client{Timeout: 5 * time.Minute},
	}
}

// ModelInfo is what a probe learned about the model.
type ModelInfo struct {
	// Name is the model as ollama reports it, which may carry a ":latest" the
	// caller did not type.
	Name string

	// Digest is ollama's content hash. Recorded in the digest manifest so that
	// a model changing underneath a corpus is caught rather than silently
	// mixing incomparable vectors.
	Digest string
}

// Probe checks ollama is reachable and the model is present, and returns what
// it can learn about the model.
//
// Called before any real work, so that a corpus that will fail to embed fails
// before it is walked and parsed rather than after.
func (c *Client) Probe(ctx context.Context) (ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/tags", nil)
	if err != nil {
		return ModelInfo{}, err
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return ModelInfo{}, c.unreachable(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ModelInfo{}, fmt.Errorf("ollama answered %s at %s/api/tags", resp.Status, c.BaseURL)
	}

	var tags struct {
		Models []struct {
			Name   string `json:"name"`
			Model  string `json:"model"`
			Digest string `json:"digest"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return ModelInfo{}, fmt.Errorf("reading the ollama model list: %w", err)
	}

	// Ollama normalises a bare name to "name:latest", so both spellings have to
	// match or a config saying "nomic-embed-text" would never find the model it
	// just pulled.
	for _, m := range tags.Models {
		for _, candidate := range []string{m.Name, m.Model} {
			if candidate == c.Model || strings.TrimSuffix(candidate, ":latest") == c.Model {
				return ModelInfo{Name: m.Name, Digest: m.Digest}, nil
			}
		}
	}

	var have []string
	for _, m := range tags.Models {
		have = append(have, m.Name)
	}

	msg := fmt.Sprintf("ollama is running but does not have the model %q; pull it with:\n    ollama pull %s", c.Model, c.Model)
	if len(have) > 0 {
		msg += "\n  models it does have: " + strings.Join(have, ", ")
	}
	return ModelInfo{}, errors.New(msg)
}

// Embed returns one vector per input text, in the same order.
//
// Requests are issued one batch at a time rather than concurrently. Ollama
// serialises them internally anyway, so parallel requests would buy little, and
// they would make the order in which work reaches the model depend on
// scheduling. Determinism is worth more here than a few seconds.
func (c *Client) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))

	for start := 0; start < len(texts); start += BatchSize {
		end := min(start+BatchSize, len(texts))

		vecs, err := c.embedBatch(ctx, texts[start:end])
		if err != nil {
			return nil, err
		}
		if len(vecs) != end-start {
			return nil, fmt.Errorf("asked ollama for %d embeddings and got %d", end-start, len(vecs))
		}

		out = append(out, vecs...)
	}

	return out, nil
}

// EmbedOne is the query path: a single text, one request.
func (c *Client) EmbedOne(ctx context.Context, text string) ([]float32, error) {
	vecs, err := c.Embed(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vecs) != 1 {
		return nil, fmt.Errorf("asked ollama for 1 embedding and got %d", len(vecs))
	}
	return vecs[0], nil
}

func (c *Client) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	body, err := json.Marshal(map[string]any{
		"model": c.Model,
		"input": texts,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, c.unreachable(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("ollama answered %s: %s", resp.Status, strings.TrimSpace(string(detail)))
	}

	var out struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("reading the ollama embedding response: %w", err)
	}

	if len(out.Embeddings) == 0 {
		return nil, fmt.Errorf("ollama returned no embeddings; is %q an embedding model rather than a chat model?", c.Model)
	}

	return out.Embeddings, nil
}

// unreachable turns a transport failure into something actionable. A bare
// "connection refused" against a URL is technically complete and practically
// useless when the fix is one command.
func (c *Client) unreachable(err error) error {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Errorf("ollama at %s timed out; a model loading for the first time can take a while, but this took longer than five minutes: %w", c.BaseURL, err)
	}

	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return fmt.Errorf("cannot reach ollama at %s.\n"+
			"  Coroner needs it running to embed text, and has no fallback on purpose:\n"+
			"  keyword-only results that silently differ from vector results would be\n"+
			"  worse than an error.\n"+
			"    ollama serve\n"+
			"    ollama pull %s\n"+
			"  If ollama listens somewhere else, set embed.base_url in your config.",
			c.BaseURL, c.Model)
	}

	return err
}
