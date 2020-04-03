#!/usr/bin/env python3
# -*- coding: utf-8 -*-
__author__ = "Thomas Frössman"


from bottle import route, run, template, static_file, get, post, request, redirect
from subprocess import Popen
import os
import sys
from urllib.parse import quote

root_dir = os.path.dirname(os.path.realpath(__file__))
files_dir = os.path.join(root_dir, "files")
# refdoc_dir = os.path.join(os.path.expanduser("~"), ".refdoc")
port = 7345


@route("/")
def index():
    return redirect("/files/index.html")


@get("/style.css")
def style_css():
    darkmode = os.path.exists(os.path.expanduser("~/.config/darkmode"))
    if darkmode:
        return redirect("/files/solarized-dark.min.css")
    else:
        return redirect("/files/solarized-light.min.css")


@get("/files/<filepath:path>")
def server_static(filepath):
    return static_file(filepath, root=files_dir)


# @get('/refdoc/<filepath:path>')
# def server_refdoc(filepath):
#     return static_file(filepath, root=refdoc_dir)


# @post('/files/<filepath:path>')
# def edit_file(filepath):
#     Popen(["emacsclient", "-a", "true", "-n", "--eval",
#            "(find-file-in-large-floating-frame \"" + os.path.join(files_dir, filepath) + "\")"])
#     return {"ok": True}


# @post('/url')
# def store_url():
#     data = request.json
#     Popen(["emacsclient", "-n", "org-protocol://capture://u/" +
#            quote(data['url'].encode('utf8'), safe='') + "/" +
#            quote(data['title'].encode('utf8'), safe='') + "/" +
#            quote(data['body'].encode('utf8'), safe='')])


# @get('/org-protocol/<arg:path>')
# def org_protocol(arg):
#     try:
#         Popen(["emacsclient", "-n", arg])
#         return {"ok": True}
#     except Exception:
#         return {"ok": False}

if __name__ == "__main__":
    if "-d" in sys.argv:
        run(host='localhost', port=port, quiet=True)
    else:
        run(host='localhost', port=port, reloader=True)
