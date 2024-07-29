#!/bin/sh
#
#   Profie
#   Man page: profile
#   Useful reference: https://help.ubuntu.com/community/EnvironmentVariables

# set -x

export WINIT_X11_SCALE_FACTOR=1.7
export PNPM_HOME="${HOME}/.local/share/pnpm"

# Prepend paths
ppath() {
 [ -d "${1}" ] && PATH="${1}:${PATH}"
}
# Append paths
apath() {
 [ -d "${1}" ] && PATH="${PATH}:${1}"
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
ppath ~/.opt/depot_tools
ppath ~/.cabal/bin
ppath  ~/.ghcup/bin

if [ -d ~/.gem/ruby ]; then
  for d in $(find ~/.gem/ruby -mindepth 1 -maxdepth 1 -type d | sort ); do
    ppath ${d}/bin
  done
fi

# Misc User home path prepends
ppath ~/.rvm/bin
ppath ~/.cask/bin
ppath ~/.rbenv/bin
ppath ~/.npm-global/bin
ppath ~/.local/bin
ppath ~/.cargo/bin
ppath "${PNPM_HOME}"
ppath ~/.dotnet/tools

# osx User home path prepends
ppath ~/Library/Haskell/bin

# ~/.opt User home path prepends
ppath ~/.opt/go/bin
ppath ~/sdk/go/bin
ppath ~/sdk/zig

ppath ~/.opt/ec2-api-tools/bin \
    && EC2_HOME=~/.opt/ec2-api-tools \
    && export EC2_HOME
ppath ~/.opt/groovy/bin
ppath ~/.opt/gradle/bin
ppath ~/.opt/apache-maven/bin
ppath ~/.opt/arm-cs-tools/bin
ppath ~/src/github.com/pfalcon/esp-open-sdk/xtensa-lx106-elf/bin/

# ppath ~/Android/Sdk/platform-tools
# ppath ~/Android/Sdk/cmdline-tools/latest/bin

[ -d ~/.deno/ ] &&
  export DENO_INSTALL="${HOME}/.deno" &&
  ppath ~/.deno/bin


[ -d ~/src/github.com/pfalcon/esp-open-sdk/ ] &&
  export ESP_ROOT=${HOME}/src/github.com/pfalcon/esp-open-sdk

# Default go env
[ -d ~/.opt/go/bin ] &&
  export GOROOT=~/.opt/go
[ -d ~/sdk/go/bin ] &&
  export GOROOT=~/sdk/go

export GOPATH="${HOME}"
ppath "${GOPATH}/bin"
export GO15VENDOREXPERIMENT=1

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

case ${OSTYPE} in
    darwin*)
        # do nothing
        ;;
    *)
      # set JAVA_HOME
      if hash javac 2>/dev/null ; then
        JAVA_HOME=$(resolvelink $(which javac) | sed "s:/bin/javac::")
        export JAVA_HOME
      elif hash java 2>/dev/null; then
        JAVA_HOME=$(resolvelink $(which java) | sed "s:/bin/java::")
        export JAVA_HOME
      fi
        ;;
esac

# Prohibit perl from complaining about missing locales
PERL_BADLANG=0 && export PERL_BADLANG
# Locale settings (man page: locale)
if hash locale 2>/dev/null ; then
    if $(locale -a 2>/dev/null | grep -q -x en_US.utf8); then
      unset LC_ALL
      LANGUAGE="en_US:en" && export LANGUAGE
      LANG="en_US.utf8" && export LANG
      LC_CTYPE="en_US.utf8" && export LC_CTYPE
      LC_NUMERIC="en_US.utf8" && export LC_NUMERIC
      LC_TIME="en_US.utf8" && export LC_TIME
      LC_COLLATE="en_US.utf8" && export LC_COLLATE
      LC_MONETARY="en_US.utf8" && export LC_MONETARY
      LC_MESSAGES="en_US.utf8" && export LC_MESSAGES
      LC_PAPER="en_US.utf8" && export LC_PAPER
      LC_NAME="en_US.utf8" && export LC_NAME
      LC_ADDRESS="en_US.utf8" && export LC_ADDRESS
      LC_TELEPHONE="en_US.utf8" && export LC_TELEPHONE
      LC_MEASUREMENT="en_US.utf8" && export LC_MEASUREMENT
      LC_IDENTIFICATION="en_US.utf8" && export LC_IDENTIFICATION
    elif $(locale -a 2>/dev/null | grep -q -x en_US.UTF-8); then
      unset LC_ALL
      LANGUAGE="en_US:en" && export LANGUAGE
      LANG="en_US.UTF-8" && export LANG
      LC_CTYPE="en_US.UTF-8" && export LC_CTYPE
      LC_NUMERIC="en_US.UTF-8" && export LC_NUMERIC
      LC_TIME="en_US.UTF-8" && export LC_TIME
      LC_COLLATE="en_US.UTF-8" && export LC_COLLATE
      LC_MONETARY="en_US.UTF-8" && export LC_MONETARY
      LC_MESSAGES="en_US.UTF-8" && export LC_MESSAGES
      LC_PAPER="en_US.UTF-8" && export LC_PAPER
      LC_NAME="en_US.UTF-8" && export LC_NAME
      LC_ADDRESS="en_US.UTF-8" && export LC_ADDRESS
      LC_TELEPHONE="en_US.UTF-8" && export LC_TELEPHONE
      LC_MEASUREMENT="en_US.UTF-8" && export LC_MEASUREMENT
      LC_IDENTIFICATION="en_US.UTF-8" && export LC_IDENTIFICATION
    fi

    if $(locale -a 2>/dev/null | grep -q -x sv_SE.utf8); then
        LC_CTYPE="sv_SE.utf8" && export LC_CTYPE
        LC_NUMERIC="sv_SE.utf8" && export LC_NUMERIC
        LC_TIME="sv_SE.utf8" && export LC_TIME
        LC_COLLATE="sv_SE.utf8" && export LC_COLLATE
        LC_MONETARY="sv_SE.utf8" && export LC_MONETARY
        LC_PAPER="sv_SE.utf8" && export LC_PAPER
        LC_NAME="sv_SE.utf8" && export LC_NAME
        LC_ADDRESS="sv_SE.utf8" && export LC_ADDRESS
        LC_TELEPHONE="sv_SE.utf8" && export LC_TELEPHONE
        LC_MEASUREMENT="sv_SE.utf8" && export LC_MEASUREMENT
        LC_IDENTIFICATION="sv_SE.utf8" && export LC_IDENTIFICATION
    elif $(locale -a 2>/dev/null | grep -q -x sv_SE.UTF-8); then
        LC_CTYPE="sv_SE.UTF-8" && export LC_CTYPE
        LC_NUMERIC="sv_SE.UTF-8" && export LC_NUMERIC
        LC_TIME="sv_SE.UTF-8" && export LC_TIME
        LC_COLLATE="sv_SE.UTF-8" && export LC_COLLATE
        LC_MONETARY="sv_SE.UTF-8" && export LC_MONETARY
        LC_PAPER="sv_SE.UTF-8" && export LC_PAPER
        LC_NAME="sv_SE.UTF-8" && export LC_NAME
        LC_ADDRESS="sv_SE.UTF-8" && export LC_ADDRESS
        LC_TELEPHONE="sv_SE.UTF-8" && export LC_TELEPHONE
        LC_MEASUREMENT="sv_SE.UTF-8" && export LC_MEASUREMENT
        LC_IDENTIFICATION="sv_SE.UTF-8" && export LC_IDENTIFICATION
    fi
fi

# ptyhon configuration
[ -d "${HOME}/.config-base/ipython" ] && \
    IPYTHONDIR="${HOME}/.config-base/ipython" && \
    export IPYTHONDIR
[ -e "${HOME}/.config-base/python/pythonrc.py" ] && \
    PYTHONSTARTUP="${HOME}/.config-base/python/pythonrc.py" && \
    export PYTHONSTARTUP

PYTHONZ_ROOT="${HOME}/opt/pythonz" && export PYTHONZ_ROOT

PIPENV_IGNORE_VIRTUALENVS=1 && export PIPENV_IGNORE_VIRTUALENVS
PIP_DEFAULT_TIMEOUT=120 && export PIP_DEFAULT_TIMEOUT

# Music player daemon client host and ports
MPD_PORT=6205 && export MPD_PORT
MPD_HOST=localhost && export MPD_HOST


[ -e "${HOME}/.config-base/dynamic-colors" ] \
    && DYNAMIC_COLORS_ROOT="${HOME}/.config-base/dynamic-colors" \
    && export DYNAMIC_COLORS_ROOT


# # Set android sdk home
# [ -d "${HOME}/.opt/android-sdks" ] \
#   && export ANDROID_SDK="${HOME}/.opt/android-sdks" \
#   && export ANDROID_HOME="${ANDROID_SDK}"

# # Set android ndk home
# [ -d "${HOME}/.opt/android-ndk" ] \
#   && export ANDROID_NDK="${HOME}/.opt/android-ndk"

# ------------------------------------------------------------------------------
# PRIVATE AND LOCAL
#
[ -e "${HOME}/.profile-private" ] && . "${HOME}/.profile-private"
[ -e "${HOME}/.profile-local" ] && . "${HOME}/.profile-local"

# Application configuration
EDITOR="editor" && export EDITOR
VISUAL="${EDITOR}" && export VISUAL
ALTERNATE_EDITOR="" && export ALTERNATE_EDITOR
hash les 2>/dev/null && PAGER="less -R" && export PAGER
. "$HOME/.cargo/env"

export MGFXC_WINE_PATH="${HOME}/.winemonogame"
