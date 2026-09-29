# Configuration

## Locations

| What | Default | Override |
| --- | --- | --- |
| ocws home | `$XDG_CONFIG_HOME/ocws` or `~/.config/ocws` (`%APPDATA%\ocws` on Windows) | `--home`, `$OCWS_HOME` |
| Config file | `<home>/config.toml` | |
| Templates root | `<home>/templates` | `--templates`, `$OCWS_TEMPLATES`, `templates` in config |
| Workspace state | `<workspace>/.ocws/manifest.json` | |

## config.toml

```toml
source = "https://github.com/you/agent-templates.git"  # set by `ocws init`
default_harnesses = ["opencode", "claude"]             # used when --harness is not given
# templates = "~/src/agent-templates"                  # use a checkout instead of <home>/templates
```

## npm launcher

The `@c0dn/ocws` npm package downloads the release binary on first run and
caches it per version in `~/.cache/ocws/<version>/` (`~/Library/Caches/ocws`
on macOS, `%LOCALAPPDATA%\ocws\cache` on Windows).

| Variable | Effect |
| --- | --- |
| `OCWS_BINARY` | Use this binary and skip the download. |
| `OCWS_CACHE_DIR` | Cache location. |
| `NODE_USE_ENV_PROXY=1` with `HTTPS_PROXY` | Download through a proxy (Node versions with `NODE_USE_ENV_PROXY` support). |
