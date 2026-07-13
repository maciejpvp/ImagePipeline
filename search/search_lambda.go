package search

import (
	"ImagePipeline/build"
	"fmt"

	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/cloudwatch"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/iam"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/lambda"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func createLambdaRole(ctx *pulumi.Context, name string) (*iam.Role, error) {
	lambdaRole, err := iam.NewRole(ctx, name, &iam.RoleArgs{
		AssumeRolePolicy: pulumi.String(`{
			"Version": "2012-10-17",
			"Statement": [{
				"Action": "sts:AssumeRole",
				"Principal": {
					"Service": "lambda.amazonaws.com"
				},
				"Effect": "Allow"
			}]
		}`),
	})
	if err != nil {
		return nil, err
	}
	return lambdaRole, nil
}

func applyBasicExecutionPolicy(ctx *pulumi.Context, name string, roleName pulumi.StringInput) error {
	_, err := iam.NewRolePolicyAttachment(ctx, name, &iam.RolePolicyAttachmentArgs{
		Role:      roleName,
		PolicyArn: pulumi.String("arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"),
	})
	return err
}

func applyCloudwatchLogGroup(ctx *pulumi.Context, name string, fn *lambda.Function, retentionDays int) (*cloudwatch.LogGroup, error) {
	logGroupName := fn.Name.ApplyT(func(lambdaName string) string {
		return fmt.Sprintf("/aws/lambda/%s", lambdaName)
	}).(pulumi.StringOutput)

	return cloudwatch.NewLogGroup(ctx, name, &cloudwatch.LogGroupArgs{
		Name:            logGroupName,
		RetentionInDays: pulumi.Int(retentionDays),
	})
}

func applyBedrockPolicy(ctx *pulumi.Context, name string, roleName pulumi.StringInput) error {
	_, err := iam.NewRolePolicy(ctx, name, &iam.RolePolicyArgs{
		Role: roleName,
		Policy: pulumi.String(`{
			"Version": "2012-10-17",
			"Statement": [{
				"Sid": "AllowTitanTextEmbeddings",
				"Effect": "Allow",
				"Action": "bedrock:InvokeModel",
				"Resource": "arn:aws:bedrock:us-east-1::foundation-model/amazon.titan-embed-image-v1"
			}]
		}`),
	})
	return err
}

// CreateSearchLambda builds the binary and deploys the AWS Lambda function resource.
func CreateSearchLambda(ctx *pulumi.Context, env string, opensearchEndpoint pulumi.StringInput) (*lambda.Function, error) {
	if err := build.BuildSearchLambda(); err != nil {
		return nil, err
	}

	lambdaRole, err := createLambdaRole(ctx, "searchLambda")
	if err != nil {
		return nil, err
	}

	err = applyBasicExecutionPolicy(ctx, "searchLambdaLogPolicy", lambdaRole.Name)
	if err != nil {
		return nil, err
	}

	err = applyBedrockPolicy(ctx, "searchLambdaBedrockPolicy", lambdaRole.Name)
	if err != nil {
		return nil, err
	}

	fn, err := lambda.NewFunction(ctx, "searchLambda", &lambda.FunctionArgs{
		Runtime:    pulumi.String("provided.al2023"),
		Role:       lambdaRole.Arn,
		Handler:    pulumi.String("bootstrap"),
		MemorySize: pulumi.Int(128),
		Timeout:    pulumi.Int(10),
		Code: pulumi.NewAssetArchive(map[string]interface{}{
			"bootstrap": pulumi.NewFileAsset(build.SearchLambdaOutputDir),
		}),
		Environment: &lambda.FunctionEnvironmentArgs{
			Variables: pulumi.StringMap{
				"env":                 pulumi.String(env),
				"OPENSEARCH_ENDPOINT": opensearchEndpoint,
			},
		},
	})
	if err != nil {
		return nil, err
	}

	_, err = applyCloudwatchLogGroup(ctx, "searchLambdaLogGroup", fn, 7)
	if err != nil {
		return nil, err
	}

	return fn, nil
}
