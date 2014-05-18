import os
import atexit
import contextlib

from IPython.core.magic import Magics, magics_class, line_magic, cell_magic
from IPython.core.magic_arguments import (argument, magic_arguments,
                                          parse_argstring)
from IPython.utils.tempdir import TemporaryDirectory


@contextlib.contextmanager
def chcwd(path):
    cwd = os.getcwd()
    try:
        os.chdir(path)
        yield
    finally:
        os.chdir(cwd)


@magics_class
class TempMagic(Magics):

    def __init__(self, *args, **kwds):
        super(TempMagic, self).__init__(*args, **kwds)
        self._temp_dirs = []
        atexit.register(self.cleanup_all)

    def cleanup_all(self):
        for td in self._temp_dirs:
            try:
                td.cleanup()
            except:
                pass
        self._temp_dirs[:] = []

    @staticmethod
    def _filter_none_values(*args, **kwds):
        return dict(
            (k, v) for (k, v) in dict(*args, **kwds).iteritems()
            if v is not None)

    @magic_arguments()
    @argument('--suffix', '-s', default=None)
    @argument('--prefix', '-p', default=None)
    @argument('--directory', '-d', default=None)
    @line_magic
    def cdtemp(self, parameter_s=''):
        """
        Make temporal directory and change the current to there.

        The temporal directory made will be removed when the current
        IPython process terminates.

        """
        args = parse_argstring(self.cdtemp, parameter_s)
        kwds = self._filter_none_values(vars(args))
        td = TemporaryDirectory(**kwds)
        self._temp_dirs.append(td)
        os.chdir(td.name)
        return td.name

    @magic_arguments()
    @argument('--suffix', '-s', default=None)
    @argument('--prefix', '-p', default=None)
    @argument('--directory', '-d', default=None)
    @cell_magic
    def with_temp_dir(self, line, cell):
        """
        Execute code in a temporal directory.
        """
        args = parse_argstring(self.with_temp_dir, line)
        kwds = self._filter_none_values(vars(args))
        with TemporaryDirectory(**kwds) as path:
            with chcwd(path):
                self.shell.run_cell(cell)


def load_ipython_extension(ip):
    """Load the extension in IPython."""
    global _loaded
    if not _loaded:
        ip.register_magics(TempMagic)
        _loaded = True

_loaded = False
