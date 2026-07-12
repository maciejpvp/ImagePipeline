// Package s3store owns the AWS S3 client and exposes high-level operations
// used by the on-create Lambda. It must be initialised once via Init before
// any other function is called.
package s3store

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"io"

	_ "image/png"

	_ "golang.org/x/image/webp"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

var client *s3.Client

func Init(cfg aws.Config) {
	client = s3.NewFromConfig(cfg)
}

func GetJPEGBytes(ctx context.Context, bucket, key string) ([]byte, error) {
	obj, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &bucket,
		Key:    &key,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch s3://%s/%s: %w", bucket, key, err)
	}
	defer obj.Body.Close()

	img, _, err := image.Decode(obj.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		return nil, fmt.Errorf("failed to encode image to JPEG: %w", err)
	}

	return buf.Bytes(), nil
}

func Delete(ctx context.Context, bucket, key string) error {
	_, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: &bucket,
		Key:    &key,
	})
	if err != nil {
		return fmt.Errorf("failed to delete s3://%s/%s: %w", bucket, key, err)
	}
	return nil
}

func GetRawBytes(ctx context.Context, bucket, key string) ([]byte, error) {
	obj, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &bucket,
		Key:    &key,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch s3://%s/%s: %w", bucket, key, err)
	}
	defer obj.Body.Close()

	data, err := io.ReadAll(obj.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read object body: %w", err)
	}
	return data, nil
}

