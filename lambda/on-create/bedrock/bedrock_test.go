package bedrock

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
)

// mockBedrockClient implements the InvokeModelAPI interface for testing.
type mockBedrockClient struct {
	InvokeModelFunc func(ctx context.Context, params *bedrockruntime.InvokeModelInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.InvokeModelOutput, error)
}

func (m *mockBedrockClient) InvokeModel(ctx context.Context, params *bedrockruntime.InvokeModelInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.InvokeModelOutput, error) {
	if m.InvokeModelFunc != nil {
		return m.InvokeModelFunc(ctx, params, optFns...)
	}
	return nil, errors.New("unimplemented")
}

func TestGetImageEmbedding_EmptyBytes(t *testing.T) {
	_, err := GetImageEmbedding(context.Background(), nil)
	if err == nil {
		t.Error("expected error for empty image bytes, got nil")
	}
}

func TestGetImageEmbedding_Success(t *testing.T) {
	originalClient := bedrockClient
	defer func() { bedrockClient = originalClient }()

	imageBytes := []byte("fake-image-bytes")
	expectedEmbedding := []float64{0.1, -0.5, 0.99}

	mockClient := &mockBedrockClient{
		InvokeModelFunc: func(ctx context.Context, params *bedrockruntime.InvokeModelInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.InvokeModelOutput, error) {
			// Verify ModelId and ContentType
			if *params.ModelId != "amazon.titan-embed-image-v1" {
				t.Errorf("unexpected model id: %s", *params.ModelId)
			}
			if *params.ContentType != "application/json" {
				t.Errorf("unexpected content type: %s", *params.ContentType)
			}

			// Verify request body
			var req TitanRequest
			if err := json.Unmarshal(params.Body, &req); err != nil {
				t.Fatalf("failed to unmarshal request body in mock: %v", err)
			}

			expectedBase64 := base64.StdEncoding.EncodeToString(imageBytes)
			if req.InputImage != expectedBase64 {
				t.Errorf("expected base64 image %q, got %q", expectedBase64, req.InputImage)
			}
			if req.EmbeddingConfig.OutputEmbeddingLength != 1024 {
				t.Errorf("expected embedding length 1024, got %d", req.EmbeddingConfig.OutputEmbeddingLength)
			}

			// Create response body
			resp := TitanResponse{
				Embedding:       expectedEmbedding,
				InputTokenCount: 1,
			}
			respBytes, err := json.Marshal(resp)
			if err != nil {
				t.Fatalf("failed to marshal response body in mock: %v", err)
			}

			return &bedrockruntime.InvokeModelOutput{
				Body: respBytes,
			}, nil
		},
	}

	bedrockClient = mockClient

	result, err := GetImageEmbedding(context.Background(), imageBytes)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !reflect.DeepEqual(result, expectedEmbedding) {
		t.Errorf("expected embedding %v, got %v", expectedEmbedding, result)
	}
}

func TestGetImageEmbedding_InvokeError(t *testing.T) {
	originalClient := bedrockClient
	defer func() { bedrockClient = originalClient }()

	mockErr := errors.New("aws api error")
	mockClient := &mockBedrockClient{
		InvokeModelFunc: func(ctx context.Context, params *bedrockruntime.InvokeModelInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.InvokeModelOutput, error) {
			return nil, mockErr
		},
	}

	bedrockClient = mockClient

	_, err := GetImageEmbedding(context.Background(), []byte("fake-image"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !errors.Is(err, mockErr) && err.Error() == "" {
		t.Errorf("expected wrapped api error, got: %v", err)
	}
}

func TestGetImageEmbedding_UnmarshalError(t *testing.T) {
	originalClient := bedrockClient
	defer func() { bedrockClient = originalClient }()

	mockClient := &mockBedrockClient{
		InvokeModelFunc: func(ctx context.Context, params *bedrockruntime.InvokeModelInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.InvokeModelOutput, error) {
			return &bedrockruntime.InvokeModelOutput{
				Body: []byte("invalid json"),
			}, nil
		},
	}

	bedrockClient = mockClient

	_, err := GetImageEmbedding(context.Background(), []byte("fake-image"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
