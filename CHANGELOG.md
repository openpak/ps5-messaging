# Changelog: ps5-messaging

## Unreleased

- 2026-11-02: first cut. Groups and messages of `/api/gamingLoungeGroups/v1/`: list my groups, create a group,
  group details, post a message, list messages. In-memory store behind an interface. Bearer auth through
  ps5-common `oauth.Require`. Tests with httptest. Not deployed, no tag, not tried by a console or the PlayStation app.
