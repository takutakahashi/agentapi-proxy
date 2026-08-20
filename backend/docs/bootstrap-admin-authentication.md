# Bootstrap Admin Authentication

Bootstrap Admin authentication is a break-glass login for a new installation.
It works independently of Static API Key, GitHub, and other authentication, so an
operator can open the admin UI before configuring an external identity provider.

## Recommended: existing Kubernetes Secret

Create the token outside Helm:

```bash
kubectl create secret generic agentapi-bootstrap-admin \
  --from-literal=token='replace-with-a-long-random-token'
```

Configure the chart:

```yaml
config:
  auth:
    bootstrapAdmin:
      enabled: true
      userId: bootstrap-admin
      username: Admin
      tokenSecretRef:
        name: agentapi-bootstrap-admin
        key: token
```

The `token` value may be specified directly instead of `tokenSecretRef`, but
this stores the credential in Helm release values and is not recommended.

## Login

The token is accepted through either standard API-key form:

```text
Authorization: Bearer <token>
X-API-Key: <token>
```

The resulting identity always has the `admin` role and `admin` permission.
It can therefore access `/admin/system-settings` even when every other
authentication provider is disabled.

## Operational guidance

- Generate a long random token and keep it in a Kubernetes Secret or external
  secret manager.
- Disable Bootstrap Admin after normal administrator authentication is working.
- Rotate the Secret and restart the backend if the token may have leaked.
- Do not expose the token in Git, Helm values, logs, or support bundles.
