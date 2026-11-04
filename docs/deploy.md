# Deploy: ps5-messaging

`m.np.playstation.com` is **one host shared** by ps5-profile, ps5-friends, ps5-trophy and this service. DNS cannot
tell them apart, so Traefik routes by path prefix. This service owns:

```
Host(`m.np.playstation.com`) && PathPrefix(`/api/gamingLoungeGroups/`)
```

forwarded to `http://openpak-ps5-messaging:8080`. Sibling services use their own prefixes (`/api/userProfile/`,
`/api/trophy/`, ...) on the same Host; Traefik picks the longest matching rule, so no priorities are needed as long
as the prefixes do not overlap. TLS for the host is terminated by Traefik with the OpenPak CA the console trusts.

## Run

```sh
export PS5_TOKEN_KEY=...   # same key as ps5-auth, 32 bytes or more
go run ./cmd/ps5-messaging
```

```sh
GOWORK=off go vet ./... && GOWORK=off go test ./...
docker build -t ps5-messaging .
```

## Configuration

| Variable | Required | Default | Meaning |
|---|---|---|---|
| `PS5_ADDR` | no | `:8080` | HTTP listen address |
| `PS5_TOKEN_KEY` | yes | | HMAC key of the bearer tokens, shared with ps5-auth |
| `PS5_LOG_LEVEL` | no | `info` | `debug`, `info`, `warn`, `error` |
| `PS5_DATABASE_URL` | yes | | Postgres URL (3-connection pool); the service exits without it unless `PS5_DEV_MEMORY=1`. Tables are created at start |
| `PS5_DEV_MEMORY` | no | | `1`: keep state in memory, lost on restart (local development only) |

Other `PS5_*` variables of ps5-common are accepted and ignored. Groups, members and every message live in Postgres (`ps5_msg_groups`, `ps5_msg_members`, `ps5_msg_messages`), so one database backup covers them.

## Vendoring

`go.mod` uses `replace github.com/openpak/ps5-common => ../ps5-common` and `vendor/` is committed. After changing
ps5-common: `GOWORK=off go mod vendor`.

## Release

Tag `v*.*.*` publishes `ghcr.io/openpak/ps5-messaging`; deployed hosts auto-update from it.

## Account erasure

`DELETE /internal/accounts/{account_id}/data` with `Authorization: Bearer <ACCOUNT_ERASE_KEY>` erases everything this service
holds for a core account (UUID). It listens on `ERASE_ADDR` (default `:9099`, container network only, never published) and is
only served when `ACCOUNT_ERASE_KEY` is set. The website finaliser calls `http://openpak-ps5-messaging:9099`; the reply is
`{"service":"ps5-messaging","rows":N}`. The ids searched are the core account id and, when the core (`PS5_ACCOUNT_CORE_URL`) has an
active `ps5` link for it, the link subject. If the core is configured but unreachable the call fails (500) rather than
skipping data.

Erased: memberships, messages the account sent, direct-message groups it was in (with all their messages), and groups left empty.
Kept: messages other members sent in group chats (they are not the erased account's data).
