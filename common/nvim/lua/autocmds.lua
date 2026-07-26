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

-- ──── LSP niceties, per attached server ───────────────────────
-- NvChad's own LspAttach handles its keymaps; this adds two things it doesn't.
vim.api.nvim_create_autocmd("LspAttach", {
  group = augroup "lsp_extras",
  callback = function(args)
    local client = vim.lsp.get_client_by_id(args.data.client_id)
    if not client then
      return
    end

    -- Inlay hints: parameter names and inferred types shown inline. Genuinely
    -- useful in TypeScript, Go and Java where types are often implicit.
    -- Off by default because it widens lines; <leader>uh toggles per buffer.
    if client:supports_method "textDocument/inlayHint" then
      vim.b[args.buf].inlay_hints_supported = true
    end

    -- Highlight other occurrences of the symbol under the cursor. Uses the
    -- LSP's understanding, so it tracks scope rather than matching text.
    if client:supports_method "textDocument/documentHighlight" then
      local hl_group = augroup("lsp_doc_hl_" .. args.buf)
      vim.api.nvim_create_autocmd({ "CursorHold", "CursorHoldI" }, {
        group = hl_group,
        buffer = args.buf,
        callback = vim.lsp.buf.document_highlight,
      })
      vim.api.nvim_create_autocmd({ "CursorMoved", "CursorMovedI" }, {
        group = hl_group,
        buffer = args.buf,
        callback = vim.lsp.buf.clear_references,
      })
      vim.api.nvim_create_autocmd("LspDetach", {
        group = hl_group,
        buffer = args.buf,
        callback = function()
          vim.lsp.buf.clear_references()
          pcall(vim.api.nvim_del_augroup_by_name, "my_lsp_doc_hl_" .. args.buf)
        end,
      })
    end
  end,
})

-- ──── JSX comment syntax ──────────────────────────────────────
-- Neovim comments natively (gcc/gc), but its commentstring is per-filetype, so
-- in a .tsx file it always inserts // — wrong inside JSX, which needs {/* */}.
--
-- nvim-ts-context-commentstring is the usual fix; it did not resolve JSX
-- context on this Neovim, returning "// %s" even with the cursor inside a
-- jsx_element, in both its hook and autocmd modes. Rather than carry a plugin
-- that silently does nothing, this walks the syntax tree directly.
vim.api.nvim_create_autocmd({ "CursorMoved", "CursorMovedI", "BufEnter" }, {
  group = augroup "jsx_commentstring",
  pattern = { "*.tsx", "*.jsx" },
  callback = function(args)
    local ok, node = pcall(vim.treesitter.get_node)
    if not ok or not node then
      return
    end
    local in_jsx = false
    while node do
      local t = node:type()
      if t == "jsx_element" or t == "jsx_fragment" or t == "jsx_self_closing_element" then
        in_jsx = true
        break
      end
      -- Stop climbing at a statement boundary: `return <div/>` is JS, not JSX.
      if t == "return_statement" or t == "statement_block" or t == "program" then
        break
      end
      node = node:parent()
    end
    vim.bo[args.buf].commentstring = in_jsx and "{/* %s */}" or "// %s"
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
