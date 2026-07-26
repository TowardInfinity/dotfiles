-- Autocommands & user commands.
-- Loaded from options.lua so init.lua stays as NvChad shipped it.

local augroup = function(name)
  return vim.api.nvim_create_augroup("my_" .. name, { clear = true })
end

-- ──── Briefly highlight yanked text ───────────────────────────
-- Confirms visually what got copied. Costs nothing, catches wrong-motion yanks.
vim.api.nvim_create_autocmd("TextYankPost", {
  group = augroup "yank_highlight",
  callback = function()
    vim.hl.on_yank { higroup = "IncSearch", timeout = 150 }
  end,
})

-- ──── Reopen a file where you left it ─────────────────────────
vim.api.nvim_create_autocmd("BufReadPost", {
  group = augroup "last_position",
  callback = function(args)
    local mark = vim.api.nvim_buf_get_mark(args.buf, '"')
    local lcount = vim.api.nvim_buf_line_count(args.buf)
    if mark[1] > 0 and mark[1] <= lcount then
      pcall(vim.api.nvim_win_set_cursor, 0, mark)
    end
  end,
})

-- ──── Per-language indentation ────────────────────────────────
-- Global default is 2. These languages conventionally use 4.
vim.api.nvim_create_autocmd("FileType", {
  group = augroup "indent_4",
  pattern = { "java", "python", "go", "rust", "sql", "kotlin", "gradle" },
  callback = function()
    vim.bo.shiftwidth = 4
    vim.bo.tabstop = 4
    vim.bo.softtabstop = 4
  end,
})

-- Go uses real tabs, not spaces
vim.api.nvim_create_autocmd("FileType", {
  group = augroup "go_tabs",
  pattern = "go",
  callback = function()
    vim.bo.expandtab = false
  end,
})

-- ──── Prose files: wrap and spell-check ───────────────────────
-- Markdown here is mostly the Obsidian vault, where hard-wrapped lines are a
-- nuisance — soft wrap instead, and turn off the "trailing whitespace" listchar
-- because two trailing spaces are a meaningful line break in Markdown.
vim.api.nvim_create_autocmd("FileType", {
  group = augroup "prose",
  pattern = { "markdown", "gitcommit", "text" },
  callback = function()
    vim.wo.wrap = true
    vim.wo.spell = true
    vim.wo.list = false
    vim.bo.textwidth = 0
  end,
})

-- ──── Close throwaway buffers with plain q ────────────────────
vim.api.nvim_create_autocmd("FileType", {
  group = augroup "quick_close",
  pattern = { "help", "qf", "man", "lspinfo", "checkhealth", "query", "startuptime" },
  callback = function(args)
    vim.bo[args.buf].buflisted = false
    vim.keymap.set("n", "q", "<cmd>close<cr>", { buffer = args.buf, silent = true })
  end,
})

-- ──── Create missing parent dirs on save ──────────────────────
-- :e src/new/deep/File.java then :w now just works.
vim.api.nvim_create_autocmd("BufWritePre", {
  group = augroup "mkdir",
  callback = function(args)
    if args.match:match "^%w%w+://" then
      return
    end
    vim.fn.mkdir(vim.fn.fnamemodify(vim.uv.fs_realpath(args.match) or args.match, ":p:h"), "p")
  end,
})

-- ──── Reload files changed outside nvim ───────────────────────
-- Matters when the Obsidian vault syncs, or a branch changes under you.
vim.api.nvim_create_autocmd({ "FocusGained", "TermClose", "TermLeave" }, {
  group = augroup "checktime",
  callback = function()
    if vim.o.buftype ~= "nofile" then
      vim.cmd "checktime"
    end
  end,
})

-- ══════════════════════════════════════════════════════════════
-- User commands
-- ══════════════════════════════════════════════════════════════

-- :FormatToggle       toggle format-on-save for this buffer
-- :FormatToggle!      toggle it globally
vim.api.nvim_create_user_command("FormatToggle", function(args)
  if args.bang then
    vim.g.disable_autoformat = not vim.g.disable_autoformat
    vim.notify("format-on-save (global): " .. (vim.g.disable_autoformat and "OFF" or "ON"))
  else
    vim.b.disable_autoformat = not vim.b.disable_autoformat
    vim.notify("format-on-save (buffer): " .. (vim.b.disable_autoformat and "OFF" or "ON"))
  end
end, { desc = "Toggle format-on-save", bang = true })

-- :Redir <cmd>  — dump the output of any command into a scratch buffer
vim.api.nvim_create_user_command("Redir", function(ctx)
  local out = vim.api.nvim_exec2(ctx.args, { output = true }).output
  vim.cmd "new"
  vim.api.nvim_buf_set_lines(0, 0, -1, false, vim.split(out, "\n"))
  vim.bo.buftype = "nofile"
end, { nargs = "+", complete = "command", desc = "Redirect command output to a buffer" })
