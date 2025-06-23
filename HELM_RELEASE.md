# Helm Chart Release Guide

This guide explains how to release new versions of the AgentAPI Proxy Helm chart.

## Release Process

### 1. Prepare the Release

1. **Update the chart** if needed:
   - Modify templates in `helm/agentapi-proxy/templates/`
   - Update default values in `helm/agentapi-proxy/values.yaml`
   - Add new features or bug fixes

2. **Test locally**:
   ```bash
   # Lint the chart
   helm lint helm/agentapi-proxy/
   
   # Test template rendering
   helm template test-release helm/agentapi-proxy/
   
   # Test installation (if you have a k8s cluster)
   helm install test-release helm/agentapi-proxy/ --dry-run
   ```

### 2. Create and Push a Tag

The chart version is automatically determined from the git tag:

```bash
# For version 0.1.0
git tag v0.1.0
git push origin v0.1.0

# For helm-specific versions (if different from app version)
git tag helm-v0.2.0
git push origin helm-v0.2.0
```

### 3. Automatic Publishing

Once you push a tag matching `v*` or `helm-v*`, GitHub Actions will:

1. ✅ Extract version from the tag (removing `v` or `helm-v` prefix)
2. ✅ Update `Chart.yaml` with the new version
3. ✅ Lint and validate the chart
4. ✅ Package the chart into a `.tgz` file
5. ✅ Login to `ghcr.io` using `GITHUB_TOKEN`
6. ✅ Push the chart to `oci://ghcr.io/takutakahashi/agentapi-proxy`
7. ✅ Create release notes and upload artifacts

### 4. Verify the Release

1. **Check GitHub Actions**: Ensure the workflow completed successfully
2. **Check Packages**: Visit https://github.com/takutakahashi?tab=packages
3. **Test Installation**:
   ```bash
   helm install test oci://ghcr.io/takutakahashi/agentapi-proxy --version 0.1.0
   ```

## Version Strategy

### Semantic Versioning

Follow [Semantic Versioning](https://semver.org/) for chart versions:

- **MAJOR** (`1.0.0`): Breaking changes
- **MINOR** (`0.1.0`): New features (backward compatible)
- **PATCH** (`0.1.1`): Bug fixes (backward compatible)

### Tag Formats

Two tag formats are supported:

1. **Application tags** (`v0.1.0`): For application releases
2. **Helm-specific tags** (`helm-v0.1.0`): For chart-only updates

### Examples

```bash
# Application release (updates both app and chart version)
git tag v1.0.0

# Chart-only update (fixes chart templates, values, etc.)
git tag helm-v1.0.1

# Major chart update with breaking changes
git tag helm-v2.0.0
```

## Chart Version vs App Version

The workflow automatically sets both versions from the tag:

- `version` in `Chart.yaml`: Chart version (for Helm)
- `appVersion` in `Chart.yaml`: Application version (Docker image tag)

If you need different versions:

1. Use `helm-v*` tags for chart-specific updates
2. Manually edit `Chart.yaml` before creating the tag
3. Create separate tags for chart and application versions

## Release Notes

The workflow automatically generates release notes with:

- Installation commands
- Upgrade commands  
- Chart and app versions
- Registry information

These are uploaded as artifacts and can be used for GitHub Releases.

## Troubleshooting

### Workflow Fails

1. **Check permissions**: Ensure repository has `packages: write` permission
2. **Check tag format**: Must match `v*` or `helm-v*` pattern
3. **Check chart syntax**: Run `helm lint` locally first
4. **Check logs**: View GitHub Actions logs for detailed errors

### Chart Not Found in Registry

1. **Wait a few minutes**: Registry updates may be delayed
2. **Check visibility**: Ensure package is public (or authenticate if private)
3. **Verify tag**: Confirm the tag triggered the workflow
4. **Check workflow status**: Ensure it completed successfully

### Version Conflicts

1. **Check existing versions**: Each version can only be published once
2. **Use new version**: Increment version number in new tag
3. **Delete and recreate**: Delete the tag and package if needed

## Manual Release (if needed)

If automation fails, you can manually release:

```bash
# 1. Update Chart.yaml version manually
vim helm/agentapi-proxy/Chart.yaml

# 2. Package the chart
helm package helm/agentapi-proxy/

# 3. Login to registry
echo $GITHUB_TOKEN | helm registry login ghcr.io --username takutakahashi --password-stdin

# 4. Push the chart
helm push agentapi-proxy-0.1.0.tgz oci://ghcr.io/takutakahashi
```

## Best Practices

1. **Test thoroughly**: Always test charts before releasing
2. **Use semantic versioning**: Follow semver for predictable updates
3. **Document changes**: Update README.md with new features
4. **Keep charts simple**: Avoid complex templating when possible
5. **Validate with kubeval**: Ensure generated manifests are valid
6. **Use consistent naming**: Follow Helm chart naming conventions

## Rollback

If you need to rollback a release:

1. **Don't delete published versions**: OCI registry versions are immutable
2. **Publish a new version**: Create a fix and bump the version
3. **Update documentation**: Note any breaking changes or issues
4. **Communicate**: Inform users about the issue and resolution