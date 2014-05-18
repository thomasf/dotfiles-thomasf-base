import os
import sys
import re

from IPython.core.magic import Magics, magics_class, line_magic
from IPython.core.magic_arguments import (argument, magic_arguments,
                                          parse_argstring)
from IPython.utils.syspathcontext import prepended_to_syspath


@magics_class
class ImportFileMagic(Magics):

    @magic_arguments()
    @argument('path', help='a file path to be imported.')
    @argument('--reload', '-r', default=False, action='store_true',
              help='reload module')
    @argument('--star', '-s', default=False, action='store_true',
              help='do "from modeule import *"')
    @argument('--verbose', '-v', default=False, action='store_true',
              help='print the commands to be executed')
    @argument('--import', '-i', default=None, nargs='+',
              metavar="name", dest="names",
              help='do "from modeule import name[, name...]"')
    @line_magic
    def importfile(self, parameter_s=''):
        """
        Import module given a file.

        This command tries to import a given file as a module.
        Following methods are applied in order:

        1. If the absolute path of a given file starts with one of the
           path in `sys.path`, the given file is imported as a normal
           module.
        2. If there is ``__init__.py`` is in each sub-directory from
           the current working directory and to the file, the given
           file is imported as a normal module.
        3. If there is `setup.py` in one of the parent directory of
           the given file, the file is imported as a module in a
           package located at the location where `setup.py` is.
        4. If file is a valid python module name, the file is imported
           as a stand alone module.
        5. If none of above matches, the file is imported using
           '%run -n' magic command.

        """
        args = parse_argstring(self.importfile, parameter_s)
        abspath = os.path.abspath(os.path.expanduser(args.path))

        for method in [self._method_sys_path,
                       self._method_init,
                       self._method_setup_py,
                       self._method_stand_alone]:
            rootpath = method(abspath)
            if rootpath:
                break
        else:
            # Given path is not a valid module path.  Use %run -n.
            if args.verbose:
                print "%run -n {0}".format(args.path)
            self.shell.run_line_magic('run', "-n {0}".format(args.path))
            return

        modulepath = self._construct_modulepath(abspath, rootpath)
        commands = ["import {0}".format(modulepath)]
        if args.reload:
            commands.append("reload({0})".format(modulepath))

        if args.star:
            commands.append("from {0} import *".format(modulepath))
        elif args.names:
            commands.append("from {0} import {1}".format(
                modulepath, ", ".join(args.names)))

        code = "\n".join(commands)
        if args.verbose:
            print code

        with prepended_to_syspath(rootpath):
            self.shell.ex(code)

    @staticmethod
    def _construct_modulepath(abspath, rootpath):
        submods = os.path.relpath(
            os.path.splitext(abspath)[0], rootpath).split(os.path.sep)
        if submods[-1] == '__init__' and len(submods) > 1:
            submods = submods[:-1]
        return '.'.join(submods)

    _valid_module_re = re.compile(r'^[a-zA-z_][0-9a-zA-Z_]*$')

    @staticmethod
    def _has_init(abspath, rootpath):
        subdirs = os.path.relpath(abspath, rootpath).split(os.path.sep)[:-1]
        while subdirs:
            initpath = os.path.join(
                os.path.join(rootpath, *subdirs), '__init__.py')
            if not os.path.exists(initpath):
                return False
            subdirs.pop()
        return True

    @classmethod
    def _is_valid_module_path(cls, abspath, rootpath):
        test = cls._valid_module_re.match
        subpaths = os.path.splitext(
            os.path.relpath(abspath, rootpath))[0].split(os.path.sep)
        return all(test(p) for p in subpaths)

    @classmethod
    def _is_vaild_root(cls, abspath, rootpath):
        """
        Test if relpath of `abspath` from `rootpath` is a valid module path.
        """
        return (cls._is_valid_module_path(abspath, rootpath) and
                cls._has_init(abspath, rootpath))

    @classmethod
    def _method_sys_path(cls, abspath):
        matches = []
        for p in filter(lambda x: x, sys.path):
            if abspath.startswith(p) and cls._is_vaild_root(abspath, p):
                matches.append(p)
        if matches:
            return sorted(matches)[-1]  # longest match

    @classmethod
    def _method_init(cls, abspath):
        cwd = os.getcwd()
        if not abspath.startswith(cwd):
            return
        if cls._is_vaild_root(abspath, cwd):
            return cwd

    @classmethod
    def _method_setup_py(cls, abspath):
        dirs = abspath.split(os.path.sep)
        matches = []
        while len(dirs) > 1:
            dirs.pop()
            rootpath = os.path.sep.join(dirs)
            if (os.path.exists(os.path.join(rootpath, 'setup.py')) and
                cls._is_vaild_root(abspath, rootpath)):
                matches.append(rootpath)
        if matches:
            # Returning shortest path make sense since some project
            # has "sub" setup.py in its package and the real setup.py
            # in its root directory.
            return sorted(matches)[0]  # shortest match

    @classmethod
    def _method_stand_alone(cls, abspath):
        if cls._valid_module_re.match(
                os.path.splitext(os.path.basename(abspath))[0]):
            return os.path.dirname(abspath)


def load_ipython_extension(ip):
    """Load the extension in IPython."""
    global _loaded
    if not _loaded:
        ip.register_magics(ImportFileMagic)
        _loaded = True

_loaded = False
