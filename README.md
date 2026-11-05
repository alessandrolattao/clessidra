# clessidra

How much of your Claude plan limits you have used, and when you run out at this pace.

Claude subscription plans have two usage windows: a 5-hour session window and a 7-day weekly window. `clessidra` (Italian for hourglass) reads both for one Claude Code account and carries the average pace since each window started forward to 100%.

```
$ clessidra
account  /home/you/.claude
5h       19%  resets 14:00             lasts until reset
7d       13%  resets Tue 10 Nov 17:00  lasts until reset
```

When the pace would use a window up before it resets:

```
5h       80%  resets 14:00             runs out in 45m (12:45)
```

## Install

```sh
go install github.com/alessandrolattao/clessidra@latest
```

or download a Linux binary (amd64, arm64) from the [releases](https://github.com/alessandrolattao/clessidra/releases).

Linux only: on macOS Claude Code keeps its login in the Keychain, which `clessidra` does not read.

## Usage

```
clessidra [--config-dir DIR]
```

The account is a Claude Code config directory, picked the way Claude Code picks it:

1. `--config-dir DIR`
2. `CLAUDE_CONFIG_DIR`, so inside a Claude Code session `clessidra` reports the account of that session
3. `~/.claude`

With several accounts side by side, one config directory each:

```sh
clessidra --config-dir ~/.claude-work
```

Exit codes: 0 on success, 1 when the usage cannot be read, 2 on a wrong command line.

## How it works

- It reads the access token from `<config dir>/.credentials.json`. It never refreshes the token and never writes the file: when the token has expired it says so, and opening Claude Code with that account renews it.
- It asks `https://api.anthropic.com/api/oauth/usage` for the `five_hour` and `seven_day` windows. Anthropic does not document this endpoint, so it can change without notice, and calling it more than about once a minute can get rate limited.
- The estimate is the average pace since the window started: used percent divided by the time elapsed. In the first 5% of a window (15 minutes of the 5-hour one, about 8 hours of the weekly one) it is too early to say.

## License

GPL-3.0. Not affiliated with or endorsed by Anthropic. Claude is a trademark of Anthropic.
