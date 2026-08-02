# Languages

Servers and formatters install themselves on first launch. If something
is missing, `:MasonToolsInstall` forces it.

| Language | Language server | Formatter |
| --- | --- | --- |
| Lua | `lua-language-server` | `stylua` |
| TypeScript / JS / TSX | `typescript-language-server` | `prettierd` → `prettier` |
| JSON / JSONC | `json-lsp` | `prettierd` → `prettier` |
| YAML | `yaml-language-server` | `prettierd` → `prettier` |
| TOML | `taplo` | `taplo` |
| Bash / sh | `bash-language-server` | `shfmt` |
| Markdown | `marksman` | `prettierd` → `prettier` |
| HTML / CSS / SCSS | `html-lsp`, `css-lsp`, `tailwindcss` | `prettierd` → `prettier` |
| Go | `gopls` | `goimports` → `gofumpt` |
| Python | `pyright`, `ruff` | `ruff` (imports, then format) |
| Java | `jdtls` + debug & test adapters | `google-java-format --aosp` |
| Dockerfile | `dockerfile-language-server` | — |
| Kotlin / Groovy | — | — highlighting only |
| Everything else | — | Trim trailing whitespace & newlines |
