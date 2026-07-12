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

func Init(osEndpoint string) {
	endpoint = strings.TrimRight(osEndpoint, "/")
	httpClient = &http.Client{Timeout: requestTimeout}
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
