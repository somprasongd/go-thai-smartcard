# JavaScript SDK publishing

Package: `@somprasongd/thai-smartcard-client`, under `sdk/javascript`.
GitHub repo: `somprasongd/go-thai-smartcard`. SDK tags use `sdk-vX.Y.Z`, separate
from agent tags `vX.Y.Z`. Creating an SDK tag does not trigger installer builds.
`sdk-test.yml` verifies the SDK on Linux, macOS and Windows for PRs/main;
`sdk-publish.yml` publishes tagged revisions already merged into main.

## Prepare the first release

1. Verify `Apache-2.0` in `package.json` and confirm the packed SDK includes
   `LICENSE` and `NOTICE`, matching the repository license and SDK attribution.
2. Run `npm ci` and `npm run check` in `sdk/javascript`. Review the actual packed
   contents; only built modules/declarations and package documentation belong
   in the tarball. No card traces, tokens or configs.
3. Version the SDK changelog to match `package.json`, keeping an empty Unreleased
   section above it. Commit on a feature branch, open a PR, and merge only after
   the exact revision passes both repository and SDK CI.
4. From the merged checkout, publish the initial real package interactively:

   ```sh
   cd sdk/javascript
   npm login --registry=https://registry.npmjs.org
   npm whoami --registry=https://registry.npmjs.org
   npm publish --access public
   ```

   Confirm `whoami` is `somprasongd`. The web login does not authenticate the
   CLI automatically. Complete browser/2FA prompts yourself; do not share a
   password, OTP or token in chat. This initial publish creates the package on
   npm so its trusted-publisher settings can be configured. Do not publish a
   placeholder package just to reserve a name.

## Configure npm trusted publishing

In npm: Packages → `@somprasongd/thai-smartcard-client` → Settings → Trusted
publishing → GitHub Actions:

| Field | Value |
| --- | --- |
| Organization/user | `somprasongd` |
| Repository | `go-thai-smartcard` |
| Workflow filename | `sdk-publish.yml` (filename only) |
| Environment | Leave blank; this workflow does not declare an environment |
| Allowed actions | Enable direct `npm publish` |

No publish token is needed in GitHub Secrets. This workflow uses a GitHub-hosted
runner, Node 24, npm 11.12.1 and `id-token: write`. Trusted publishing needs npm
11.5.1+ and Node 22.14.0+. For a public package built in a public repo, npm
automatically includes provenance. Details:
https://docs.npmjs.com/trusted-publishers/.

## Subsequent releases

1. Update the package version and SDK changelog in a PR. Merge after CI passes.
2. Annotate the release commit with `sdk-vX.Y.Z` and push that tag. For example,
   when package.json contains 0.1.1:

   ```sh
   git tag -a sdk-v0.1.1 -m "release javascript sdk v0.1.1"
   git push origin sdk-v0.1.1
   ```

3. Check the publish workflow, then `npm view
   @somprasongd/thai-smartcard-client@0.1.1 version dist.integrity` and install
   that exact version into a consumer. Check exports, type declarations and
   provenance on npm. Publishing a version already present will fail: do not
   overwrite/reuse version numbers.

After the initial interactive publish, configure trusted publishing and push
the annotated `sdk-v0.1.0` tag. The workflow skips publication only if the
registered tarball matches the local artifact. If gzip implementations differ,
it verifies the downloaded registry integrity and compares every uncompressed
tar byte. Different contents fail instead of claiming success. Subsequent
versions publish by OIDC.
Prereleases need an explicit npm dist-tag such as `next`; the current workflow
is for stable SDK versions only.
