package storage

import (
	"context"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// S3 implements Storage using AWS S3.
type S3 struct {
	Client *s3.Client
	Bucket string
}

func (s *S3) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := s.Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &s.Bucket,
		Key:    &key,
	})
	if err != nil {
		// Check for NoSuchKey.
		var nsk *types.NoSuchKey
		if isNotFound(err) || asError(err, &nsk) {
			return nil, &ErrNotFound{Key: key}
		}
		return nil, err
	}
	return out.Body, nil
}

func (s *S3) Put(ctx context.Context, key string, body io.Reader, contentType string) error {
	input := &s3.PutObjectInput{
		Bucket: &s.Bucket,
		Key:    &key,
		Body:   body,
	}
	if contentType != "" {
		input.ContentType = &contentType
	}
	_, err := s.Client.PutObject(ctx, input)
	return err
}

func (s *S3) List(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	paginator := s3.NewListObjectsV2Paginator(s.Client, &s3.ListObjectsV2Input{
		Bucket: &s.Bucket,
		Prefix: &prefix,
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, obj := range page.Contents {
			keys = append(keys, aws.ToString(obj.Key))
		}
	}
	return keys, nil
}

func (s *S3) Delete(ctx context.Context, keys []string) error {
	if len(keys) == 0 {
		return nil
	}

	// S3 DeleteObjects supports up to 1000 keys per call.
	for i := 0; i < len(keys); i += 1000 {
		end := i + 1000
		if end > len(keys) {
			end = len(keys)
		}
		batch := keys[i:end]

		objects := make([]types.ObjectIdentifier, len(batch))
		for j, key := range batch {
			objects[j] = types.ObjectIdentifier{Key: aws.String(key)}
		}

		_, err := s.Client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: &s.Bucket,
			Delete: &types.Delete{Objects: objects},
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// isNotFound checks if the error message suggests a 404/not-found response.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "NoSuchKey") || strings.Contains(msg, "404") || strings.Contains(msg, "not found")
}

// asError is a helper for errors.As with a specific type.
func asError[T error](err error, target *T) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "NoSuchKey") // simplified check
}
