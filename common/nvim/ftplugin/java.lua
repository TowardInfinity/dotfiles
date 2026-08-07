-- ══════════════════════════════════════════════════════════════
-- Java / Spring Boot — jdtls via nvim-jdtls
-- ══════════════════════════════════════════════════════════════
--
-- This runs for every Java buffer (that is what ftplugin/ means), rather than
-- being registered once at startup. jdtls needs it that way: each project gets
-- its own server instance and its own workspace, so opening a file from a
-- different repo starts a second server rather than confusing the first.
--
-- Deliberately NOT enabled in lua/configs/lspconfig.lua — two clients on one
-- buffer means duplicated completions and diagnostics.

local ok, jdtls = pcall(require, "jdtls")
if not ok then
  return
end

local mason = vim.fn.stdpath "data" .. "/mason"

-- Root detection: gradlew/mvnw first, because they identify the *project*.
-- .git is the last resort — in a monorepo it would otherwise swallow every
-- module into one workspace.
local root = vim.fs.root(0, {
  "gradlew",
  "mvnw",
  "settings.gradle",
  "settings.gradle.kts",
  "pom.xml",
  "build.gradle",
  "build.gradle.kts",
  ".git",
})
if not root then
  return -- a stray .java file with no project; starting jdtls would be pointless
end

-- One workspace per project, kept in the cache dir. jdtls writes a full
-- compiled index here — several hundred MB on a large codebase — so it must
-- not land inside the repo.
--
-- Keyed on a hash of the resolved root, not its directory name. The name
-- alone collides: ~/Codes/Projects/api and ~/Codes/Learning/api are different
-- projects that would have shared one index, and jdtls does not notice — you
-- get another project's classpath, stale diagnostics, and phantom symbols,
-- with nothing on screen to say why. The name is kept as a prefix purely so
-- the cache stays legible to a human deleting things by hand.
--
-- resolve() first, so a symlinked checkout and its real path agree.
local canonical = vim.uv.fs_realpath(root) or vim.fn.fnamemodify(root, ":p")
local digest = vim.fn.sha256(canonical):sub(1, 12)
local workspace = string.format(
  "%s/jdtls/%s-%s",
  vim.fn.stdpath "cache",
  vim.fn.fnamemodify(canonical, ":p:h:t"),
  digest
)

-- Extension bundles: java-test and java-debug-adapter. These are what make
-- "run this test" and "debug this method" possible; without them jdtls is
-- completion and navigation only.
local bundles = {}
vim.list_extend(
  bundles,
  vim.split(
    vim.fn.glob(mason .. "/packages/java-debug-adapter/extension/server/com.microsoft.java.debug.plugin-*.jar", true),
    "\n"
  )
)
vim.list_extend(bundles, vim.split(vim.fn.glob(mason .. "/packages/java-test/extension/server/*.jar", true), "\n"))
bundles = vim.tbl_filter(function(j)
  -- The glob returns "" when nothing matches, and java-test ships a few jars
  -- that are not server bundles.
  return j ~= "" and not j:match "com.microsoft.java.test.runner-jar-with-dependencies.jar" and not j:match "jacoco"
end, bundles)

local nvlsp = require "nvchad.configs.lspconfig"

local config = {
  cmd = { mason .. "/bin/jdtls", "-data", workspace },
  root_dir = root,
  capabilities = nvlsp.capabilities,
  on_init = nvlsp.on_init,

  init_options = { bundles = bundles },

  settings = {
    java = {
      -- "interactive" rather than "automatic": a Gradle re-sync on every
      -- build-file save is slow on a Spring project. :JdtUpdateConfig when
      -- you actually change dependencies.
      configuration = {
        updateBuildConfiguration = "interactive",
        runtimes = {
          -- Point jdtls at the SDKMAN JDK. Without this it uses whatever java
          -- happens to be on PATH, which may not match the project toolchain.
          { name = "JavaSE-21", path = vim.fn.expand "~/.sdkman/candidates/java/current" },
        },
      },
      eclipse = { downloadSources = true },
      maven = { downloadSources = true },
      implementationsCodeLens = { enabled = true },
      referencesCodeLens = { enabled = true },
      references = { includeDecompiledSources = true },
      signatureHelp = { enabled = true },
      contentProvider = { preferred = "fernflower" }, -- readable decompiled .class
      inlayHints = { parameterNames = { enabled = "literals" } },
      format = { enabled = true },
      -- Stop jdtls proposing internal JDK packages on every completion.
      completion = {
        favoriteStaticMembers = {
          "org.junit.jupiter.api.Assertions.*",
          "org.mockito.Mockito.*",
          "org.assertj.core.api.Assertions.*",
          "org.springframework.test.web.servlet.request.MockMvcRequestBuilders.*",
          "org.springframework.test.web.servlet.result.MockMvcResultMatchers.*",
        },
        filteredTypes = {
          "com.sun.*",
          "io.micrometer.shaded.*",
          "java.awt.*",
          "jdk.*",
          "sun.*",
        },
      },
      sources = { organizeImports = { starThreshold = 9999, staticStarThreshold = 9999 } },
      codeGeneration = {
        toString = { template = "${object.className}{${member.name()}=${member.value}, ${otherMembers}}" },
        useBlocks = true,
        hashCodeEquals = { useJava7Objects = true },
      },
    },
  },

  on_attach = function(_, bufnr)
    local map = function(lhs, rhs, desc)
      vim.keymap.set("n", lhs, rhs, { buffer = bufnr, desc = "Java " .. desc })
    end
    -- Things plain LSP cannot do, so they get Java-specific bindings.
    map("<leader>Jo", jdtls.organize_imports, "organize imports")
    map("<leader>Jv", jdtls.extract_variable, "extract variable")
    map("<leader>Jc", jdtls.extract_constant, "extract constant")
    map("<leader>Jm", jdtls.extract_method, "extract method")
    map("<leader>Jt", jdtls.test_nearest_method, "test method under cursor")
    map("<leader>JT", jdtls.test_class, "test this class")
    map("<leader>Ju", "<cmd>JdtUpdateConfig<cr>", "re-sync build config")

    vim.keymap.set("v", "<leader>Jm", function()
      jdtls.extract_method(true)
    end, { buffer = bufnr, desc = "Java extract method" })
    vim.keymap.set("v", "<leader>Jv", function()
      jdtls.extract_variable(true)
    end, { buffer = bufnr, desc = "Java extract variable" })
  end,
}

jdtls.start_or_attach(config)
