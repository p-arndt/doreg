# Reverse proxy

doreg speaks plain HTTP and expects a TLS-terminating proxy in front of it: docker only sends
credentials over HTTPS, except to `localhost`. The proxy has to get four things right.

| | Why |
|---|---|
| Keep the original `Host` header | The web UI's CSRF check compares it with the browser's origin, and the token URL in the registry's auth challenge is built from it (`X-Forwarded-Host` takes precedence there). A rewritten `Host` breaks every change in the UI and anonymous pulls of [public repositories](access.md#public-repositories). |
| Set `X-Forwarded-Proto: https` | Same token URL, and the session cookie gets the `Secure` flag only over HTTPS. |
| No request body limit | Layers are often uploaded in a single request and can be gigabytes. |
| No or long read and write timeouts | A large layer upload or download takes minutes. doreg itself sets no read or write timeout. |

`X-Forwarded-For` is used for the client IP in the audit log.

Symptoms of a wrong setup:

- `docker pull` of a public image fails with `no basic auth credentials` or tries to reach `http://` or an internal host name: `Host` or `X-Forwarded-Proto` is missing.
- Saving anything in the web UI fails with 403: the `Host` doreg sees differs from the browser's origin.
- Large pushes fail with `413` or abort after about a minute: body limit or read timeout.

## Traefik

Traefik keeps `Host` and sets the `X-Forwarded-*` headers by default. Only the timeout needs raising,
because Traefik v3 cuts requests after 60 seconds:

```yaml
services:
  traefik:
    command:
      - --entrypoints.websecure.address=:443
      - --entrypoints.websecure.transport.respondingTimeouts.readTimeout=0
      # ...
  doreg:
    labels:
      - traefik.enable=true
      - traefik.http.routers.doreg.rule=Host(`reg.example.com`)
      - traefik.http.routers.doreg.entrypoints=websecure
      - traefik.http.routers.doreg.tls.certresolver=letsencrypt
      - traefik.http.services.doreg.loadbalancer.server.port=8080
```

[`deploy/compose.yaml`](../deploy/compose.yaml) is a complete example.

## Caddy

Caddy keeps `Host`, sets `X-Forwarded-Proto`, and has no body limit or read timeout by default:

```caddyfile
reg.example.com {
	reverse_proxy doreg:8080
}
```

## nginx

nginx needs all four set explicitly; by default it rewrites `Host`, limits bodies to 1 MB and times out
after 60 seconds:

```nginx
server {
    listen 443 ssl;
    http2 on;
    server_name reg.example.com;
    # ssl_certificate ...

    client_max_body_size 0;
    proxy_request_buffering off;
    proxy_buffering off;
    proxy_read_timeout 900s;
    proxy_send_timeout 900s;

    location / {
        proxy_pass http://doreg:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }
}
```

## Without a proxy

For a quick test on one machine, `docker push localhost:8080/...` works over plain HTTP because docker
trusts `localhost`. Any other host needs TLS, or an `insecure-registries` entry in every client's
`daemon.json`, which also sends passwords in clear text.
