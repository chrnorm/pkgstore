#!/usr/bin/env bash
set -euo pipefail

# Determine version
if [[ -n "${INPUT_VERSION:-}" ]]; then
  VERSION="${INPUT_VERSION}"
else
  VERSION="$(cat "${GITHUB_ACTION_PATH}/VERSION")"
fi

# Map runner OS/arch to GoReleaser archive naming
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "${ARCH}" in
  x86_64)  ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
  arm64)   ARCH="arm64" ;;
  *)
    echo "::error::Unsupported architecture: ${ARCH}"
    exit 1
    ;;
esac

ARCHIVE_NAME="pkgstore_${VERSION}_${OS}_${ARCH}.tar.gz"
DOWNLOAD_URL="https://github.com/chrnorm/pkgstore/releases/download/v${VERSION}/${ARCHIVE_NAME}"

# Download
TMPDIR="$(mktemp -d)"
trap 'rm -rf "${TMPDIR}"' EXIT

echo "Downloading pkgstore v${VERSION} (${OS}/${ARCH})..."
curl -fsSL -o "${TMPDIR}/${ARCHIVE_NAME}" "${DOWNLOAD_URL}"

# Compute SHA256 of downloaded archive
if command -v sha256sum &>/dev/null; then
  ACTUAL_HASH="$(sha256sum "${TMPDIR}/${ARCHIVE_NAME}" | awk '{print $1}')"
else
  ACTUAL_HASH="$(shasum -a 256 "${TMPDIR}/${ARCHIVE_NAME}" | awk '{print $1}')"
fi

# Determine expected checksum
EXPECTED_HASH=""
if [[ -n "${INPUT_CHECKSUM:-}" ]]; then
  # Custom checksum provided — strip optional sha256: prefix
  EXPECTED_HASH="${INPUT_CHECKSUM#sha256:}"
elif [[ -z "${INPUT_VERSION:-}" ]]; then
  # Using baked-in version — look up from committed checksums.txt
  CHECKSUMS_FILE="${GITHUB_ACTION_PATH}/checksums.txt"
  if [[ -f "${CHECKSUMS_FILE}" ]]; then
    EXPECTED_HASH="$(grep "${ARCHIVE_NAME}" "${CHECKSUMS_FILE}" | awk '{print $1}')"
  fi
fi

# Verify
if [[ -n "${EXPECTED_HASH}" ]]; then
  if [[ "${ACTUAL_HASH}" != "${EXPECTED_HASH}" ]]; then
    echo "::error::Checksum mismatch for ${ARCHIVE_NAME}"
    echo "::error::Expected: ${EXPECTED_HASH}"
    echo "::error::Actual:   ${ACTUAL_HASH}"
    exit 1
  fi
  echo "Checksum verified: ${ACTUAL_HASH}"
elif [[ -n "${INPUT_VERSION:-}" ]]; then
  echo "::warning::No checksum provided for custom version v${VERSION}. Binary is unverified."
fi

# Extract and install
tar -xzf "${TMPDIR}/${ARCHIVE_NAME}" -C "${TMPDIR}"
chmod +x "${TMPDIR}/pkgstore"

INSTALL_DIR="${RUNNER_TOOL_CACHE:-${TMPDIR}}/pkgstore-${VERSION}"
mkdir -p "${INSTALL_DIR}"
mv "${TMPDIR}/pkgstore" "${INSTALL_DIR}/pkgstore"
echo "${INSTALL_DIR}" >> "${GITHUB_PATH}"

echo "pkgstore v${VERSION} installed."
