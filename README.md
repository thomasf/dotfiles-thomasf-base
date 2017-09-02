# My base dotfiles

## Description
These are my personal dotfiles, which I manage with the help of git
and a nice tool called [dotfiles].  This is the base dotfiles
repository  which contains what I want to have available in a basic 
shell environment.

## Installation 

Install the [dotfiles] package

    pip install dotfiles

Create some directory where to store multiple dotfiles repositories.
   
    mkdir -p ~/src/dotfiles
   
Clone this repository into that directory.
   
    git clone https://github.com/thomasf/dotfiles-thomasf-base ~/src/dotfiles/base
   
And symlink it's contents into your home directory.

    dotfiles -s -R ~/src/dotfiles/base
     
Also check out `dotfiles -h` or the [dotfiles]
manual for more information on the hows and whats of that tool.




[dotfiles]: https://github.com/jbernard/dotfiles "dotfiles"
