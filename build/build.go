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
)

// BuildLambda builds the Go binary for the Lambda function.
func BuildLambda() error {
	fmt.Println(">>> Building Lambda binary...")

	absOutputDir, err := filepath.Abs(LambdaOutputDir)
	if err != nil {
		return fmt.Errorf("failed to get absolute path for output dir: %w", err)
	}

	cmd := exec.Command("go", "build", "-o", absOutputDir, "./")
	cmd.Dir = LambdaSrcDir
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

	fmt.Println(">>> Lambda binary built successfully.")
	return nil
}
