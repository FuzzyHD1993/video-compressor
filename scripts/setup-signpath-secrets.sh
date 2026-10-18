#!/usr/bin/env bash
# Installs SignPath credentials as GitHub Actions secrets.
#
# Usage:
#   ./scripts/setup-signpath-secrets.sh /path/to/signpath.env
#   SIGNPATH_API_TOKEN=... SIGNPATH_ORGANIZATION_ID=... \
#     ./scripts/setup-signpath-secrets.sh
#
# The env file must contain:
#   SIGNPATH_API_TOKEN=...
#   SIGNPATH_ORGANIZATION_ID=...
#
# The secrets are sent to GitHub over the API and are never printed.
set -euo pipefail

REPO="${REPO:-FuzzyHD1993/video-compressor}"
ENV_FILE="${1:-}"

if [[ -n "$ENV_FILE" ]]; then
  [[ -f "$ENV_FILE" ]] || { echo "ERROR: file not found: $ENV_FILE" >&2; exit 1; }
  while IFS= read -r line || [[ -n "$line" ]]; do
    line="${line%$'\r'}"
    [[ -z "$line" || "$line" == \#* ]] && continue
    key="${line%%=*}"
    value="${line#*=}"
    case "$key" in
      SIGNPATH_API_TOKEN|SIGNPATH_ORGANIZATION_ID)
        # Tolerate quoted values copied from a password manager.
        value="${value%\"}"; value="${value#\"}"
        value="${value%\'}"; value="${value#\'}"
        export "$key=$value"
        ;;
    esac
  done < "$ENV_FILE"
fi

if [[ -z "${SIGNPATH_API_TOKEN:-}" ]]; then
  echo "ERROR: SIGNPATH_API_TOKEN is not set." >&2
  echo "Pass an env file or export SIGNPATH_API_TOKEN and SIGNPATH_ORGANIZATION_ID." >&2
  exit 1
fi
if [[ -z "${SIGNPATH_ORGANIZATION_ID:-}" ]]; then
  echo "ERROR: SIGNPATH_ORGANIZATION_ID is not set." >&2
  exit 1
fi

command -v gh >/dev/null 2>&1 || { echo "ERROR: gh CLI not found." >&2; exit 1; }
gh auth status >/dev/null 2>&1 || { echo "ERROR: gh is not authenticated." >&2; exit 1; }

printf '%s' "$SIGNPATH_API_TOKEN" | gh secret set SIGNPATH_API_TOKEN --repo "$REPO"
printf '%s' "$SIGNPATH_ORGANIZATION_ID" | gh secret set SIGNPATH_ORGANIZATION_ID --repo "$REPO"

unset SIGNPATH_API_TOKEN SIGNPATH_ORGANIZATION_ID

echo
echo "SignPath secrets configured for $REPO:"
gh secret list --repo "$REPO"
echo
echo "Create a test tag to verify signing:"
echo "  git tag -a v1.2.3-test -m 'Test signed build' && git push origin v1.2.3-test"
