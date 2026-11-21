#!/bin/sh
set -e

export GOPATH="${HOME}"
export PATH="${HOME}/sdk/go/bin:${GOPATH}/bin:${PATH}"

DOTFILES_DIR="${HOME}/src/dotfiles"
mkdir -p "${DOTFILES_DIR}"

if [ ! -d "${DOTFILES_DIR}/base" ]; then
    git clone https://github.com/thomasf/dotfiles-thomasf-base.git "${DOTFILES_DIR}/base"
fi

go install github.com/some-programs/dotfiles@latest

dotfiles repos clone "$@"
dotfiles repos fix
dotfiles install
