package cdn

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront"
	cftypes "github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
)

// InvalidateCloudFront creates a CloudFront invalidation for the Release files
// of the given distribution. Only Release, Release.gpg, and InRelease are
// invalidated — Packages and by-hash files are not, since by-hash paths are
// content-addressed and stale canonical Packages paths are harmless when
// Acquire-By-Hash is enabled.
func InvalidateCloudFront(ctx context.Context, client *cloudfront.Client, distributionID string, suite string) error {
	items := []string{
		fmt.Sprintf("/dists/%s/Release", suite),
		fmt.Sprintf("/dists/%s/Release.gpg", suite),
		fmt.Sprintf("/dists/%s/InRelease", suite),
	}

	_, err := client.CreateInvalidation(ctx, &cloudfront.CreateInvalidationInput{
		DistributionId: &distributionID,
		InvalidationBatch: &cftypes.InvalidationBatch{
			CallerReference: aws.String(fmt.Sprintf("pkgstore-%d", time.Now().UnixNano())),
			Paths: &cftypes.Paths{
				Quantity: aws.Int32(int32(len(items))),
				Items:    items,
			},
		},
	})
	if err != nil {
		return fmt.Errorf("creating CloudFront invalidation: %w", err)
	}
	return nil
}
