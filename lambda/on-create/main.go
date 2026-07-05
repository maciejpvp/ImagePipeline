package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"

	"lambda-on-create/internal/recognition"
	"lambda-on-create/internal/s3store"
)

type App struct {
	moderationCfg recognition.ModerationConfig
}

func (a *App) Handler(ctx context.Context, s3Event events.S3Event) error {
	var errs []error
	for _, record := range s3Event.Records {
		if err := a.processRecord(ctx, record); err != nil {
			slog.Error("Failed to process record",
				"bucket", record.S3.Bucket.Name,
				"key", record.S3.Object.Key,
				"error", err,
			)
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (a *App) processRecord(ctx context.Context, record events.S3EventRecord) error {
	bucket := record.S3.Bucket.Name
	key := record.S3.Object.Key
	lowerKey := strings.ToLower(key)

	slog.Info("Received S3 event", "event_name", record.EventName, "bucket_name", bucket, "object_key", key)

	isNativeJPG := strings.HasSuffix(lowerKey, ".jpg") || strings.HasSuffix(lowerKey, ".jpeg")

	var imageBytes []byte
	if !isNativeJPG {
		slog.Info("Non-JPEG detected, executing in-memory conversion", "key", key)
		var err error
		imageBytes, err = s3store.GetJPEGBytes(ctx, bucket, key)
		if err != nil {
			return fmt.Errorf("get JPEG bytes for %q: %w", key, err)
		}
	} else {
		slog.Info("Native JPEG detected, skipping conversion step", "key", key)
	}

	result, err := recognition.Detect(ctx, a.moderationCfg, bucket, key, imageBytes, isNativeJPG)
	if err != nil {
		return fmt.Errorf("detect moderation labels for %q: %w", key, err)
	}

	if result.Action == "delete" {
		if err := s3store.Delete(ctx, bucket, key); err != nil {
			return fmt.Errorf("delete inappropriate image %q: %w", key, err)
		}
		slog.Info("Deleted inappropriate image", "bucket", bucket, "key", key)
		return nil
	}
	slog.Info("Image passed moderation check", "key", key)

	return nil
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	ctx := context.Background()

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		slog.Error("Failed to load AWS SDK config", "error", err)
		os.Exit(1)
	}

	recognition.Init(cfg)
	s3store.Init(cfg)

	app := &App{
		moderationCfg: recognition.LoadModerationConfig(),
	}

	lambda.Start(app.Handler)
}
