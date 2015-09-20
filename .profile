#!/bin/sh
#
#   Profie
#   Man page: profile
#   Useful reference: https://help.ubuntu.com/community/EnvironmentVariables

#set -x

# Prepend paths
ppath() {
 [ -d "${1}" ] && PATH="${1}:${PATH}"
}

# general global path prepends
ppath /sbin
ppath /bin
ppath /usr/sbin
ppath /usr/bin
ppath /usr/local/bin

# debian(?) global path prepends
ppath /var/lib/gems/1.8/bin
ppath /var/lib/gems/1.9/bin

# osx (homebrew) global path prepends
ppath /usr/local/opt/ruby/bin
ppath /usr/local/opt/python/bin
ppath /usr/local/opt/git/bin

# User home path prepends
ppath ~/.opt/depot_tools
ppath ~/.cabal/bin

# Misc User home path prepends
ppath ~/.virtualenvs/default/bin
ppath ~/.rvm/bin
ppath ~/.cask/bin
ppath ~/.rbenv/bin

# osx User home path prepends
ppath ~/Library/Haskell/bin

# ~/.opt User home path prepends
ppath ~/.opt/go/bin
ppath ~/.opt/go-tools/bin
ppath ~/repos/go/bin

ppath ~/.opt/ec2-api-tools/bin \
    && EC2_HOME=~/.opt/ec2-api-tools \
    && export EC2_HOME
ppath ~/.opt/groovy/bin
ppath ~/.opt/gradle/bin
ppath ~/.opt/apache-maven/bin
ppath ~/.opt/android-sdks/build-tools/19.0.3
ppath ~/.opt/android-sdks/tools
ppath ~/.opt/android-sdks/platform-tools
ppath ~/.opt/AdobeAIRSDK/bin
ppath ~/.opt/arm-cs-tools/bin

# Default gopath
export GOPATH="${HOME}/repos/go"
export GO15VENDOREXPERIMENT=1

# Perl local
export PERL_LOCAL_LIB_ROOT="${HOME}/.config/perl5";
export PERL_MB_OPT="--install_base ${PERL_LOCAL_LIB_ROOT}";
export PERL_MM_OPT="INSTALL_BASE=${PERL_LOCAL_LIB_ROOT}";
export PERL5LIB="${PERL_LOCAL_LIB_ROOT}/lib/perl5/x86_64-linux-gnu-thread-multi:${PERL_LOCAL_LIB_ROOT}/lib/perl5";
ppath "${PERL_LOCAL_LIB_ROOT}/bin"

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
        if [ $(which javac) ]; then
            JAVA_HOME=$(readlink -f $(which javac) | sed "s:/bin/javac::")
            export JAVA_HOME
        elif [ $(which java) ]; then
            JAVA_HOME=$(readlink -f $(which java) | sed "s:/bin/java::")
            export JAVA_HOME
        fi
        ;;
esac

# Prohibit perl from complaining about missing locales
PERL_BADLANG=0 && export PERL_BADLANG
# Locale settings (man page: locale)
if [ $(which locale) ]; then
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

# coffeelint configuration file
COFFEELINT_CONFIG="${HOME}/.config-base/coffeelint.json"
export COFFEELINT_CONFIG


# Music player daemon client host and ports
MPD_PORT=6205 && export MPD_PORT
MPD_HOST=localhost && export MPD_HOST


[ -e "${HOME}/.config-base/dynamic-colors" ] \
    && DYNAMIC_COLORS_ROOT="${HOME}/.config-base/dynamic-colors" \
    && export DYNAMIC_COLORS_ROOT

# ------------------------------------------------------------------------------
# PRIVATE AND LOCAL
#
[ -e "${HOME}/.profile-private" ] && . "${HOME}/.profile-private"
[ -e "${HOME}/.profile-local" ] && . "${HOME}/.profile-local"

# Application configuration
EDITOR="editor" && export EDITOR
VISUAL="${EDITOR}" && export VISUAL
ALTERNATE_EDITOR="" && export ALTERNATE_EDITOR
[ $(which less) ] && PAGER="less -R" && export PAGER
