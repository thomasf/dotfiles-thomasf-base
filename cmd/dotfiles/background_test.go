package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/lucasb-eyer/go-colorful"
)

var backgroundColors = map[string]string{
	"bg_0": "#103c48",
	"bg_1": "#184956",
	"bg_2": "#2d5b69",
}

func TestGeneratedBackgroundErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		value, want string
	}{
		{":waves()", "unknown name waves"},
		{":bg_0", "the value is string, not a background"},
		{":diagonal([bg_0])", "diagonal: colors: at least two colors"},
		{`:stripes([bg_0, "#12345"])`, "stripes: colors: #12345 is not a #rrggbb color"},
		{":diagonal(bg_0)", "colors is string, not a list"},
		{":stripes([bg_0, bg_1], 8)", "stripes: got 2 arguments"},
	}
	for _, tt := range tests {
		_, ok, err := generatedBackground(tt.value, backgroundColors, "dark", 0)
		if !ok || err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("generatedBackground(%q) = ok %v, error %v; want an error with %q", tt.value, ok, err, tt.want)
		}
	}

	for _, value := range []string{"assets/background.png", "/abs/bg.png", ""} {
		if _, ok, err := generatedBackground(value, backgroundColors, "dark", 0); ok || err != nil {
			t.Errorf("generatedBackground(%q) = ok %v, error %v; want a file", value, ok, err)
		}
	}
}

func TestDiagonalBackground(t *testing.T) {
	t.Parallel()
	// The stripes run from the top left to the bottom right, 20 pixels apart
	// in a 28x28 tile, the tile starts in the middle of the first color and
	// is in the middle of the second halfway across, and it is the same for
	// every seed.
	draw, _, err := generatedBackground(`:diagonal([bg_0, "#ffffff"])`, backgroundColors, "dark", 0)
	if err != nil {
		t.Fatal(err)
	}
	img := draw(96).(*image.RGBA)
	const n = 28
	if got := img.Bounds(); got != image.Rect(0, 0, n, n) {
		t.Fatalf("the tile is %v, want %dx%d", got, n, n)
	}
	for y := range n {
		for x := range n {
			if got, want := img.RGBAAt(x, y), img.RGBAAt((x+1)%n, (y+1)%n); got != want {
				t.Errorf("(%d,%d) is %v, (%d,%d) is %v, want the same", x, y, got, (x+1)%n, (y+1)%n, want)
			}
		}
	}
	if got := img.RGBAAt(0, 0); got.R != 0x10 || got.G != 0x3c || got.B != 0x48 {
		t.Errorf("(0,0) is %v, want bg_0", got)
	}
	if got := img.RGBAAt(n/2, 0); got.R != 0xff || got.G != 0xff || got.B != 0xff {
		t.Errorf("(%d,0) is %v, want #ffffff", n/2, got)
	}
	other, _, _ := generatedBackground(`:diagonal([bg_0, "#ffffff"])`, backgroundColors, "dark", 1)
	if got := other(96).(*image.RGBA); string(got.Pix) != string(img.Pix) {
		t.Error("another seed gives another diagonal background")
	}
}

func TestDefaultStripesLightness(t *testing.T) {
	t.Parallel()
	// bg_1 is mixed toward bg_0 until they differ in lightness by
	// maxStripesLightness, and kept if they differ by less.
	for bg1, want := range map[string]float64{"#ffffff": maxStripesLightness, "#123e4a": -1} {
		colors := map[string]string{"bg_0": "#103c48", "bg_1": bg1}
		draw, _, err := generatedBackground(":diagonal()", colors, "dark", 0)
		if err != nil {
			t.Fatal(err)
		}
		img := draw(96).(*image.RGBA)
		c := img.RGBAAt(img.Bounds().Dx()/2, 0)
		got := colorful.Color{R: float64(c.R) / 255, G: float64(c.G) / 255, B: float64(c.B) / 255}
		if want < 0 {
			if got.Hex() != bg1 {
				t.Errorf("with bg_1 %s: the second stripe is %s, want bg_1", bg1, got.Hex())
			}
			continue
		}
		bg0, _ := colorful.Hex(colors["bg_0"])
		l0, _, _ := bg0.OkLab()
		l, _, _ := got.OkLab()
		if d := l - l0; math.Abs(d-want) > 0.005 {
			t.Errorf("with bg_1 %s: the stripes differ in lightness by %.3f, want %.3f", bg1, d, want)
		}
	}
}

func TestStripesBackground(t *testing.T) {
	t.Parallel()
	pix := func(seed uint64) string {
		draw, _, err := generatedBackground(":stripes([bg_0, bg_1, bg_2])", backgroundColors, "dark", seed)
		if err != nil {
			t.Fatal(err)
		}
		img := draw(96).(*image.RGBA)
		return img.Bounds().String() + string(img.Pix)
	}
	if pix(7) != pix(7) {
		t.Error("the same seed gives different backgrounds")
	}
	seen := map[string]bool{}
	for seed := range uint64(20) {
		seen[pix(seed)] = true
	}
	if len(seen) < 10 {
		t.Errorf("20 seeds give %d different backgrounds, want at least 10", len(seen))
	}
}

func TestThemeBackground(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	themeDir := t.TempDir()
	active := func(background string) *activeTheme {
		v := &Variant{Mode: "dark", Colors: backgroundColors, Apps: map[string]string{"background": background}}
		return &activeTheme{theme: &Theme{Name: "test", Dir: themeDir, Variants: map[string]*Variant{"dark": v}}, variant: "dark"}
	}

	for background, want := range map[string]string{
		"assets/bg.png":    filepath.Join(themeDir, "assets/bg.png"),
		"/somewhere/b.png": "/somewhere/b.png",
	} {
		got, err := themeBackground(active(background), "")
		if err != nil || got != want {
			t.Errorf("themeBackground with %q = %q, %v; want %q", background, got, err, want)
		}
	}

	// Without a background, a tile of bg_0, at the default DPI.
	path, err := themeBackground(active(""), "")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if got := img.Bounds(); got != image.Rect(0, 0, 47, 47) {
		t.Errorf("without a background: %v, want 47x47 at 160 DPI", got)
	}
	if r, g, b, _ := img.At(5, 3).RGBA(); r>>8 != 0x10 || g>>8 != 0x3c || b>>8 != 0x48 {
		t.Errorf("without a background: %v at 5,3, want bg_0", img.At(5, 3))
	}

	if _, err := themeBackground(active(":diagonal()"), "high"); err == nil || !strings.Contains(err.Error(), "vars.dpi") {
		t.Errorf("themeBackground with dpi high: %v, want a vars.dpi error", err)
	}
	if _, err := themeBackground(active(":nope()"), ""); err == nil || !strings.Contains(err.Error(), "apps.background") {
		t.Errorf("themeBackground with :nope(): %v, want an apps.background error", err)
	}
}

func TestThemeBackgroundCommand(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	repo := filepath.Join(t.TempDir(), "repo")
	files := fstest.MapFS{"dotfiles.toml": tomlFile(`
[vars]
dpi = "96"

[theme]
dirs  = ["themes"]
dark  = "test:dark"
light = "test:light"
`)}
	for name, content := range themeFiles("themes/test") {
		files[name] = tomlFile(content)
	}
	for _, mode := range []string{"dark", "light"} {
		files["themes/test/variants/"+mode+".toml"] = tomlFile(variantTOML(mode, testColors(), map[string]string{"background": ":diagonal()"}))
	}
	if err := os.CopyFS(repo, files); err != nil {
		t.Fatal(err)
	}
	out := &bytes.Buffer{}
	d := &Dotfiles{Stdout: out, Stderr: &bytes.Buffer{}, DstPath: t.TempDir(), repos: []string{repo}}
	if err := d.ThemeCommand([]string{"background-image"}); err != nil {
		t.Fatalf("theme background-image: %v\nstderr:\n%s", err, d.Stderr)
	}
	data, err := os.ReadFile(strings.TrimSuffix(out.String(), "\n"))
	if err != nil {
		t.Fatal(err)
	}
	// Drawn at the dpi var, in background.png in $XDG_RUNTIME_DIR/dotfiles.
	if want := filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "dotfiles", "background.png"); out.String() != want+"\n" {
		t.Errorf("theme background-image printed %q, want %s", out, want)
	}
	if cfg, err := png.DecodeConfig(bytes.NewReader(data)); err != nil || cfg.Width != 28 || cfg.Height != 28 {
		t.Errorf("the background is %dx%d, %v; want 28x28", cfg.Width, cfg.Height, err)
	}

	// Test -expr and -preview flags
	out.Reset()
	if err := d.ThemeCommand([]string{"background-image", "-expr", ":chevron()", "-preview"}); err != nil {
		t.Fatalf("theme background-image -expr :chevron() -preview: %v", err)
	}
	if !strings.Contains(out.String(), "▀") {
		t.Errorf("theme background-image -preview output does not contain ANSI blocks: %q", out.String())
	}

	// Test -all flag
	out.Reset()
	if err := d.ThemeCommand([]string{"background-image", "-all"}); err != nil {
		t.Fatalf("theme background-image -all: %v", err)
	}
	for _, p := range []string{"chevron", "herringbone", "houndstooth", "basketweave", "diamond", "meander", "crosses", "parquet", "tatami", "stripes", "diagonal"} {
		if !strings.Contains(out.String(), p) {
			t.Errorf("theme background-image -all output missing %q: %q", p, out.String())
		}
	}
}

func TestClassicPatterns(t *testing.T) {
	t.Parallel()
	patterns := []string{
		":chevron()", ":herringbone()", ":houndstooth()", ":basketweave()", ":diamond()",
		":meander()", ":crosses()", ":parquet()", ":tatami()",
	}
	for _, name := range patterns {
		draw, ok, err := generatedBackground(name, backgroundColors, "dark", 0)
		if !ok || err != nil {
			t.Fatalf("generatedBackground(%q) = %v, %v; want ok", name, ok, err)
		}
		img := draw(96).(*image.RGBA)
		b := img.Bounds()
		if b.Dx() <= 0 || b.Dy() <= 0 {
			t.Fatalf("%s: invalid bounds %v", name, b)
		}
		// Zero smoothing: every pixel in the tile must be either the background
		// color or the foreground color, with no intermediate blended colors.
		counts := make(map[color.RGBA]int)
		for y := range b.Dy() {
			for x := range b.Dx() {
				counts[img.RGBAAt(x, y)]++
			}
		}
		if len(counts) != 2 {
			t.Errorf("%s: tile contains %d distinct colors, want exactly 2 (no smoothing)", name, len(counts))
		}
		// Exact 50/50 balance between the two colors
		total := b.Dx() * b.Dy()
		for c, count := range counts {
			if count != total/2 {
				t.Errorf("%s: color %v has %d pixels out of %d (%.1f%%), want 50%%", name, c, count, total, float64(count)*100/float64(total))
			}
		}
	}
}

func TestRandomBackground(t *testing.T) {
	t.Parallel()
	// Test :random without parens and with parens
	for _, expr := range []string{":random", ":random()", ":random([bg_0, bg_1])", ":random([chevron(), houndstooth()])"} {
		draw, ok, err := generatedBackground(expr, backgroundColors, "dark", 42)
		if !ok || err != nil {
			t.Fatalf("generatedBackground(%q) = %v, %v; want ok", expr, ok, err)
		}
		img := draw(96)
		if img == nil || img.Bounds().Dx() <= 0 {
			t.Errorf("generatedBackground(%q) produced empty image", expr)
		}
	}

	// Random backgrounds across multiple seeds should produce variety
	seen := make(map[string]bool)
	for seed := range uint64(30) {
		draw, ok, err := generatedBackground(":random()", backgroundColors, "dark", seed)
		if !ok || err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		img := draw(96).(*image.RGBA)
		key := fmt.Sprintf("%v:%x", img.Bounds(), img.Pix[:min(len(img.Pix), 32)])
		seen[key] = true
	}
	if len(seen) < 4 {
		t.Errorf("got %d unique patterns across 30 seeds, want at least 4", len(seen))
	}
}
