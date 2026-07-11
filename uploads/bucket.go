package uploads

import (
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/lambda"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/s3"
	"github.com/pulumi/pulumi-random/sdk/v4/go/random"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func CreateUploadsBucket(ctx *pulumi.Context, env string, onCreateLambda *lambda.Function) (*s3.Bucket, error) {
	baseBucketName := "uploads-bucket"

	randomSuffix, err := random.NewRandomId(ctx, "bucket-suffix", &random.RandomIdArgs{
		ByteLength: pulumi.Int(3),
	})
	if err != nil {
		return nil, err
	}

	fullBucketName := pulumi.Sprintf("%s-%s", baseBucketName, randomSuffix.Hex)

	bucket, err := s3.NewBucket(ctx, baseBucketName, &s3.BucketArgs{
		Bucket: fullBucketName,
		Tags: pulumi.StringMap{
			"Name":        fullBucketName,
			"Environment": pulumi.String(env),
		},
	})
	if err != nil {
		return nil, err
	}

	permission, err := lambda.NewPermission(ctx, "allowBucketToInvokeLambda", &lambda.PermissionArgs{
		Action:    pulumi.String("lambda:InvokeFunction"),
		Function:  onCreateLambda.Name,
		Principal: pulumi.String("s3.amazonaws.com"),
		SourceArn: bucket.Arn,
	})
	if err != nil {
		return nil, err
	}

	_, err = s3.NewBucketNotification(ctx, "bucketNotification", &s3.BucketNotificationArgs{
		Bucket: bucket.ID(),
		LambdaFunctions: s3.BucketNotificationLambdaFunctionArray{
			&s3.BucketNotificationLambdaFunctionArgs{
				LambdaFunctionArn: onCreateLambda.Arn,
				Events: pulumi.StringArray{
					pulumi.String("s3:ObjectCreated:*"),
				},
			},
		},
	}, pulumi.DependsOn([]pulumi.Resource{permission}))
	if err != nil {
		return nil, err
	}

	publicAccessBlock, err := s3.NewBucketPublicAccessBlock(ctx, "bucketPublicAccessBlock", &s3.BucketPublicAccessBlockArgs{
		Bucket:                bucket.ID(),
		BlockPublicAcls:       pulumi.Bool(true),
		BlockPublicPolicy:     pulumi.Bool(false),
		IgnorePublicAcls:      pulumi.Bool(true),
		RestrictPublicBuckets: pulumi.Bool(false),
	})
	if err != nil {
		return nil, err
	}

	_, err = s3.NewBucketPolicy(ctx, "bucketPolicy", &s3.BucketPolicyArgs{
		Bucket: bucket.ID(),
		Policy: pulumi.Sprintf(`{
			"Version": "2012-10-17",
			"Statement": [
				{
					"Sid": "PublicReadGetObject",
					"Effect": "Allow",
					"Principal": "*",
					"Action": "s3:GetObject",
					"Resource": "arn:aws:s3:::%s/*"
				}
			]
		}`, bucket.ID()),
	}, pulumi.DependsOn([]pulumi.Resource{publicAccessBlock}))
	if err != nil {
		return nil, err
	}

	return bucket, nil
}

