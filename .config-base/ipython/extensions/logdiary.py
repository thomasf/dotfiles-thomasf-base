import os
import sys
from IPython.core.magic import Magics, magics_class, line_magic
from IPython.utils.warn import warn
from IPython.utils.py3compat import str_to_unicode
from IPython.core.magics.logging import LoggingMagics
from IPython.core.magic import Magics, magics_class, line_magic
import time


class RedirectStdStreams(object):
    def __init__(self, stdout=None, stderr=None):
        self._stdout = stdout or sys.stdout
        self._stderr = stderr or sys.stderr

    def __enter__(self):
        self.old_stdout, self.old_stderr = sys.stdout, sys.stderr
        self.old_stdout.flush()
        self.old_stderr.flush()
        sys.stdout, sys.stderr = self._stdout, self._stderr

    def __exit__(self, exc_type, exc_value, traceback):
        self._stdout.flush()
        self._stderr.flush()
        sys.stdout = self.old_stdout
        sys.stderr = self.old_stderr


@magics_class
class LogDiaryMagics(LoggingMagics):
    """Magics related to all logging machinery."""

    @line_magic
    def logdiary(self, parameter_s=''):
        logdir = os.path.expanduser(time.strftime("~/notes/history/ipython/%Y/%m/"))
        logfile = os.path.join(logdir, time.strftime("%d.py"))
        if not os.path.exists(logdir):
            os.makedirs(logdir)

        devnull = open(os.devnull, "w")
        with RedirectStdStreams(stdout=devnull, stderr=devnull):
            self.logstart(logfile)


def load_ipython_extension(ip):
    """Load the extension in IPython."""
    ip.register_magics(LogDiaryMagics)
