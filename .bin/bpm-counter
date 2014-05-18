#!/usr/bin/env python
# -*- coding: iso-8859-1 -*-
# ncurses doesn't understand utf8 (yet)

# Copyright (C) 2009  H. Dieter Wilhelm
# Author: H. Dieter Wilhelm <dieter@duenenhof-wilhelm.de>
# Created: 2009-01
# Version: 1.4

# This code is free software; you can redistribute it and/or modify
# it under the terms of the GNU General Public License as published
# by the Free Software Foundation; either version 3, or (at your
# option) any later version.
#
# This python script is distributed in the hope that it will be
# useful, but WITHOUT ANY WARRANTY; without even the implied warranty
# of MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.
#
# Permission is granted to distribute copies of this pyhton script
# provided the copyright notice and this permission are preserved in
# all copies.
#
# You should have received a copy of the GNU General Public License
# along with this program; if not, you can either send email to this
# program's maintainer or write to: The Free Software Foundation,
# Inc.; 675 Massachusetts Avenue; Cambridge, MA 02139, USA.

# --- Usage ---

# For a summary of this file's prerequisites and its usage please have
# a look at http://code.google.com/p/bpm-counter/

# --- TODO ---

# python 3 rewrite or 2.5 compatibility mode?
# TCL/TK or GTK version for our mice lovers and Windows pampered? ;-)

# -- not so important ones --

# version variable
# include a metronome function (thread?)
# (command line option for) choice of precision?
# Adjust accuracy during run?

# --- Issues/Bugs ---

# When resizing the terminal frame during a run the original terminal
# state is not preserved any longer.

# -- minor stuff --
# curser is not switched off under cygwin
# curses.flush() not working under cygwin

# --- History ---

# -- V 1.3 --

# 1.) removed import of deprecated string module
# 2.) a tiny typo and some textual polishing 8-|

# -- V 1.2 --

# 1.) Squashed status printout bug when leaving bpm-counter
# 2.) Resizing of the terminal does not change the count any longer

# -- V 1.1 --

# 1.) Statistical error estimation of the mean and rounded result display
# 2.) Log of moving averages (of 10 strokes, every 10 beats)
# 3.) Deviations not only in frequencies but also in time (msec)

"""Timer for counting something regular, like the beats of music.
"""

__author__ = 'dieter@duenenhof-wilhelm.de (Dieter Wilhelm)'
_version = "1.4"

# --- os checking ---
# necessitated by a bug in the cygwin port of ncurses? 2009-02-11

import os

os_name = os.uname()[ 0]
host_name = os.uname()[ 1]

cygwin = 0
if  os_name.find("CYGWIN") > -1:
    cygwin = 1

#print "Operating system:", os_name

# --- command line help ---

import sys

if len( sys.argv) > 1:
    print """Display the frequency of keystrokes in 1/min (bpm).

    usage: This should be self evident ;-).
Version 1.3
""", sys.argv[ 0]
    exit( 1)

# --- helper functions ---

import math

def mean( A):
    """Mean of the list A."""
    l = len( A)
    return sum( A) / float( l)

def variance( A):
    """Variance (n-1) of the list A.

Return 0 if len( A) < 2.
    """
    m = mean( A)
    l = len( A)
    v = 0
    for x in A:
        v = v + ( x - m)**2
    if l > 1:
        return v / float( l - 1)
    else:
        return 0.

def standardDeviation( A):
    """Standard deviation of the list A."""
    return math.sqrt( variance( A))
    
def movingAverage( A, n = 10):
    """List A's mean of the last n members.

If len( A) < n, return the mean of less than n elements."""
    return mean( A[ -n:]) 

# --- class definitions ---
    
#from time import *
import time
 # time.clock() is the CPU time!
 # time.time() is the wall (real world) time!

class StopWatch():
    """Container of points in time measured in s."""
    def __init__( self):
        """Resetting the container."""
        self.times = []
        
    def ClockIn( self):
        """Adding the current time."""
        t = time.time()
        self.times.append( t)
        return t
    
    def Times( self):
        """Return the time list."""
        return self.times
        

class FrequencyCounter( StopWatch):
    """Frequencies are counted in 1/min."""
    tolerance = 0.2             # max deviation of time differences
    def __init__( self):
        """so what?"""
        StopWatch.__init__( self)
        self.frequencies = []
        self.td_min = 0             # smallest time diff
        self.td_max = 0             # biggest time diff
        
    def TriggerCounter( self):
        """Starting the stopWatch for the first time."""
        self.ClockIn()
        
    def Count( self):
        """Clocking in and counting in 1/min."""
        tt = self.ClockIn()        # the latest time
        t  = self.times[-2:-1][ 0] # the previous time
        diff = tt - t
        # raise exception: first we need a triggered time list!
        l = len( self.times)
        if l < 2:
            raise IndexError('Counter must first be triggered')        
        if l == 2:
            tol = FrequencyCounter.tolerance
            self.td_min = ( 1 - tol) * diff
            self.td_max = ( 1 + tol) * diff
        if diff > self.td_max or diff < self.td_min:
            return 1             # count not accurate enough
        else:
            self.frequencies.append( 60 / diff )
            return 0

    def Range( self):
        """Return the valid range of frequencies."""
        return 60 / self.td_max, 60 / self.td_min

    def Frequencies( self):
        """Return frequency list."""
        return self.frequencies

    def Times( self):
        """Return time list."""
        return self.times

    def PrintStatus( self):
        """bla"""
        print "bpm-counter output of last sample:"
        b = len( self.frequencies)
        k = len( self.times)
        print "  Keystrokes:", k
        print "  Beats counted:", b
        if b > 1:
            bpm =  round( mean( self.frequencies), 1)
            std = round( standardDeviation( self.frequencies), 2)
            print "  Mean:", str( round( bpm)), "bpm"
            print "  Standard deviation", str( std), "bpm"
            bpm =  round( movingAverage( self.frequencies, 10), 1)
            print "  Moving Average:", str( bpm), "bpm"
        
# --- interface stuff ---

import curses

def endCurses():
    """Return the previous terminal state.
    """
    curses.nocbreak()
    stdscr.keypad( 0)
    curses.echo()

    if not cygwin:
        curses.curs_set( 1)
    curses.endwin()                 # restore everything

def tui ( n):                   # text user interface 8-)
    """Text User Interphase."""
    stdscr.erase()              # remove vestiges from previous run
    max_x, max_y = stdscr.getmaxyx()
    Fc = FrequencyCounter()
    # addstr uses (y,x) co-ordinates!
    s = "Press a key to start counting, \"q\" to quit."
    stdscr.addstr( 1, 1, s, curses.A_DIM)
    stdscr.addstr( 3, 1, "Run " + str( n) + " on " +
                   host_name + " waiting", curses.A_BOLD)
    stdscr.addstr( " (Ver. 1.2) ", curses.A_BOLD)
    stdscr.addstr( 5, 1, "Keystrokes: 0", curses.A_DIM)
# --- first count
    
    c = stdscr.getch()      # is also triggered by mouse keys and resizing!
    while c == curses.KEY_RESIZE or c == -1:
        c = stdscr.getch()
        
    # if c == curses.KEY_MOUSE:
    #     id, x, y, z, button = curses.getmouse()
    #     s = "Mouse-Ereignis bei (%d ; %d ; %d), ID= %d, button = %d" % (x, y, z, id, button)
    #     stdscr.addstr(0, 1, s)

    if c == ord('q') or c == ord('Q') :
        stdscr.addstr( "\n")
        endCurses()
        return 1
    
    Fc.TriggerCounter()
    t0 = time.time()          # time.time() is the wall (real world) time!
    y = 1
    s = "Press \"q\" to quit, \"n\" to start a new count.    "
    stdscr.addstr( y, 1, s, curses.A_DIM)
    y = 3
    stdscr.addstr( y, 1, "Run " + str( n) + " on " +
                   host_name + " active", curses.A_BOLD)
    stdscr.addstr( " (Ver. " + _version + ")   ", curses.A_BOLD)
    y = 5
    stdscr.addstr( y, 1, "One keystroke", curses.A_DIM)
# --- second count
    c = stdscr.getch()
    while c == curses.KEY_RESIZE or c == -1 :
        c = stdscr.getch()
    
    # if c == curses.KEY_MOUSE:
    #     id, x, y, z, button = curses.getmouse()
    #     s = "Mouse-Ereignis bei (%d ; %d ; %d), ID= %d,
    #     button = %d" % (x, y, z, id, button)
    #     stdscr.addstr(0, 1, s)

    if c == ord('n') or c == ord('N') :
        return 0
    
    elif c == ord('q') or c == ord('Q') :
        endCurses()
        Fc.PrintStatus()
        return 1

    Fc.Count()
    # documenation 1 DIM
    # Status 3 BOLD
    y = 3
    td = round( time.time() - t0, 1)
    stdscr.addstr( y, 1, "Run " + str( n) + " on " + host_name  +
                   " active for " + str( td) + " s", curses.A_BOLD)
    stdscr.addstr( " (Ver. 1.3)", curses.A_BOLD)
    # Keystrokes 5 DIM
    y = 5
    l = len( Fc.Times())
    stdscr.addstr( y, 1, "Keystrokes: " +  str( l), curses.A_DIM)
    # Keystrokes discarded 6 BOLD
    # Range 7 DIM
    r1 = int( round( Fc.Range()[ 0]))
    r2 = int( round( Fc.Range()[ 1]))
    y = 7
    stdscr.addstr( y, 1, "Valid beat range: [" + str( r1) +
                   " .. " + str( r2) + "] bpm", curses.A_DIM)
    # Mean 8 BOLD
    y = 8
    bpm = int( round( mean( Fc.Frequencies())))
    stdscr.addstr( y, 1, "Mean:", curses.A_BOLD)
    stdscr.addstr( y, 6, " " + str( bpm) + " bpm ",
                   curses.color_pair( 1) | curses.A_BOLD)
    # if bpm < 100:   # overwrite possible A_REVERSE from
    # counts faster than 99 bpm
    #             stdscr.addstr( y, 15, " ")
 # --- following counts
    yy = 14                 # for the moving averages
    while 1:
        c = stdscr.getch()
        while c == curses.KEY_RESIZE or c == -1 :
            c = stdscr.getch()
        
        if c == ord('n') or c == ord('N') :
            return 0
        
        elif c == ord('q') or c == ord('Q') :
            endCurses()
            Fc.PrintStatus()
            return 1

        else:
            # if c == curses.KEY_MOUSE:
            #     id, x, y, z, button = curses.getmouse()
            #     s = "Mouse-Ereignis bei (%d ; %d ; %d), ID= %d,
            #     button = %d" % (x, y, z, id, button)
            #     stdscr.addstr(0, 1, s)
            if Fc.Count():
                curses.flash()  # not accurate enough
            # Status BOLD
            y = 3
            td = round( time.time() - t0, 1)
            stdscr.addstr( y, 1, "Run " + str( n) + " on " + host_name  +
                           " active for " + str( td) + " s", curses.A_BOLD)
            stdscr.addstr( " (Ver. 1.2)", curses.A_BOLD)
            # Keystrokes 5 DIM
            l = len( Fc.Times())
            y = 5
            stdscr.addstr( y, 1, "Keystrokes: " +  str( l), curses.A_DIM)
            # discarded strokes 6 BOLD
            y = 6
            b = len( Fc.Times()) - len( Fc.Frequencies()) - 1
            stdscr.addstr( y, 1, "Skipped keystrokes: " +  str( b), curses.A_BOLD)
            # Range 7 DIM remains constant
            # Mean 8 BOLD
            y = 8
            bpm = round( mean( Fc.Frequencies()), 1)
            stdscr.addstr( y, 1, "Mean: ", curses.A_BOLD)
            std = round( standardDeviation( Fc.Frequencies()), 2)
            fl = len( Fc.Frequencies())
            # +/- 1.96 is the normalised gaussian variable for the
            # confidence level of 95 % (both sided)
            # Studend distribution (for the mean with unknown variance)
            # approximated with Gaussian
            acc = round( 1.96 * std / math.sqrt( fl), 2)
            if acc > 2 : # precision above 2 bpm: red alert 8-(
                stdscr.addstr( str( bpm), curses.color_pair( 1) | curses.A_BOLD)
            elif acc > 1 :    # accuracy above 1 bpm: yellow 8-S
                stdscr.addstr( str( bpm), curses.color_pair( 2) | curses.A_BOLD)
            else :          # green 8-)
                stdscr.addstr( str( bpm), curses.color_pair( 3) | curses.A_BOLD)
            stdscr.addstr(" bpm ", curses.A_BOLD)
            stdscr.addch( curses.ACS_PLMINUS)
            stdscr.addstr( " ")
            if fl < 15 :      # red: approximation of the student distribution
                # with gaussian too bad
                stdscr.addstr( str( acc), curses.color_pair( 1) | curses.A_BOLD)
            elif fl < 30 :   # yellow not yet good enough
                stdscr.addstr( str( acc), curses.color_pair( 2) | curses.A_BOLD)
            else:               # this is fine
                stdscr.addstr( str( acc), curses.color_pair( 3) | curses.A_BOLD)
            stdscr.addstr(" bpm ", curses.A_BOLD)
            if acc < 2 and fl > 9:     # give the huddled masses a nicely rounded result
                stdscr.addstr(y, 30, "=>  ", curses.A_BOLD) # indent a bit so that
                                                            # the result is sticking out
                stdscr.addstr( " " + str( int( round( bpm))), curses.A_REVERSE)
                stdscr.addstr(" bpm ",  curses.A_REVERSE)
                stdscr.addstr(" ") # remove possible vestiges from rounding process(es)
            else :          # overwrite invalid results (acc >= 2)
                stdscr.addstr(y, 34, "                     ") 

            # Moving average: 10 samples for moving targets
            y = 9
            m_bpm = round( movingAverage( Fc.Frequencies(), 10), 1) # leave it with 10 samples
            stdscr.addstr( y, 1, "Moving average: " +  str( m_bpm) + " bpm ", curses.A_DIM)
            # Standard & relative deviation
            dev = round( 100 * std / float( bpm), 2) # relative deviation in percent
            y = 10
            stdscr.addstr( y, 1, "Relative deviation: " +  str( dev) + " %  ", curses.A_BOLD)
            y = 11
            stdscr.addstr( y, 1, "Standard deviation: " +  str( std) + " bpm <=> ", curses.A_DIM)
            stdscr.addstr( str( int( round( 10 * dev / (bpm / 60)))) + " msec ", curses.A_DIM)
            # Moving deviations, lets stay consistent with 10 samples
            m_std = round( standardDeviation( Fc.Frequencies()[-10:]), 2) 
            m_dev = round( 100 * m_std / m_bpm, 2) # relative moving deviation in percent
            y = 12
            stdscr.addstr( y, 1, "Moving relative deviation: " +
                           str( m_dev) + " % ", curses.A_BOLD)
            y = 13
            stdscr.addstr( y, 1, "Moving standard deviation: " +
                           str( m_std) + " bpm ", curses.A_DIM)
            # set of moving averages
            y = 14
            if fl == 10:
                stdscr.addstr( y, 1, "Moving average (per 10 beats):", curses.A_BOLD)
            elif fl == 20:
                stdscr.addstr( y, 1, "Moving averages (per 10 beats):", curses.A_BOLD)
            max_y, max_x = stdscr.getmaxyx()
            if fl % 10 == 0 and yy < max_y:
                stdscr.addstr( yy, 33, str( m_bpm) + " bpm", curses.A_BOLD)
                yy = yy + 1
                    
    
# ----------------------------------------------------------------------
# run interphase loop ------------------------------

# screen init        
stdscr = curses.initscr()      # this is a screen exactly the terminal size

# color init
curses.start_color()
curses.init_pair(1, curses.COLOR_RED, curses.COLOR_BLACK)
curses.init_pair(2, curses.COLOR_YELLOW, curses.COLOR_BLACK)
curses.init_pair(3, curses.COLOR_GREEN, curses.COLOR_BLACK)
curses.init_pair(4, curses.COLOR_BLACK, curses.COLOR_WHITE)

# mouse init # mouse input (mouse queue?) seems to be not precise enough 
# avail, oldmask = curses.mousemask(curses.BUTTON1_PRESSED)
# curses.mousemask(avail)

# misc
curses.noecho()                # do not show the keys
curses.cbreak()                # react to keys without Carriage return
if not cygwin:
    curses.curs_set( 0)            # 0: switch off cursor
stdscr.keypad( 1)              # return nice keyboard shortcuts
#stdscr.border(0,0,0,0,0,0,0,0)

try:
    i = 1                 # run counter
    while not tui( i):
        i = i + 1
except KeyboardInterrupt:
    endCurses()

# --- Emacs stuff ---
# Local variables:
# coding: iso-8859-1
# end:
#######################################################################
