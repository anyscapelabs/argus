# Releases and secrets

## The rule

**Values are injected at build time, never committed to source.** The values
that must be present in a shipped app (OAuth client IDs, gateway URLs) are
stored as GitHub Actions secrets/variables and compiled in with `-ldflags -X`.
Runtime environment variables still take priority, and the `.env` file is
git-ignored and only read in development (`ARGUS_LOAD_DOTENV=1`).

A caveat: a compiled-in **client secret** can be extracted from a downloaded
installer. Client IDs and URLs are public by design; for a desktop OAuth client
the stronger pattern is PKCE with no secret, or a token-exchange endpoint on
the gateway. Until then, treat the bundled secret as non-sensitive configuration
and rotate it if it is ever leaked.

## Where each secret lives

| Secret | Where it is used | Bundled into app? |
|---|---|---|
| `SKILLS_SIGNING_KEY` | `publish-skills.yml` signs `index.json` | No — public key only |
| `MACOS_CERTIFICATE_BASE64`, `MACOS_CERTIFICATE_PASSWORD`, `MACOS_SIGNING_IDENTITY`, `MACOS_NOTARY_*` | `build.yml` macOS job: keychain import, `codesign`, `notarytool` | No — runner temp keychain, deleted after |
| `WINDOWS_CERTIFICATE_BASE64`, `WINDOWS_CERTIFICATE_PASSWORD` | `build.yml` Windows job: `$RUNNER_TEMP/argus-signing.pfx`, `signtool` | No — file removed after signing |
| `ARGUS_GOOGLE_CLIENT_ID`, `ARGUS_GOOGLE_CLIENT_SECRET`, `GOOGLE_OAUTH_CLIENT_ID`, `GOOGLE_OAUTH_CLIENT_SECRET`, `ARGUS_GITHUB_CLIENT_ID`, `ARGUS_GITHUB_CLIENT_SECRET` | `build.yml` compiles them into `Services/Shared.Build*` with `-ldflags -X` | Yes, via ldflags — see caveat above |
| `ANYSCAPE_GATEWAY_URL`, `ANYSCAPE_CONSOLE_URL`, `ARGUS_PUBLIC_URL` | GitHub variables → frontend `VITE_*` env and `Shared.ConfigValue("ARGUS_PUBLIC_URL")` | Yes — public URLs |
| `GITHUB_TOKEN` | `release` job uploads assets via `softprops/action-gh-release` | No — provided automatically by Actions |
| User's `argus_pk_` service key | Runtime, stored in the OS keyring (`Keyring/keyring.go`) | No — pasted by the user after install |

Non-secret `vars.*` (AWS role ARN, bucket, distribution ID, CDN URL) are also
kept in GitHub Actions variables, not in the binary.

## Setting the secrets

The `gh` CLI reads the value from stdin, so the secret never appears in shell
history, logs, or the repository:

```bash
# Skills registry signing key (already set)
gh secret set SKILLS_SIGNING_KEY < /path/to/skills_signing.pem

# macOS: base64 of the .p12 certificate, plus its password and identity
base64 -i /path/to/DeveloperID.p12 | gh secret set MACOS_CERTIFICATE_BASE64
gh secret set MACOS_CERTIFICATE_PASSWORD --body '...'
gh secret set MACOS_SIGNING_IDENTITY --body 'Developer ID Application: Anyscape Labs PLC (TEAMID)'
gh secret set MACOS_NOTARY_APPLE_ID --body '...'
gh secret set MACOS_NOTARY_TEAM_ID --body '...'
gh secret set MACOS_NOTARY_PASSWORD --body '...'

# Windows: base64 of the code-signing .pfx, plus its password
base64 -w0 /path/to/code-signing.pfx | gh secret set WINDOWS_CERTIFICATE_BASE64
gh secret set WINDOWS_CERTIFICATE_PASSWORD --body '...'
```

The `if:` guards in `build.yml` mean a job builds unsigned when its secrets are
absent. Check what is configured without printing values:

```bash
gh secret list
```

## App configuration baked into the build

`Services/Shared/build_config.go` declares the `Build*` string variables.
`Shared.ConfigValue(key)` checks the process environment first and then the
baked value, so a runtime override always wins.

`build.yml` runs `go run ./tools/gen-build-config` in every platform job before
the build. That command reads the GitHub secrets/variables and writes the
git-ignored `Services/Shared/build_config_generated.go`, which assigns those
values at init time. Each platform job starts with a "Verify OAuth build config
is present" step that fails the release if `ARGUS_GITHUB_CLIENT_ID` or
`ARGUS_GITHUB_CLIENT_SECRET` is empty (an unset secret would otherwise be baked
in as an empty string and the shipped app would report "github sign-in is not
configured in this build"). To change a shipped value, update the GitHub secret
or variable and make a new release — no source change is needed.

Local dotenv loading happens **only at startup**: `main.go` (and
`cmd/server/main.go`) call `Env.Load()` before constructing any service, and
`Env/load.go` parses the file. Services never read or parse `.env` themselves —
they only call `Shared.ConfigValue`, which prefers the process environment and
falls back to the generated build config.

In a **source checkout**, `.env` is loaded automatically, so `go run .` and
`task dev` find the same OAuth credentials. **Production builds**
(`-tags production`) deliberately ignore a stray `.env` in the launch directory;
set `ARGUS_LOAD_DOTENV=1` explicitly if a staging build needs to read one.


Frontend URLs use Vite's `import.meta.env`: `VITE_ANYSCAPE_GATEWAY_URL` and
`VITE_ANYSCAPE_CONSOLE_URL` are supplied to the build job from GitHub variables
and end up in the JS bundle (`frontend/src/domains/account/api.ts`).

## Publishing a release

Releases are tag-only (`on: push: tags: - 'v*'` in `build.yml`):

```bash
git tag v0.1.0
git push origin v0.1.0
```

The workflow then:

1. Builds macOS (x64 + arm64), Linux, and Windows artifacts with the injected
   signing secrets.
2. The `release` job downloads all artifacts, writes `SHA256SUMS`, and calls
   `softprops/action-gh-release@v2` with the automatic `GITHUB_TOKEN` to publish
   the GitHub Release.
3. The desktop updater (`Services/Updater`) checks `anyscapelabs/argus`
   releases and verifies downloads against `SHA256SUMS`.

No key needs to be added to the app for any of this. To point the updater at a
staging fork, use the `ARGUS_UPDATE_REPO` environment variable; to stage the
skills registry, use `ARGUS_SKILLS_INDEX_URL`. Both are public configuration,
not secrets.
