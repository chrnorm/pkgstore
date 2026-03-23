package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/chrnorm/pkgstore/internal/cdn"
	"github.com/chrnorm/pkgstore/internal/prune"
	"github.com/chrnorm/pkgstore/internal/publish"
	"github.com/chrnorm/pkgstore/internal/storage"
	"github.com/urfave/cli/v2"
)

func main() {
	app := &cli.App{
		Name:  "pkgstore",
		Usage: "Publish packages to serverless APT repositories on S3",
		Commands: []*cli.Command{
			publishCommand(),
			pruneCommand(),
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

func publishCommand() *cli.Command {
	return &cli.Command{
		Name:  "publish",
		Usage: "Publish .deb packages to the APT repository",
		Flags: []cli.Flag{
			&cli.StringSliceFlag{
				Name:     "deb",
				Usage:    "Path to a .deb file (can be specified multiple times)",
				Required: true,
			},
			&cli.StringFlag{
				Name:     "bucket",
				Usage:    "S3 bucket name (without s3:// prefix)",
				Required: true,
			},
			&cli.StringFlag{
				Name:  "distribution",
				Usage: "APT distribution/suite name",
				Value: "stable",
			},
			&cli.StringFlag{
				Name:  "component",
				Usage: "APT component name",
				Value: "main",
			},
			&cli.StringFlag{
				Name:  "origin",
				Usage: "Origin field for Release file",
			},
			&cli.StringFlag{
				Name:  "label",
				Usage: "Label field for Release file",
			},
			&cli.StringFlag{
				Name:  "description",
				Usage: "Description field for Release file",
			},
			&cli.StringFlag{
				Name:    "gpg-key",
				Usage:   "ASCII-armored GPG private key for signing (or path to file prefixed with @)",
				EnvVars: []string{"PKGSTORE_GPG_KEY"},
			},
			&cli.StringFlag{
				Name:    "gpg-passphrase",
				Usage:   "Passphrase for the GPG key",
				EnvVars: []string{"PKGSTORE_GPG_PASSPHRASE"},
			},
			&cli.StringFlag{
				Name:  "endpoint",
				Usage: "S3-compatible endpoint URL (for non-AWS stores like R2, GCS, MinIO)",
			},
			&cli.StringFlag{
				Name:  "region",
				Usage: "AWS region for S3 bucket",
			},
			&cli.StringFlag{
				Name:    "aws-cloudfront-distribution-id",
				Usage:   "CloudFront distribution ID to invalidate after publish",
				EnvVars: []string{"PKGSTORE_AWS_CLOUDFRONT_DISTRIBUTION_ID"},
			},
			&cli.StringFlag{
				Name:    "cloudflare-zone-id",
				Usage:   "Cloudflare zone ID to purge after publish (requires CLOUDFLARE_API_TOKEN env var)",
				EnvVars: []string{"PKGSTORE_CLOUDFLARE_ZONE_ID"},
			},
			&cli.StringFlag{
				Name:    "cloudflare-domain",
				Usage:   "Public domain for Cloudflare cache purge URLs (required with --cloudflare-zone-id)",
				EnvVars: []string{"PKGSTORE_CLOUDFLARE_DOMAIN"},
			},
		},
		Action: func(c *cli.Context) error {
			return runPublish(c)
		},
	}
}

func pruneCommand() *cli.Command {
	return &cli.Command{
		Name:  "prune",
		Usage: "Remove old by-hash entries from the APT repository",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "bucket",
				Usage:    "S3 bucket name (without s3:// prefix)",
				Required: true,
			},
			&cli.StringFlag{
				Name:  "distribution",
				Usage: "APT distribution/suite name",
				Value: "stable",
			},
			&cli.StringFlag{
				Name:  "component",
				Usage: "APT component name",
				Value: "main",
			},
			&cli.DurationFlag{
				Name:  "older-than",
				Usage: "Delete by-hash entries older than this duration (e.g. 1h, 24h, 7d)",
				Value: 1 * time.Hour,
			},
			&cli.StringFlag{
				Name:  "endpoint",
				Usage: "S3-compatible endpoint URL (for non-AWS stores like R2, GCS, MinIO)",
			},
			&cli.StringFlag{
				Name:  "region",
				Usage: "AWS region for S3 bucket",
			},
		},
		Action: func(c *cli.Context) error {
			return runPrune(c)
		},
	}
}

func runPublish(c *cli.Context) error {
	ctx := context.Background()

	store, cfg, err := makeS3Store(ctx, c)
	if err != nil {
		return err
	}

	// Load GPG key.
	gpgKey := c.String("gpg-key")
	if gpgKey != "" && gpgKey[0] == '@' {
		keyBytes, err := os.ReadFile(gpgKey[1:])
		if err != nil {
			return fmt.Errorf("reading GPG key file: %w", err)
		}
		gpgKey = string(keyBytes)
	}

	opts := publish.Options{
		DebPaths:      c.StringSlice("deb"),
		Distribution:  c.String("distribution"),
		Component:     c.String("component"),
		Origin:        c.String("origin"),
		Label:         c.String("label"),
		Description:   c.String("description"),
		GPGPrivateKey: gpgKey,
		GPGPassphrase: c.String("gpg-passphrase"),
	}

	result, err := publish.Publish(ctx, store, opts)
	if err != nil {
		return err
	}

	for _, pkg := range result.Packages {
		fmt.Printf("Published %s %s (%s) to %s\n", pkg.Package, pkg.Version, pkg.Architecture, pkg.PoolPath)
	}

	// CDN cache invalidation.
	if cfDistID := c.String("aws-cloudfront-distribution-id"); cfDistID != "" {
		cfClient := cloudfront.NewFromConfig(cfg)
		if err := cdn.InvalidateCloudFront(ctx, cfClient, cfDistID, opts.Distribution); err != nil {
			return err
		}
		fmt.Printf("Created CloudFront invalidation for distribution %s\n", cfDistID)
	}

	if cfZoneID := c.String("cloudflare-zone-id"); cfZoneID != "" {
		domain := c.String("cloudflare-domain")
		if domain == "" {
			return fmt.Errorf("--cloudflare-domain is required when using --cloudflare-zone-id")
		}
		if err := cdn.InvalidateCloudflare(ctx, cfZoneID, domain, opts.Distribution); err != nil {
			return err
		}
		fmt.Printf("Purged Cloudflare cache for zone %s\n", cfZoneID)
	}

	return nil
}

func runPrune(c *cli.Context) error {
	ctx := context.Background()

	store, _, err := makeS3Store(ctx, c)
	if err != nil {
		return err
	}

	opts := prune.Options{
		Distribution: c.String("distribution"),
		Component:    c.String("component"),
		OlderThan:    c.Duration("older-than"),
	}

	result, err := prune.Prune(ctx, store, opts)
	if err != nil {
		return err
	}

	if result.Deleted > 0 {
		fmt.Printf("Pruned %d old by-hash entries\n", result.Deleted)
	} else {
		fmt.Println("No entries to prune")
	}

	return nil
}

func makeS3Store(ctx context.Context, c *cli.Context) (*storage.S3, aws.Config, error) {
	var cfgOpts []func(*config.LoadOptions) error
	if region := c.String("region"); region != "" {
		cfgOpts = append(cfgOpts, config.WithRegion(region))
	}
	cfg, err := config.LoadDefaultConfig(ctx, cfgOpts...)
	if err != nil {
		return nil, cfg, fmt.Errorf("loading AWS config: %w", err)
	}

	var s3Opts []func(*awss3.Options)
	if endpoint := c.String("endpoint"); endpoint != "" {
		s3Opts = append(s3Opts, func(o *awss3.Options) {
			o.BaseEndpoint = &endpoint
			o.UsePathStyle = true
		})
	}

	s3Client := awss3.NewFromConfig(cfg, s3Opts...)
	store := &storage.S3{
		Client: s3Client,
		Bucket: c.String("bucket"),
	}

	return store, cfg, nil
}
