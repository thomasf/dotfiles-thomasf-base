# -*- coding: utf-8 -*-
__author__ = 'Thomas Frössman'

from IPython.core.magic import Magics, magics_class, line_magic


def import_settings(shell, module_import="settings"):
    """
    """
    global _settings_imported
    import importlib
    settings = importlib.import_module(module_import)
    import django.core.management
    django.core.management.setup_environ(settings)
    shell.push({'settings': settings})
    _settings_imported = True


def _autoimport_settings(shell):
    if not _settings_imported:
        import_settings(shell)

_settings_imported = False


def import_objects(options, style):

    class ObjectImportError(Exception):
        pass

    # XXX: (Temporary) workaround for ticket #1796: force early loading of all
    # models from installed apps. (this is fixed by now, but leaving it here
    # for people using 0.96 or older trunk (pre [5919]) versions.

    from django.db.models.loading import get_models, get_apps
    # from django.db.models.loading import get_models, get_apps
    loaded_models = get_models()  # NOQA

    from django.conf import settings
    imported_objects = {'settings': settings}

    dont_load_cli = options.get('dont_load')  # optparse will set this to [] if it doensnt exists
    dont_load_conf = getattr(settings, 'SHELL_PLUS_DONT_LOAD', [])
    dont_load = dont_load_cli + dont_load_conf
    quiet_load = options.get('quiet_load')

    model_aliases = getattr(settings, 'SHELL_PLUS_MODEL_ALIASES', {})

    for app_mod in get_apps():
        app_models = get_models(app_mod)
        if not app_models:
            continue

        app_name = app_mod.__name__.split('.')[-2]
        if app_name in dont_load:
            continue

        app_aliases = model_aliases.get(app_name, {})
        model_labels = []

        for model in app_models:
            try:
                imported_object = getattr(__import__(app_mod.__name__, {}, {}, model.__name__), model.__name__)
                model_name = model.__name__

                if "%s.%s" % (app_name, model_name) in dont_load:
                    continue

                alias = app_aliases.get(model_name, model_name)
                imported_objects[alias] = imported_object
                if model_name == alias:
                    model_labels.append(model_name)
                else:
                    model_labels.append("%s (as %s)" % (model_name, alias))

            except AttributeError as e:
                if not quiet_load:
                    print(style.ERROR("Failed to import '%s' from '%s' reason: %s" % (model.__name__, app_name, str(e))))
                continue
        if not quiet_load:
            print(style.SQL_COLTYPE("From '%s' autoload: %s" % (app_mod.__name__.split('.')[-2], ", ".join(model_labels))))

    return imported_objects


# TODO Support printing using texttable.py or https://github.com/epmoyer/ipy_table
def dprint(object, stream=None, indent=1, width=80, depth=None):
    """
    A small addition to pprint that converts any Django model objects to dictionaries so they print prettier.

    h3. Example usage

        >>> from toolbox.dprint import dprint
        >>> from app.models import Dummy
        >>> dprint(Dummy.objects.all().latest())
         {'first_name': u'Ben',
          'last_name': u'Welsh',
          'city': u'Los Angeles',
          'slug': u'ben-welsh',
    """
    from django.db.models.query import QuerySet
    from pprint import PrettyPrinter
    # Catch any singleton Django model object that might get passed in
    if getattr(object, '__metaclass__', None):
        if object.__metaclass__.__name__ == 'ModelBase':
            # Convert it to a dictionary
            object = object.__dict__

    # Catch any Django QuerySets that might get passed in
    elif isinstance(object, QuerySet):
        # Convert it to a list of dictionaries
        object = [i.__dict__ for i in object]

    # Pass everything through pprint in the typical way
    printer = PrettyPrinter(stream=stream, indent=indent, width=width, depth=depth)
    printer.pprint(object)


@magics_class
class DjangoMagics(Magics):
    """
    """

    def __init__(self, *args, **kwds):
        """

        Arguments:
        - `*args`:
        - `**kwds`:
        """
        super(DjangoMagics, self).__init__(*args, **kwds)

    @line_magic
    def django_settings(self, arg):
        if len(arg) > 0:
            import_settings(self.shell, module_import=arg)
        else:
            import_settings(self.shell)

    @line_magic
    def django_models(self, arg):
        """
        """
        _autoimport_settings(self.shell)
        from django.core.management.color import no_style
        imported_objects = import_objects(options={'dont_load': []},
                                          style=no_style())
        self.shell.push(imported_objects)

    @line_magic
    def django_printmodels(self, models):
        """
        """
        dprint(self.shell.ev(models))


def load_ipython_extension(ip):
    """Load the extension in IPython."""
    ip.register_magics(DjangoMagics)
    global _loaded
    if not _loaded:
        ip.register_magics(DjangoMagics)
        _loaded = True

_loaded = False
