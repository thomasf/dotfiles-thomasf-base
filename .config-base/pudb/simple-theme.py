# Supported 16 color values:
#   'h0' (color number 0) through 'h15' (color number 15)
#    or
#   'default' (use the terminal's default foreground),
#   'black', 'dark red', 'dark green', 'brown', 'dark blue',
#   'dark magenta', 'dark cyan', 'light gray', 'dark gray',
#   'light red', 'light green', 'yellow', 'light blue',
#   'light magenta', 'light cyan', 'white'
#
# Supported 256 color values:
#   'h0' (color number 0) through 'h255' (color number 255)
#
# 256 color chart: http://en.wikipedia.org/wiki/File:Xterm_color_chart.png
#
# "setting_name": (foreground_color, background_color),

myFocusedBg = "dark magenta"
myFocusedFg = "black"
myFocusedColor = ("h15", "black")
myOuterColor = ("h15", "black")
myInnerColor = ("black", "black")
myTextColor = ("default", "default")
myLabelColor = ("brown", "default")

test = ("dark magenta", "dark green")

palette.update({
    "comment": ("h10", "default"),  # ok
    "header": myOuterColor, # ok
    "breakpoint source": ("h9", "default"), # ok
    "breakpoint focused source": ("h9", "black"), # ok
    "current breakpoint source": ("black", "h9"), # ok
    "current breakpoint focused source": ("black", "h9"), # ok
    "variables": myTextColor,  # ok
    "variable separator": ("dark cyan", "default"),
    "var label": myLabelColor, # ok
    "var value": ("default", "default"), # ok
    "focused var label": ("brown", "black"), # ok
    "focused var value": myFocusedColor, # ok
    "highlighted var label": ("default", "default"), # ok
    "highlighted var value": ("default", "default"), # ok
    "focused highlighted var label": myFocusedColor, # ok
    "focused highlighted var value": ("default", "default"), # ok
    "return label": ("dark blue", "default"), # ok
    "return value": ("default", "default"), # ok
    "focused return label": ("dark blue", "black"), # ok
    "focused return value": ("h15", "black"), # ok
    "stack": ("default", "default"), # ok
    "frame name": ("dark cyan", "default"), # ok
    "focused frame name": ("dark cyan", "black"), # ok
    "frame class": ("dark blue", "default"), # ok
    "focused frame class": ("dark blue", "black"), # ok
    "frame location": ("default", "default"), # ok
    "focused frame location": ("h15", "black"), # ok
    "current frame name": ("default", "default"), # ok
    "focused current frame name": myOuterColor, # ok
    "current frame class": ("default", "default"), # ok
    "focused current frame class": myFocusedColor, # ok
    "current frame location": ("light cyan", "default"), # ok
    "focused current frame location": ("light cyan", "default"), # ok
    "breakpoint": myTextColor,  # ok
    "focused breakpoint": myFocusedColor, # ok
    "current breakpoint": ("h9", "default"),# ok
    "focused current breakpoint": ("h9", "black"),# ok
    "selectable": ("black", "brown"),
    "focused selectable": myFocusedColor,
    "button": ("default", "default"),
    "focused button": myFocusedColor,
    "background": myOuterColor,
    "hotkey": ("dark cyan", "black"),  # ok
    "focused sidebar": ("black", "dark green"), # ok
    "warning": ("default", "h1"),
    "label": ("default", "default"),
    "value": ("brown", "default"),
    "fixed value": ("default", "default"),
    "group head": myOuterColor, # ok
    "search box": ("dark magenta", "default"), # ok
    "search not found": ("black", "h1"), # ok
    "dialog title": ("default", "default"), # ok
    # highlight source
    "source": ("default", "default"),  # ok
    "focused source": myFocusedColor, # ok
    "highlighted source": ("dark magenta", "default"),
    "current source": ("black", "dark cyan"), # ok
    "current focused source": ("black", "dark cyan"), # ok
    "current highlighted source": ("black", "dark cyan"),
    "line number": ("h10", "default"), # ok
    "keyword": ("dark green", "default"), # ok
    "name": ("dark blue", "default"), # ok
    "literal": ("dark cyan", "default"), # ok
    "string": ("dark cyan", "default"), # ok
    "doublestring": ("dark cyan", "default"), # ok
    "singlestring": ("dark cyan", "default"), # ok
    "docstring": ("h10", "default"), # ok
    "punctuation": ("brown", "default"), # ok
    "comment": ("h10", "default"), # ok
    "bp_star": ("h13", "default"), # ???
})
