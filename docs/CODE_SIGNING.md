# Code Signing Policy

Free code signing provided by [SignPath.io](https://signpath.io/), certificate
by [SignPath Foundation](https://signpath.org/).

## What gets signed

- Windows executable: `video-compressor.exe`

The executable is built from this repository by GitHub Actions, uploaded as a
workflow artifact, and submitted to SignPath for Authenticode signing. The
signed file is attached to the corresponding GitHub Release.

## How to verify

1. Right-click `video-compressor.exe` → **Properties** → **Digital Signatures**.
2. The signer should be **SignPath Foundation**.
3. Or in PowerShell:

   ```powershell
   Get-AuthenticodeSignature .\video-compressor.exe | Format-List
   ```

   `Status` should be `Valid`, and `SignerCertificate.Subject` should contain
   `SignPath Foundation`.

## Build pipeline

- GitHub Actions: <https://github.com/FuzzyHD1993/video-compressor/actions>
- Workflow: [`.github/workflows/release.yml`](../.github/workflows/release.yml)
- Source: <https://github.com/FuzzyHD1993/video-compressor>
- Build environment: GitHub-hosted `ubuntu-latest` runners
- FFmpeg: gyan.dev release essentials (NVENC), checksum verified in CI

The signing job is skipped when the SignPath secrets are not configured. In
that case the release contains an unsigned executable and the workflow still
succeeds.

## Maintainer setup (one-time)

These steps require access to the GitHub repository and the SignPath dashboard.

1. Apply at <https://signpath.org/apply> and wait for approval.
2. Create/verify the SignPath project (default slug: `video-compressor`),
   signing policy (default slug: `release-signing`) and the artifact
   configuration. If no artifact configuration slug is set in the workflow,
   SignPath uses the project's default artifact configuration.
3. Add repository secrets under **Settings → Secrets and variables → Actions**:

   | Secret | Value |
   |---|---|
   | `SIGNPATH_API_TOKEN` | API token from SignPath |
   | `SIGNPATH_ORGANIZATION_ID` | SignPath organization ID |

4. Optional repository variables (override the defaults baked into the
   workflow):

   | Variable | Default |
   |---|---|
   | `SIGNPATH_PROJECT_SLUG` | `video-compressor` |
   | `SIGNPATH_SIGNING_POLICY_SLUG` | `release-signing` |

5. Push a test tag to verify the pipeline:

   ```bash
   git tag -a v1.1.1-test -m "Test signed build"
   git push origin v1.1.1-test
   ```

   Check the **Release** workflow: `Build Windows exe` → `Sign Windows exe
   (SignPath)` → `Publish GitHub release`. Download the asset and run
   `Get-AuthenticodeSignature`.

## Limitations

- Without `SIGNPATH_API_TOKEN` and `SIGNPATH_ORGANIZATION_ID`, signing is
  skipped and the released `.exe` is unsigned.
- SmartScreen reputation builds over time; a valid signature does not
  guarantee an immediate SmartScreen warning-free experience.
- The workflow signs only `video-compressor.exe`; the Docker image and Linux
  binaries are not code-signed.
