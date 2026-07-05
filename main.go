package main

import (
	"ImagePipeline/opensearch"
	"ImagePipeline/uploads"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

const env string = "dev"

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// --- OpenSearch on EC2 (free-tier, Docker-based) ---
		// Deploy first so its public IP is available as an Output for the Lambda.
		os, err := opensearch.Deploy(ctx, env)
		if err != nil {
			return err
		}

		opensearchEndpoint := pulumi.Sprintf("http://%s:9200", os.Instance.PublicIp)

		// Export the raw IP so it can be used in other tools (e.g. curl, Postman).
		ctx.Export("opensearchPublicIp", os.Instance.PublicIp)
		ctx.Export("opensearchEndpoint", opensearchEndpoint)

		// --- Uploads pipeline (S3 + Lambda) ---
		fn, err := uploads.CreateOnCreateLambda(ctx, env, opensearchEndpoint)
		if err != nil {
			return err
		}

		bucket, err := uploads.CreateUploadsBucket(ctx, env, fn)
		if err != nil {
			return err
		}

		ctx.Export("bucketName", bucket.ID())
		ctx.Export("lambdaFunctionName", fn.Name)

		return nil
	})
}
