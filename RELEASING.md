# Releasing

## How Releases Work

When a new tagged release is created (e.g., `v0.12.3`), the GitHub Actions workflow in `.github/workflows/release.yaml` automatically:

1. Builds binaries for all platforms (darwin, linux, windows)
2. Creates a GitHub release with the binaries
3. Updates the Homebrew formula in [dbt-labs/homebrew-dbt-cli](https://github.com/dbt-labs/homebrew-dbt-cli)

## Creating a Release

### Using `gh` CLI (preferred)

```bash
gh release create v0.x.x --generate-notes
```

This creates both the tag and the release in one step. The `--generate-notes` flag auto-generates release notes from merged PRs.

### Using the GitHub UI

1. Go to the repository on GitHub
2. Click "Releases" in the right sidebar
3. Click "Draft a new release"
4. Click "Choose a tag" and type a new tag (e.g., `v0.x.x`)
5. Click "Create new tag: v0.x.x on publish"
6. Add a release title and description
7. Click "Publish release"

### Using git

```bash
git tag v0.x.x
git push origin v0.x.x
```

Note: This only creates the tag. The release workflow will create the GitHub release automatically.

## Required Secrets

### `GH_HOMEBREW`

A Personal Access Token (PAT) with write access to the [dbt-labs/homebrew-dbt-cli](https://github.com/dbt-labs/homebrew-dbt-cli) repository. This is needed because the default `GITHUB_TOKEN` only has access to the current repository.

**Required permissions:**
- `repo` scope (or fine-grained: Contents read/write on `dbt-labs/homebrew-dbt-cli`)

**To rotate the token:**

1. Create a new PAT at https://github.com/settings/tokens
2. Grant the required permissions above
3. Update the secret in this repository: Settings > Secrets and variables > Actions > `GH_HOMEBREW`

**Note:** Classic PATs expire based on your configured expiration. Fine-grained tokens may have shorter lifespans. Check the token expiration and rotate before it expires to avoid failed releases.
