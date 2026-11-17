# Webhooks and audit log

Every content change (push, pull, tag or manifest delete, repository delete, quota exceeded, GC
completed) is written to the audit log and delivered to matching webhooks. Admin actions (members,
robots, settings) appear in the audit log only.

## Webhooks

Project admins add webhooks to their project, system admins add ones that hear every project. Pick
from `push`, `pull`, `delete_tag`, `delete_manifest`, `delete_repository`, `quota_exceeded` and
`gc_completed`; the test button sends a `ping` and every delivery attempt is listed.

Deliveries are JSON `POST`s, signed with HMAC-SHA256 in `X-Doreg-Signature-256` when a secret is set,
and retried after 5 and 30 seconds. Private, loopback and link-local targets are refused unless
`DOREG_WEBHOOK_ALLOW_PRIVATE=true`, so project admins can't poke at internal services.

Images copied by [import](migrating.md) fire no webhooks.

## Payload and signature

Headers: `X-Doreg-Event` (the event type), `X-Doreg-Attempt` (1 to 3) and `X-Doreg-Signature-256`
(`sha256=<hex>`). Anything other than a 2xx counts as failed; redirects are not followed.
`occurredAt` is a Unix timestamp, `operatorKind` is `user`, `robot` or `anonymous`.

```json
{
  "type": "push",
  "occurredAt": 1763280000,
  "operator": "ci",
  "operatorKind": "robot",
  "project": "team",
  "repository": "team/api",
  "tag": "1.0",
  "digest": "sha256:9f2c4e7b1a...",
  "resourceUrl": "reg.example.com/team/api:1.0"
}
```

Verify the signature in Go, over the raw body:

```go
body, _ := io.ReadAll(r.Body)
mac := hmac.New(sha256.New, []byte(secret))
mac.Write(body)
want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
valid := hmac.Equal([]byte(want), []byte(r.Header.Get("X-Doreg-Signature-256")))
```
