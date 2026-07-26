-- LSP setup.
--
-- NvChad's defaults() already calls vim.lsp.config("*", ...) with its shared
-- capabilities/on_init and enables lua_ls. Everything below layers on top using
-- the same native API (Neovim 0.11+), NOT the old
-- require("lspconfig").x.setup{} framework — that one is deprecated and emits
-- "vim.lsp.start_client() is deprecated" on this Neovim.
--
-- The servers themselves are installed by mason-tool-installer, declared in
-- lua/plugins/init.lua.

require("nvchad.configs.lspconfig").defaults()

-- ──── Servers that need no extra settings ─────────────────────
local servers = {
  "html",
  "cssls",
  "ts_ls", -- TypeScript / JavaScript — Cloudflare Workers, node
  "bashls",
  "taplo", -- TOML — wrangler.toml, pyproject.toml
  "marksman", -- Markdown — the Obsidian vault
  "dockerls",
}

-- ──── Per-server settings ─────────────────────────────────────

-- Tailwind attaches to far too much by default (it will happily latch onto
-- Markdown). Restrict it to real web filetypes, and only in projects that
-- actually have a Tailwind config.
vim.lsp.config("tailwindcss", {
  filetypes = {
    "html",
    "css",
    "scss",
    "javascript",
    "javascriptreact",
    "typescript",
    "typescriptreact",
    "svelte",
    "vue",
    "astro",
  },
  -- root_markers alone is not enough: with no marker found, the server still
  -- starts in single-file mode and attaches to every .ts buffer. Resolving the
  -- root ourselves and simply not calling on_dir means it never attaches
  -- outside a real Tailwind project.
  root_dir = function(bufnr, on_dir)
    local markers = {
      "tailwind.config.js",
      "tailwind.config.cjs",
      "tailwind.config.mjs",
      "tailwind.config.ts",
      "postcss.config.js",
      "postcss.config.mjs",
    }
    local found = vim.fs.find(markers, {
      upward = true,
      path = vim.fs.dirname(vim.api.nvim_buf_get_name(bufnr)),
    })[1]
    if found then
      on_dir(vim.fs.dirname(found))
    end
  end,
})

-- JSON / YAML validated against SchemaStore, so package.json, tsconfig,
-- GitHub Actions workflows and friends get real completion and errors.
local ok_schemastore, schemastore = pcall(require, "schemastore")

vim.lsp.config("jsonls", {
  settings = {
    json = {
      schemas = ok_schemastore and schemastore.json.schemas() or nil,
      validate = { enable = true },
    },
  },
})

vim.lsp.config("yamlls", {
  settings = {
    yaml = {
      schemaStore = { enable = false, url = "" }, -- use the plugin's list instead
      schemas = ok_schemastore and schemastore.yaml.schemas() or nil,
      keyOrdering = false, -- otherwise it nags about unsorted keys constantly
    },
  },
})

-- Python: pyright for types, ruff for lint + import sorting.
-- Hover is disabled on ruff so the two don't both answer the same request.
vim.lsp.config("pyright", {
  settings = {
    pyright = { disableOrganizeImports = true }, -- ruff owns this
    python = {
      analysis = {
        typeCheckingMode = "basic",
        autoSearchPaths = true,
        useLibraryCodeForTypes = true,
      },
    },
  },
})

vim.lsp.config("ruff", {
  on_attach = function(client)
    client.server_capabilities.hoverProvider = false
  end,
})

-- Go
vim.lsp.config("gopls", {
  settings = {
    gopls = {
      analyses = { unusedparams = true, shadow = true },
      staticcheck = true,
      gofumpt = true,
    },
  },
})

-- Java is deliberately NOT configured here. jdtls is driven by nvim-jdtls
-- instead, from ftplugin/java.lua — it needs per-project workspaces, extension
-- bundles for tests and debugging, and refactorings the plain LSP client
-- cannot express. Enabling it in both places starts two clients on the same
-- buffer, which manifests as duplicated completions and diagnostics.

vim.lsp.enable(servers)
vim.lsp.enable { "tailwindcss", "jsonls", "yamlls", "pyright", "ruff", "gopls" }
