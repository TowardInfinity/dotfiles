require "nvchad.mappings"

local map = vim.keymap.set

-- ──── Kept from before ────────────────────────────────────────
map("n", ";", ":", { desc = "CMD enter command mode" })
map("i", "jk", "<ESC>", { desc = "Escape insert mode" })

-- ──── Save / quit ─────────────────────────────────────────────
map({ "n", "i", "v" }, "<C-s>", "<cmd>w<cr><esc>", { desc = "Save file" })
map("n", "<leader>qq", "<cmd>qa<cr>", { desc = "Quit all" })

-- ──── Splits ──────────────────────────────────────────────────
-- NvChad already maps <C-h/j/k/l> to move between windows.
map("n", "<leader>sv", "<cmd>vsplit<cr>", { desc = "Split vertical" })
map("n", "<leader>ss", "<cmd>split<cr>", { desc = "Split horizontal" })
map("n", "<leader>sc", "<cmd>close<cr>", { desc = "Close split" })
map("n", "<C-Up>", "<cmd>resize +2<cr>", { desc = "Height +" })
map("n", "<C-Down>", "<cmd>resize -2<cr>", { desc = "Height -" })
map("n", "<C-Left>", "<cmd>vertical resize -2<cr>", { desc = "Width -" })
map("n", "<C-Right>", "<cmd>vertical resize +2<cr>", { desc = "Width +" })

-- ──── Keep the cursor still ───────────────────────────────────
-- Centre the view when jumping, so you never lose your place.
map("n", "<C-d>", "<C-d>zz", { desc = "Half page down (centred)" })
map("n", "<C-u>", "<C-u>zz", { desc = "Half page up (centred)" })
map("n", "n", "nzzzv", { desc = "Next search result (centred)" })
map("n", "N", "Nzzzv", { desc = "Prev search result (centred)" })
map("n", "J", "mzJ`z", { desc = "Join lines, keep cursor put" })

-- ──── Moving lines ────────────────────────────────────────────
map("n", "<A-j>", "<cmd>m .+1<cr>==", { desc = "Move line down" })
map("n", "<A-k>", "<cmd>m .-2<cr>==", { desc = "Move line up" })
map("v", "<A-j>", ":m '>+1<cr>gv=gv", { desc = "Move selection down" })
map("v", "<A-k>", ":m '<-2<cr>gv=gv", { desc = "Move selection up" })

-- ──── Indenting: keep the selection after < and > ─────────────
map("v", "<", "<gv", { desc = "Outdent, keep selection" })
map("v", ">", ">gv", { desc = "Indent, keep selection" })

-- ──── Clipboard ───────────────────────────────────────────────
-- Paste over a selection without losing what you had yanked.
map("v", "p", '"_dP', { desc = "Paste without clobbering register" })
map({ "n", "v" }, "<leader>y", '"+y', { desc = "Yank to system clipboard" })
map("n", "<leader>Y", '"+Y', { desc = "Yank line to system clipboard" })

-- ──── Diagnostics ─────────────────────────────────────────────
-- <leader>dd rather than <leader>d: NvChad already owns <leader>ds
-- (diagnostic loclist), so a bare <leader>d would stall for timeoutlen
-- waiting to see whether an "s" follows.
map("n", "<leader>dd", vim.diagnostic.open_float, { desc = "Show diagnostic under cursor" })
map("n", "[d", function()
  vim.diagnostic.jump { count = -1, float = true }
end, { desc = "Prev diagnostic" })
map("n", "]d", function()
  vim.diagnostic.jump { count = 1, float = true }
end, { desc = "Next diagnostic" })

-- ──── LSP ─────────────────────────────────────────────────────
-- Neovim 0.11+ ships grn/gra/grr/gri natively; these are the familiar aliases.
map("n", "gr", vim.lsp.buf.references, { desc = "LSP references" })
map("n", "gi", vim.lsp.buf.implementation, { desc = "LSP implementation" })
map({ "n", "v" }, "<leader>ca", vim.lsp.buf.code_action, { desc = "LSP code action" })
-- Deliberately NOT mapping <leader>rn to rename: NvChad uses that for
-- "toggle relative number", and rename already lives on <leader>ra (NvRenamer).
map("n", "<leader>sg", vim.lsp.buf.signature_help, { desc = "LSP signature help" })
map("i", "<C-k>", vim.lsp.buf.signature_help, { desc = "LSP signature help" })

-- ──── Toggles (<leader>u…) ────────────────────────────────────
map("n", "<leader>uf", "<cmd>FormatToggle<cr>", { desc = "Toggle format-on-save (buffer)" })
map("n", "<leader>uF", "<cmd>FormatToggle!<cr>", { desc = "Toggle format-on-save (global)" })
map("n", "<leader>uw", "<cmd>set wrap!<cr>", { desc = "Toggle wrap" })
map("n", "<leader>us", "<cmd>set spell!<cr>", { desc = "Toggle spellcheck" })
-- (relative numbers already toggle on NvChad's <leader>rn, line numbers <leader>n)

map("n", "<leader>uv", function()
  local shown = vim.diagnostic.config().virtual_text
  vim.diagnostic.config { virtual_text = not shown }
  vim.notify("diagnostic virtual text: " .. (not shown and "ON" or "OFF"))
end, { desc = "Toggle diagnostic virtual text" })

-- ──── Search ──────────────────────────────────────────────────
map("n", "<Esc>", "<cmd>noh<cr>", { desc = "Clear search highlight" })
-- Replace the word under the cursor across the file
map("n", "<leader>rw", [[:%s/\<<C-r><C-w>\>//gI<Left><Left><Left>]], { desc = "Replace word under cursor" })

-- ──── Quickfix ────────────────────────────────────────────────
map("n", "[q", "<cmd>cprev<cr>zz", { desc = "Prev quickfix item" })
map("n", "]q", "<cmd>cnext<cr>zz", { desc = "Next quickfix item" })
