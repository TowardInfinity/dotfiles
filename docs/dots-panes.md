---
title: Navigation & Keys
group: dots
order: 20
summary: The route shell, focus model, keyboard shortcuts, and mouse behavior
---

# Navigation & Keys

## One route shell

The bare `dots` command opens one shell with a route sidebar. It starts on
Overview and keeps the same focus model everywhere:

| | Pane | What it is for |
|---|---|---|
| — | **Overview** | Attention first: workspace, fleet freshness, and local facts. |
| — | **Changes** | Review local edits and incoming commits; sync, apply, or publish through plans. |
| — | **AI** | Read local cross-tool session metadata; `dots agent` performs the terminal handoff. |
| — | **Fleet** | Inspect cached machine results, select hosts, and prepare exact-revision rollout. |
| — | **Health** | Tools, frameworks, config, and package checks with repair actions. |
| — | **Services / Packages / Projects** | Local inventories and their scoped actions. |
| — | **Docs** | Topics, rendered Markdown, and outline navigation. |

The sidebar is visible at 76 columns and above. On compact terminals it becomes
a breadcrumb so the content remains readable rather than being squeezed into
two competing rails. `Ctrl-P` or `:` always opens the searchable palette, so
every route remains reachable when the sidebar is hidden.

### The arrow keys

All four work, everywhere something can move. The status bar shows the letter
form because it is shorter, but the arrows are always aliases for it:

| Axis | Shortcut | Meaning |
|---|---|---|
| Vertical | `↑` `↓` or `j` `k` | Move in the focused list or scroll the focused document. |
| Horizontal | `←` `→` or `h` `l` | Move focus between the sidebar and content, or back/forward in detail. |
| Focus | `Tab` / `Shift-Tab` | Move between interactive regions; with two regions both toggle sidebar/content; never changes route. |
| Selection | `Enter` / `Space` | Inspect the focused row / toggle selection where supported. |
| Back | `Esc` | Close a modal, clear input, close detail, or return to the route. |

Filters own every key while open. `q` quits only at the base layer; a filter,
dialog, or action owns it instead.

### Global keys

| Key | Where | Does |
|---|---|---|
| `Ctrl-P` / `:` | everywhere | Open the command and route palette. |
| `?` / `F1` | everywhere | Show contextual help. |
| `/` | lists and Docs | Filter the focused collection. |
| `a` | route content | Show actions available for that route. |
| `r` | route content | Refresh the current route. |
| `q` / `Ctrl-C` | base layer | Quit safely. |

Changes uses `Space` for file selection, `m` to edit the commit message, `L` for
local Apply, `u` for inbound sync, and `p` for a reviewed publish plan. Fleet
uses `Space` for machine selection and `Enter` for inspection. Health uses `f`
to switch between Problems and All checks. Services uses `f` for the running
only filter; Packages retains its visible upgrade shortcut. All mutations are
also discoverable from `a`.

The status bar shows the highest-value actions for the focused route. Full
details live here and in `?`, so the footer never becomes a wall of letters.

## When it says you are ahead

```
  !  repo           1 uncommitted file, 2 unpushed commits
     publish reviewed paths with `dots publish`, then use `dots rollout`
```

The badge in the status bar and that row in `dots doctor` mean this machine's
configs have moved on and the others have not. Neither pushes anything — see
`dots operations` for the separate repository and fleet actions.
