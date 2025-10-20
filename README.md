# codex-sessions TUI

`codex-sessions` is a cross-platform terminal user interface for browsing, searching, and managing [Codex CLI](https://docs.codex.com/) session logs. It provides a fast fuzzy-searchable list of past sessions, lets you resume one with a single press, and can prune obsolete session artifacts without leaving the terminal.

## Features

- **Fuzzy search** as you type across session IDs, working directories, timestamps, and last actions.
- **Keyboard-first navigation** with arrow keys, Page Up/Down, and instant highlighting.
- **Quick resume** with `Enter`, invoking `codex resume <session-id>` (or printing the ID with `--no-resume`).
- **Safe deletion** of a session and all associated log files via `Del`.
- **Responsive layout** powered by [`tview`](https://github.com/rivo/tview) and [`tcell`](https://github.com/gdamore/tcell) that works on Windows, Linux, and macOS terminals.

## Installation

Requires Go 1.22+ (module targets Go 1.25) and the Codex CLI (`codex`) available on your `PATH`.

```bash
go install github.com/Uri2001/codex-sessions@latest
```

If you prefer to build locally:

```bash
git clone https://github.com/Uri2001/codex-sessions.git
cd codex-sessions
go build ./...
```

## Usage

Run the TUI directly:

```bash
codex-sessions
```

By default the tool scans `~/.codex/sessions`. Command-line flags:

| Flag | Description |
|------|-------------|
| `--sessions-dir <path>` | Override the sessions directory (default `~/.codex/sessions`). |
| `--codex-bin <path>` | Path to the Codex CLI binary to execute (default `codex`). |
| `--no-resume` | Do not spawn `codex resume`; instead print the selected session ID to stdout. |

### Keybindings

| Keys | Action |
|------|--------|
| `Type` | Append characters to the search query (fuzzy search). |
| `Backspace` | Remove the last character from the search query. |
| `Esc` | Clear the search query; when empty, exit the app. |
| `Ctrl+C` | Quit immediately. |
| `Up` / `Down` | Move selection one row. |
| `PgUp` / `PgDn` | Page selection up/down. |
| `Enter` | Resume the highlighted session (or print its ID when `--no-resume` is set). |
| `Del` | Delete the highlighted session and its log files. |

## Development

```bash
go fmt ./...
go test ./...
go run .
```

### Project layout

- `main.go` — entrypoint parsing flags, invoking the UI, and running `codex resume`.
- `internal/sessions` — parsing and aggregating Codex CLI session JSONL logs.
- `internal/ui` — the TUI implementation built with `tview`.

## Contributing

1. Fork and clone the repository.
2. Create a feature branch: `git checkout -b feature/my-idea`.
3. Make changes, add tests if applicable, and run `go fmt ./...` + `go test ./...`.
4. Submit a pull request describing your changes.

## License

This project is licensed under the [MIT License](LICENSE).
