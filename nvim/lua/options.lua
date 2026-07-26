require "nvchad.options"

local o = vim.o

-- ──── Movement & view ─────────────────────────────────────────
o.relativenumber = true   -- relative line numbers: 5j / 12k become obvious
o.cursorlineopt = "both"  -- highlight the line AND the number
o.scrolloff = 8           -- keep 8 lines of context above/below the cursor
o.sidescrolloff = 8
o.wrap = false
o.splitright = true       -- new vertical splits open to the RIGHT
o.splitbelow = true       -- new horizontal splits open BELOW
o.linebreak = true        -- if wrap is ever on, break at words not mid-word

-- ──── Searching ───────────────────────────────────────────────
o.ignorecase = true
o.smartcase = true        -- ...unless the query has a capital letter
o.inccommand = "split"    -- live preview of :s/foo/bar as you type

-- ──── Persistent undo ─────────────────────────────────────────
-- Undo history survives closing the file. Probably the single biggest
-- quality-of-life option in this file.
o.undofile = true
o.undolevels = 10000

-- ──── Editing ─────────────────────────────────────────────────
o.expandtab = true
o.shiftwidth = 2          -- per-language overrides live in autocmds.lua
o.tabstop = 2
o.softtabstop = 2
o.smartindent = true
o.confirm = true          -- ask to save instead of failing :q with changes
o.updatetime = 250        -- faster CursorHold -> gitsigns/diagnostics appear sooner
o.timeoutlen = 400
o.signcolumn = "yes"      -- always on, so text doesn't jump when a sign appears
o.virtualedit = "block"   -- let visual-block select past end of line

-- ──── Whitespace made visible ─────────────────────────────────
o.list = true
vim.opt.listchars = { tab = "» ", trail = "·", nbsp = "␣" }

-- ──── Files ───────────────────────────────────────────────────
o.swapfile = false        -- undofile + git make swapfiles more annoying than useful
o.backup = false
o.writebackup = false
o.autoread = true

-- ──── Completion ──────────────────────────────────────────────
vim.opt.completeopt = { "menu", "menuone", "noselect" }
o.pumheight = 12          -- cap the completion popup height

-- ──── Diagnostics ─────────────────────────────────────────────
-- Virtual text is OFF by default: it shoves code sideways and gets very noisy
-- in Java. Diagnostics still show in the sign column, and <leader>d floats the
-- full message. Toggle virtual text any time with <leader>uv.
vim.diagnostic.config {
  virtual_text = false,
  signs = true,
  underline = true,
  update_in_insert = false,
  severity_sort = true,
  float = { border = "rounded", source = true },
}

-- Autocommands live in their own file
require "autocmds"
