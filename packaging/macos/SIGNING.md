# Signing and notarizing the macOS tray (decision 20)

The plan does not accept an unsigned tray: every user would be fighting
Gatekeeper on first launch. The packaging workflow signs and notarizes when
the four repository secrets below are set, and builds an **unsigned** package
otherwise — an unsigned `.pkg` is fine for local testing, not for shipping.

## One-time prerequisites (Apple side)

An Apple Developer Program membership is required (about US$99 a year — the
one recurring cost in the packaging story).

1. **Two certificates**, created at developer.apple.com → Certificates:
   - *Developer ID Application* — signs the tray `.app`.
   - *Developer ID Installer* — signs the `.pkg` itself (optional; the
     workflow currently signs the app and notarizes the package, which is
     what Gatekeeper asks for on install).
   Download and import both into a Mac's keychain; `security find-identity -v
   -p codesigning` lists them as `Developer ID Application: <name> (<TEAMID>)`.

2. **App Store Connect API key**, for `notarytool`:
   App Store Connect → Users and Access → Integrations (or "Keys") →
   **Generate API Key** with the *Developer* role (App Manager also works).
   Download the `.p8` **once** — Apple will not offer it again — and note the
   10-character **Key ID** and the **Issuer ID** shown above the key list.

## Repository secrets

GitHub → Settings → Secrets and variables → Actions → New repository secret:

| Secret | Value |
| :----- | :---- |
| `MACOS_SIGNING_IDENTITY` | The signing identity's name, e.g. `Developer ID Application: Your Name (ABC1234567)` |
| `APPLE_API_KEY_BASE64` | The `.p8`, base64-encoded: `base64 -i AuthKey_XXXXXXXXXX.p8 \| pbcopy` |
| `APPLE_API_KEY_ID` | The 10-character Key ID |
| `APPLE_API_ISSUER` | The Issuer ID (a UUID, from App Store Connect) |

Secrets live in Actions Secrets, never in the repo. The workflow gates the
signing step on `MACOS_SIGNING_IDENTITY` being non-empty, so removing it
returns the job to unsigned builds.

## What the workflow does with them

On a release tag, the `macos` job:

1. Builds a universal tray (amd64 + arm64, `lipo`-ed) into
   `ThaiSmartcardTray.app` and assembles the `.pkg` with `pkgbuild`.
2. **With secrets:** `codesign --deep --options runtime` the app with
   `MACOS_SIGNING_IDENTITY`, rebuilds the pkg, stores the notarytool
   credentials from the API key, submits for notarization with `--wait`, and
   `stapler staple`s the ticket onto the package.
3. Uploads the `.pkg` to the GitHub release either way.

## Local check before pushing a tag

```sh
codesign --verify --deep --strict --verbose=2 ThaiSmartcardTray.app
spctl --assess --type install thai-smartcard-agent-<v>.pkg
stapler validate thai-smartcard-agent-<v>.pkg
```

On a clean Mac (not the build one), installing the stapled `.pkg` must not
show a Gatekeeper warning — that is the outcome decision 20 requires.
