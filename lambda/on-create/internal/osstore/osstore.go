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
	ImageKey   string   `json:"image_key"`
	Labels     []string `json:"labels"`
	Categories []string `json:"categories"`
	Parents    []string `json:"parents"`
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

	// 200 OK  → updated existing document
	// 201 Created → new document
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("OpenSearch returned %s for %q: %s", resp.Status, rec.ImageKey, raw)
	}

	return nil
}

func sanitiseID(key string) string {
	// Strip extension: "d018e69e.webp" → "d018e69e"
	if idx := strings.LastIndex(key, "."); idx != -1 {
		key = key[:idx]
	}
	// Replace path separators that would confuse the REST URL.
	return strings.ReplaceAll(key, "/", "_")
}
