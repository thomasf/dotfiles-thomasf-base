#!/bin/sh

## git track Copyright 2010 Gavin Beatty <gavinbeatty@gmail.com>.
## 
## This program is free software: you can redistribute it and/or modify
## it under the terms of the GNU General Public License as published by
## the Free Software Foundation, either version 3 of the License, or (at
## your option) any later version.
## 
## This program is distributed in the hope that it will be useful,
## but WITHOUT ANY WARRANTY; without even the implied warranty of
## MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
## GNU General Public License for more details.
## 
## You can find the GNU General Public License at:
## http://www.gnu.org/licenses/

set -e

# @VERSION@

SUBDIRECTORY_OK=Yes
OPTIONS_KEEPDASHDASH=""
OPTIONS_SPEC="\
git track [options] [<remote>]
git track [options] -l <local_branch> -d
--
v,verbose   print each command as it is run
n,dry-run   don't run any commands, just print them
f,force     don't do any checks on whether <local_branch> is tracking a branch already
r,remote-branch=    the remote branch defaults to the local branch name - use this to override
l,local-branch= the local branch whose tracking information we will change
d,delete    delete tracking configuration for <local_branch>
version print version info in 'git track version \$version' format"

. "$(git --exec-path)/git-sh-setup"

version_print() {
    echo "git track version ${VERSION}"
}

doit() {
    if test "$verbose" -gt 0 ; then
        echo "$@"
    fi
    if test -z "$dryrun" ; then
        "$@"
    fi
}
full_name() {
    git rev-parse --symbolic-full-name "$@"
}

die_head() {
    die "Cannot operate with detached HEAD without being given <local_branch>"
}

default_local_branch() {
    lb_="$(full_name "HEAD")"
    lb_="$(echo "$lb_" | sed -e 's|^refs/heads/||')"
    echo "$lb_"
}

main() {
    has_head=""
    if git rev-parse --verify -q "HEAD" >/dev/null ; then
        has_head=yes
    fi

    verbose=0
    dry_run=""
    force=""
    delete=""
    lbranch=""
    rbranch=""
    while test $# -ne 0 ; do
        case "$1" in
        -v|--verbose)
            verbose="$(expr "$verbose" "+" 1)"
            ;;
        -n|--dry-run)
            verbose="$(expr "$verbose" "+" 1)"
            dry_run=true
            ;;
        -f|--force)
            force=true
            ;;
        -d|--delete)
            delete=true
            ;;
        -r|--remote-branch)
            rbranch="$2"
            shift
            ;;
        -l|--local-branch)
            lbranch="$2"
            shift
            ;;
        --version)
            version_print
            exit 0
            ;;
        --)
            shift
            break
            ;;
        *)
            usage
            ;;
        esac
        shift
    done

    if test -n "$delete" ; then
        if test -z "${lbranch}" ; then
            test -z "$has_head" && die_head
            lbranch="$(default_local_branch)"
        fi
        # don't worry about deleting the section - an empty section is ok
        # and deleting other options in the section by removing it entirely
        # is too destructive
        doit git config --unset "branch.${lbranch}.remote" || e=$?
        doit git config --unset "branch.${lbranch}.merge" || e=$?
    else
        if test $# -gt 0 ; then
            remote="$1"
        fi
        if test -z "${lgiven}" ; then
            test -z "$has_head" && die_head
            lbranch="$(default_local_branch)"
        fi
        remote="${remote:-origin}"
        if test -z "$rgiven" ; then
            rbranch="$lbranch"
        else
            rbranch="$(echo "$rbranch" | sed -e 's|^refs/heads/||')"
        fi

        if test -z "$force" ; then
            remote_ref="$(git show-ref "remotes/${remote}/${rbranch}" || e=$?)"
            if test -z "$remote_ref" ; then
                die "Remote branch '${rbranch}' does not exist on '${remote}'."
            fi
            track_merge="$(git config "branch.${lbranch}.merge" || e=$?)"
            if test -n "$track_merge" ; then
                track_remote="$(git config "branch.${lbranch}.remote" || e=$?)"
                die "Local branch '${lbranch}' is already tracking '${track_merge}' on '${track_remote}'."
            fi
        fi

        doit git config "branch.${lbranch}.remote" "$remote"
        doit git config "branch.${lbranch}.merge" "refs/heads/${rbranch}"
    fi
}


trap "echo \"caught SIGINT\" ; exit 1 ;" INT
trap "echo \"caught SIGTERM\" ; exit 1 ;" TERM
trap "echo \"caught SIGHUP\" ; exit 1 ;" HUP

main "$@"

