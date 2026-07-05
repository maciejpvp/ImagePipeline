// Package recognition owns the AWS Rekognition client and the moderation
// policy evaluation logic. It must be initialised once via Init before any
// other function is called.
package recognition

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/rekognition"
	"github.com/aws/aws-sdk-go-v2/service/rekognition/types"
)

var client *rekognition.Client

func Init(cfg aws.Config) {
	client = rekognition.NewFromConfig(cfg)
}

type ModerationRule struct {
	Name          string  `json:"name"`
	Action        string  `json:"action"` // "delete" | "flag" | "allow"
	MinConfidence float64 `json:"min_confidence"`
}

type ModerationConfig struct {
	Rules         []ModerationRule `json:"rules"`
	DefaultAction string           `json:"default_action"`
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
	},
	DefaultAction: "allow",
}

func LoadModerationConfig() ModerationConfig {
	raw := os.Getenv("MODERATION_CONFIG")
	if raw == "" {
		slog.Info("No MODERATION_CONFIG env var set, using default Pinterest-like policy")
		return DefaultModerationConfig
	}

	var cfg ModerationConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		slog.Error("Failed to parse MODERATION_CONFIG, falling back to default Pinterest-like policy", "error", err)
		return DefaultModerationConfig
	}

	return normalize(cfg)
}

type Result struct {
	Action  string // "delete" | "allow"
	Deletes []types.ModerationLabel
}



// ---------------------------------------------------------------------------
// Detect — public entry point
// ---------------------------------------------------------------------------

// Detect calls DetectModerationLabels for the given image, evaluates the
// active moderation policy, and returns a Result.
//
// If isNativeJPG is true the image is referenced directly via its S3
// coordinates; otherwise imageBytes (a pre-converted JPEG) is sent inline.
func Detect(ctx context.Context, cfg ModerationConfig, bucket, key string, imageBytes []byte, isNativeJPG bool) (Result, error) {
	input := buildInput(bucket, key, imageBytes, isNativeJPG)

	output, err := client.DetectModerationLabels(ctx, input)
	if err != nil {
		return Result{}, fmt.Errorf("DetectModerationLabels failed for %s/%s: %w", bucket, key, err)
	}

	action, deletes := overallAction(cfg, output.ModerationLabels)
	return Result{Action: action, Deletes: deletes}, nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func normalize(cfg ModerationConfig) ModerationConfig {
	if cfg.DefaultAction == "" {
		cfg.DefaultAction = "allow"
	}
	for i := range cfg.Rules {
		normalizeRule(&cfg.Rules[i])
	}
	return cfg
}

func normalizeRule(r *ModerationRule) {
	r.Action = strings.ToLower(r.Action)
	if r.MinConfidence <= 0 {
		r.MinConfidence = 80.0
	}
}

func labelProps(l types.ModerationLabel) (string, float64) {
	name := ""
	if l.Name != nil {
		name = *l.Name
	}
	conf := 0.0
	if l.Confidence != nil {
		conf = float64(*l.Confidence)
	}
	return name, conf
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

func evaluateAction(cfg ModerationConfig, labelName string, parentName *string, confidence float64) string {
	for _, rule := range cfg.Rules {
		if ruleMatches(rule, labelName, parentName, confidence) {
			return rule.Action
		}
	}
	return ""
}

func labelAction(cfg ModerationConfig, l types.ModerationLabel) string {
	name, conf := labelProps(l)
	action := evaluateAction(cfg, name, l.ParentName, conf)
	if action != "" {
		return action
	}
	if conf >= 80.0 {
		return cfg.DefaultAction
	}
	return ""
}

func overallAction(cfg ModerationConfig, labels []types.ModerationLabel) (string, []types.ModerationLabel) {
	var deletes []types.ModerationLabel
	for _, l := range labels {
		if labelAction(cfg, l) == "delete" {
			deletes = append(deletes, l)
		}
	}
	if len(deletes) > 0 {
		return "delete", deletes
	}
	return "allow", nil
}

func buildInput(bucket, key string, imageBytes []byte, isNativeJPG bool) *rekognition.DetectModerationLabelsInput {
	if isNativeJPG {
		s3Obj := &types.S3Object{Bucket: &bucket, Name: &key}
		return &rekognition.DetectModerationLabelsInput{Image: &types.Image{S3Object: s3Obj}}
	}
	return &rekognition.DetectModerationLabelsInput{Image: &types.Image{Bytes: imageBytes}}
}
