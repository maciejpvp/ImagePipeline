package main

import (
	"bytes"
	"context"
	"encoding/json"
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

type ModerationRule struct {
	Name          string  `json:"name"`
	Action        string  `json:"action"` // "delete", "flag", "allow"
	MinConfidence float64 `json:"min_confidence"`
}

type ModerationConfig struct {
	Rules         []ModerationRule `json:"rules"`
	DefaultAction string           `json:"default_action"` // "allow", "flag", "delete"
}

var DefaultModerationConfig = ModerationConfig{
	Rules: []ModerationRule{
		{Name: "Explicit Nudity", Action: "delete", MinConfidence: 80.0},
		{Name: "Non-Explicit Nudity of Intimate parts and Kissing", Action: "delete", MinConfidence: 80.0},
		{Name: "Non-Explicit Nudity", Action: "delete", MinConfidence: 80.0},
		{Name: "Violence", Action: "delete", MinConfidence: 80.0},
		{Name: "Graphic Violence or Gore", Action: "delete", MinConfidence: 80.0},
		{Name: "Drugs", Action: "delete", MinConfidence: 80.0},
		{Name: "Hate Symbols", Action: "delete", MinConfidence: 80.0},
		{Name: "Suggestive", Action: "flag", MinConfidence: 80.0},
		{Name: "Swimwear or Underwear", Action: "flag", MinConfidence: 80.0},
		{Name: "Visually Disturbing", Action: "flag", MinConfidence: 80.0},
		{Name: "Tobacco", Action: "flag", MinConfidence: 80.0},
		{Name: "Alcohol", Action: "flag", MinConfidence: 80.0},
	},
	DefaultAction: "allow",
}

func normalizeRule(rule *ModerationRule) {
	rule.Action = strings.ToLower(rule.Action)
	if rule.MinConfidence <= 0 {
		rule.MinConfidence = 80.0
	}
}

func normalizeConfig(config ModerationConfig) ModerationConfig {
	if config.DefaultAction == "" {
		config.DefaultAction = "allow"
	}
	for i := range config.Rules {
		normalizeRule(&config.Rules[i])
	}
	return config
}

func loadModerationConfig() ModerationConfig {
	configStr := os.Getenv("MODERATION_CONFIG")
	if configStr == "" {
		slog.Info("No MODERATION_CONFIG env var set, using default Pinterest-like policy")
		return DefaultModerationConfig
	}

	var config ModerationConfig
	if err := json.Unmarshal([]byte(configStr), &config); err != nil {
		slog.Error("Failed to parse MODERATION_CONFIG, falling back to default Pinterest-like policy", "error", err)
		return DefaultModerationConfig
	}

	return normalizeConfig(config)
}

func ruleMatches(rule ModerationRule, name string, parent *string, confidence float64) bool {
	if confidence < rule.MinConfidence {
		return false
	}
	if strings.EqualFold(rule.Name, name) {
		return true
	}
	return parent != nil && strings.EqualFold(rule.Name, *parent)
}

func evaluateAction(config ModerationConfig, labelName string, parentName *string, confidence float64) string {
	for _, rule := range config.Rules {
		if ruleMatches(rule, labelName, parentName, confidence) {
			return rule.Action
		}
	}
	return ""
}

func getLabelProps(label types.ModerationLabel) (string, float64) {
	name := ""
	if label.Name != nil {
		name = *label.Name
	}
	conf := 0.0
	if label.Confidence != nil {
		conf = float64(*label.Confidence)
	}
	return name, conf
}

func getLabelAction(config ModerationConfig, label types.ModerationLabel) string {
	name, conf := getLabelProps(label)
	action := evaluateAction(config, name, label.ParentName, conf)
	if action != "" {
		return action
	}
	if conf >= 80.0 {
		return config.DefaultAction
	}
	return ""
}

func collectLabel(action string, label types.ModerationLabel, deletes, flags *[]types.ModerationLabel) {
	if action == "delete" {
		*deletes = append(*deletes, label)
	}
	if action == "flag" {
		*flags = append(*flags, label)
	}
}

func getOverallAction(config ModerationConfig, labels []types.ModerationLabel) (string, []types.ModerationLabel, []types.ModerationLabel) {
	var deleteLabels, flagLabels []types.ModerationLabel
	for _, label := range labels {
		action := getLabelAction(config, label)
		collectLabel(action, label, &deleteLabels, &flagLabels)
	}

	if len(deleteLabels) > 0 {
		return "delete", deleteLabels, flagLabels
	}
	if len(flagLabels) > 0 {
		return "flag", deleteLabels, flagLabels
	}
	return "allow", deleteLabels, flagLabels
}

func formatLabels(labels []types.ModerationLabel) []string {
	var result []string
	for _, label := range labels {
		name, conf := getLabelProps(label)
		result = append(result, fmt.Sprintf("%s (%.2f%%)", name, conf))
	}
	return result
}

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

func getRecordImageBytes(ctx context.Context, bucket, key string, isNativeJPG bool) ([]byte, error) {
	if isNativeJPG {
		slog.Info("Native JPEG detected, skipping conversion step", "key", key)
		return nil, nil
	}
	slog.Info("Non-JPEG detected, executing in-memory conversion", "key", key)
	return convertToJPEGBytes(ctx, bucket, key)
}

func buildDetectInput(bucket, key string, imageBytes []byte, isNativeJPG bool) *rekognition.DetectModerationLabelsInput {
	if isNativeJPG {
		s3Obj := &types.S3Object{Bucket: &bucket, Name: &key}
		img := &types.Image{S3Object: s3Obj}
		return &rekognition.DetectModerationLabelsInput{Image: img}
	}
	img := &types.Image{Bytes: imageBytes}
	return &rekognition.DetectModerationLabelsInput{Image: img}
}

func deleteInappropriateImage(ctx context.Context, bucket, key string, deletes []types.ModerationLabel) {
	slog.Warn("Inappropriate content detected! Deleting image...", "key", key, "deleted_by_labels", formatLabels(deletes))
	input := &s3.DeleteObjectInput{Bucket: &bucket, Key: &key}
	_, err := s3Client.DeleteObject(ctx, input)
	if err != nil {
		slog.Error("Failed to delete inappropriate image from S3", "bucket", bucket, "key", key, "error", err)
		return
	}
	slog.Info("Successfully deleted inappropriate image from S3", "bucket", bucket, "key", key)
}

func executeModerationAction(ctx context.Context, bucket, key, action string, deletes, flags []types.ModerationLabel) {
	if action == "delete" {
		deleteInappropriateImage(ctx, bucket, key, deletes)
		return
	}
	if action == "flag" {
		slog.Warn("Inappropriate content detected! Flagging image...", "key", key, "flagged_by_labels", formatLabels(flags))
		return
	}
	slog.Info("Image passed moderation check", "key", key)
}

func processRecord(ctx context.Context, record events.S3EventRecord, config ModerationConfig) {
	bucket := record.S3.Bucket.Name
	key := record.S3.Object.Key
	lowerKey := strings.ToLower(key)

	slog.Info("Received S3 event", "event_name", record.EventName, "bucket_name", bucket, "object_key", key)

	isNativeJPG := strings.HasSuffix(lowerKey, ".jpg") || strings.HasSuffix(lowerKey, ".jpeg")
	imageBytes, err := getRecordImageBytes(ctx, bucket, key, isNativeJPG)
	if err != nil {
		slog.Error("Failed to get image bytes", "key", key, "error", err)
		return
	}

	input := buildDetectInput(bucket, key, imageBytes, isNativeJPG)
	output, err := rekognitionClient.DetectModerationLabels(ctx, input)
	if err != nil {
		slog.Error("Failed to detect moderation labels", "key", key, "error", err)
		return
	}

	action, deletes, flags := getOverallAction(config, output.ModerationLabels)
	executeModerationAction(ctx, bucket, key, action, deletes, flags)
}

func handler(ctx context.Context, s3Event events.S3Event) error {
	config := loadModerationConfig()
	for _, record := range s3Event.Records {
		processRecord(ctx, record, config)
	}
	return nil
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	lambda.Start(handler)
}
