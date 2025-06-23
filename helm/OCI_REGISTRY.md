# Publishing Helm Chart to OCI Registry (ghcr.io)

This document explains how to publish the AgentAPI Proxy Helm chart to GitHub Container Registry (ghcr.io) as an OCI artifact.

## Prerequisites

- Helm 3.8+ (with OCI support)
- GitHub Personal Access Token with appropriate permissions
- Write access to the repository

## Required GitHub Token Permissions

Create a Personal Access Token with the following scopes:
- `write:packages` - To push packages to ghcr.io
- `read:packages` - To pull packages from ghcr.io
- `repo` - If the repository is private

## Publishing Steps

### 1. Package the Chart

```bash
helm package helm/agentapi-proxy/
```

This creates `agentapi-proxy-0.1.0.tgz` in the current directory.

### 2. Login to ghcr.io

```bash
export GITHUB_TOKEN="your-github-token"
echo $GITHUB_TOKEN | helm registry login ghcr.io --username YOUR_GITHUB_USERNAME --password-stdin
```

### 3. Push to OCI Registry

```bash
helm push agentapi-proxy-0.1.0.tgz oci://ghcr.io/takutakahashi
```

### 4. Verify the Push

Check the packages page: https://github.com/takutakahashi?tab=packages

## Installing from OCI Registry

Once published, users can install the chart directly from the OCI registry:

### Method 1: Direct Install

```bash
helm install agentapi-proxy oci://ghcr.io/takutakahashi/agentapi-proxy --version 0.1.0
```

### Method 2: Pull and Install

```bash
# Pull the chart
helm pull oci://ghcr.io/takutakahashi/agentapi-proxy --version 0.1.0

# Extract and install
tar -xzf agentapi-proxy-0.1.0.tgz
helm install agentapi-proxy ./agentapi-proxy
```

### Method 3: With Custom Values

```bash
helm install agentapi-proxy oci://ghcr.io/takutakahashi/agentapi-proxy \
  --version 0.1.0 \
  --set ingress.enabled=true \
  --set ingress.hosts[0].host=agentapi.yourdomain.com
```

## Updating the Chart

When updating the chart version:

1. Update `version` in `Chart.yaml`
2. Package the new version
3. Push to the registry

```bash
# Update Chart.yaml version to 0.2.0
helm package helm/agentapi-proxy/
helm push agentapi-proxy-0.2.0.tgz oci://ghcr.io/takutakahashi
```

## Automation with GitHub Actions

Add this workflow to automatically publish charts:

```yaml
# .github/workflows/helm-publish.yml
name: Publish Helm Chart

on:
  push:
    tags:
      - 'v*'

jobs:
  publish:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: write
    steps:
      - uses: actions/checkout@v4
      
      - name: Install Helm
        uses: azure/setup-helm@v4
        with:
          version: '3.18.3'
      
      - name: Package Chart
        run: helm package helm/agentapi-proxy/
      
      - name: Login to ghcr.io
        run: |
          echo "${{ secrets.GITHUB_TOKEN }}" | helm registry login ghcr.io --username ${{ github.actor }} --password-stdin
      
      - name: Push Chart
        run: |
          helm push agentapi-proxy-*.tgz oci://ghcr.io/${{ github.repository_owner }}
```

## Private Registry Access

For private packages, users need to authenticate:

```bash
helm registry login ghcr.io --username YOUR_USERNAME
# Enter your GitHub token when prompted
```

## Troubleshooting

### Permission Denied
- Ensure your GitHub token has `write:packages` scope
- Verify you have write access to the repository
- Check that the repository/package visibility settings allow pushing

### Chart Not Found
- Verify the registry URL format: `oci://ghcr.io/username/chart-name`
- Check package visibility (public vs private)
- Ensure you're authenticated if the package is private

### Version Conflicts
- Each chart version can only be pushed once
- Increment the version in `Chart.yaml` for updates
- Use semantic versioning (e.g., 0.1.0, 0.1.1, 0.2.0)

## Best Practices

1. **Versioning**: Use semantic versioning for chart releases
2. **Testing**: Test charts locally before publishing
3. **Documentation**: Keep README.md updated with installation instructions
4. **Security**: Use GitHub tokens with minimal required permissions
5. **Automation**: Use GitHub Actions for consistent releases