package storage

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
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
		if isS3NotFound(err) {
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

// ListWithModTime returns objects with their modification times.
func (s *S3) ListWithModTime(ctx context.Context, prefix string) ([]ObjectWithModTime, error) {
	var objects []ObjectWithModTime
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
			modTime := time.Time{}
			if obj.LastModified != nil {
				modTime = *obj.LastModified
			}
			objects = append(objects, ObjectWithModTime{
				Key:     aws.ToString(obj.Key),
				ModTime: modTime,
			})
		}
	}
	return objects, nil
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

// isS3NotFound checks if an error is an S3 "not found" error using proper
// AWS SDK type assertions.
func isS3NotFound(err error) bool {
	// Check for the specific NoSuchKey error type.
	var nsk *types.NoSuchKey
	if errors.As(err, &nsk) {
		return true
	}

	// Also check for HTTP 404 responses (some S3 operations return 404
	// without a typed error, e.g. HeadObject).
	var respErr *awshttp.ResponseError
	if errors.As(err, &respErr) {
		return respErr.HTTPStatusCode() == http.StatusNotFound
	}

	return false
}
