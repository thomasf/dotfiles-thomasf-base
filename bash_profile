#!/bin/bash

# Load the default dash or non bash profile
[ -f "${HOME}/.profile" ] && source "${HOME}/.profile"

# Load .bashrc and handle non login shell suff there too
[ -f "${HOME}/.bashrc" ] && source "${HOME}/.bashrc"

