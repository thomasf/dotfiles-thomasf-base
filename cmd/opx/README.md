# opx

1Password helper around the `op` CLI.

- `opx signin` prints an `export OP_SESSION_…` line for `eval`.
- One session shared by all shells and the picker.
- Session kept in `$XDG_RUNTIME_DIR/opx/` (`$TMPDIR/opx/` on macOS), mode 0600.
- Signs in again when the session is rejected.
- Password from a command (e.g. `pass`), the terminal or rofi.
- Profiles in `~/.config/opx/profiles/<name>.toml`.
- Default profile in `~/.config/opx/config.toml`.
- Pick a profile with `-p name`.
- `opx rofi` picks an item, then a field (Linux, X11).
- Last used item listed first.
- Next field preselected for 5 minutes.
- Enter copies, Ctrl+Enter types.
- Single-field items skip the field menu.
- Secrets masked in menus and never in notifications.
- Copied secrets replaced by the previous clipboard after 45s, like `pass -c`.
- One-time codes: waits for a fresh code near expiry.
