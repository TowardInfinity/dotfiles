return {
  -- ══════════════════════════════════════════════════════════
  -- NvChad's own repos — branch pins, not new plugins
  -- ══════════════════════════════════════════════════════════
  -- init.lua pins NvChad core to v2.5, but the two repos it pulls in
  -- (`nvchad/base46`, `nvchad/ui`) are declared with no branch at all in
  -- nvchad/plugins/init.lua. An unpinned plugin follows whatever the remote's
  -- default branch was *at clone time*, frozen into that clone's origin/HEAD —
  -- so two machines set up months apart land on different branches and both
  -- think they are right. lazy-lock.json cannot fix this: `:Lazy restore` only
  -- ever checks out commits, never branches, and it rewrites the lockfile from
  -- whatever is on disk afterwards, so drift gets recorded as truth.
  --
  -- These entries merge into NvChad's specs by name, adding only the branch.
  -- The values are the combination both machines are actually running today,
  -- verified working — not an upgrade. base46 v2.5 is deliberate: it is the
  -- branch that matches the pinned v2.5 core. ui v3.0 is likewise deliberate:
  -- it owns nvconfig.lua, whose nested `base46 = {}` schema is the one
  -- chadrc.lua is written against.
  { "nvchad/base46", branch = "v2.5" },
  { "nvchad/ui", branch = "v3.0" },

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
    -- lazy-lock.json records a `branch` field for this plugin, but nothing
    -- ever acts on it — lazy only runs `git checkout <sha>`, so `:Lazy
    -- restore` will not put a drifted clone back on master either. This pin
    -- is the only thing that enforces the branch. (An earlier version of this
    -- comment claimed restore honoured the lockfile; it does not, and acting
    -- on that belief is what let base46 drift apart across two machines. The
    -- note at the top of this file has it right.)
    branch = "master",
    -- NvChad's spec sets `build = ":TSUpdate | TSInstallAll"`, but TSInstallAll
    -- is not a command on the master branch we pin — every fresh install and
    -- every :Lazy update threw "E492: Not an editor command: TSInstallAll".
    -- Parsers still arrived via ensure_installed below, so the error was pure
    -- noise, but it is the first thing a new machine shows you. Override it.
    build = ":TSUpdate",
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
        "lua",
        "luadoc",
        "vim",
        "vimdoc",
        "query",
        "regex",
        "bash",
        "json",
        "jsonc",
        "yaml",
        "toml",
        "dockerfile",
        "gitignore",
        "gitcommit",
        "git_rebase",
        "diff",
        -- work stack
        "java",
        "kotlin",
        "groovy", -- Spring Boot + Gradle Kotlin DSL
        "typescript",
        "tsx",
        "javascript",
        "jsdoc",
        "html",
        "css",
        "scss",
        "python",
        "go",
        "gomod",
        "gosum",
        "sql",
        -- writing
        "markdown",
        "markdown_inline",
      }
      opts.highlight = { enable = true }
      opts.indent = { enable = true }
      -- Syntax-node text objects, provided by nvim-treesitter-textobjects.
      opts.textobjects = {
        select = {
          enable = true,
          lookahead = true, -- jump forward to the next one if not inside it
          keymaps = {
            ["af"] = "@function.outer",
            ["if"] = "@function.inner",
            ["ac"] = "@class.outer",
            ["ic"] = "@class.inner",
            ["aa"] = "@parameter.outer",
            ["ia"] = "@parameter.inner",
            ["ai"] = "@conditional.outer",
            ["ii"] = "@conditional.inner",
            ["al"] = "@loop.outer",
            ["il"] = "@loop.inner",
          },
        },
        move = {
          enable = true,
          set_jumps = true, -- so <C-o> comes back
          goto_next_start = { ["]f"] = "@function.outer", ["]c"] = "@class.outer", ["]a"] = "@parameter.inner" },
          goto_previous_start = { ["[f"] = "@function.outer", ["[c"] = "@class.outer", ["[a"] = "@parameter.inner" },
        },
        swap = {
          enable = true,
          -- Reorder function arguments without touching the commas.
          swap_next = { ["<leader>na"] = "@parameter.inner" },
          swap_previous = { ["<leader>pa"] = "@parameter.inner" },
        },
      }
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
    -- mason-org, not williamboman. The repo transferred orgs, and NvChad v2.5's
    -- own spec already says mason-org — lazy merges specs by short name, so the
    -- two URLs collided into one entry and the stale one happened to win here.
    -- It works today only because GitHub still 301-redirects the old path.
    dependencies = { "mason-org/mason.nvim" },
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
        "java-debug-adapter", -- nvim-jdtls: debugging
        "java-test", -- nvim-jdtls: run/debug JUnit from the buffer
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
      {
        "s",
        function()
          require("flash").jump()
        end,
        mode = { "n", "x", "o" },
        desc = "Flash jump",
      },
      {
        "S",
        function()
          require("flash").treesitter()
        end,
        mode = { "n", "x", "o" },
        desc = "Flash treesitter",
      },
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

  -- ══════════════════════════════════════════════════════════
  -- Syntax-aware editing
  -- ══════════════════════════════════════════════════════════

  -- Select and move by syntax node instead of by line:
  --   vif / vaf  inner / around function      ]f [f  next / prev function
  --   vic / vac  inner / around class         ]c [c  next / prev class
  --   via / vaa  inner / around argument      ]a [a  next / prev argument
  -- `daf` deletes a whole function regardless of how many lines it spans —
  -- the single biggest editing win available in a treesitter config.
  {
    "nvim-treesitter/nvim-treesitter-textobjects",
    branch = "master", -- must match nvim-treesitter's pinned branch
    dependencies = { "nvim-treesitter/nvim-treesitter" },
    event = { "BufReadPost", "BufNewFile" },
  },

  -- Auto-close and auto-rename JSX/HTML tags. Typing <div> gives you </div>,
  -- and renaming one end renames the other. Earns its place on the Workers /
  -- React side of things.
  {
    "windwp/nvim-ts-autotag",
    ft = { "html", "xml", "javascriptreact", "typescriptreact", "svelte", "vue", "markdown" },
    opts = {},
  },

  -- ══════════════════════════════════════════════════════════
  -- Writing & history
  -- ══════════════════════════════════════════════════════════

  -- Renders Markdown in the buffer — headings, tables, code blocks, callouts.
  -- Worth it because a lot of Markdown editing here is the Obsidian vault.
  -- <leader>um toggles back to raw text.
  {
    "MeanderingProgrammer/render-markdown.nvim",
    ft = { "markdown" },
    dependencies = { "nvim-treesitter/nvim-treesitter", "nvim-tree/nvim-web-devicons" },
    opts = {
      heading = { sign = false },
      code = { sign = false, width = "block", left_pad = 1, right_pad = 1 },
      checkbox = { enabled = true },
    },
  },

  -- Browse the undo tree. options.lua enables persistent undo, which means
  -- history survives closing the file — this is what makes that reachable.
  {
    "mbbill/undotree",
    cmd = { "UndotreeToggle", "UndotreeShow" },
    keys = {
      { "<leader>uu", "<cmd>UndotreeToggle<cr>", desc = "Toggle undo tree" },
    },
  },

  -- ══════════════════════════════════════════════════════════
  -- Java / Spring Boot
  -- ══════════════════════════════════════════════════════════

  -- jdtls is not a normal language server: it wants a workspace per project
  -- and exposes refactorings (extract method, organize imports) and a test
  -- runner that the plain LSP protocol has no way to express. nvim-jdtls is
  -- the client for those. All the actual setup lives in ftplugin/java.lua,
  -- which Neovim runs per Java buffer.
  {
    "mfussenegger/nvim-jdtls",
    ft = "java",
    dependencies = { "mfussenegger/nvim-dap" },
  },

  -- Required by nvim-jdtls for `test_nearest_method` / `test_class` — those
  -- run through the debug adapter even when you are not debugging. No UI
  -- configured; add nvim-dap-ui later if you want breakpoint inspection.
  {
    "mfussenegger/nvim-dap",
    lazy = true,
    keys = {
      {
        "<leader>Jb",
        function()
          require("dap").toggle_breakpoint()
        end,
        desc = "Java toggle breakpoint",
      },
      {
        "<leader>Jd",
        function()
          require("dap").continue()
        end,
        desc = "Java debug continue",
      },
    },
  },
}
