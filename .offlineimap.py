# -*- coding: utf-8 -*-
__author__ = 'Thomas Frössman'

from subprocess import Popen, PIPE
import os
import json


def load_gpg_json_dict():
    data = None
    try:
        data = Popen(["gpg",
                      "--quiet",
                      "--no-tty",
                      "--decrypt",
                      os.path.expanduser("~/.accounts.json.gpg")],
                     stdout=PIPE).communicate()[0]
    except Exception:
        print("error retreiving data")
    return json.loads(data)


def get_value(account, key):
    return load_gpg_json_dict()[account][key]


def get_username(account):
    return get_value(account, "username")


def get_password(account):
    return get_value(account, "password")
