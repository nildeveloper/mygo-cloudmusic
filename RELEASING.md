# Releasing

## This fork

Release tags come from `main`, after the feature and version commits have been
merged there. Signing changes live on `features/macos-signing`.

The workflow uses the original macOS, Windows and Linux matrix (ARM64 and x64).
Forks remove upstream update and notarization configuration from the build copy
and upload the builder's packages to their own repository. macOS packages are
then signed with the fork's P12 before upload. These builds are not notarized and
do not support in-app automatic updates; download new versions from this fork's
Releases page. The release notes must state these limitations. Wait for every
build and checksum job to succeed before publishing. Do not deploy the upstream
website or check its update feed for a fork release.

### macOS P12 signing

Reuse the signing setup from
[paste-rs](https://github.com/nildeveloper/paste-rs/blob/main/.github/workflows/build.yml):
`rcodesign` 0.29.0, with its archive SHA-256 pinned in the workflow. Configure
these **repository Actions secrets** in this repository's Settings → Secrets
and variables → Actions (secrets in another repository are not inherited):

| Secret | Value |
| --- | --- |
| `MACOS_SIGNING_P12_BASE64` | Base64 of the P12 containing the signing certificate and private key |
| `MACOS_SIGNING_P12_PASSWORD` | The P12 export password |

On your Mac, copy the certificate data to the clipboard with
`base64 -i /path/to/certificate.p12 | tr -d '\n' | pbcopy`, then paste it into the
first secret. Keep using the same P12 across versions for a stable signing
identity. Neither the P12 nor its password belongs in the repository.

macOS releases require both secrets and fail if signing or verification fails;
there is no ad hoc fallback. Temporary credential files are owner-only and
removed even after a failed step. No keychain password, Apple ID or Apple team
secret is needed for this path. A self-signed P12 provides a stable identity but
does not provide Apple Developer ID trust or notarization.

MyGo first builds its normal DMGs. The workflow converts each to a writable
image, signs the app inside it, verifies the app signature and bundle identifier,
then compresses and signs the DMG. It remounts the final image read-only and
checks the app signature, signing certificate and target architecture again.
This preserves MyGo's Finder layout and volume icon while ensuring the shipped
app uses the P12. Both macOS architectures use
the same certificate. Windows/Linux and the upstream signing path are unchanged.

Use a **new version/tag** for the first signed release. Uploads intentionally
skip existing asset names, so rerunning an old release will not replace its
already-published ad hoc packages. `workflow_dispatch` checks out the version's
tag for application code; it can retry packaging that version after configuring
the secrets. The signing steps are kept inline so retrying an older tag does not
depend on a new script existing in that tag.

## Upstream release procedure

1. Commit pending work, bring main up to date with origin, and run `go test ./internal/...`. Others push to main too, so a release cut from a stale main leaves out their work:

   ```bash
   git pull --rebase origin main
   ```

2. Bump `version` in `mygo.json`, and add a `## 0.1.8` section at the top of `CHANGELOG.md` saying, in Chinese, what changed for people using the app since the last release (`git log v0.1.7..HEAD`, after the pull). Commit both in a commit titled just the version, e.g. `0.1.8`. MyGo's updater shows that section in the app's update window, and the release page takes it as its notes; `mygo build` fails without one.
3. Push main first, and tag only once that push has gone through. Pushing both at once still pushes the tag when main is rejected, and the tag starts a release build of a commit that isn't on main:

   ```bash
   git push origin main
   git tag v0.1.8
   git push origin v0.1.8
   ```

   If main is rejected, pull again, add what came in to the changelog, amend the version commit, test, and push main again before tagging. If a tag went up on the wrong commit anyway, cancel its run, then delete the tag and the draft it made before tagging again:

   ```bash
   git push origin :refs/tags/v0.1.8
   gh release delete v0.1.8 --yes
   ```

4. `.github/workflows/release.yml` builds macOS, Windows and Linux and uploads them to a draft release. Wait for every job to pass:

   ```bash
   gh run watch "$(gh run list --workflow release.yml -L1 --json databaseId -q '.[0].databaseId')" --exit-status
   ```

   If a job fails, don't publish; fix it first.

5. Publish the draft and mark it latest:

   ```bash
   gh release edit v0.1.8 --draft=false --latest
   ```

6. Check the update feed shows the new version:

   ```bash
   curl -sL https://github.com/egoist/mygo-cloudmusic/releases/latest/download/update-darwin-arm64.json
   ```

7. Deploy the website. It reads the latest release when it's prerendered, so it has to be rebuilt after publishing:

   ```bash
   cd website && bun run deploy
   ```

   Then check https://mygo-cloudmusic.egoist.workers.dev/download/ shows the new version.

## The speech recognition libraries

Podcast captions run sherpa-onnx, whose libraries the app downloads with the model from the `sherpa-onnx-<version>` release, not from the app's own. They only change with sherpa-onnx's version, in `scripts/sherpa`. Then pack them again, signing the macOS ones with the app's Developer ID (the app's hardened runtime loads no others), commit the `internal/speech/natives.go` it writes, and upload them as a prerelease, which never becomes the latest that updates look for:

```bash
go run ./scripts/sherpa -sign "Developer ID Application: xinyu zeng (GJE9R5VE87)"
gh release create sherpa-onnx-2.0.1 --prerelease --latest=false build/sherpa/*
```
