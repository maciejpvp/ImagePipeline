package osstore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	indexName      = "images"
	requestTimeout = 10 * time.Second
)

var (
	endpoint   string
	httpClient *http.Client
)

// Init configures the package-level OpenSearch endpoint and HTTP client.
// Must be called once from main() before any handler invocation.
func Init(osEndpoint string) {
	endpoint = strings.TrimRight(osEndpoint, "/")
	httpClient = &http.Client{Timeout: requestTimeout}
}

// SearchResult is a single image record returned from a k-NN search.
type SearchResult struct {
	ImageKey   string   `json:"image_key"`
	Labels     []string `json:"labels"`
	Categories []string `json:"categories"`
	Parents    []string `json:"parents"`
	Score      float64  `json:"score"`
}

// knnQuery is the OpenSearch k-NN query payload.
type knnQuery struct {
	Size  int `json:"size"`
	Query struct {
		Knn struct {
			ImageVector struct {
				Vector []float64 `json:"vector"`
				K      int       `json:"k"`
			} `json:"image_vector"`
		} `json:"knn"`
	} `json:"query"`
}

type osHit struct {
	Source struct {
		ImageKey   string   `json:"image_key"`
		Labels     []string `json:"labels"`
		Categories []string `json:"categories"`
		Parents    []string `json:"parents"`
	} `json:"_source"`
	Score float64 `json:"_score"`
}

type osSearchResponse struct {
	Hits struct {
		Hits []osHit `json:"hits"`
	} `json:"hits"`
}

// SearchByVector performs a k-NN search against the images index using the supplied
// embedding vector and returns up to `limit` results ranked by similarity score.
func SearchByVector(ctx context.Context, vector []float64, limit int) ([]SearchResult, error) {
	url := fmt.Sprintf("%s/%s/_search", endpoint, indexName)

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var q knnQuery
	q.Size = limit
	q.Query.Knn.ImageVector.Vector = vector
	q.Query.Knn.ImageVector.K = limit

	body, err := json.Marshal(q)
	if err != nil {
		return nil, fmt.Errorf("marshal k-NN query: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build OpenSearch search request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("POST %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("OpenSearch returned %s: %s", resp.Status, raw)
	}

	var osResp osSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&osResp); err != nil {
		return nil, fmt.Errorf("decode OpenSearch response: %w", err)
	}

	results := make([]SearchResult, len(osResp.Hits.Hits))
	for i, hit := range osResp.Hits.Hits {
		results[i] = SearchResult{
			ImageKey:   hit.Source.ImageKey,
			Labels:     hit.Source.Labels,
			Categories: hit.Source.Categories,
			Parents:    hit.Source.Parents,
			Score:      hit.Score,
		}
	}

	return results, nil
}
