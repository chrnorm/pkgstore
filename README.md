# pkgstore

Publish signed APT repositories to S3.

pkgstore takes `.deb` files, generates the APT repository metadata (Packages, Release, InRelease), GPG-signs everything, and uploads it to an S3-compatible bucket.

Works with AWS S3, Cloudflare R2, Google Cloud Storage, MinIO, and anything else that speaks the S3 API. No server required — your APT repo is just files in a bucket.

## Usage

### GitHub Action

See [chrnorm/pkgstore-action](https://github.com/chrnorm/pkgstore-action).

```yaml
- uses: chrnorm/pkgstore-action@v1
  with:
    deb: my-package_1.0.0_amd64.deb
    bucket: my-apt-repo
    origin: "My Project"
    gpg-key: ${{ secrets.GPG_PRIVATE_KEY }}
  env:
    AWS_ACCESS_KEY_ID: ${{ secrets.AWS_ACCESS_KEY_ID }}
    AWS_SECRET_ACCESS_KEY: ${{ secrets.AWS_SECRET_ACCESS_KEY }}
    AWS_REGION: us-east-1
```

### CLI

```sh
pkgstore publish \
  --deb my-package_1.0.0_amd64.deb \
  --bucket my-apt-repo \
  --distribution stable \
  --component main \
  --origin "My Project" \
  --gpg-key @/path/to/private.key
```

The GPG key can be passed inline, via the `PKGSTORE_GPG_KEY` env var, or as a file path prefixed with `@`.

### Pruning old metadata

Over time, `by-hash` entries accumulate. Clean them up with:

```sh
pkgstore prune --bucket my-apt-repo --older-than 24h
```

### CDN cache invalidation

If you front your bucket with a CDN, pkgstore can automatically invalidate the Release files after publishing. Pass the flag for your provider:

```sh
# AWS CloudFront
pkgstore publish --deb foo.deb --bucket my-repo \
  --aws-cloudfront-distribution-id E1234567890

# Cloudflare
pkgstore publish --deb foo.deb --bucket my-repo \
  --endpoint https://acct.r2.cloudflarestorage.com \
  --cloudflare-zone-id abc123 --cloudflare-domain apt.example.com
```

See [CDN cache invalidation](https://github.com/chrnorm/pkgstore-action#cdn-cache-invalidation) in the action README for GitHub Actions examples.

## Setting up the APT repository on S3

1. Create an S3-compatible bucket with public read access (or front it with a CDN)
2. Upload your GPG public key somewhere users can fetch it
3. Run `pkgstore publish` to push your first packages

Users add your repo with:

```sh
curl -fsSL https://your-domain/gpg.key | sudo gpg --dearmor -o /usr/share/keyrings/your-repo.gpg

echo "deb [signed-by=/usr/share/keyrings/your-repo.gpg] https://your-domain stable main" \
  | sudo tee /etc/apt/sources.list.d/your-repo.list

sudo apt update
```

## Building from source

```sh
go build ./cmd/pkgstore
```

## License

MIT
