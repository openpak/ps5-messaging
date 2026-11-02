# Protocol: ps5-messaging

Groups and messages as the PSN web API exposes them on `m.np.playstation.com`, prefix `/api/gamingLoungeGroups/v1`.
Shapes come from the public psn-api and PSNAWP client libraries (internal research notes, `ps5/psn-endpoints.md`,
section 1.3); nothing here was captured from a console. Whether the PS5 system software uses these paths is unknown.

Every endpoint except `GET /healthz` needs `Authorization: Bearer <token>` (ps5-common `oauth`, issued by ps5-auth);
otherwise `401` with the PSN error body `{"error":{"referenceId","code","message"}}`.

| Request | Body | Answer |
|---|---|---|
| `GET /members/me/groups` | | `{groups:[Group], totalItemCount}` |
| `POST /groups` | `{invitees:[{accountId}], groupName?}` | `{groupId, mainThread:{threadId}}` |
| `GET /groups/{groupId}` | | `Group` |
| `POST /groups/{groupId}/threads/{threadId}/messages` | `{messageType:1, body}` | `{messageUid, createdTimestamp}` |
| `GET /groups/{groupId}/threads/{threadId}/messages?limit=` | | `{messages:[Message], messageCount, reachedEndOfPage, previous, next}` |
| `GET /members/me/groups/{groupId}/threads/{threadId}/messages?limit=` | | same (the path psn-api uses) |

`Group`: `groupId, groupName?, groupType (0 direct message, 1 group), members:[{accountId, onlineId?}],
mainThread:{threadId, latestMessage?}, createdTimestamp, modifiedTimestamp`.
`Message`: `messageUid, messageType, alternativeMessageType, body, createdTimestamp, sender:{accountId, onlineId}`.

Behaviour: the creator is always a member; a group of two is a direct message; at least one invitee is required.
Messages are newest first, `limit` defaults to 20 (max 100), body at most 4000 bytes, only `messageType` 1 (text).
Non-members get `403` (code 2113411), unknown groups or threads `404`. `previous` and `next` are always empty:
there is no paging beyond `limit` yet. An invitee's `onlineId` is filled in after their first message.

Unverified: the `mainThread` field and the `groupName` request field (not in the sources read), the error codes
2113401 to 2113499 (invented), message ordering, and all of the above against a real client.
