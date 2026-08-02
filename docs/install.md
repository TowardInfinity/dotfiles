---
title: Install
group: Start
order: 10
summary: Set up a new machine with one command
---

# Install

One command on a new machine, macOS or Ubuntu. No GitHub account needed —
the repo is public, so a box you will never log into GitHub from can still
install from it.

```sh
sh -c "$(curl -fsSL https://toin.in/install)"
```

Clone or update, symlink the configs, install Oh My Zsh and TPM.

```sh
sh -c "$(curl -fsSL https://toin.in/install)" -- --deps
```

The above, plus every CLI tool the configs call. What you want on a fresh
machine.

```sh
sh -c "$(curl -fsSL https://toin.in/install)" -- --dry
```

Print what would happen and change nothing. Always safe.
