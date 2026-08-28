# Discord channel — full setup guide

This guide walks you through adding the Discord channel to omega, end to end,
on **Windows** and **macOS/Linux**. By the end you'll be able to DM an omega
bot and get answers with full per-channel session memory.

The Discord channel is the "output/channel seam" from the roadmap: a bot that
relays DMs to omega's HTTP gateway, mapping **one omega session per Discord
channel** so multi-turn DMs keep their memory.

```
You DM bot ──► omega discord ──► omega serve (HTTP /chat, session persisted)
     ▲                                  │
     └────────────── reply ◄────────────┘
```

---

## 1. Install omega

### macOS / Linux

```bash
git clone https://github.com/EndoTheDev/omega.git
cd omega
./build.sh
```

This runs vet + tests and builds `bin/omega`. (Or `go install
github.com/EndoTheDev/omega/cmd/omega@latest` to install to `~/go/bin`.)

### Windows

```bat
git clone https://github.com/EndoTheDev/omega.git
cd omega
build.bat
```

This builds `bin\omega.exe`.

> **Note:** the Discord extension is compiled into omega. If you're building
> from a source tree that includes `extensions/discord`, the `omega discord`
> subcommand is available automatically. If your build predates it, pull the
> latest source and rebuild.

---

## 2. Create the Discord bot

1. Open the [Discord Developer Portal](https://discord.com/developers/applications)
   and click **New Application**. Name it anything (e.g. "Omega").
2. Go to the **Bot** tab and click **Reset Token**, then copy the token.
3. On the same Bot tab, under **Privileged Gateway Intents**, enable
   **Message Content Intent**. This is required — without it Discord hides DM
   text from bots, so it can't read your messages.
4. Invite the bot to a server (or DM it): use the **OAuth2 → URL Generator**,
   select the **bot** scope, and grant:
   - Send Messages
   - Read Message History
   - Read Messages / View Channels

   (No `applications.commands` scope needed — this uses text commands, not
   native slash commands.)

---

## 3. Configure the token

Add a `discord` section to your `config.yaml`. Omega reads `config.yaml` from
the directory containing the binary (or pass `--config <path>`).

```yaml
discord:
  token: "your-bot-token-here"
```

Your `config.yaml` also needs a provider (so the gateway can reach a model):

```yaml
provider:
  type: ollama
  model_name: deepseek-v4-flash:cloud   # or any model you have
  host: http://localhost:11434
  api_key: ""
```

---

## 4. Run it

Start the gateway first, then the Discord bot in a second terminal.

### macOS / Linux

```bash
bin/omega serve          # terminal 1 — the HTTP gateway
bin/omega discord        # terminal 2 — the Discord bot
```

### Windows

```bat
bin\omega.exe serve
bin\omega.exe discord
```

You should see `omega: discord bot starting (gateway http://localhost:8099)`.
DM the bot (or message it in the server you invited it to) and it replies.

---

## 5. Commands

The bot treats any message that doesn't start with `/` as an agent prompt.
The `/` commands are handled by the bot itself:

| Command | What it does |
|---|---|
| `hi` / any text | Run the agent on that prompt in the channel's session |
| `/new` | Start a fresh session for this channel (forgets the old thread) |
| `/sessions` | List all sessions |
| `/sessions delete <id>` | Delete a session |
| `/resume <id>` | Switch this channel to an existing session |
| `/help` | Show the command list |

`/model` is not yet supported over Discord (the gateway has no model-switch
endpoint) — set the model in `config.yaml` and restart instead.

---

## 6. Troubleshooting

| Symptom | Cause / fix |
|---|---|
| Bot connects but can't read messages | **Message Content Intent** is off. Enable it in the Bot tab and restart `omega discord`. |
| `omega: discord bot starting ... unreachable` | The gateway isn't running. Start `omega serve` first. |
| `no token set` | `discord.token` is missing from config. |
| Bot replies "Cannot send an empty message" | The gateway returned an empty reply — usually the agent loop isn't wired. Rebuild from the latest source (this was fixed). |
| `omega: config: provider.model_name is required` | The config isn't being read. Put `config.yaml` next to the binary, or pass `--config <path>`. |

---

## 7. Notes

- Only **DMs** are handled for now (guild messages are ignored) to keep scope
  tight and avoid the bot reacting to channel noise.
- The bot ignores its own messages, so no reply loops.
- Long replies are chunked to Discord's 2000-char message limit.
