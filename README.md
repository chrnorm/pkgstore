# pkgstore

Publish signed APT repositories to S3.

pkgstore takes `.deb` files, generates the APT repository metadata (Packages, Release, InRelease), GPG-signs everything, and uploads it to an S3 bucket. Optionally invalidates CloudFront so clients pick up changes immediately.

No server required — your APT repo is just files on S3.

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

## Setting up the APT repository on S3

1. Create an S3 bucket with public read access (or use CloudFront with an OAI)
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
