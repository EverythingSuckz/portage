# portage

A read-only [MCP](https://modelcontextprotocol.io) server that lets AI agents
(Claude, Cursor, VS Code, and others) see balances and transactions from
Canadian bank accounts. It runs on your computer, and your logins never leave
it.

> Direct adapters use unofficial bank APIs that can change or break at any time.
> Use them with your own accounts, at your own risk.

## Supported banks

| Bank | Connection | Accounts | Transaction history |
|------|------------|----------|---------------------|
| Rogers Bank | Direct adapter | Credit cards | Current and previous 2 statement periods |
| [Banks on SimpleFIN Bridge](https://beta-bridge.simplefin.org/search-institutions) | [SimpleFIN Bridge](https://bridge.simplefin.org) (paid) | Depends on the bank | Last 90 days |

Balances and pending transactions are included for all of them.

## Install

**Windows** (PowerShell):

```
irm https://go.wrench.rocks/portage/install.ps1 | iex
```

**macOS and Linux:**

```
curl -fsSL https://go.wrench.rocks/portage/install.sh | sh
```

The installer downloads the latest release, checks it against the published
SHA-256 checksums, installs it for your user only (no admin rights needed), and
then runs `portage install` to add portage to your AI apps. Restart the app
afterwards. Run the same command again to update.

<details>
<summary>Other ways to install</summary>

- Download the archive for your system from
  [Releases](https://github.com/EverythingSuckz/portage/releases/latest),
  unzip it, and run `portage install`.
- With Go 1.26 or newer: `go install go.wrench.rocks/portage/cmd/portage@latest`.
- Installer settings: `PORTAGE_VERSION` (a specific release, like `v0.2.0`),
  `PORTAGE_INSTALL_DIR` (where to put it), and `PORTAGE_NO_APPS=1` (skip
  setting up AI apps). On Windows, `PORTAGE_NO_MODIFY_PATH=1` leaves your PATH
  alone.

</details>

## Connect your banks

The easiest way is to ask your AI app, for example "connect my Rogers account".
It calls the `connect_bank` tool, which opens a sign-in page in your browser.
Passwords and verification codes are typed on that page, never in the chat.

You can also open the same page from a terminal:

```
portage connect rogers
portage connect simplefin
```

Logins are saved, readable only by you, in your user config folder
(`%AppData%\portage` on Windows, `~/.config/portage` on Linux,
`~/Library/Application Support/portage` on macOS). Pass `-data <dir>` or set
`PORTAGE_DATA` to use another folder.

- **SimpleFIN Bridge** needs a setup token from the Bridge website. Each token
  works only once.
- **Rogers Bank** asks for your username, password, and a verification code sent
  by email or text. Rogers then trusts this device for a few months; when it
  stops, the tools return a warning asking you to connect again.

Prefer the terminal only? `portage simplefin claim <setup-token>` and
`portage rogers login` do the same without a browser. `portage rogers login`
also supports `-name <name>` for more than one Rogers login, and
`-store-password=false` to keep the password out of the saved file (give the
server a `ROGERS_PASSWORD` environment variable instead).

## Add it to your AI app

```
portage install
```

This adds portage to every supported app it finds: Claude Desktop, Claude Code,
Cursor, and VS Code. Name apps to pick only some, for example
`portage install claude-desktop`. Existing settings are kept, and the old file
is saved with a `.bak` extension. Restart the app afterwards.

To set it up by hand instead, add this to `claude_desktop_config.json` (or your
app's MCP settings), using the full path to `portage` if it isn't on your
`PATH`:

```json
{
  "mcpServers": {
    "portage": { "command": "portage", "args": ["serve"] }
  }
}
```

**MCP Inspector** (for testing tools directly in your browser):

```
bunx @modelcontextprotocol/inspector portage serve
```

Use `npx` instead of `bunx` if you don't have Bun. Add `-data <dir>` after
`serve` if your logins are somewhere other than the default folder.

## Tools

| Tool | What it does |
|------|--------------|
| `list_accounts` | Every connected account with its balance and available balance. |
| `get_transactions` | Transactions, newest first. Optional filters: `account_id`, `start_date`, `end_date` (YYYY-MM-DD, inclusive), `search` (description or category), `limit` (default 100, max 1000). |
| `connect_bank` | Opens a sign-in page in your browser to connect `rogers` or `simplefin`. |

`list_accounts` and `get_transactions` are read-only. Nothing can move money.

- Amounts are decimal strings. Positive is money in, negative is money out. A
  negative credit card balance is the amount owed.
- Results are cached for 15 minutes, so agents don't log in to your bank on
  every question.
- If one bank fails, the others still return data, and the problem is listed in
  `warnings`.

## Adding a bank

A source implements one interface from `internal/bank`:

```go
type Source interface {
    Name() string
    Fetch(ctx context.Context) (Snapshot, error)
}
```

`Fetch` returns every account and recent transaction. Filtering, caching, and
merging happen in `internal/server`. Load the new source in `loadSources` in
`cmd/portage/main.go`. See `internal/rogers` for a full example with login and
two-factor verification.

## Development

```
go test ./...
go vet ./...
```

## License

[MIT](LICENSE)
