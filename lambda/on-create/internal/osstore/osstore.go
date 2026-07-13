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

	// indexMapping creates the images index with k-NN enabled and image_vector
	// explicitly typed as knn_vector. This MUST be applied before any documents
	// are indexed; otherwise OpenSearch's dynamic mapping will infer image_vector
	// as a plain float array and k-NN queries will fail with a 400 error.
	indexMapping = `{
		"settings": {
			"index": {
				"knn": true,
				"knn.algo_param.ef_search": 100
			}
		},
		"mappings": {
			"properties": {
				"image_key":   { "type": "keyword" },
				"labels":      { "type": "keyword" },
				"categories": { "type": "keyword" },
				"parents":     { "type": "keyword" },
				"image_vector": {
					"type":      "knn_vector",
					"dimension": 1024
				}
			}
		}
	}`
)

var (
	endpoint   string
	httpClient *http.Client
)

func Init(osEndpoint string) {
	endpoint = strings.TrimRight(osEndpoint, "/")
	httpClient = &http.Client{Timeout: requestTimeout}
}

// EnsureIndex creates the images index with the correct k-NN mapping if it
// does not already exist. It is idempotent: a 200/OK on HEAD means the index
// is already present and no action is taken.
func EnsureIndex(ctx context.Context) error {
	indexURL := fmt.Sprintf("%s/%s", endpoint, indexName)

	// Check whether the index exists.
	headReq, err := http.NewRequestWithContext(ctx, http.MethodHead, indexURL, nil)
	if err != nil {
		return fmt.Errorf("build HEAD request for index check: %w", err)
	}
	headResp, err := httpClient.Do(headReq)
	if err != nil {
		return fmt.Errorf("HEAD %s: %w", indexURL, err)
	}
	headResp.Body.Close()

	if headResp.StatusCode == http.StatusOK {
		// Index already exists; nothing to do.
		return nil
	}

	if headResp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("unexpected status checking index existence: %s", headResp.Status)
	}

	// Create the index with the k-NN mapping.
	putReq, err := http.NewRequestWithContext(ctx, http.MethodPut, indexURL, bytes.NewBufferString(indexMapping))
	if err != nil {
		return fmt.Errorf("build PUT request for index creation: %w", err)
	}
	putReq.Header.Set("Content-Type", "application/json")

	putResp, err := httpClient.Do(putReq)
	if err != nil {
		return fmt.Errorf("PUT %s: %w", indexURL, err)
	}
	defer putResp.Body.Close()

	if putResp.StatusCode != http.StatusOK && putResp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(io.LimitReader(putResp.Body, 4096))
		return fmt.Errorf("create index returned %s: %s", putResp.Status, raw)
	}

	return nil
}

type Record struct {
	ImageKey    string    `json:"image_key"`
	Labels      []string  `json:"labels"`
	Categories  []string  `json:"categories"`
	Parents     []string  `json:"parents"`
	ImageVector []float64 `json:"image_vector,omitempty"`
}

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

type openSearchSearchResponse struct {
	Hits struct {
		Hits []struct {
			Source Record  `json:"_source"`
			Score  float64 `json:"_score"`
		} `json:"hits"`
	} `json:"hits"`
}

func IndexRecord(ctx context.Context, rec Record) error {
	docID := sanitiseID(rec.ImageKey)
	url := fmt.Sprintf("%s/%s/_doc/%s", endpoint, indexName, docID)

	body, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal record for %q: %w", rec.ImageKey, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build OpenSearch request for %q: %w", rec.ImageKey, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("PUT %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("OpenSearch returned %s for %q: %s", resp.Status, rec.ImageKey, raw)
	}

	return nil
}

func SearchByVector(ctx context.Context, vector []float64, limit int) ([]Record, error) {
	url := fmt.Sprintf("%s/%s/_search", endpoint, indexName)

	var q knnQuery
	q.Size = limit
	q.Query.Knn.ImageVector.Vector = vector
	q.Query.Knn.ImageVector.K = limit

	body, err := json.Marshal(q)
	if err != nil {
		return nil, fmt.Errorf("marshal search query: %w", err)
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
		return nil, fmt.Errorf("OpenSearch search returned %s: %s", resp.Status, raw)
	}

	var osResp openSearchSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&osResp); err != nil {
		return nil, fmt.Errorf("decode OpenSearch search response: %w", err)
	}

	records := make([]Record, len(osResp.Hits.Hits))
	for i, hit := range osResp.Hits.Hits {
		records[i] = hit.Source
	}

	return records, nil
}

func sanitiseID(key string) string {
	if idx := strings.LastIndex(key, "."); idx != -1 {
		key = key[:idx]
	}
	return strings.ReplaceAll(key, "/", "_")
}
