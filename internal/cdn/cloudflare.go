package cdn

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

// InvalidateCloudflare purges the Release files from Cloudflare's cache for
// the given zone and domain.
func InvalidateCloudflare(ctx context.Context, zoneID string, domain string, suite string) error {
	apiToken := os.Getenv("CLOUDFLARE_API_TOKEN")
	if apiToken == "" {
		return fmt.Errorf("CLOUDFLARE_API_TOKEN environment variable is required for Cloudflare cache invalidation")
	}

	files := []string{
		fmt.Sprintf("https://%s/dists/%s/Release", domain, suite),
		fmt.Sprintf("https://%s/dists/%s/Release.gpg", domain, suite),
		fmt.Sprintf("https://%s/dists/%s/InRelease", domain, suite),
	}

	body, err := json.Marshal(map[string][]string{"files": files})
	if err != nil {
		return fmt.Errorf("marshaling purge request: %w", err)
	}

	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/purge_cache", zoneID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating purge request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("purging Cloudflare cache: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Cloudflare purge failed with status %d", resp.StatusCode)
	}

	return nil
}
