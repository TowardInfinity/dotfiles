return {
  -- ══════════════════════════════════════════════════════════
  -- Core: formatting, LSP, syntax
  -- ══════════════════════════════════════════════════════════

  {
    "stevearc/conform.nvim",
    event = "BufWritePre", -- required for format-on-save to fire
    opts = require "configs.conform",
  },

  {
    "neovim/nvim-lspconfig",
    config = function()
      require "configs.lspconfig"
    end,
  },

  -- JSON/YAML schemas: package.json, tsconfig, GH Actions, wrangler…
  { "b0o/schemastore.nvim", lazy = true },

  {
    "nvim-treesitter/nvim-treesitter",
    -- Pin the branch. Upstream has made `main` (the rewrite) the default, but
    -- NvChad v2.5 drives the old API — `require("nvim-treesitter.configs")`
    -- exists only on `master`. Without this, a fresh clone on a new machine
    -- silently gets `main` and every parser fails with:
    --   module 'nvim-treesitter.configs' not found
    -- lazy-lock.json records the branch too, but only `:Lazy restore` honours
    -- it; `:Lazy sync` follows the remote default. This makes it deterministic.
    branch = "master",
    -- opts MUST be a function here. NvChad declares
    --   opts = function() return require "nvchad.configs.treesitter" end
    -- which ignores its arguments and returns a fresh table — so a plain
    -- `opts = {...}` from this file gets silently thrown away and you end up
    -- with NvChad's 5 parsers. The function form receives the merged table
    -- and extends it instead.
    opts = function(_, opts)
      opts = opts or {}
      opts.ensure_installed = {
        -- editor / config
        "lua", "luadoc", "vim", "vimdoc", "query", "regex",
        "bash", "json", "jsonc", "yaml", "toml", "dockerfile",
        "gitignore", "gitcommit", "git_rebase", "diff",
        -- work stack
        "java", "kotlin", "groovy", -- Spring Boot + Gradle Kotlin DSL
        "typescript", "tsx", "javascript", "jsdoc",
        "html", "css", "scss",
        "python", "go", "gomod", "gosum", "sql",
        -- writing
        "markdown", "markdown_inline",
      }
      opts.highlight = { enable = true }
      opts.indent = { enable = true }
      return opts
    end,
    config = function(_, opts)
      require("nvim-treesitter.configs").setup(opts)
      -- Must run AFTER the plugin registers its own directives, so the
      -- force=true re-registration wins. See the file for the full story.
      require "configs.tscompat"
    end,
  },

  -- Auto-install every LSP server / formatter this config references, so a
  -- fresh machine converges on its own instead of failing quietly.
  {
    "WhoIsSethDaniel/mason-tool-installer.nvim",
    dependencies = { "williamboman/mason.nvim" },
    event = "VeryLazy",
    opts = {
      ensure_installed = {
        -- language servers
        "lua-language-server",
        "typescript-language-server",
        "json-lsp",
        "yaml-language-server",
        "taplo",
        "bash-language-server",
        "marksman",
        "dockerfile-language-server",
        "html-lsp",
        "css-lsp",
        "tailwindcss-language-server",
        "gopls",
        "pyright",
        "ruff",
        "jdtls",
        -- formatters
        "stylua",
        "prettierd",
        "shfmt",
        "google-java-format",
        "gofumpt",
        "goimports",
      },
      run_on_start = true,
      start_delay = 3000, -- let the UI settle before downloading
    },
  },

  -- ══════════════════════════════════════════════════════════
  -- Editing
  -- ══════════════════════════════════════════════════════════

  -- cs"'  change surrounding " to '  |  ysiw)  wrap word in parens
  -- ds"   delete surrounding "       |  (visual) S"  wrap selection
  {
    "kylechui/nvim-surround",
    event = "VeryLazy",
    opts = {},
  },

  -- s{char}{char} jumps anywhere on screen — replaces most / searching.
  {
    "folke/flash.nvim",
    event = "VeryLazy",
    opts = { modes = { char = { enabled = false } } }, -- leave f/t/F/T alone
    keys = {
      { "s", function() require("flash").jump() end, mode = { "n", "x", "o" }, desc = "Flash jump" },
      { "S", function() require("flash").treesitter() end, mode = { "n", "x", "o" }, desc = "Flash treesitter" },
    },
  },

  -- ══════════════════════════════════════════════════════════
  -- Navigation & diagnostics
  -- ══════════════════════════════════════════════════════════

  -- A real diagnostics / symbol list.
  {
    "folke/trouble.nvim",
    cmd = "Trouble",
    opts = { focus = true },
    keys = {
      { "<leader>xx", "<cmd>Trouble diagnostics toggle<cr>", desc = "Diagnostics (all)" },
      { "<leader>xb", "<cmd>Trouble diagnostics toggle filter.buf=0<cr>", desc = "Diagnostics (buffer)" },
      { "<leader>xs", "<cmd>Trouble symbols toggle<cr>", desc = "Symbol outline" },
      { "<leader>xq", "<cmd>Trouble qflist toggle<cr>", desc = "Quickfix list" },
    },
  },

  -- Highlights TODO/FIXME/HACK/NOTE and makes them searchable.
  {
    "folke/todo-comments.nvim",
    event = { "BufReadPost", "BufNewFile" },
    dependencies = { "nvim-lua/plenary.nvim" },
    opts = { signs = false },
    keys = {
      { "<leader>ft", "<cmd>TodoTelescope<cr>", desc = "Find TODOs" },
    },
  },
}
