package main

import (
	"ImagePipeline/opensearch"
	"ImagePipeline/uploads"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

const env string = "dev"

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// --- Uploads pipeline (S3 + Lambda) ---
		fn, err := uploads.CreateOnCreateLambda(ctx, env)
		if err != nil {
			return err
		}

		bucket, err := uploads.CreateUploadsBucket(ctx, env, fn)
		if err != nil {
			return err
		}

		ctx.Export("bucketName", bucket.ID())
		ctx.Export("lambdaFunctionName", fn.Name)

		// --- OpenSearch on EC2 (free-tier, Docker-based) ---
		os, err := opensearch.Deploy(ctx, env)
		if err != nil {
			return err
		}

		// Export the raw IP so it can be used in other tools (e.g. curl, Postman).
		ctx.Export("opensearchPublicIp", os.Instance.PublicIp)

		// Export the full HTTP endpoint using pulumi.Sprintf so that the
		// Output[string] dependency graph is preserved — Pulumi resolves the
		// public IP only after the instance is created.
		ctx.Export("opensearchEndpoint", pulumi.Sprintf("http://%s:9200", os.Instance.PublicIp))

		return nil
	})
}
