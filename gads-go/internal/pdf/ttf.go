package pdf

import (
	"embed"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"unicode"
)

// The fonts of CtrlO::PDF: Liberation Sans (SIL Open Font License 1.1,
// fonts/LICENSE), the files that it loads from
// /usr/share/fonts/truetype/liberation. They are built in, so that a PDF
// does not depend on the fonts installed.
//
//go:embed fonts/LiberationSans-Regular.ttf fonts/LiberationSans-Bold.ttf
var fontFiles embed.FS

// Font is a TrueType font, measured as PDF::Builder's ttfont
// (PDF::Builder::Resource::CIDFont::TrueType::FontFile): the widths, the
// ascender and descender are in thousandths of the font size, truncated to
// integers as Perl's int
type Font struct {
	Name      string // the PostScript name
	data      []byte
	upem      int
	wx        []int // width of each glyph
	missing   int   // missingwidth
	u2g       map[rune]uint16
	g2u       map[uint16]rune
	Ascender  int
	Descender int
	bbox      [4]int
	capHeight int
	italic    float64
	flags     int
	stemV     int
}

var fontCache = map[string]*Font{}

// LoadFont returns a built in font: "regular" or "bold"
func LoadFont(style string) (*Font, error) {
	if f := fontCache[style]; f != nil {
		return f, nil
	}
	file := map[string]string{"regular": "LiberationSans-Regular.ttf", "bold": "LiberationSans-Bold.ttf"}[style]
	if file == "" {
		return nil, fmt.Errorf("unknown font style %s", style)
	}
	data, err := fontFiles.ReadFile("fonts/" + file)
	if err != nil {
		return nil, err
	}
	f, err := ParseFont(data)
	if err != nil {
		return nil, err
	}
	fontCache[style] = f
	return f, nil
}

// perlInt is Perl's int: truncation towards zero
func perlInt(v float64) int { return int(math.Trunc(v)) }

// ParseFont reads the tables of a TrueType font
func ParseFont(data []byte) (*Font, error) {
	if len(data) < 12 {
		return nil, errors.New("font too short")
	}
	tables := map[string][]byte{}
	n := int(binary.BigEndian.Uint16(data[4:]))
	for i := 0; i < n; i++ {
		rec := 12 + 16*i
		if rec+16 > len(data) {
			return nil, errors.New("bad font table directory")
		}
		tag := string(data[rec : rec+4])
		off := int(binary.BigEndian.Uint32(data[rec+8:]))
		ln := int(binary.BigEndian.Uint32(data[rec+12:]))
		if off+ln > len(data) {
			return nil, fmt.Errorf("bad font table %s", tag)
		}
		tables[tag] = data[off : off+ln]
	}
	for _, t := range []string{"head", "hhea", "hmtx", "cmap", "maxp"} {
		if tables[t] == nil {
			return nil, fmt.Errorf("font has no %s table", t)
		}
	}
	u16 := func(b []byte, o int) int { return int(binary.BigEndian.Uint16(b[o:])) }
	i16 := func(b []byte, o int) int { return int(int16(binary.BigEndian.Uint16(b[o:]))) } // #nosec G115 -- a signed 16-bit field

	f := &Font{data: data, u2g: map[rune]uint16{}, g2u: map[uint16]rune{}}
	head, hhea := tables["head"], tables["hhea"]
	f.upem = u16(head, 18)
	scale := func(v int) int { return perlInt(float64(v) * 1000 / float64(f.upem)) }
	f.bbox = [4]int{scale(i16(head, 36)), scale(i16(head, 38)), scale(i16(head, 40)), scale(i16(head, 42))}
	f.Ascender = scale(i16(hhea, 4))
	f.Descender = scale(i16(hhea, 6))
	f.missing = scale(u16(hhea, 10))
	if f.missing == 0 {
		f.missing = 1000
	}
	numHM := u16(hhea, 34)
	numGlyphs := u16(tables["maxp"], 4)

	// hmtx: the glyphs after the last metric have its advance
	hmtx := tables["hmtx"]
	f.wx = make([]int, numGlyphs)
	last := 0
	for g := 0; g < numGlyphs; g++ {
		if g < numHM && 4*g+2 <= len(hmtx) {
			last = u16(hmtx, 4*g)
		}
		if last != 0 {
			f.wx[g] = scale(last)
		} else {
			f.wx[g] = f.missing
		}
	}

	if err := f.readCmap(tables["cmap"]); err != nil {
		return nil, err
	}

	f.capHeight = perlInt(float64(f.bbox[3]) * 0.8)
	if os2 := tables["OS/2"]; len(os2) >= 90 && u16(os2, 0) >= 2 {
		f.capHeight = i16(os2, 88)
	}
	weight := 400
	if os2 := tables["OS/2"]; len(os2) >= 6 {
		weight = u16(os2, 4)
	}
	f.stemV = 50 + perlInt(float64(weight*weight)/65)/100
	if post := tables["post"]; len(post) >= 16 {
		f.italic = float64(int32(binary.BigEndian.Uint32(post[4:]))) / 65536 // #nosec G115 -- a signed 16.16 fixed field
		if binary.BigEndian.Uint32(post[12:]) != 0 {
			f.flags |= 1
		}
	}
	f.flags |= 32 // nonsymbolic
	if f.italic != 0 {
		f.flags |= 64
	}
	f.Name = "LiberationSans"
	if name := tables["name"]; name != nil {
		if ps := postScriptName(name); ps != "" {
			f.Name = ps
		}
	}
	return f, nil
}

// readCmap reads the Unicode subtables (format 4, and format 12 when there
// is one)
func (f *Font) readCmap(cmap []byte) error {
	u16 := func(o int) int { return int(binary.BigEndian.Uint16(cmap[o:])) }
	u32 := func(o int) int { return int(binary.BigEndian.Uint32(cmap[o:])) }
	var off4, off12 int
	for i := 0; i < u16(2); i++ {
		rec := 4 + 8*i
		pid, eid, off := u16(rec), u16(rec+2), u32(rec+4)
		if off+2 > len(cmap) {
			continue
		}
		switch format := u16(off); {
		case format == 4 && (pid == 3 && eid == 1 || pid == 0):
			off4 = off
		case format == 12 && (pid == 3 && eid == 10 || pid == 0):
			off12 = off
		}
	}
	add := func(r rune, g int) {
		if g <= 0 || g > 0xFFFF {
			return
		}
		gid := uint16(g) // #nosec G115 -- checked above
		if _, ok := f.u2g[r]; !ok {
			f.u2g[r] = gid
		}
		if _, ok := f.g2u[gid]; !ok {
			f.g2u[gid] = r
		}
	}
	switch {
	case off12 != 0:
		n := u32(off12 + 12)
		for i := 0; i < n; i++ {
			rec := off12 + 16 + 12*i
			start, end, g := u32(rec), u32(rec+4), u32(rec+8)
			for c := start; c <= end && c <= unicode.MaxRune; c++ {
				add(rune(c), g+c-start) // #nosec G115 -- at most MaxRune
			}
		}
	case off4 != 0:
		segs := u16(off4+6) / 2
		ends, starts, deltas, ranges := off4+14, off4+16+2*segs, off4+16+4*segs, off4+16+6*segs
		for s := 0; s < segs; s++ {
			end, start := u16(ends+2*s), u16(starts+2*s)
			delta, rangeOff := u16(deltas+2*s), u16(ranges+2*s)
			for c := start; c <= end && c != 0xFFFF; c++ {
				g := 0
				if rangeOff == 0 {
					g = (c + delta) & 0xFFFF
				} else {
					at := ranges + 2*s + rangeOff + 2*(c-start)
					if at+2 > len(cmap) {
						continue
					}
					if g = u16(at); g != 0 {
						g = (g + delta) & 0xFFFF
					}
				}
				add(rune(c), g) // #nosec G115 -- a 16-bit code
			}
		}
	default:
		return errors.New("font has no Unicode cmap")
	}
	return nil
}

// postScriptName is name ID 6 of the name table
func postScriptName(name []byte) string {
	if len(name) < 6 {
		return ""
	}
	count := int(binary.BigEndian.Uint16(name[2:]))
	strs := int(binary.BigEndian.Uint16(name[4:]))
	for i := 0; i < count; i++ {
		rec := 6 + 12*i
		if rec+12 > len(name) {
			break
		}
		pid := binary.BigEndian.Uint16(name[rec:])
		id := binary.BigEndian.Uint16(name[rec+6:])
		ln := int(binary.BigEndian.Uint16(name[rec+8:]))
		off := strs + int(binary.BigEndian.Uint16(name[rec+10:]))
		if id != 6 || off+ln > len(name) {
			continue
		}
		b := name[off : off+ln]
		if pid == 3 || pid == 0 { // UTF-16BE
			var s []rune
			for j := 0; j+1 < len(b); j += 2 {
				s = append(s, rune(binary.BigEndian.Uint16(b[j:])))
			}
			return string(s)
		}
		return string(b)
	}
	return ""
}

// Glyph is the glyph of a character (0, .notdef, when the font has none)
func (f *Font) Glyph(r rune) uint16 { return f.u2g[r] }

// Width is the width of a string at a font size (PDF::Builder advancewidth
// without word or character spacing): the sum of the integer glyph widths
func (f *Font) Width(s string, size float64) float64 {
	w := 0
	for _, r := range s {
		g := int(f.Glyph(r))
		if g < len(f.wx) {
			w += f.wx[g]
		} else {
			w += f.missing
		}
	}
	return float64(w) / 1000 * size
}
