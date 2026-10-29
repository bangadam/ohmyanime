package desktop

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// iconPaths resolves the icon files relative to the package, which sits at
// internal/desktop in the repository.
func iconPaths(t *testing.T) (svg, png string) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, "assets", "icon.svg"), filepath.Join(root, "resources", "icon.png")
}

type svgDoc struct {
	XMLName xml.Name  `xml:"svg"`
	ViewBox string    `xml:"viewBox,attr"`
	Images  []svgEl   `xml:"image"`
	Uses    []svgEl   `xml:"use"`
	Rects   []svgRect `xml:"rect"`
	Paths   []struct {
		StrokeWidth string `xml:"stroke-width,attr"`
	} `xml:"path"`
}

type svgEl struct {
	Href string `xml:"href,attr"`
}

type svgRect struct {
	Width  string `xml:"width,attr"`
	Height string `xml:"height,attr"`
	RX     string `xml:"rx,attr"`
}

// TestIconSourceIsOriginalGeometry verifies the icon is a self-contained
// geometric SVG: square viewBox, no embedded or referenced bitmaps (which
// would risk carrying third-party artwork), a badge rect and a drawn glyph
// whose stroke keeps small sizes legible.
func TestIconSourceIsOriginalGeometry(t *testing.T) {
	svgPath, _ := iconPaths(t)
	body, err := os.ReadFile(svgPath)
	if err != nil {
		t.Fatalf("read icon source: %v", err)
	}
	var doc svgDoc
	if err := xml.Unmarshal(body, &doc); err != nil {
		t.Fatalf("parse icon SVG: %v", err)
	}
	parts := strings.Fields(doc.ViewBox)
	if len(parts) != 4 || parts[2] != parts[3] {
		t.Fatalf("viewBox %q is not square", doc.ViewBox)
	}
	for _, img := range append(doc.Images, doc.Uses...) {
		if img.Href != "" {
			t.Errorf("icon references external content %q; the mark must be pure geometry", img.Href)
		}
	}
	if len(doc.Rects) == 0 || len(doc.Paths) == 0 {
		t.Fatal("icon has no badge rect or glyph path")
	}
	size := strings.TrimSpace(parts[2])
	for _, rect := range doc.Rects {
		if rect.Width != size || rect.Height != size {
			t.Errorf("badge rect %sx%s does not fill the %s canvas", rect.Width, rect.Height, size)
		}
	}
	// The glyph stroke must be at least 2% of the canvas so the mark stays
	// legible when the icon renders at 16 px (2% of 1024 = 20 units).
	for _, p := range doc.Paths {
		if p.StrokeWidth == "" {
			continue
		}
		var width float64
		if _, err := fmt.Sscan(p.StrokeWidth, &width); err != nil {
			t.Fatalf("parse stroke-width %q: %v", p.StrokeWidth, err)
		}
		if width < 20 {
			t.Errorf("glyph stroke-width %s is thinner than 2%% of the canvas", p.StrokeWidth)
		}
	}
}

// TestIconPngIsSquare verifies the packaged icon raster is square, so every
// platform size (16, 32, 128, 512 …) resamples from one square source.
func TestIconPngIsSquare(t *testing.T) {
	_, pngPath := iconPaths(t)
	file, err := os.Open(pngPath)
	if err != nil {
		t.Fatalf("open packaged icon: %v", err)
	}
	defer file.Close()
	config, _, err := image.DecodeConfig(file)
	if err != nil {
		t.Fatalf("decode packaged icon: %v", err)
	}
	if config.Width != config.Height {
		t.Fatalf("packaged icon is %dx%d, not square", config.Width, config.Height)
	}
	if config.Width < 512 {
		t.Fatalf("packaged icon is %dpx wide; 1024 is expected for crisp small sizes", config.Width)
	}
}

// TestPackagingConfigWiresIcon verifies mygo.json points the build at the
// icon, which mygo build turns into the macOS .icns, the Windows
// executable icon resource and the Linux desktop entry icon.
func TestPackagingConfigWiresIcon(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(root, "mygo.json"))
	if err != nil {
		t.Fatalf("read mygo.json: %v", err)
	}
	var config struct {
		Icon string `json:"icon"`
	}
	if err := json.Unmarshal(body, &config); err != nil {
		t.Fatalf("parse mygo.json: %v", err)
	}
	if config.Icon == "" {
		t.Fatal("mygo.json has no icon field")
	}
	if _, err := os.Stat(filepath.Join(root, config.Icon)); err != nil {
		t.Fatalf("configured icon %s missing: %v", config.Icon, err)
	}
}
