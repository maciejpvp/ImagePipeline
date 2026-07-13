package search

import (
	"fmt"

	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/apigatewayv2"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/lambda"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// CreateApiGateway provisions an API Gateway HTTP API, routes, stages, and integration permissions.
func CreateApiGateway(ctx *pulumi.Context, env string, searchFn *lambda.Function) (*apigatewayv2.Api, *apigatewayv2.Stage, error) {
	api, err := apigatewayv2.NewApi(ctx, "searchApi", &apigatewayv2.ApiArgs{
		Name:         pulumi.String(fmt.Sprintf("search-api-%s", env)),
		ProtocolType: pulumi.String("HTTP"),
		CorsConfiguration: &apigatewayv2.ApiCorsConfigurationArgs{
			AllowOrigins: pulumi.StringArray{pulumi.String("*")},
			AllowMethods: pulumi.StringArray{pulumi.String("*")},
			AllowHeaders: pulumi.StringArray{pulumi.String("*")},
		},
	})
	if err != nil {
		return nil, nil, err
	}

	integration, err := apigatewayv2.NewIntegration(ctx, "searchApiIntegration", &apigatewayv2.IntegrationArgs{
		ApiId:                api.ID(),
		IntegrationType:      pulumi.String("AWS_PROXY"),
		IntegrationUri:       searchFn.Arn,
		PayloadFormatVersion: pulumi.String("2.0"),
	})
	if err != nil {
		return nil, nil, err
	}

	_, err = apigatewayv2.NewRoute(ctx, "searchRoute", &apigatewayv2.RouteArgs{
		ApiId:    api.ID(),
		RouteKey: pulumi.String("GET /search"),
		Target:   pulumi.Sprintf("integrations/%s", integration.ID()),
	})
	if err != nil {
		return nil, nil, err
	}

	stage, err := apigatewayv2.NewStage(ctx, "searchApiStage", &apigatewayv2.StageArgs{
		ApiId:      api.ID(),
		Name:       pulumi.String(env),
		AutoDeploy: pulumi.Bool(true),
	})
	if err != nil {
		return nil, nil, err
	}

	_, err = lambda.NewPermission(ctx, "apiGatewayPermission", &lambda.PermissionArgs{
		Action:    pulumi.String("lambda:InvokeFunction"),
		Function:  searchFn.Name,
		Principal: pulumi.String("apigateway.amazonaws.com"),
		SourceArn: pulumi.Sprintf("%s/*/*", api.ExecutionArn),
	})
	if err != nil {
		return nil, nil, err
	}

	return api, stage, nil
}
