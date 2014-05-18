# My base dotfiles

## Description
These are my personal dotfiles, which I manage with the help of git
and a nice tool called [dotfiles].  This is the base dotfiles
repository  which contains what I want to have available in a basic 
shell environment.

## Whats included

**This section is a working draft**

###  Bash
Bash is a great shell and available in most *nix based OSes. I belive
that my configuration requires bash 4 but I'm not sure.

Bash related features are:

 * /[.bashrc]
   * Detection and activation of default environment for rvm/nvm/venvburrito 
   * [Minimalistic but feature packed prompt][prompt-article] 
   * Small collection of aliases
 * /[.bash.d]/ 
   * Tab completions
   * z
 * [.dircolors]
 * [.inputrc]
 
### Individual tool configurations
 * [.tigrc] - [Tig] is as nice and simple Git gui-like text mode based tool.
 * [.tmux.conf] - [tmux] is a terminal multiplexer (like the more famous screen).
 * [.ackrc] - [ack] is a programmers grep for searching source code without too much hassle.

### Utility scripts 
A bunch of scripts in /[.bin]/. Both general scripts things specifically targeting git.

[.profile]:       https://github.com/thomasf/dotfiles-thomasf-base/blob/master/.profile   ".profile"
[.bashrc]:        https://github.com/thomasf/dotfiles-thomasf-base/blob/master/.bashrc    ".bashrc"
[.bash.d]:        https://github.com/thomasf/dotfiles-thomasf-base/tree/master/.bash.d/   "bash.d/"
[.dircolors]:     https://github.com/thomasf/dotfiles-thomasf-base/blob/master/.dircolors ".dircolors"
[.inputrc]:       https://github.com/thomasf/dotfiles-thomasf-base/blob/master/.inputrc   ".inputrc"
[.tigrc]:         https://github.com/thomasf/dotfiles-thomasf-base/blob/master/.tigrc     ".tigrc"
[.tmux.conf]:     https://github.com/thomasf/dotfiles-thomasf-base/blob/master/.tmux.conf ".tmux-conf"
[.ackrc]:         https://github.com/thomasf/dotfiles-thomasf-base/blob/master/.ackrc     ".ackrc"
[.bin]:           https://github.com/thomasf/dotfiles-thomasf-base/tree/master/.bin/      ".bin/"
[prompt-article]: http://datamaskinen.medeltiden.org/tools/bash-prompt-v2.html            "My bash prompt revisited"
[Tig]:            http://jonas.nitro.dk/tig/screenshots/                                  "Tig"
[ack]:            http://betterthangrep.com/                                              "Ack"
[tmux]:           http://tmux.sourceforge.net/                                            "tmux"

## Installation 

Install the [dotfiles] package, either using `pip` (recommended) or 
`easy_install`. Maybe with some help of `sudo`.

    pip install dotfiles

Create some directory where to store multiple dotfiles repositories.
   
    mkdir -p ~/repos/dotfiles
   
Clone this repository into that directory.
   
    git clone https://github.com/thomasf/dotfiles-thomasf-base ~/repos/dotfiles/base
   
And symlink it's contents into your home directory.

    dotfiles -s -R ~/repos/dotfiles/base
     
Also check out `dotfiles -h` or the [dotfiles]
manual for more information on the hows and whats of that tool.




[dotfiles]: https://github.com/jbernard/dotfiles "dotfiles"
