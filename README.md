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

Download the program for your system from
[Releases](https://github.com/EverythingSuckz/portage/releases/latest) and
unzip it. Each release lists SHA-256 checksums in `checksums.txt`.

With Go 1.25 or newer installed, you can instead run:

```
go install go.wrench.rocks/portage/cmd/portage@latest
```

## Connect your banks

Run these once in a terminal. Logins are saved, readable only by you, in your
user config folder (`%AppData%\portage` on Windows, `~/.config/portage` on
Linux, `~/Library/Application Support/portage` on macOS). Pass `-data <dir>` or
set `PORTAGE_DATA` to use another folder.

**SimpleFIN Bridge:** create a setup token on the Bridge website, then run
`portage simplefin claim <setup-token>`. Each token works only once.

**Rogers Bank:** run `portage rogers login`. It asks for your username,
password, and a verification code sent by email or text. Rogers then trusts this
device for a few months; when it stops, the tools return a warning asking you
to log in again.

- `-name <name>` connects more than one Rogers login.
- `-store-password=false` keeps the password out of the saved file. Give the
  server a `ROGERS_PASSWORD` environment variable instead.

## Add it to your AI app

**Claude Desktop:** add this to `claude_desktop_config.json`, then restart the
app:

```json
{
  "mcpServers": {
    "portage": { "command": "portage", "args": ["serve"] }
  }
}
```

**Claude Code:** `claude mcp add portage -- portage serve`

If `portage` isn't on your `PATH`, use the full path to the program.

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

Both tools are read-only.

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
