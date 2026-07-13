package bedrock

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
)

// InvokeModelAPI is the subset of the Bedrock Runtime client we need.
// Defined as an interface so it can be swapped out in tests.
type InvokeModelAPI interface {
	InvokeModel(ctx context.Context, params *bedrockruntime.InvokeModelInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.InvokeModelOutput, error)
}

var client InvokeModelAPI

// Init wires up the package-level Bedrock client using the supplied AWS config.
// Must be called once from main() before any handler invocation.
func Init(cfg aws.Config) {
	client = bedrockruntime.NewFromConfig(cfg)
}

// titanTextRequest is the payload sent to amazon.titan-embed-image-v1 for text input.
type titanTextRequest struct {
	InputText       string          `json:"inputText"`
	EmbeddingConfig embeddingConfig `json:"embeddingConfig"`
}

type embeddingConfig struct {
	OutputEmbeddingLength int `json:"outputEmbeddingLength"`
}

// titanResponse is the payload returned by amazon.titan-embed-image-v1.
type titanResponse struct {
	Embedding       []float64 `json:"embedding"`
	InputTokenCount int       `json:"inputTokenCount"`
}

const (
	modelID             = "amazon.titan-embed-image-v1"
	embeddingDimensions = 1024
)

// GetTextEmbedding converts a text string into a 1024-dimensional embedding vector
// using the Amazon Titan Multimodal Embeddings model. The vector lives in the same
// space as image embeddings generated during indexing, enabling cross-modal k-NN search.
func GetTextEmbedding(ctx context.Context, text string) ([]float64, error) {
	if text == "" {
		return nil, fmt.Errorf("query text cannot be empty")
	}

	reqPayload := titanTextRequest{
		InputText: text,
		EmbeddingConfig: embeddingConfig{
			OutputEmbeddingLength: embeddingDimensions,
		},
	}

	reqBytes, err := json.Marshal(reqPayload)
	if err != nil {
		return nil, fmt.Errorf("marshal Titan text request: %w", err)
	}

	contentType := "application/json"
	model := modelID
	output, err := client.InvokeModel(ctx, &bedrockruntime.InvokeModelInput{
		ModelId:     &model,
		ContentType: &contentType,
		Body:        reqBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("invoke Titan model for text embedding: %w", err)
	}

	var resp titanResponse
	if err := json.Unmarshal(output.Body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal Titan text response: %w", err)
	}

	if len(resp.Embedding) == 0 {
		return nil, fmt.Errorf("received empty embedding from model")
	}

	return resp.Embedding, nil
}
