# -*- coding: utf-8 -*-

import json
import os
from subprocess import PIPE, Popen


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


if __name__ == '__main__':
    print(get_password("myGmail"))
