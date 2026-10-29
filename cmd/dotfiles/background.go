package main

import (
	"bytes"
	"cmp"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
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
// the colors of the variant as variables, mix, and the generators diagonal
// and stripes. The image is drawn at the dpi var of the repository, or
// defaultDPI.

// defaultDPI is the DPI generated backgrounds are drawn at without a dpi var.
const defaultDPI = 160

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

// generatedBackground evaluates value, an apps.background, with the colors of
// a variant, and returns the function that draws it. ok is false if value is
// not an expression but a file. The variations of stripes come from seed, so
// that the same seed gives the same background.
func generatedBackground(value string, colors map[string]string, seed uint64) (draw drawBackground, ok bool, err error) {
	src, ok := strings.CutPrefix(value, ":")
	if !ok {
		return nil, false, nil
	}
	// ChaCha8, unlike PCG, draws unrelated values for nearby seeds.
	var key [32]byte
	binary.LittleEndian.PutUint64(key[:], seed)
	rng := rand.New(rand.NewChaCha8(key))
	env := map[string]any{
		"mix":      mixColors,
		"diagonal": func(args ...any) (drawBackground, error) { return stripes("diagonal", colors, nil, args) },
		"stripes":  func(args ...any) (drawBackground, error) { return stripes("stripes", colors, rng, args) },
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

// stripes is the generator name, diagonal(colors) or stripes(colors): soft
// stripes. Across them, the color goes smoothly through colors, a list of at
// least two #rrggbb colors ([bg_0, bg_1] of the variant without one), and
// back, so a,b,c is the cycle a,b,c,b. Without rng, for diagonal, they are
// the Selenized backgrounds: from the top left to the bottom right, 6 pixels
// apart at 96 DPI. With it, for stripes, they run from the top left or the
// top right, tilted by up to 10 degrees, 5 to 7 pixels apart.
func stripes(name string, colors map[string]string, rng *rand.Rand, args []any) (drawBackground, error) {
	if len(args) > 1 {
		return nil, fmt.Errorf("%s: got %d arguments, want colors", name, len(args))
	}
	list := []any{colors["bg_0"], colors["bg_1"]}
	if len(args) > 0 {
		var ok bool
		if list, ok = args[0].([]any); !ok {
			return nil, fmt.Errorf("%s: colors is %T, not a list of colors", name, args[0])
		}
	}
	var stops []colorful.Color
	for _, c := range list {
		s, _ := c.(string)
		cc, err := colorful.Hex(s)
		if err != nil || !hexColorRe.MatchString(s) {
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

	period, angle := 6.0, 45.0 // angle clockwise from horizontal, in degrees
	if rng != nil {
		period = float64(5 + rng.IntN(3))
		angle = float64([]int{45, 135}[rng.IntN(2)] - 10 + rng.IntN(21))
	}
	return func(dpi float64) image.Image {
		// The color of a pixel is that at the phase of the pixel across the
		// stripes, y*cos-x*sin of the angle in periods. It is made to change
		// by one period along the width w and the height h of the tile, so
		// that the tile repeats without a seam; that rounds the angle and the
		// period a little.
		p := period * dpi / 96
		sin, cos := math.Sincos(angle * math.Pi / 180)
		w := max(1, int(math.Round(p/math.Abs(sin))))
		h := max(1, int(math.Round(p/math.Abs(cos))))
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := range h {
			for x := range w {
				phase := math.Copysign(float64(y)/float64(h), cos) - math.Copysign(float64(x)/float64(w), sin)
				s := (phase - math.Floor(phase)) * float64(len(cycle))
				k := min(int(s), len(cycle)-1)
				t := (1 - math.Cos(math.Pi*(s-float64(k)))) / 2 // eased, without kinks at the colors
				r, g, b := cycle[k].BlendOkLab(cycle[(k+1)%len(cycle)], t).Clamped().RGB255()
				img.SetRGBA(x, y, color.RGBA{r, g, b, 0xff})
			}
		}
		return img
	}, nil
}

// themeBackground is dotfiles theme background-image: the path of the
// background image of the theme variant a, a tile of bg_0 if it has none. A
// generated background is drawn at dpiVar, the dpi var, or defaultDPI if it
// is "", on every call, and written to background.png in backgroundDir,
// replacing the last one. It is not compressed, which makes encoding it
// instant. Its random values come from the seed of the theme state.
func themeBackground(a *activeTheme, dpiVar string) (string, error) {
	v := a.theme.Variants[a.variant]
	value := cmp.Or(v.Apps["background"], ":diagonal([bg_0, bg_0])") // a tile of bg_0
	draw, ok, err := generatedBackground(value, v.Colors, a.seed)
	if err != nil {
		return "", fmt.Errorf("theme background-image: %s: apps.background: %w", a.ref(), err)
	}
	if !ok {
		if filepath.IsAbs(value) {
			return value, nil
		}
		return filepath.Join(a.theme.Dir, value), nil
	}

	dpi := float64(defaultDPI)
	if dpiVar != "" {
		dpi, err = strconv.ParseFloat(dpiVar, 64)
		if err != nil || dpi <= 0 || math.IsInf(dpi, 0) {
			return "", fmt.Errorf("theme background-image: vars.dpi: %q is not a positive number", dpiVar)
		}
	}
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.NoCompression}
	if err := enc.Encode(&buf, draw(dpi)); err != nil {
		return "", fmt.Errorf("theme background-image: %w", err)
	}
	path := filepath.Join(backgroundDir(), "background.png")
	if _, err := writeFile(path, buf.Bytes(), 0o644); err != nil {
		return "", fmt.Errorf("theme background-image: %w", err)
	}
	return path, nil
}
