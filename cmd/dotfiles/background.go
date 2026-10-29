package main

import (
	"bytes"
	"cmp"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/lucasb-eyer/go-colorful"
)

// The background of a theme variant, apps.background, is an image file in
// the theme directory, or an expression after a colon that generates the
// image, as in :stripes([bg_0, bg_1]). Expressions are expr expressions with
// the colors of the variant as variables, mix, and the generators diagonal,
// stripes, chevron, herringbone, houndstooth, basketweave, diamond, and
// random. The image is drawn at the dpi var of the repository, or defaultDPI.

// defaultDPI is the DPI generated backgrounds are drawn at without a dpi var.
const defaultDPI = 160

// maxStripesLightness is the most difference in OKLab lightness, from 0 to 1,
// between the default stripes, of bg_0 and bg_1. OKLab, unlike WCAG contrast,
// weighs a difference between dark colors as it looks.
const maxStripesLightness = 0.03

// backgroundDir returns the directory that the generated background is
// written to: dotfiles in $XDG_RUNTIME_DIR, or a directory of the user in the
// temporary directory without it.
func backgroundDir() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "dotfiles")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("dotfiles-%d", os.Getuid()))
}

// A drawBackground draws a generated background at a DPI.
type drawBackground func(dpi float64) image.Image

// generatedBackground evaluates value, an apps.background, with the colors and
// the mode, "dark" or "light", of a variant, and returns the function that
// draws it. ok is false if value is not an expression but a file. The
// variations of stripes come from seed, so that the same seed gives the same
// background.
func generatedBackground(value string, colors map[string]string, mode string, seed uint64) (draw drawBackground, ok bool, err error) {
	src, ok := strings.CutPrefix(value, ":")
	if !ok {
		return nil, false, nil
	}
	switch strings.TrimSpace(src) {
	case "random", "chevron", "herringbone", "houndstooth", "basketweave", "diamond", "stripes", "diagonal",
		"meander", "parquet", "tatami", "crosses":
		src = strings.TrimSpace(src) + "()"
	}
	// ChaCha8, unlike PCG, draws unrelated values for nearby seeds.
	var key [32]byte
	binary.LittleEndian.PutUint64(key[:], seed)
	rng := rand.New(rand.NewChaCha8(key))
	env := map[string]any{
		"mix":         mixColors,
		"diagonal":    func(args ...any) (drawBackground, error) { return stripes("diagonal", colors, mode, nil, args) },
		"stripes":     func(args ...any) (drawBackground, error) { return stripes("stripes", colors, mode, rng, args) },
		"chevron":     func(args ...any) (drawBackground, error) { return chevron(colors, mode, rng, args) },
		"herringbone": func(args ...any) (drawBackground, error) { return herringbone(colors, mode, rng, args) },
		"houndstooth": func(args ...any) (drawBackground, error) { return houndstooth(colors, mode, rng, args) },
		"basketweave": func(args ...any) (drawBackground, error) { return basketweave(colors, mode, rng, args) },
		"diamond":     func(args ...any) (drawBackground, error) { return diamond(colors, mode, rng, args) },
		"meander":     func(args ...any) (drawBackground, error) { return meander(colors, mode, rng, args) },
		"parquet":     func(args ...any) (drawBackground, error) { return parquet(colors, mode, rng, args) },
		"tatami":      func(args ...any) (drawBackground, error) { return tatami(colors, mode, rng, args) },
		"crosses":     func(args ...any) (drawBackground, error) { return crosses(colors, mode, rng, args) },
		"random":      func(args ...any) (drawBackground, error) { return randomPattern(colors, mode, rng, args) },
	}
	for name, c := range colors {
		env[name] = c
	}
	out, err := evalExpr(env, src)
	if err != nil {
		return nil, true, fmt.Errorf("%s: %w", value, err)
	}
	draw, isDraw := out.(drawBackground)
	if !isDraw {
		return nil, true, fmt.Errorf("%s: the value is %T, not a background", value, out)
	}
	return draw, true, nil
}

// stripes is the generator name, diagonal(colors) or stripes(colors):
// stripes of bands of equal width, with sharp edges. Across them, the bands
// go through colors, a list of at least two #rrggbb colors, and back, so
// a,b,c is the cycle a,b,c,b. Without colors, they are bg_0 and bg_1 of the
// variant, with bg_1 mixed toward bg_0 just enough that they differ in
// lightness by no more than maxStripesLightness. Without rng, for diagonal,
// they run from the top left to the bottom right, 20 pixels apart at 96 DPI.
// With it, for stripes, they run from the top left or the top right, tilted
// by up to 10 degrees, 20 to 29 pixels apart. mode is that of the variant,
// "dark" or "light".
func stripes(name string, colors map[string]string, mode string, rng *rand.Rand, args []any) (drawBackground, error) {
	if len(args) > 1 {
		return nil, fmt.Errorf("%s: got %d arguments, want colors", name, len(args))
	}
	list := []any{colors["bg_0"], colors["bg_1"]}
	bg0, err0 := parseColor(colors["bg_0"])
	bg1, err1 := parseColor(colors["bg_1"])
	if err0 == nil && err1 == nil {
		l0, _, _ := bg0.OkLab()
		l1, _, _ := bg1.OkLab()
		if d := math.Abs(l1 - l0); d > maxStripesLightness {
			list[1], _ = mixColors(colors["bg_0"], colors["bg_1"], maxStripesLightness/d)
		}
	}
	if len(args) > 0 {
		var ok bool
		if list, ok = args[0].([]any); !ok {
			return nil, fmt.Errorf("%s: colors is %T, not a list of colors", name, args[0])
		}
	}
	var stops []colorful.Color
	for _, c := range list {
		s, _ := c.(string)
		cc, err := parseColor(s)
		if err != nil {
			return nil, fmt.Errorf("%s: colors: %v is not a #rrggbb color", name, c)
		}
		stops = append(stops, cc)
	}
	if len(stops) < 2 {
		return nil, fmt.Errorf("%s: colors: at least two colors are needed", name)
	}
	cycle := slices.Clone(stops)
	for i := len(stops) - 2; i > 0; i-- {
		cycle = append(cycle, stops[i])
	}

	period, angle := 20.0, 45.0 // angle clockwise from horizontal, in degrees
	if rng != nil {
		periods := []float64{20, 28, 36, 48}
		period = periods[rng.IntN(len(periods))]
		angle = float64([]int{45, 135}[rng.IntN(2)] - 10 + rng.IntN(21))
	}
	return func(dpi float64) image.Image {
		p := period * dpi / 96
		sin, cos := math.Sincos(angle * math.Pi / 180)
		w := max(1, int(math.Round(p/math.Abs(sin))))
		h := max(1, int(math.Round(p/math.Abs(cos))))
		n := len(cycle)
		px := float64(n) / p
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := range h {
			for x := range w {
				phase := math.Copysign(float64(y)/float64(h), cos) - math.Copysign(float64(x)/float64(w), sin)
				s := (phase-math.Floor(phase))*float64(n) + 0.5 // the tile starts in the middle of the first band
				e := math.Round(s)                              // the nearest edge, from the band e-1 to e
				t := min(max((s-e)/px+0.5, 0), 1)
				k := int(e) % n
				r, g, b := cycle[(k+n-1)%n].BlendOkLab(cycle[k], t).Clamped().RGB255()
				img.SetRGBA(x, y, color.RGBA{r, g, b, 0xff})
			}
		}
		return img
	}, nil
}

// patternColors extracts c0 and c1 for patterns of two colors: c0 as background,
// c1 as foreground. Without colors, they are bg_0 and bg_1 of the variant,
// with bg_1 mixed toward bg_0 just enough that they differ in lightness by no
// more than maxStripesLightness.
func patternColors(name string, colors map[string]string, args []any) (c0, c1 color.RGBA, err error) {
	if len(args) > 1 {
		return color.RGBA{}, color.RGBA{}, fmt.Errorf("%s: got %d arguments, want colors", name, len(args))
	}
	list := []any{colors["bg_0"], colors["bg_1"]}
	bg0, err0 := parseColor(colors["bg_0"])
	bg1, err1 := parseColor(colors["bg_1"])
	if err0 == nil && err1 == nil {
		l0, _, _ := bg0.OkLab()
		l1, _, _ := bg1.OkLab()
		if d := math.Abs(l1 - l0); d > maxStripesLightness {
			list[1], _ = mixColors(colors["bg_0"], colors["bg_1"], maxStripesLightness/d)
		}
	}
	if len(args) > 0 {
		var ok bool
		if list, ok = args[0].([]any); !ok {
			return color.RGBA{}, color.RGBA{}, fmt.Errorf("%s: colors is %T, not a list of colors", name, args[0])
		}
	}
	if len(list) < 2 {
		return color.RGBA{}, color.RGBA{}, fmt.Errorf("%s: colors: at least two colors are needed", name)
	}
	var rgb [2]color.RGBA
	for i := range 2 {
		s, ok := list[i].(string)
		if !ok {
			return color.RGBA{}, color.RGBA{}, fmt.Errorf("%s: colors: %v is not a #rrggbb color", name, list[i])
		}
		cc, err := parseColor(s)
		if err != nil {
			return color.RGBA{}, color.RGBA{}, fmt.Errorf("%s: colors: %v is not a #rrggbb color", name, list[i])
		}
		r, g, b := cc.Clamped().RGB255()
		rgb[i] = color.RGBA{r, g, b, 0xff}
	}
	return rgb[0], rgb[1], nil
}

// chevron draws a continuous 45-degree zig-zag chevron of equal-width bands.
func chevron(colors map[string]string, mode string, rng *rand.Rand, args []any) (drawBackground, error) {
	c0, c1, err := patternColors("chevron", colors, args)
	if err != nil {
		return nil, err
	}
	band := 24.0
	colWidth := 48.0
	if rng != nil {
		bands := []float64{24, 30, 36}
		band = bands[rng.IntN(len(bands))]
		colWidth = band * 2
	}
	return func(dpi float64) image.Image {
		b := max(2, int(math.Round(band*dpi/96)))
		w := max(4, int(math.Round(colWidth*dpi/96)))
		tileW := 2 * w
		tileH := 2 * b
		img := image.NewRGBA(image.Rect(0, 0, tileW, tileH))
		for y := range tileH {
			for x := range tileW {
				offset := x
				if x >= w {
					offset = tileW - 1 - x
				}
				if (y+offset)%tileH < b {
					img.SetRGBA(x, y, c1)
				} else {
					img.SetRGBA(x, y, c0)
				}
			}
		}
		return img
	}, nil
}

// herringbone draws a classic staggered parquet weave of 45-degree diagonal blocks.
func herringbone(colors map[string]string, mode string, rng *rand.Rand, args []any) (drawBackground, error) {
	c0, c1, err := patternColors("herringbone", colors, args)
	if err != nil {
		return nil, err
	}
	band := 20.0
	if rng != nil {
		bands := []float64{16, 20, 24, 32}
		band = bands[rng.IntN(len(bands))]
	}
	return func(dpi float64) image.Image {
		b := max(2, int(math.Round(band*dpi/96)))
		w := 2 * b
		tileW := 2 * w
		tileH := 2 * b
		img := image.NewRGBA(image.Rect(0, 0, tileW, tileH))
		for y := range tileH {
			for x := range tileW {
				var val int
				if x < w {
					val = (y + x) % tileH
				} else {
					val = (y - (x - w) + b) % tileH
				}
				if val < 0 {
					val += tileH
				}
				if val < b {
					img.SetRGBA(x, y, c1)
				} else {
					img.SetRGBA(x, y, c0)
				}
			}
		}
		return img
	}, nil
}

// houndstoothMatrix is the classic 2/2 twill 8x8 houndstooth weaving matrix.
var houndstoothMatrix = [8][8]byte{
	{1, 1, 1, 1, 0, 0, 1, 1},
	{1, 1, 1, 1, 1, 0, 0, 1},
	{1, 1, 1, 1, 1, 1, 0, 0},
	{1, 1, 1, 1, 0, 1, 1, 0},
	{1, 1, 0, 0, 0, 0, 0, 0},
	{0, 1, 1, 0, 0, 0, 0, 0},
	{0, 0, 1, 1, 0, 0, 0, 0},
	{1, 0, 0, 1, 0, 0, 0, 0},
}

// houndstooth draws the classic 4-pointed menswear broken-check pattern.
func houndstooth(colors map[string]string, mode string, rng *rand.Rand, args []any) (drawBackground, error) {
	c0, c1, err := patternColors("houndstooth", colors, args)
	if err != nil {
		return nil, err
	}
	scalePt := 6.0
	if rng != nil {
		scales := []float64{6, 8, 10}
		scalePt = scales[rng.IntN(len(scales))]
	}
	return func(dpi float64) image.Image {
		s := max(1, int(math.Round(scalePt*dpi/96)))
		dim := 8 * s
		img := image.NewRGBA(image.Rect(0, 0, dim, dim))
		for y := range dim {
			for x := range dim {
				if houndstoothMatrix[y/s][x/s] == 1 {
					img.SetRGBA(x, y, c1)
				} else {
					img.SetRGBA(x, y, c0)
				}
			}
		}
		return img
	}, nil
}

// basketweave draws alternating blocks of horizontal and vertical bars.
func basketweave(colors map[string]string, mode string, rng *rand.Rand, args []any) (drawBackground, error) {
	c0, c1, err := patternColors("basketweave", colors, args)
	if err != nil {
		return nil, err
	}
	blockSize := 32.0
	if rng != nil {
		sizes := []float64{24, 32, 40, 48}
		blockSize = sizes[rng.IntN(len(sizes))]
	}
	return func(dpi float64) image.Image {
		b := max(4, int(math.Round(blockSize*dpi/96)))
		if b%2 != 0 {
			b++
		}
		stripe := max(1, b/4)
		tileDim := 2 * b
		img := image.NewRGBA(image.Rect(0, 0, tileDim, tileDim))
		for y := range tileDim {
			for x := range tileDim {
				block := ((x / b) + (y / b)) % 2
				var isForeground bool
				if block == 0 {
					isForeground = (y/stripe)%2 == 0
				} else {
					isForeground = (x/stripe)%2 == 0
				}
				if isForeground {
					img.SetRGBA(x, y, c1)
				} else {
					img.SetRGBA(x, y, c0)
				}
			}
		}
		return img
	}, nil
}

// diamond draws a 45-degree diamond lattice check of equal-area lozenges.
func diamond(colors map[string]string, mode string, rng *rand.Rand, args []any) (drawBackground, error) {
	c0, c1, err := patternColors("diamond", colors, args)
	if err != nil {
		return nil, err
	}
	periodPt := 48.0
	if rng != nil {
		periods := []float64{36, 48, 60, 72}
		periodPt = periods[rng.IntN(len(periods))]
	}
	return func(dpi float64) image.Image {
		p := max(4, int(math.Round(periodPt*dpi/96)))
		if p%2 != 0 {
			p++
		}
		half := p / 2
		img := image.NewRGBA(image.Rect(0, 0, p, p))
		for y := range p {
			for x := range p {
				u := ((x + y) / half) % 2
				v := ((x - y + p) / half) % 2
				if (u+v)%2 == 0 {
					img.SetRGBA(x, y, c1)
				} else {
					img.SetRGBA(x, y, c0)
				}
			}
		}
		return img
	}, nil
}

// meanderMatrix is the classic 12x12 interlocking Greek key fretwork matrix.
var meanderMatrix = [12][12]byte{
	{1, 1, 1, 1, 1, 1, 0, 0, 0, 0, 0, 0},
	{1, 0, 0, 0, 0, 1, 0, 1, 1, 1, 1, 0},
	{1, 0, 1, 1, 0, 1, 0, 1, 0, 0, 1, 0},
	{1, 0, 1, 1, 0, 1, 0, 1, 0, 0, 1, 0},
	{1, 0, 0, 0, 0, 1, 0, 1, 1, 1, 1, 0},
	{1, 1, 1, 1, 1, 1, 0, 0, 0, 0, 0, 0},
	{0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 1, 1},
	{0, 1, 1, 1, 1, 0, 1, 0, 0, 0, 0, 1},
	{0, 1, 0, 0, 1, 0, 1, 0, 1, 1, 0, 1},
	{0, 1, 0, 0, 1, 0, 1, 0, 1, 1, 0, 1},
	{0, 1, 1, 1, 1, 0, 1, 0, 0, 0, 0, 1},
	{0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 1, 1},
}

// meander draws a continuous interlocking Greek key (meander) architectural fretwork.
func meander(colors map[string]string, mode string, rng *rand.Rand, args []any) (drawBackground, error) {
	c0, c1, err := patternColors("meander", colors, args)
	if err != nil {
		return nil, err
	}
	scalePt := 6.0
	if rng != nil {
		scales := []float64{6, 8, 10}
		scalePt = scales[rng.IntN(len(scales))]
	}
	return func(dpi float64) image.Image {
		s := max(1, int(math.Round(scalePt*dpi/96)))
		dim := 12 * s
		img := image.NewRGBA(image.Rect(0, 0, dim, dim))
		for y := range dim {
			for x := range dim {
				if meanderMatrix[y/s][x/s] == 1 {
					img.SetRGBA(x, y, c1)
				} else {
					img.SetRGBA(x, y, c0)
				}
			}
		}
		return img
	}, nil
}

// parquetMatrix is the 4x4 windmill parquet layout.
var parquetMatrix = [4][4]byte{
	{1, 1, 1, 0},
	{0, 1, 0, 0},
	{0, 0, 1, 0},
	{0, 1, 1, 1},
}

// parquet draws an interlocking windmill parquet wood-block floor pattern.
func parquet(colors map[string]string, mode string, rng *rand.Rand, args []any) (drawBackground, error) {
	c0, c1, err := patternColors("parquet", colors, args)
	if err != nil {
		return nil, err
	}
	blockPt := 24.0
	if rng != nil {
		blocks := []float64{16, 20, 24, 32}
		blockPt = blocks[rng.IntN(len(blocks))]
	}
	return func(dpi float64) image.Image {
		s := max(2, int(math.Round(blockPt*dpi/96)))
		dim := 4 * s
		img := image.NewRGBA(image.Rect(0, 0, dim, dim))
		for y := range dim {
			for x := range dim {
				if parquetMatrix[y/s][x/s] == 1 {
					img.SetRGBA(x, y, c1)
				} else {
					img.SetRGBA(x, y, c0)
				}
			}
		}
		return img
	}, nil
}

// tatamiMatrix is the traditional Japanese 8-mat 2:1 tatami layout.
var tatamiMatrix = [4][4]byte{
	{0, 0, 1, 0},
	{1, 0, 1, 0},
	{1, 0, 1, 1},
	{0, 0, 1, 1},
}

// tatami draws traditional interlocking 2:1 tatami mats.
func tatami(colors map[string]string, mode string, rng *rand.Rand, args []any) (drawBackground, error) {
	c0, c1, err := patternColors("tatami", colors, args)
	if err != nil {
		return nil, err
	}
	matPt := 24.0
	if rng != nil {
		mats := []float64{16, 20, 24, 32}
		matPt = mats[rng.IntN(len(mats))]
	}
	return func(dpi float64) image.Image {
		s := max(2, int(math.Round(matPt*dpi/96)))
		dim := 4 * s
		img := image.NewRGBA(image.Rect(0, 0, dim, dim))
		for y := range dim {
			for x := range dim {
				if tatamiMatrix[y/s][x/s] == 1 {
					img.SetRGBA(x, y, c1)
				} else {
					img.SetRGBA(x, y, c0)
				}
			}
		}
		return img
	}, nil
}

// crosses draws an interlocking Greek cross Byzantine mosaic tessellation.
func crosses(colors map[string]string, mode string, rng *rand.Rand, args []any) (drawBackground, error) {
	c0, c1, err := patternColors("crosses", colors, args)
	if err != nil {
		return nil, err
	}
	armPt := 8.0
	if rng != nil {
		arms := []float64{8, 10, 12}
		armPt = arms[rng.IntN(len(arms))]
	}
	return func(dpi float64) image.Image {
		s := max(1, int(math.Round(armPt*dpi/96)))
		dim := 10 * s
		img := image.NewRGBA(image.Rect(0, 0, dim, dim))
		for y := range dim {
			gy := y / s
			for x := range dim {
				gx := x / s
				var colorVal byte
				for _, d := range [][2]int{{0, 0}, {1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
					cx, cy := gx+d[0], gy+d[1]
					if (2*cx+cy)%5 == 0 {
						a := (2*cx + cy) / 5
						b := (2*cy - cx) / 5
						if (a+b)%2 != 0 {
							colorVal = 1
						}
						break
					}
				}
				if colorVal == 1 {
					img.SetRGBA(x, y, c1)
				} else {
					img.SetRGBA(x, y, c0)
				}
			}
		}
		return img
	}, nil
}

// randomPattern selects randomly among available background generators.
func randomPattern(colors map[string]string, mode string, rng *rand.Rand, args []any) (drawBackground, error) {
	generators := []func([]any) (drawBackground, error){
		func(a []any) (drawBackground, error) { return stripes("stripes", colors, mode, rng, a) },
		func(a []any) (drawBackground, error) { return chevron(colors, mode, rng, a) },
		func(a []any) (drawBackground, error) { return herringbone(colors, mode, rng, a) },
		func(a []any) (drawBackground, error) { return houndstooth(colors, mode, rng, a) },
		func(a []any) (drawBackground, error) { return basketweave(colors, mode, rng, a) },
		func(a []any) (drawBackground, error) { return diamond(colors, mode, rng, a) },
		func(a []any) (drawBackground, error) { return meander(colors, mode, rng, a) },
		func(a []any) (drawBackground, error) { return parquet(colors, mode, rng, a) },
		func(a []any) (drawBackground, error) { return tatami(colors, mode, rng, a) },
		func(a []any) (drawBackground, error) { return crosses(colors, mode, rng, a) },
	}

	if len(args) > 0 {
		var list []any
		if l, ok := args[0].([]any); ok && len(args) == 1 {
			list = l
		} else {
			list = args
		}
		hasFuncs := false
		for _, item := range list {
			switch item.(type) {
			case drawBackground, func(...any) (drawBackground, error), func() (drawBackground, error):
				hasFuncs = true
			}
		}
		if hasFuncs {
			pick := 0
			if rng != nil && len(list) > 1 {
				pick = rng.IntN(len(list))
			}
			switch fn := list[pick].(type) {
			case drawBackground:
				return fn, nil
			case func(...any) (drawBackground, error):
				return fn()
			case func() (drawBackground, error):
				return fn()
			default:
				return nil, fmt.Errorf("random: item %d is %T, not a background", pick, list[pick])
			}
		}
		pick := 0
		if rng != nil {
			pick = rng.IntN(len(generators))
		}
		return generators[pick](args)
	}

	pick := 0
	if rng != nil {
		pick = rng.IntN(len(generators))
	}
	return generators[pick](nil)
}

// themeBackground is dotfiles theme background-image: the path of the
// background image of the theme variant a, a tile of bg_0 if it has none.
func themeBackground(a *activeTheme, dpiVar string) (string, error) {
	path, _, err := themeBackgroundExpr(a, dpiVar, "")
	return path, err
}

// themeBackgroundExpr renders the background for expression (or the variant's
// default if expr is "") and returns the written image path and image.
func themeBackgroundExpr(a *activeTheme, dpiVar string, expr string) (string, image.Image, error) {
	v := a.theme.Variants[a.variant]
	value := cmp.Or(expr, v.Apps["background"], ":diagonal([bg_0, bg_0])") // a tile of bg_0
	if expr != "" && !strings.HasPrefix(value, ":") && !strings.Contains(value, "/") && !strings.HasSuffix(value, ".png") {
		value = ":" + value
	}
	draw, ok, err := generatedBackground(value, v.Colors, v.Mode, a.seed)
	if err != nil {
		return "", nil, fmt.Errorf("theme background-image: %s: apps.background: %w", a.ref(), err)
	}
	if !ok {
		if filepath.IsAbs(value) {
			return value, nil, nil
		}
		return filepath.Join(a.theme.Dir, value), nil, nil
	}

	dpi := float64(defaultDPI)
	if dpiVar != "" {
		var parseErr error
		dpi, parseErr = strconv.ParseFloat(dpiVar, 64)
		if parseErr != nil || dpi <= 0 || math.IsInf(dpi, 0) {
			return "", nil, fmt.Errorf("theme background-image: vars.dpi: %q is not a positive number", dpiVar)
		}
	}
	img := draw(dpi)
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.NoCompression}
	if err := enc.Encode(&buf, img); err != nil {
		return "", nil, fmt.Errorf("theme background-image: %w", err)
	}
	path := filepath.Join(backgroundDir(), "background.png")
	if _, err := writeFile(path, buf.Bytes(), 0o644); err != nil {
		return "", nil, fmt.Errorf("theme background-image: %w", err)
	}
	return path, img, nil
}

// renderTerminalBackground writes an ANSI 24-bit color half-block preview of img to w.
func renderTerminalBackground(w io.Writer, img image.Image, cols, rows int) {
	if img == nil {
		return
	}
	b := img.Bounds()
	bw, bh := b.Dx(), b.Dy()
	if bw <= 0 || bh <= 0 {
		return
	}
	// Each text row represents 2 vertical pixels (upper half block \u2580).
	pixHeight := rows * 2
	for y := 0; y < pixHeight; y += 2 {
		var line strings.Builder
		for x := range cols {
			cTop := img.At(b.Min.X+(x%bw), b.Min.Y+(y%bh))
			cBot := img.At(b.Min.X+(x%bw), b.Min.Y+((y+1)%bh))
			r1, g1, b1, _ := cTop.RGBA()
			r2, g2, b2, _ := cBot.RGBA()
			fmt.Fprintf(&line, "\033[38;2;%d;%d;%dm\033[48;2;%d;%d;%dm▀", r1>>8, g1>>8, b1>>8, r2>>8, g2>>8, b2>>8)
		}
		line.WriteString("\033[0m\n")
		io.WriteString(w, line.String())
	}
}
