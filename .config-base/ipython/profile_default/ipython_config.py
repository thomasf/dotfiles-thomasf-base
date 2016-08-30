# -*- coding: utf-8 -*-
__author__ = 'Thomas Frössman'

c = get_config()  # noqa

c.InteractiveShellApp.extensions = [
    'hierarchymagic',
    'tempmagic',
    'importfilemagic',
    'djangomagic',
    # 'logdiary'
]

c.TerminalIPythonApp.display_banner = False
c.TerminalIPythonApp.profile = u'default'
c.TerminalInteractiveShell.history_length = 10000
c.TerminalInteractiveShell.autoindent = True
c.TerminalInteractiveShell.confirm_exit = False

# Start logging to the default log file.
# c.TerminalInteractiveShell.logstart = False
# The name of the logfile to use.
# c.TerminalInteractiveShell.logfile = ''
# c.TerminalInteractiveShell.pager = 'vless'
c.TerminalInteractiveShell.term_title = True
# c.InteractiveShellApp.exec_lines = ["%logdiary"]
# c.InteractiveShellApp.log_level = default_log_level
