# Changelog: ps5-messaging

## v0.1.0 (2026-11-03)

- 2026-11-03: groups, members and message history are durable in Postgres (`messaging.Postgres`, tables `ps5_msg_*`). The service
  exits without `PS5_DATABASE_URL` unless `PS5_DEV_MEMORY=1`. Restart test against a real Postgres. Not deployed.

- 2026-11-02: first cut. Groups and messages of `/api/gamingLoungeGroups/v1/`: list my groups, create a group,
  group details, post a message, list messages. In-memory store behind an interface. Bearer auth through
  ps5-common `oauth.Require`. Tests with httptest. Not deployed, no tag, not tried by a console or the PlayStation app.
