-- ══════════════════════════════════════════════════════════════
-- nvim-treesitter (master branch) × Neovim 0.12 compatibility shim
-- ══════════════════════════════════════════════════════════════
--
-- THE BUG THIS FIXES
--   Opening a Markdown file threw, and highlighting died:
--     Decoration provider "start" (ns=nvim.treesitter.highlighter):
--     .../vim/treesitter.lua:197: attempt to call method 'range' (a nil value)
--
-- WHY
--   nvim-treesitter registers its custom query directives with
--   `{ force = true, all = false }`. The `all = false` flag asked Neovim to
--   hand each capture back as a single TSNode. Neovim 0.12 dropped that
--   option, so every directive now receives a LIST of nodes instead.
--   The plugin's handlers still do:
--       local node = match[capture_id]
--       vim.treesitter.get_node_text(node, bufnr)
--   With `node` now a plain Lua list, get_node_text lands on `node:range()`
--   against a table that has no such method — hence the error.
--
--   Markdown hits it because its injections.scm uses
--   `(#set-lang-from-info-string! @_lang)` to resolve ```lua fences.
--   Neovim's own bundled query uses the standard @injection.language capture
--   and is fine, but the plugin's copy shadows it on the runtimepath.
--
-- WHY NOT JUST UPGRADE
--   nvim-treesitter's master branch is frozen; the rewrite lives on `main`,
--   which NvChad v2.5 does not target yet. So the directives are re-registered
--   here with list-aware versions.
--
-- WHEN TO DELETE THIS FILE
--   Once nvim-treesitter master fixes it, or NvChad moves to the `main`
--   branch. Test by removing the file and opening a Markdown file with a
--   fenced code block — no error means this is no longer needed.

local query = vim.treesitter.query

-- Neovim ≤0.10 passed a single node; 0.11+ passes a list. Normalise.
-- The old `all = false` semantics used the LAST captured node.
local function as_node(v)
  if type(v) == "table" then
    return v[#v]
  end
  return v
end

local html_script_type_languages = {
  ["importmap"] = "json",
  ["module"] = "javascript",
  ["application/ecmascript"] = "javascript",
  ["text/ecmascript"] = "javascript",
}

local injection_language_aliases = {
  ex = "elixir",
  pl = "perl",
  sh = "bash",
  uxn = "uxntal",
  ts = "typescript",
}

local function lang_from_info_string(alias)
  local ft = vim.filetype.match { filename = "a." .. alias }
  return ft or injection_language_aliases[alias] or alias
end

-- Used by markdown/injections.scm (```lua fences) and hurl
query.add_directive("set-lang-from-info-string!", function(match, _, bufnr, pred, metadata)
  local node = as_node(match[pred[2]])
  if not node then
    return
  end
  local alias = vim.treesitter.get_node_text(node, bufnr):lower()
  metadata["injection.language"] = lang_from_info_string(alias)
end, { force = true })

-- Used by html/injections.scm (<script type="...">)
query.add_directive("set-lang-from-mimetype!", function(match, _, bufnr, pred, metadata)
  local node = as_node(match[pred[2]])
  if not node then
    return
  end
  local mime = vim.treesitter.get_node_text(node, bufnr)
  local configured = html_script_type_languages[mime]
  if configured then
    metadata["injection.language"] = configured
  else
    local parts = vim.split(mime, "/", {})
    metadata["injection.language"] = parts[#parts]
  end
end, { force = true })

-- Used by 4 query files to make @injection.language case-insensitive
query.add_directive("downcase!", function(match, _, bufnr, pred, metadata)
  local id = pred[2]
  local node = as_node(match[id])
  if not node then
    return
  end
  local text = vim.treesitter.get_node_text(node, bufnr, { metadata = metadata[id] }) or ""
  if not metadata[id] then
    metadata[id] = {}
  end
  metadata[id].text = string.lower(text)
end, { force = true })
