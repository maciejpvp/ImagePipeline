package build

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	LambdaSrcDir    = "./lambda/on-create"
	LambdaOutputDir = "./lambda/on-create/bootstrap"

	SearchLambdaSrcDir    = "./lambda/search"
	SearchLambdaOutputDir = "./lambda/search/bootstrap"
)

// BuildLambda builds the Go binary for the on-create Lambda function.
func BuildLambda() error {
	return buildGoLambda(LambdaSrcDir, LambdaOutputDir)
}

// BuildSearchLambda builds the Go binary for the search Lambda function.
func BuildSearchLambda() error {
	return buildGoLambda(SearchLambdaSrcDir, SearchLambdaOutputDir)
}

func buildGoLambda(srcDir, outputDir string) error {
	fmt.Printf(">>> Building Lambda binary for %s...\n", srcDir)

	absOutputDir, err := filepath.Abs(outputDir)
	if err != nil {
		return fmt.Errorf("failed to get absolute path for output dir: %w", err)
	}

	cmd := exec.Command("go", "build", "-o", absOutputDir, "./")
	cmd.Dir = srcDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(),
		"GOOS=linux",
		"GOARCH=amd64",
		"CGO_ENABLED=0",
	)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("lambda build failed: %w", err)
	}

	fmt.Printf(">>> Lambda binary for %s built successfully.\n", srcDir)
	return nil
}
