# PS5 Messaging Server

OpenPak's PlayStation 5 Messaging Server.

| | |
| --- | --- |
| Network | PlayStation 5 |
| Role | Groups, direct messages and text messages of the PSN web API (`gamingLoungeGroups`) |
| Used by | PlayStation 5, PlayStation App |
| Image | `ghcr.io/openpak/ps5-messaging` |
| Licence | AGPL-3.0-only |

## What it serves

All paths are on `m.np.playstation.com` under `/api/gamingLoungeGroups/v1`, behind a bearer token.

| Endpoint | What |
| --- | --- |
| `GET /members/me/groups` | My groups |
| `POST /groups` | Create a group or direct message |
| `GET /groups/{groupId}` | Group details |
| `POST /groups/{groupId}/threads/{threadId}/messages` | Post a text message |
| `GET /groups/{groupId}/threads/{threadId}/messages` | List messages (also under `/members/me/groups/...`) |

Party voice is out of scope. Image messages, group editing and invitee/leave calls are not implemented yet.

Storage is in memory behind the `Store` interface; the OpenPak universal social/chat service
(`platforms/core/infra/chat`) is the intended backend.

## Run

```sh
PS5_TOKEN_KEY=$(head -c 32 /dev/zero | tr '\0' k) go run ./cmd/ps5-messaging   # listens on :8080
```

## Docs

- [Protocol](docs/protocol.md)
- [Deploy](docs/deploy.md)

## Licence

AGPL-3.0-only. See [LICENSE](LICENSE).
