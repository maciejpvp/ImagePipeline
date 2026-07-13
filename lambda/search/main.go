package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"

	"lambda-search/internal/bedrock"
	"lambda-search/internal/osstore"
)

func handler(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	q := req.QueryStringParameters["q"]
	if q == "" {
		return jsonResponse(400, map[string]string{"error": "missing required query parameter: q"}), nil
	}

	slog.Info("Search request received", "q", q)
	fmt.Printf("Search Query Request: %+v\n", req)

	slog.Info("Generating text embedding via Bedrock", "q", q)
	vector, err := bedrock.GetTextEmbedding(ctx, q)
	if err != nil {
		slog.Error("Failed to generate text embedding", "error", err)
		return jsonResponse(502, map[string]string{"error": fmt.Sprintf("embedding error: %s", err.Error())}), nil
	}
	slog.Info("Embedding generated", "dimensions", len(vector))

	slog.Info("Searching OpenSearch with k-NN", "k", 20)
	results, err := osstore.SearchByVector(ctx, vector, 20)
	if err != nil {
		slog.Error("OpenSearch k-NN search failed", "error", err)
		return jsonResponse(502, map[string]string{"error": fmt.Sprintf("search error: %s", err.Error())}), nil
	}
	slog.Info("Search complete", "hits", len(results))

	return jsonResponse(200, results), nil
}

func jsonResponse(statusCode int, body any) events.APIGatewayProxyResponse {
	raw, err := json.Marshal(body)
	if err != nil {
		// Fallback — should never happen with the types we use
		raw = []byte(`{"error":"internal server error"}`)
		statusCode = 500
	}
	return events.APIGatewayProxyResponse{
		StatusCode: statusCode,
		Headers: map[string]string{
			"Content-Type":                 "application/json",
			"Access-Control-Allow-Origin":  "*",
			"Access-Control-Allow-Methods": "GET,OPTIONS",
			"Access-Control-Allow-Headers": "Content-Type",
		},
		Body: string(raw),
	}
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	ctx := context.Background()

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		slog.Error("Failed to load AWS SDK config", "error", err)
		os.Exit(1)
	}

	// Initialise Bedrock client (us-east-1 is where Titan is available)
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	bedrock.Init(cfg)

	osEndpoint := os.Getenv("OPENSEARCH_ENDPOINT")
	if osEndpoint == "" {
		slog.Error("OPENSEARCH_ENDPOINT env var is not set")
		os.Exit(1)
	}
	slog.Info("OpenSearch endpoint configured", "endpoint", osEndpoint)
	osstore.Init(osEndpoint)

	lambda.Start(handler)
}
