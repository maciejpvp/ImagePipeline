package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"log/slog"
	"os"
	"strings"

	_ "image/png"

	_ "golang.org/x/image/webp"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/rekognition"
	"github.com/aws/aws-sdk-go-v2/service/rekognition/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

var (
	rekognitionClient *rekognition.Client
	s3Client          *s3.Client
)

func init() {
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		slog.Error("Failed to load AWS SDK config", "error", err)
		os.Exit(1)
	}
	rekognitionClient = rekognition.NewFromConfig(cfg)
	s3Client = s3.NewFromConfig(cfg)
}

// convertToJPEGBytes downloads an image from S3 and converts it to a JPEG byte slice entirely in memory.
func convertToJPEGBytes(ctx context.Context, bucket, key string) ([]byte, error) {
	s3Obj, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &bucket,
		Key:    &key,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch from S3: %w", err)
	}
	defer s3Obj.Body.Close()

	img, _, err := image.Decode(s3Obj.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to decode image format: %w", err)
	}

	var buffer bytes.Buffer
	err = jpeg.Encode(&buffer, img, &jpeg.Options{Quality: 85})
	if err != nil {
		return nil, fmt.Errorf("failed to encode image to JPEG: %w", err)
	}

	return buffer.Bytes(), nil
}

func handler(ctx context.Context, s3Event events.S3Event) error {
	slog.Info("TESTTT")
	for _, record := range s3Event.Records {
		bucket := record.S3.Bucket.Name
		key := record.S3.Object.Key
		lowerKey := strings.ToLower(key)

		slog.Info("Received S3 event",
			"event_name", record.EventName,
			"bucket_name", bucket,
			"object_key", key,
		)

		var imageBytes []byte
		isNativeJPG := strings.HasSuffix(lowerKey, ".jpg") || strings.HasSuffix(lowerKey, ".jpeg")

		if isNativeJPG {
			slog.Info("Native JPEG detected, skipping conversion step", "key", key)
		} else {
			slog.Info("Non-JPEG detected, executing in-memory conversion", "key", key)
			convertedBytes, err := convertToJPEGBytes(ctx, bucket, key)
			if err != nil {
				slog.Error("Conversion pipeline failed", "key", key, "error", err)
				continue
			}
			imageBytes = convertedBytes
		}

		input := &rekognition.DetectModerationLabelsInput{}
		if isNativeJPG {
			input.Image = &types.Image{
				S3Object: &types.S3Object{
					Bucket: &bucket,
					Name:   &key,
				},
			}
		} else {
			input.Image = &types.Image{
				Bytes: imageBytes,
			}
		}

		output, err := rekognitionClient.DetectModerationLabels(ctx, input)
		if err != nil {
			slog.Error("Failed to detect moderation labels", "key", key, "error", err)
			continue
		}

		if len(output.ModerationLabels) > 0 {
			slog.Warn("Inappropriate content detected! Flagging image...", "key", key)
			for _, label := range output.ModerationLabels {
				slog.Warn("Moderation Label Found",
					"name", *label.Name,
					"parent", *label.ParentName,
					"confidence", *label.Confidence,
				)
			}
		} else {
			slog.Info("Image passed moderation check", "key", key)
		}
	}
	return nil
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	lambda.Start(handler)
}
