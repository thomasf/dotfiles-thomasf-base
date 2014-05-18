# First, let's define some basic functions so that we don't duplicate
# code in both emacs.js and vi.js later. We can also declare private
# variables within a file to share between functions/bindings.

# WebKit normally scrolls 1 line on up/down and the viewport's height
# minus 1 line on pageup/pagedown. FIXME: need this to scale with font.
lineHeight = 40

# Horizontal scrolling seems to use the same number of pixels.
lineWidth = 40
yak.functions.add
  scrollLines: (n) ->
    window.scrollBy 0, n * lineHeight

  scrollCols: (n) ->
    window.scrollBy n * lineWidth, 0

  scrollPages: (n) ->
    direction = (if n >= 0 then 1 else -1)
    distance = Math.abs(n) * window.innerHeight - lineHeight
    window.scrollBy 0, direction * distance

  colLeft: ->
    yak.functions.scrollCols -1

  colRight: ->
    yak.functions.scrollCols 1

  lineUp: ->
    yak.functions.scrollLines -1

  lineDown: ->
    yak.functions.scrollLines 1

  pageDown: ->
    yak.functions.scrollPages 1

  pageUp: ->
    yak.functions.scrollPages -1

  gotoBottom: ->
    window.scroll window.scrollX, document.height

  gotoTop: ->
    window.scroll window.scrollX, 0

  gotoLeft: ->
    window.scroll 0, window.scrollY

  gotoRight: ->
    window.scroll document.width, window.scrollY

  tabSelect: (n) ->
    yak.tabs.getAllInWindow null, (tabs) ->
      tabs.forEach (t) ->
        if t.index is n
          yak.tabs.update t.id,
            selected: true




  tabSelectRelative: (n) ->
    yak.tabs.getSelected null, (tab) ->
      yak.functions.tabSelect tab.index + n


  tabLeft: ->
    yak.functions.tabSelectRelative -1

  tabRight: ->
    yak.functions.tabSelectRelative 1

  goBack: ->
    history.go -1

  goUp: ->
    components = location.pathname.split("/")
    components.pop()  if components.pop() is ""
    components.push ""
    location.href = location.protocol + "//" + location.host + components.join("/")

  goRoot: ->
    location.href = location.protocol + "//" + location.host + "/"

  pass: ->
    false


# Since we only have scrolling commands in here so far, we can do
# everything with precooked functions.
yak.bindings.add
  #
  "C-b":
    exclude: yak.textElements
    onkeydown: yak.functions.colLeft

  "C-p":
    exclude: yak.textElements
    onkeydown: yak.functions.lineUp

  "C-n":
    exclude: yak.textElements
    onkeydown: yak.functions.lineDown

  "M-v":
    exclude: yak.textElements
    onkeydown: yak.functions.pageUp

  "C-v":
    exclude: yak.textElements
    onkeydown: yak.functions.pageDown

  "C-a":
    exclude: yak.textElements
    onkeydown: yak.functions.gotoLeft

  "C-e":
    exclude: yak.textElements
    onkeydown: yak.functions.gotoRight

  "M-<":
    exclude: yak.textElements
    onkeydown: yak.functions.gotoTop

  "M->":
    exclude: yak.textElements
    onkeydown: yak.functions.gotoBottom

  "M-n":
    exclude: yak.textElements
    onkeydown: yak.functions.tabRight

  "M-p":
    exclude: yak.textElements
    onkeydown: yak.functions.tabLeft

  "M-:":
    onkeydown: (event) ->
      eval_ prompt("Eval:")



miniserv_url = "http://localhost:7345"

# other bindings
#
yak.bindings.add
  "C-M-r":
    onkeydown: ->
      r = new XMLHttpRequest()
      r.open "POST", "#{miniserv_url}/url"
      r.setRequestHeader "Content-Type", "application/json;charset=UTF-8"
      r.send JSON.stringify {
        url: window.location.toString()
        title: document.title
        body: window.getSelection().toString()
      }

# Local Variables:
# eval: (coffee-cos-mode 1)
# End:
