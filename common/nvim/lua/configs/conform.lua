-- Formatting via conform.nvim.
--
-- Format-on-save is ON. Toggle it with :FormatToggle (buffer) or
-- :FormatToggle! (global) — defined in lua/autocmds.lua — or <leader>uf.
-- Manual format is <leader>fm.

local prettier = { "prettierd", "prettier", stop_after_first = true }

local options = {
  formatters_by_ft = {
    lua = { "stylua" },

    -- Web / Cloudflare Workers
    javascript = prettier,
    javascriptreact = prettier,
    typescript = prettier,
    typescriptreact = prettier,
    css = prettier,
    scss = prettier,
    html = prettier,
    json = prettier,
    jsonc = prettier,
    yaml = prettier,
    markdown = prettier,

    -- Backend
    java = { "google-java-format" },
    go = { "goimports", "gofumpt" }, -- imports first, then stricter gofmt
    python = { "ruff_organize_imports", "ruff_format" },
    sh = { "shfmt" },
    bash = { "shfmt" },
    toml = { "taplo" },

    -- Trim trailing whitespace + fix final newline everywhere else
    ["_"] = { "trim_whitespace", "trim_newlines" },
  },

  formatters = {
    shfmt = { prepend_args = { "-i", "2", "-ci" } },
    ["google-java-format"] = { prepend_args = { "--aosp" } }, -- 4-space, not 2
  },

  -- A function (rather than a table) so the toggle can veto per buffer.
  format_on_save = function(bufnr)
    if vim.g.disable_autoformat or vim.b[bufnr].disable_autoformat then
      return
    end
    -- Don't reformat files you're only passing through — e.g. node_modules,
    -- or the Obsidian vault where Prettier would rewrite other tools' output.
    --
    -- The vault check used to be described here but not actually implemented,
    -- so every note save was silently reformatted by prettier. That matters
    -- for Markdown specifically: prettier strips the trailing double-space
    -- hard breaks that autocmds.lua's prose mode treats as meaningful, and
    -- reflows callouts, checklists and wiki-links that Obsidian owns.
    --
    -- Matched by vault-directory name rather than an absolute path: this file
    -- is shared verbatim with Linux servers, and hardcoding a macOS iCloud
    -- path under a home directory would be both broken there and needlessly
    -- personal in a public repo.
    local path = vim.api.nvim_buf_get_name(bufnr)
    if
      path:match "/node_modules/"
      or path:match "/%.git/"
      or path:match "iCloud~md~obsidian"
      or path:match "/Obsidian Notes/"
      or vim.fs.root(bufnr, ".obsidian") ~= nil
    then
      return
    end
    return { timeout_ms = 1500, lsp_format = "fallback" }
  end,

  notify_on_error = true,
}

return options
