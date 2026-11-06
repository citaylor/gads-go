// Package pdf writes PDF documents as GADS does with CtrlO::PDF, PDF::Table
// and PDF::Builder: the same page layout, fonts, text positions, table
// geometry and pagination, so that the text of a document (as extracted by
// pdftotext) is that of Perl. The bytes of the file are not the same.
package pdf

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"image"
	"image/color"
	_ "image/gif" // logos
	"image/jpeg"
	_ "image/png" // logos
	"sort"
	"strconv"
	"strings"
)

// Document is a PDF document (PDF::Builder)
type Document struct {
	pages  []*Page
	fonts  []*fontUse
	images []*Image
}

// fontUse is a font of a document with the glyphs used
type fontUse struct {
	font *Font
	name string
	used map[uint16]bool
}

// Page is a page of a document
type Page struct {
	doc      *Document
	W, H     float64
	contents []*Content
}

// Content is a content stream of a page (PDF::Builder::Content: the
// page's text and gfx)
type Content struct {
	page    *Page
	buf     bytes.Buffer
	text    bool
	font    *fontUse
	size    float64
	started bool
	images  map[*Image]bool
}

// Image is an image (a logo)
type Image struct {
	name          string
	Width, Height int // in pixels
	jpeg          []byte
	components    int
	rgb, alpha    []byte
}

// NewDocument creates an empty document
func NewDocument() *Document { return &Document{} }

// AddPage adds a page (PDF::Builder page, with its mediabox)
func (d *Document) AddPage(w, h float64) *Page {
	p := &Page{doc: d, W: w, H: h}
	d.pages = append(d.pages, p)
	return p
}

// Pages is the number of pages
func (d *Document) Pages() int { return len(d.pages) }

// OpenPage is page n (from 1)
func (d *Document) OpenPage(n int) *Page { return d.pages[n-1] }

func (d *Document) useFont(f *Font) *fontUse {
	for _, u := range d.fonts {
		if u.font == f {
			return u
		}
	}
	u := &fontUse{font: f, name: fmt.Sprintf("F%d", len(d.fonts)+1), used: map[uint16]bool{}}
	d.fonts = append(d.fonts, u)
	return u
}

// Text adds a text content stream to the page (Page text)
func (p *Page) Text() *Content {
	c := &Content{page: p, text: true}
	p.contents = append(p.contents, c)
	return c
}

// Gfx adds a graphics content stream to the page (Page gfx)
func (p *Page) Gfx() *Content {
	c := &Content{page: p}
	p.contents = append(p.contents, c)
	return c
}

// num formats a number as PDF::Builder does: with no exponent, and at
// most 5 decimals
func num(v float64) string {
	s := strconv.FormatFloat(v, 'f', 5, 64)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "-0" || s == "" {
		return "0"
	}
	return s
}

func (c *Content) add(ops ...string) {
	c.buf.WriteString(strings.Join(ops, " "))
	c.buf.WriteByte('\n')
}

func (c *Content) begin() {
	if c.text && !c.started {
		c.add("BT")
		c.started = true
	}
}

// Font sets the font and size of text (Content font)
func (c *Content) Font(f *Font, size float64) {
	c.begin()
	c.font = c.page.doc.useFont(f)
	c.size = size
	c.add("/"+c.font.name, num(size), "Tf")
}

// Translate moves the text position (Content translate)
func (c *Content) Translate(x, y float64) {
	c.begin()
	c.add("1 0 0 1", num(x), num(y), "Tm")
}

// AdvanceWidth is the width of a string in the current font
func (c *Content) AdvanceWidth(s string) float64 {
	if c.font == nil {
		return 0
	}
	return c.font.font.Width(s, c.size)
}

// Show writes text at the current position (Content text), returning its
// width
func (c *Content) Show(s string) float64 {
	c.begin()
	var hex strings.Builder
	for _, r := range s {
		g := c.font.font.Glyph(r)
		c.font.used[g] = true
		fmt.Fprintf(&hex, "%04x", g)
	}
	c.add("<"+hex.String()+">", "Tj")
	return c.AdvanceWidth(s)
}

// TextAt writes text with its left (align "left"), centre ("center") or
// right ("right") at a position, as text, text_center and text_right
func (c *Content) TextAt(x, y float64, s, align string) float64 {
	w := c.AdvanceWidth(s)
	switch align {
	case "center":
		x -= w / 2
	case "right":
		x -= w
	}
	c.Translate(x, y)
	return c.Show(s)
}

// colorOps are the operators of a colour (PDF::Builder's names or #rgb)
func colorOps(s string, stroke bool) string {
	r, g, b := parseColor(s)
	op := "rg"
	if stroke {
		op = "RG"
	}
	return num(r) + " " + num(g) + " " + num(b) + " " + op
}

var colorNames = map[string]string{
	"black": "#000000", "white": "#ffffff", "red": "#ff0000", "green": "#008000",
	"blue": "#0000ff", "gray": "#808080", "grey": "#808080", "yellow": "#ffff00",
}

func parseColor(s string) (float64, float64, float64) {
	s = strings.ToLower(strings.TrimSpace(s))
	if n, ok := colorNames[s]; ok {
		s = n
	}
	s = strings.TrimPrefix(s, "#")
	if len(s) == 3 {
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}
	if len(s) != 6 {
		return 0, 0, 0
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0, 0, 0
	}
	return float64(v>>16&255) / 255, float64(v>>8&255) / 255, float64(v&255) / 255
}

// FillColor sets the fill colour (Content fillcolor)
func (c *Content) FillColor(s string) { c.begin(); c.add(colorOps(s, false)) }

// StrokeColor sets the stroke colour (Content strokecolor)
func (c *Content) StrokeColor(s string) { c.begin(); c.add(colorOps(s, true)) }

// LineWidth sets the line width
func (c *Content) LineWidth(w float64) { c.add(num(w), "w") }

// LineDash sets a dash pattern, or a solid line with none
func (c *Content) LineDash(dash ...float64) {
	var parts []string
	for _, d := range dash {
		parts = append(parts, num(d))
	}
	c.add("["+strings.Join(parts, " ")+"]", "0 d")
}

// Move starts a path
func (c *Content) Move(x, y float64) { c.add(num(x), num(y), "m") }

// HLine draws a horizontal line from the current point (gfx hline)
func (c *Content) HLine(x, y float64) { c.add(num(x), num(y), "l") }

// Line draws a line from the current point
func (c *Content) Line(x, y float64) { c.add(num(x), num(y), "l") }

// Stroke strokes the path
func (c *Content) Stroke() { c.add("S") }

// Rect adds a rectangle to the path
func (c *Content) Rect(x, y, w, h float64) { c.add(num(x), num(y), num(w), num(h), "re") }

// Fill fills the path
func (c *Content) Fill() { c.add("f") }

// Image draws an image at a scale of its size in pixels (gfx image)
func (c *Content) Image(img *Image, x, y, scale float64) {
	if c.images == nil {
		c.images = map[*Image]bool{}
	}
	c.images[img] = true
	c.add("q", num(float64(img.Width)*scale), "0 0", num(float64(img.Height)*scale), num(x), num(y), "cm /"+img.name, "Do Q")
}

// maxImagePixels is the largest image added (40 million pixels: a logo
// is far smaller)
const maxImagePixels = 40_000_000

// AddImage reads an image (PNG, JPEG or GIF) for the document
func (d *Document) AddImage(data []byte) (*Image, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	// Deviation: an image of more than maxImagePixels is refused before
	// it is decoded, as its pixels are held in memory (Perl's worker
	// would run out of memory; here, the server)
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > maxImagePixels {
		return nil, fmt.Errorf("image of %dx%d pixels is too large", cfg.Width, cfg.Height)
	}
	img := &Image{name: fmt.Sprintf("Im%d", len(d.images)+1), Width: cfg.Width, Height: cfg.Height}
	if format == "jpeg" {
		img.jpeg = data
		img.components = 3
		if cfg.ColorModel == color.GrayModel {
			img.components = 1
		} else if cfg.ColorModel == color.CMYKModel {
			img.components = 4
		}
		// Check that it decodes
		if _, err := jpeg.Decode(bytes.NewReader(data)); err != nil {
			return nil, err
		}
	} else {
		m, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		b := m.Bounds()
		hasAlpha := false
	alpha:
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				if color.NRGBAModel.Convert(m.At(x, y)).(color.NRGBA).A != 255 {
					hasAlpha = true
					break alpha
				}
			}
		}
		img.rgb = make([]byte, 0, 3*b.Dx()*b.Dy())
		if hasAlpha {
			img.alpha = make([]byte, 0, b.Dx()*b.Dy())
		}
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				c := color.NRGBAModel.Convert(m.At(x, y)).(color.NRGBA)
				img.rgb = append(img.rgb, c.R, c.G, c.B)
				if hasAlpha {
					img.alpha = append(img.alpha, c.A)
				}
			}
		}
	}
	d.images = append(d.images, img)
	return img, nil
}

func deflate(b []byte) []byte {
	var out bytes.Buffer
	w, _ := zlib.NewWriterLevel(&out, zlib.BestCompression)
	w.Write(b)
	w.Close()
	return out.Bytes()
}

// Bytes is the PDF file
func (d *Document) Bytes() []byte {
	var out bytes.Buffer
	var offsets []int
	newObj := func() int {
		offsets = append(offsets, 0)
		return len(offsets)
	}
	write := func(n int, body string) {
		offsets[n-1] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", n, body)
	}
	stream := func(n int, dict string, data []byte) {
		offsets[n-1] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n<< %s /Length %d >>\nstream\n", n, dict, len(data))
		out.Write(data)
		out.WriteString("\nendstream\nendobj\n")
	}
	out.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")

	catalog := newObj()
	pagesObj := newObj()
	fontObjs := map[*fontUse]int{}
	for _, f := range d.fonts {
		fontObjs[f] = newObj()
	}
	imageObjs := map[*Image]int{}
	for _, img := range d.images {
		imageObjs[img] = newObj()
	}

	var kids []string
	for _, p := range d.pages {
		pn := newObj()
		kids = append(kids, fmt.Sprintf("%d 0 R", pn))
		var contents []string
		images := map[*Image]bool{}
		for _, c := range p.contents {
			if c.text && c.started {
				c.add("ET")
				c.started = false
			}
			cn := newObj()
			contents = append(contents, fmt.Sprintf("%d 0 R", cn))
			stream(cn, "/Filter /FlateDecode", deflate(c.buf.Bytes()))
			for img := range c.images {
				images[img] = true
			}
		}
		var fonts, xobjects []string
		for _, f := range d.fonts {
			fonts = append(fonts, fmt.Sprintf("/%s %d 0 R", f.name, fontObjs[f]))
		}
		for _, img := range d.images {
			if images[img] {
				xobjects = append(xobjects, fmt.Sprintf("/%s %d 0 R", img.name, imageObjs[img]))
			}
		}
		res := "/ProcSet [/PDF /Text /ImageC] /Font << " + strings.Join(fonts, " ") + " >>"
		if len(xobjects) > 0 {
			res += " /XObject << " + strings.Join(xobjects, " ") + " >>"
		}
		write(pn, fmt.Sprintf("<< /Type /Page /Parent %d 0 R /MediaBox [0 0 %s %s] /Contents [%s] /Resources << %s >> >>",
			pagesObj, num(p.W), num(p.H), strings.Join(contents, " "), res))
	}

	for _, f := range d.fonts {
		d.writeFont(f, fontObjs[f], newObj, write, stream)
	}
	for _, img := range d.images {
		n := imageObjs[img]
		if img.jpeg != nil {
			cs := map[int]string{1: "/DeviceGray", 3: "/DeviceRGB", 4: "/DeviceCMYK"}[img.components]
			stream(n, fmt.Sprintf("/Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace %s /BitsPerComponent 8 /Filter /DCTDecode",
				img.Width, img.Height, cs), img.jpeg)
			continue
		}
		smask := ""
		if img.alpha != nil {
			sn := newObj()
			stream(sn, fmt.Sprintf("/Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceGray /BitsPerComponent 8 /Filter /FlateDecode",
				img.Width, img.Height), deflate(img.alpha))
			smask = fmt.Sprintf(" /SMask %d 0 R", sn)
		}
		stream(n, fmt.Sprintf("/Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /FlateDecode%s",
			img.Width, img.Height, smask), deflate(img.rgb))
	}

	write(pagesObj, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(d.pages)))
	write(catalog, fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pagesObj))
	info := newObj()
	write(info, "<< /Producer (GADS) >>")

	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1)
	for _, o := range offsets {
		fmt.Fprintf(&out, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root %d 0 R /Info %d 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, catalog, info, xref)
	return out.Bytes()
}

// writeFont writes a font as a Type0 font with Identity-H encoding (as
// PDF::Builder's ttfont), the whole TrueType file embedded
func (d *Document) writeFont(f *fontUse, n int, newObj func() int, write func(int, string), stream func(int, string, []byte)) {
	ft := f.font
	cid, desc, file, tounicode := newObj(), newObj(), newObj(), newObj()
	write(n, fmt.Sprintf("<< /Type /Font /Subtype /Type0 /BaseFont /%s /Encoding /Identity-H /DescendantFonts [%d 0 R] /ToUnicode %d 0 R >>",
		ft.Name, cid, tounicode))
	var glyphs []int
	for g := range f.used {
		glyphs = append(glyphs, int(g))
	}
	sort.Ints(glyphs)
	var w strings.Builder
	for _, g := range glyphs {
		width := ft.missing
		if g < len(ft.wx) {
			width = ft.wx[g]
		}
		fmt.Fprintf(&w, "%d [%d] ", g, width)
	}
	write(cid, fmt.Sprintf("<< /Type /Font /Subtype /CIDFontType2 /BaseFont /%s /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /FontDescriptor %d 0 R /DW %d /W [%s] /CIDToGIDMap /Identity >>",
		ft.Name, desc, ft.missing, strings.TrimSpace(w.String())))
	write(desc, fmt.Sprintf("<< /Type /FontDescriptor /FontName /%s /Flags %d /FontBBox [%d %d %d %d] /ItalicAngle %s /Ascent %d /Descent %d /CapHeight %d /StemV %d /FontFile2 %d 0 R >>",
		ft.Name, ft.flags, ft.bbox[0], ft.bbox[1], ft.bbox[2], ft.bbox[3], num(ft.italic), ft.Ascender, ft.Descender, ft.capHeight, ft.stemV, file))
	stream(file, fmt.Sprintf("/Filter /FlateDecode /Length1 %d", len(ft.data)), deflate(ft.data))

	var cmap strings.Builder
	cmap.WriteString("/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def\n/CMapName /Adobe-Identity-UCS def\n/CMapType 2 def\n1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n")
	var entries []string
	for _, g := range glyphs {
		r, ok := ft.g2u[uint16(g)] // #nosec G115 -- glyph IDs are 16-bit
		if !ok {
			continue
		}
		var u strings.Builder
		for _, c := range utf16Units(r) {
			fmt.Fprintf(&u, "%04X", c)
		}
		entries = append(entries, fmt.Sprintf("<%04X> <%s>", g, u.String()))
	}
	for i := 0; i < len(entries); i += 100 {
		end := min(i+100, len(entries))
		fmt.Fprintf(&cmap, "%d beginbfchar\n%s\nendbfchar\n", end-i, strings.Join(entries[i:end], "\n"))
	}
	cmap.WriteString("endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n")
	stream(tounicode, "/Filter /FlateDecode", deflate([]byte(cmap.String())))
}

func utf16Units(r rune) []uint16 {
	if r < 0x10000 {
		return []uint16{uint16(r)} // #nosec G115 -- below 0x10000
	}
	r -= 0x10000
	return []uint16{uint16(0xD800 + (r >> 10)), uint16(0xDC00 + (r & 0x3FF))} // #nosec G115 -- surrogates of a rune
}
