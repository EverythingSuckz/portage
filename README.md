<p align="center">
  <img src="assets/logo.svg" width="112" alt="portage logo">
</p>

<h1 align="center">portage</h1>

<p align="center">
  Let your AI see your Canadian bank accounts. Read-only, and your logins never leave your computer.
</p>

<p align="center">
  <a href="https://github.com/EverythingSuckz/portage/releases/latest"><img src="https://img.shields.io/github/v/release/EverythingSuckz/portage" alt="Latest release"></a>
  <a href="https://github.com/EverythingSuckz/portage/actions/workflows/ci.yml"><img src="https://github.com/EverythingSuckz/portage/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/EverythingSuckz/portage" alt="MIT license"></a>
</p>

---

Portage is an [MCP](https://modelcontextprotocol.io) server for Claude, Cursor,
VS Code, and other AI apps. Ask things like:

> *What's my credit card balance?* · *How much did I spend on groceries last
> month?* · *Show my biggest purchases since September.*

## Install

**Claude Desktop:** download `portage-mcp_<version>.mcpb` from
[Releases](https://github.com/EverythingSuckz/portage/releases/latest) and
double-click it.

**Windows** (PowerShell):

```
irm https://go.wrench.rocks/portage/install.ps1 | iex
```

**macOS and Linux:**

```
curl -fsSL https://go.wrench.rocks/portage/install.sh | sh
```

The installer checks the download against the release checksums, installs it
for your user only, and adds portage to Claude Desktop, Claude Code, Cursor,
and VS Code. Restart your AI app, then ask it to **"connect my bank"**: a
sign-in page opens in your browser, so passwords never go through the chat.

## Supported banks

| Bank | Accounts | History |
|------|----------|---------|
| Rogers Bank | Credit cards | Current and last 2 statements |
| [Any bank on SimpleFIN Bridge](https://beta-bridge.simplefin.org/search-institutions) (paid service) | Depends on the bank | Last 90 days |

> Rogers uses an unofficial API that can change at any time. Use it with your
> own account, at your own risk.

## Privacy

Portage runs entirely on your computer and collects no analytics. Logins are
saved in a file only your user account can read, and portage talks only to your
bank or SimpleFIN Bridge. Your AI app sees account names, balances, and
transactions, never passwords or verification codes. To remove everything,
delete the `portage` folder in your user config folder (`%AppData%` on Windows,
`~/Library/Application Support` on macOS, `~/.config` on Linux).

<details>
<summary><b>More: other install methods, terminal setup, tools</b></summary>

### Other ways to install

- Download the archive for your system from
  [Releases](https://github.com/EverythingSuckz/portage/releases/latest), unzip
  it, and run `portage install`.
- With Go 1.26 or newer: `go install go.wrench.rocks/portage/cmd/portage@latest`.
- Installer settings: `PORTAGE_VERSION` (like `v0.2.0`), `PORTAGE_INSTALL_DIR`,
  `PORTAGE_NO_APPS=1`, and on Windows `PORTAGE_NO_MODIFY_PATH=1`.

### Commands

| Command | What it does |
|---------|--------------|
| `portage install [app...]` | Add portage to AI apps (`claude-desktop`, `claude-code`, `cursor`, `vscode`). Keeps a `.bak` of each config. |
| `portage connect <rogers\|simplefin>` | Open the sign-in page from a terminal. |
| `portage rogers login` | Sign in to Rogers in the terminal. `-store-password=false` keeps the password out of the saved file (set `ROGERS_PASSWORD` instead). |
| `portage simplefin claim <token>` | Connect SimpleFIN Bridge with a setup token. |
| `portage serve` | Run the MCP server over stdio (what AI apps start). |

Every command takes `-data <dir>` (or `PORTAGE_DATA`) to use another data folder.

To add portage to an app by hand, use command `portage` with args `["serve"]`
(full path if it isn't on your `PATH`).

### Tools

| Tool | What it does |
|------|--------------|
| `list_accounts` | Every connected account with its balance. |
| `get_transactions` | Transactions, newest first. Filters: `account_id`, `start_date`, `end_date`, `search`, `limit`. |
| `connect_bank` | Opens the sign-in page to connect `rogers` or `simplefin`. |

Amounts are decimal strings: positive is money in, negative is money out, and a
negative card balance is the amount owed. Results are cached for 15 minutes.

### Adding a bank

Implement `bank.Source` (`Name` and `Fetch`) in a new package under `internal/`
and load it in `loadSources` in `cmd/portage/main.go`. `internal/rogers` is a
full example with sign-in and two-factor verification.

### Development

```
go test ./...
go vet ./...
bunx @modelcontextprotocol/inspector go run ./cmd/portage serve
```

</details>

## License

[MIT](LICENSE)
