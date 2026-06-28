package uploads

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

func applyRekognitionPolicy(ctx *pulumi.Context, roleName pulumi.StringInput) error {
	_, err := iam.NewRolePolicyAttachment(ctx, "lambdaRekognitionPolicy", &iam.RolePolicyAttachmentArgs{
		Role:      roleName,
		PolicyArn: pulumi.String("arn:aws:iam::aws:policy/AmazonRekognitionReadOnlyAccess"),
	})
	if err != nil {
		return err
	}
	return nil
}

func applyS3ReadPolicy(ctx *pulumi.Context, name string, roleName pulumi.StringInput) error {
	_, err := iam.NewRolePolicy(ctx, name, &iam.RolePolicyArgs{
		Role: roleName,
		Policy: pulumi.String(`{
			"Version": "2012-10-17",
			"Statement": [{
				"Effect": "Allow",
				"Action": [
					"s3:GetObject",
					"s3:GetObjectVersion"
				],
				"Resource": "arn:aws:s3:::uploads-bucket-*/*"
			}]
		}`),
	})
	return err
}

func CreateOnCreateLambda(ctx *pulumi.Context, env string) (*lambda.Function, error) {
	if err := build.BuildLambda(); err != nil {
		return nil, err
	}

	lambdaRole, err := createLambdaRole(ctx, "onCreateLambda")
	if err != nil {
		return nil, err
	}

	err = applyBasicExecutionPolicy(ctx, "lambdaLogPolicy", lambdaRole.Name)
	if err != nil {
		return nil, err
	}

	err = applyRekognitionPolicy(ctx, lambdaRole.Name)
	if err != nil {
		return nil, err
	}

	err = applyS3ReadPolicy(ctx, "lambdaS3ReadPolicy", lambdaRole.Name)
	if err != nil {
		return nil, err
	}

	fn, err := lambda.NewFunction(ctx, "onCreateLambda", &lambda.FunctionArgs{
		Runtime: pulumi.String("provided.al2023"),
		Role:    lambdaRole.Arn,
		Handler: pulumi.String("bootstrap"),
		Code: pulumi.NewAssetArchive(map[string]interface{}{
			"bootstrap": pulumi.NewFileAsset(build.LambdaOutputDir),
		}),
		MemorySize: pulumi.Int(128),
		Timeout:    pulumi.Int(10),
		Environment: &lambda.FunctionEnvironmentArgs{
			Variables: pulumi.StringMap{
				"env": pulumi.String(env),
			},
		},
	})
	if err != nil {
		return nil, err
	}

	_, err = applyCloudwatchLogGroup(ctx, "onCreateLamdaLogGroup", fn, 7)
	if err != nil {
		return nil, err
	}

	return fn, nil
}
