# mautrix-mattermost

An **unofficial** Matrix-Mattermost puppeting bridge built on [mautrix-go](https://github.com/mautrix/go)'s bridgev2 framework. It is not affiliated with mautrix or Mattermost. The goal is to contribute it upstream. Until then it lives here.

## Status

Early: text messages in channels, direct messages and group messages, both directions.

Works:
- Login flow `credentials`: first the server URL, then username or email and password. No admin rights are needed, so it works on servers where personal access tokens are disabled. Login IDs are `<host>/<user id>`, so one bridge can serve logins on several servers.
- New posts in public and private channels arrive in Matrix as they're sent, one portal room per channel. Markdown is converted, and thread replies become Matrix threads.
- Direct messages become DM rooms with the other user's ghost, group messages become group rooms with every member joined and Mattermost's "alice, bob, carol" name. Both are per login: a channel room is shared by every Matrix user logged in to that server, a DM or group room is not.
- Starting chats from Matrix: `start-chat alice`, `start-chat alice@example.org` or a user ID opens the DM, `resolve-identifier` only looks the user up. A group message needs 3 to 8 members including you, as in Mattermost.
- Text messages sent from Matrix are posted to the channel, with Matrix formatting converted to Markdown. Notices are sent too if `bridge_notices` is enabled in the config. Underline has no Markdown equivalent and is dropped.
- Replies in a Matrix thread become replies in the Mattermost thread.
- Mentions (pills) of Mattermost users become `@username`, including Matrix users logged in to the same Mattermost server. Other Matrix users stay as their display name.
- Your own posts come back from Mattermost as echoes and don't show up twice in Matrix.
- An expired or revoked session is reported as bad credentials, so the user knows to log in again.

Matrix replies outside a thread also become thread replies, attached to the thread of the message you replied to. Mattermost has no other kind of reply: its reply button opens the thread. Posting a reply as a plain message would lose the context it refers to. The bridge advertises threads but not replies in its room capabilities, so bridgev2 does this conversion itself.

Images, files, emotes, locations, edits, deletions and reactions from Matrix are rejected with an error message status. Rejected images, files and emotes also get a notice in the room.

Not yet:
- Edits, deletions, reactions, attachments, backfill
- `@room` to `@channel`
- Personal access token login, MFA
- Syncing channel members and teams as spaces

## Running

Images for amd64 and arm64 are published to `ghcr.io/robinchoice/mautrix-mattermost` on every push to `main`, tagged `latest` and `sha-<commit>`.

Setup follows the [mautrix bridgev2 docs](https://docs.mau.fi/bridges/go/setup.html):

1. Start the container with an empty `/data` volume. It writes `config.yaml` and exits.
2. Edit the config (homeserver address and domain, database, permissions) and start it again. It writes `registration.yaml` and exits.
3. Register `registration.yaml` with your homeserver, then start the bridge.
4. Open a DM with the bridge bot (`@mattermostbot:your.domain` by default) and send `login credentials`.

The bridge stores the Mattermost session token in its database, not your password. Logging out from the bridge ends that session on the Mattermost server.

## Development

Building needs cgo (SQLite). Encryption uses goolm:

```sh
go vet -tags goolm ./...
go test -race -tags goolm ./...
go build -tags goolm ./cmd/mautrix-mattermost
```

Contributions are signed off under the [Developer Certificate of Origin](https://developercertificate.org/): `git commit -s`.

## License

[AGPL-3.0](LICENSE), like the other mautrix bridges.
