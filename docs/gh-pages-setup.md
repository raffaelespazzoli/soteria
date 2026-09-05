# GitHub Pages Setup for Helm Chart Hosting

This document describes how to set up the `gh-pages` branch and GitHub Pages
for hosting the Soteria Helm chart repository.

## Prerequisites

- Repository admin access (to configure GitHub Pages)
- `git` CLI
- The `release.yml` workflow uses `helm/chart-releaser-action` which requires
  the `gh-pages` branch to exist before the first release

## 1. Create the gh-pages Orphan Branch

Run these commands from a local clone of the repository:

````bash
# Create an orphan branch (no commit history)
git checkout --orphan gh-pages

# Remove all tracked files
git rm -rf .

# Create a README for the Helm repository
cat > README.md << 'EOF'
# Soteria Helm Chart Repository

This branch hosts the packaged Helm charts for the Soteria project.

## Usage

```bash
helm repo add soteria https://raffaelespazzoli.github.io/soteria
helm repo update
helm search repo soteria
helm install soteria soteria/soteria --namespace soteria --create-namespace
```

## Repository URL

`https://raffaelespazzoli.github.io/soteria`

For documentation and source code visit the
[main branch](https://github.com/soteria-project/soteria).
EOF

# Commit and push
git add README.md
git commit -m "Initialize gh-pages branch for Helm chart hosting"
git push origin gh-pages
````

## 2. Enable GitHub Pages

1. Go to **Settings → Pages** in the GitHub repository.
2. Under **Source**, select **Deploy from a branch**.
3. Set the branch to `gh-pages` and the folder to `/ (root)`.
4. Click **Save**.

GitHub Pages will serve the Helm repository index at:
`https://raffaelespazzoli.github.io/soteria`

## 3. Configure Repository Secrets

The release workflow (`.github/workflows/release.yml`) and the operator release
workflow (`.github/workflows/release-operator.yml`) require the following secrets
to push container images to `quay.io`:

| Secret              | Description                          |
|---------------------|--------------------------------------|
| `REGISTRY_USERNAME` | quay.io robot account or username    |
| `REGISTRY_PASSWORD` | quay.io robot account token/password |

To configure:

1. Go to **Settings → Secrets and variables → Actions**.
2. Click **New repository secret**.
3. Add `REGISTRY_USERNAME` with your quay.io username or robot account name.
4. Add `REGISTRY_PASSWORD` with the corresponding password or token.

The Helm chart publishing step uses the built-in `GITHUB_TOKEN` — no additional
secret is required for chart-releaser.

## 4. Test the Release Pipeline

To validate the full pipeline, push a pre-release tag:

```bash
# Ensure you are on the main branch with all changes merged
git checkout main
git pull origin main

# Create and push a pre-release tag
git tag v0.1.0-rc1
git push origin v0.1.0-rc1
```

This triggers both release workflows:

- **`release-operator.yml`** — builds and pushes the controller image via the
  reusable operator workflow
- **`release.yml`** — builds/pushes console-plugin and standalone-ui images,
  then packages and publishes the Helm chart to `gh-pages`

### Verify

1. Check **Actions** tab for workflow run status.
2. Confirm images appear on quay.io:
   - `quay.io/raffaelespazzoli/soteria:v0.1.0-rc1`
   - `quay.io/raffaelespazzoli/soteria-console-plugin:0.1.0-rc1`
   - `quay.io/raffaelespazzoli/soteria-standalone-ui:0.1.0-rc1`
3. Confirm the Helm chart index is published:
   ```bash
   helm repo add soteria https://raffaelespazzoli.github.io/soteria
   helm repo update
   helm search repo soteria --versions
   ```

## Container Images

The release pipeline publishes three container images:

| Image                                                  | Dockerfile                         | Context          |
|--------------------------------------------------------|------------------------------------|------------------|
| `quay.io/raffaelespazzoli/soteria`                      | `Dockerfile`                       | repo root        |
| `quay.io/raffaelespazzoli/soteria-console-plugin`       | `console-plugin/Dockerfile`        | `console-plugin/`|
| `quay.io/raffaelespazzoli/soteria-standalone-ui`        | `console-plugin/Dockerfile.standalone` | repo root    |

All images are built for `linux/amd64`, `linux/arm64`, and `linux/ppc64le`.
