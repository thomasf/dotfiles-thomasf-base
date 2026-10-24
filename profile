#!/bin/sh
#
#   Profie
#   Man page: profile
#   Useful reference: https://help.ubuntu.com/community/EnvironmentVariables

# set -x

export WINIT_X11_SCALE_FACTOR=1.7
export PNPM_HOME="${HOME}/.local/share/pnpm"

# Prepend paths that exist and aren't in PATH already
ppath() {
  [ -d "${1}" ] || return 1
  case ":${PATH}:" in *":${1}:"*) return 0 ;; esac
  PATH="${1}:${PATH}"
}
# Append paths that exist and aren't in PATH already
apath() {
  [ -d "${1}" ] || return 1
  case ":${PATH}:" in *":${1}:"*) return 0 ;; esac
  PATH="${PATH}:${1}"
}

# general global path prepends
ppath /snap/bin
ppath /sbin
ppath /bin
ppath /usr/sbin
ppath /usr/bin
ppath /usr/local/bin

if [ -d /var/lib/gems ]; then
  for d in $(find /var/lib/gems/ -mindepth 1 -maxdepth 1 -type d | sort ); do
    ppath ${d}/bin
  done
fi

if [ -e /opt/homebrew/bin/brew ]; then
  eval "$(/opt/homebrew/bin/brew shellenv)"
fi

# osx (homebrew) global path prepends
ppath /usr/local/opt/ruby/bin
ppath /usr/local/opt/python/bin
ppath /usr/local/opt/git/bin
ppath ~/.pyenv/bin

ppath ~/sdk/flutter/bin

# User home path prepends
ppath ~/.cabal/bin
ppath  ~/.ghcup/bin

if [ -d ~/.gem/ruby ]; then
  for d in $(find ~/.gem/ruby -mindepth 1 -maxdepth 1 -type d | sort ); do
    ppath ${d}/bin
  done
fi

# Misc User home path prepends
ppath ~/.cask/bin
ppath ~/.rbenv/bin
ppath ~/.npm-global/bin
ppath ~/.local/bin
ppath ~/.cargo/bin
ppath "${PNPM_HOME}"
ppath ~/.dotnet/tools

# osx User home path prepends
ppath ~/Library/Haskell/bin

ppath ~/sdk/go/bin
ppath ~/sdk/zig
ppath ~/sdk/odin

ppath ~/sdk/groovy/bin
ppath ~/sdk/gradle/bin
ppath ~/sdk/apache-maven/bin
ppath ~/sdk/arm-cs-tools/bin

# ppath ~/Android/Sdk/platform-tools
# ppath ~/Android/Sdk/cmdline-tools/latest/bin

[ -d ~/.deno/ ] &&
  export DENO_INSTALL="${HOME}/.deno" &&
  ppath ~/.deno/bin

# Default go env
[ -d ~/sdk/go/bin ] &&
  export GOROOT=~/sdk/go

export GOPATH="${HOME}"
ppath "${GOPATH}/bin"

export DOCKER_CLI_HINTS=false

# Perl local
export PERL_LOCAL_LIB_ROOT="${HOME}/.config/perl5";
export PERL_MB_OPT="--install_base ${PERL_LOCAL_LIB_ROOT}";
export PERL_MM_OPT="INSTALL_BASE=${PERL_LOCAL_LIB_ROOT}";
export PERL5LIB="${PERL_LOCAL_LIB_ROOT}/lib/perl5/x86_64-linux-gnu-thread-multi:${PERL_LOCAL_LIB_ROOT}/lib/perl5";
ppath "${PERL_LOCAL_LIB_ROOT}/bin"
ppath ~/.config/perl5/bin

ppath ~/bin

# Add all ~/.bin and all ~/.bin-* directories to path
for D in $(find $HOME -maxdepth 1 -name ".bin-*" -o -name ".bin" | sort); do
    ppath ${D}
done

export PATH

PASSWORD_STORE_DIR="${HOME}/src/dotfiles/pass"
export PASSWORD_STORE_DIR
AWS_VAULT_BACKEND=pass
export AWS_VAULT_BACKEND
AWS_VAULT_PASS_CMD=gopass
export AWS_VAULT_PASS_CMD
AWS_VAULT_PASS_PASSWORD_STORE_DIR="${PASSWORD_STORE_DIR}"
export AWS_VAULT_PASS_PASSWORD_STORE_DIR

export SKIP_SIMPLE_GIT_HOOKS=1

# Prohibit perl from complaining about missing locales
PERL_BADLANG=0 && export PERL_BADLANG
# Locale settings (man page: locale): English, with Swedish formats if
# installed. The same locale is called en_US.utf8 or en_US.UTF-8 depending on
# the system.
if hash locale 2>/dev/null ; then
    _locales=$(locale -a 2>/dev/null)
    # _first_locale prints the first of its arguments that is installed.
    _first_locale() {
        for _l in "$@"; do
            if printf '%s\n' "${_locales}" | grep -q -x "${_l}"; then
                echo "${_l}"
                return 0
            fi
        done
        return 1
    }
    if _l=$(_first_locale en_US.utf8 en_US.UTF-8); then
        unset LC_ALL
        LANGUAGE="en_US:en" && export LANGUAGE
        LANG="${_l}" && export LANG
        for _v in LC_CTYPE LC_NUMERIC LC_TIME LC_COLLATE LC_MONETARY LC_MESSAGES \
                  LC_PAPER LC_NAME LC_ADDRESS LC_TELEPHONE LC_MEASUREMENT LC_IDENTIFICATION; do
            eval "${_v}=\${_l}; export ${_v}"
        done
    fi
    if _l=$(_first_locale sv_SE.utf8 sv_SE.UTF-8); then
        for _v in LC_CTYPE LC_NUMERIC LC_TIME LC_COLLATE LC_MONETARY \
                  LC_PAPER LC_NAME LC_ADDRESS LC_TELEPHONE LC_MEASUREMENT LC_IDENTIFICATION; do
            eval "${_v}=\${_l}; export ${_v}"
        done
    fi
    unset -f _first_locale
    unset _locales _l _v
fi

# ptyhon configuration
[ -e "${HOME}/.config-base/python/pythonrc.py" ] && \
    PYTHONSTARTUP="${HOME}/.config-base/python/pythonrc.py" && \
    export PYTHONSTARTUP

PYTHONZ_ROOT="${HOME}/opt/pythonz" && export PYTHONZ_ROOT

PIPENV_IGNORE_VIRTUALENVS=1 && export PIPENV_IGNORE_VIRTUALENVS
PIP_DEFAULT_TIMEOUT=120 && export PIP_DEFAULT_TIMEOUT

# Music player daemon client host and ports
MPD_PORT=6205 && export MPD_PORT
MPD_HOST=localhost && export MPD_HOST


# ------------------------------------------------------------------------------
# PRIVATE AND LOCAL
#
[ -e "${HOME}/.profile-private" ] && . "${HOME}/.profile-private"
[ -e "${HOME}/.profile-local" ] && . "${HOME}/.profile-local"
[ -e "$HOME/.cargo/env" ] && . "$HOME/.cargo/env"

# Application configuration
EDITOR="editor" && export EDITOR
VISUAL="${EDITOR}" && export VISUAL
ALTERNATE_EDITOR="" && export ALTERNATE_EDITOR
hash less 2>/dev/null && PAGER="less -R" && export PAGER

export MGFXC_WINE_PATH="${HOME}/.winemonogame"
