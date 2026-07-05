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

// ContentLabel is a single general-purpose image label returned by AnalyzeLabels.
// It is intentionally decoupled from the SDK type so callers (and tests) do not
// depend on the AWS package directly.
type ContentLabel struct {
	Name       string   `json:"name"`
	Confidence float64  `json:"confidence"`
	Parents    []string `json:"parents,omitempty"`
	Categories []string `json:"categories,omitempty"`
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
	input := &rekognition.DetectModerationLabelsInput{Image: buildImage(bucket, key, imageBytes, isNativeJPG)}

	output, err := client.DetectModerationLabels(ctx, input)
	if err != nil {
		return Result{}, fmt.Errorf("DetectModerationLabels failed for %s/%s: %w", bucket, key, err)
	}

	action, deletes := overallAction(cfg, output.ModerationLabels)
	return Result{Action: action, Deletes: deletes}, nil
}

const (
	detectLabelsMinConfidence float32 = 75.0
	detectLabelsMaxLabels     int32   = 10
)

func AnalyzeLabels(ctx context.Context, bucket, key string, imageBytes []byte, isNativeJPG bool) ([]ContentLabel, error) {
	minConf := detectLabelsMinConfidence
	maxLabels := detectLabelsMaxLabels
	input := &rekognition.DetectLabelsInput{
		Image:         buildImage(bucket, key, imageBytes, isNativeJPG),
		MinConfidence: &minConf,
		MaxLabels:     &maxLabels,
	}

	output, err := client.DetectLabels(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("DetectLabels failed for %s/%s: %w", bucket, key, err)
	}

	labels := make([]ContentLabel, 0, len(output.Labels))
	for _, l := range output.Labels {
		if l.Name == nil {
			continue
		}
		cl := ContentLabel{
			Name:       *l.Name,
			Parents:    extractParentNames(l.Parents),
			Categories: extractCategoryNames(l.Categories),
		}
		if l.Confidence != nil {
			cl.Confidence = float64(*l.Confidence)
		}
		labels = append(labels, cl)
	}
	return deduplicateLabels(labels), nil
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

// buildImage constructs the Rekognition Image value used by both Detect and
// AnalyzeLabels. When the source is a native JPEG it references the object
// directly via S3 coordinates; otherwise it sends the pre-converted bytes inline.
func buildImage(bucket, key string, imageBytes []byte, isNativeJPG bool) *types.Image {
	if isNativeJPG {
		return &types.Image{S3Object: &types.S3Object{Bucket: &bucket, Name: &key}}
	}
	return &types.Image{Bytes: imageBytes}
}

func extractParentNames(parents []types.Parent) []string {
	if len(parents) == 0 {
		return nil
	}
	names := make([]string, 0, len(parents))
	for _, p := range parents {
		if p.Name != nil {
			names = append(names, *p.Name)
		}
	}
	return names
}

func extractCategoryNames(categories []types.LabelCategory) []string {
	if len(categories) == 0 {
		return nil
	}
	names := make([]string, 0, len(categories))
	for _, c := range categories {
		if c.Name != nil {
			names = append(names, *c.Name)
		}
	}
	return names
}

// deduplicateLabels removes any label whose name appears as a parent of another label in the same slice.
func deduplicateLabels(labels []ContentLabel) []ContentLabel {
	parentNames := make(map[string]struct{})
	for _, l := range labels {
		for _, p := range l.Parents {
			parentNames[p] = struct{}{}
		}
	}

	out := labels[:0] // reuse the backing array; safe because we only shrink
	for _, l := range labels {
		if _, isParent := parentNames[l.Name]; !isParent {
			out = append(out, l)
		}
	}
	return out
}
