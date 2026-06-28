package main

import (
	"ImagePipeline/uploads"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

const env string = "dev"

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
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
		return nil
	})
}
