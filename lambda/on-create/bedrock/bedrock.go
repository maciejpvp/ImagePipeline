package bedrock

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
)

// InvokeModelAPI defines the interface for the Bedrock Runtime InvokeModel API.
// This is used to mock the client during unit testing.
type InvokeModelAPI interface {
	InvokeModel(ctx context.Context, params *bedrockruntime.InvokeModelInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.InvokeModelOutput, error)
}

// bedrockClient is a package-level client variable that can be overridden in tests.
var bedrockClient InvokeModelAPI

// TitanRequest defines the input payload for the amazon.titan-embed-image-v1 model.
type TitanRequest struct {
	InputImage      string          `json:"inputImage"`
	EmbeddingConfig EmbeddingConfig `json:"embeddingConfig"`
}

// EmbeddingConfig defines the configuration for the embedding.
type EmbeddingConfig struct {
	OutputEmbeddingLength int `json:"outputEmbeddingLength"`
}

// TitanResponse defines the output payload from the amazon.titan-embed-image-v1 model.
type TitanResponse struct {
	Embedding       []float64 `json:"embedding"`
	InputTokenCount int       `json:"inputTokenCount"`
}

// GetImageEmbedding takes raw image bytes, base64-encodes them, and queries the Bedrock
// Titan Multimodal Embeddings model to generate a 1024-dimensional embedding vector.
func GetImageEmbedding(ctx context.Context, imageBytes []byte) ([]float64, error) {
	if len(imageBytes) == 0 {
		return nil, fmt.Errorf("image bytes cannot be empty")
	}

	client := bedrockClient
	if client == nil {
		cfg, err := config.LoadDefaultConfig(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to load default AWS config: %w", err)
		}

		// Fallback to us-east-1 if no region is resolved from the environment/config
		if cfg.Region == "" {
			cfg.Region = "us-east-1"
		}

		client = bedrockruntime.NewFromConfig(cfg)
	}

	base64Image := base64.StdEncoding.EncodeToString(imageBytes)

	reqPayload := TitanRequest{
		InputImage: base64Image,
		EmbeddingConfig: EmbeddingConfig{
			OutputEmbeddingLength: 1024,
		},
	}

	reqBytes, err := json.Marshal(reqPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Titan request: %w", err)
	}

	modelID := "amazon.titan-embed-image-v1"
	contentType := "application/json"

	output, err := client.InvokeModel(ctx, &bedrockruntime.InvokeModelInput{
		ModelId:     &modelID,
		ContentType: &contentType,
		Body:        reqBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to invoke model %s: %w", modelID, err)
	}

	var respPayload TitanResponse
	if err := json.Unmarshal(output.Body, &respPayload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal Titan response: %w", err)
	}

	if len(respPayload.Embedding) == 0 {
		return nil, fmt.Errorf("received empty embedding from model")
	}

	return respPayload.Embedding, nil
}
