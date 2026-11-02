package pdf

import (
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

// CtrlO is CtrlO::PDF 0.36 (on PDF::Builder 3.028): an A4 portrait
// document with a margin of 40, Liberation Sans 10, and a header, footer
// and logo on every page
type CtrlO struct {
	Doc         *Document
	Header      string
	Footer      string
	logo        *Image
	LogoScaling float64
	LogoPadding float64
	Width       float64
	Height      float64
	Margin      float64
	TopPadding  float64
	FontSize    float64
	Font        *Font
	FontBold    *Font

	page      *Page
	y         float64
	yBuilt    bool
	isNewPage bool
	PageCount int
}

// NewCtrlO creates a document (CtrlO::PDF new), with a logo (PNG, JPEG
// or GIF) when given
func NewCtrlO(header, footer string, logo []byte) (*CtrlO, error) {
	regular, err := LoadFont("regular")
	if err != nil {
		return nil, err
	}
	bold, err := LoadFont("bold")
	if err != nil {
		return nil, err
	}
	p := &CtrlO{
		Doc: NewDocument(), Header: header, Footer: footer,
		LogoScaling: 0.5, LogoPadding: 10, Width: 595, Height: 842, Margin: 40, FontSize: 10,
		Font: regular, FontBold: bold, isNewPage: true,
	}
	if len(logo) > 0 {
		if p.logo, err = p.Doc.AddImage(logo); err != nil {
			return nil, fmt.Errorf("Unable to identify image type: %v", err)
		}
	}
	return p, nil
}

// HasLogo is whether the document has a logo
func (p *CtrlO) HasLogo() bool { return p.logo != nil }

func (p *CtrlO) logoHeight() float64 {
	if p.logo == nil {
		return 0
	}
	return float64(p.logo.Height) * p.LogoScaling
}

func (p *CtrlO) logoWidth() float64 {
	if p.logo == nil {
		return 0
	}
	return float64(p.logo.Width) * p.LogoScaling
}

func lineHeight(size, ratio float64) float64 {
	if ratio == 0 {
		ratio = 1.5
	}
	return size * ratio
}

// MarginTop is the margin, the top padding, and room for the header and
// the logo, as an integer
func (p *CtrlO) MarginTop() float64 {
	size := p.Margin + p.TopPadding
	if perlTrue(p.Header) {
		size += lineHeight(p.FontSize, 0)
	}
	if p.logo != nil {
		size += p.logoHeight() + p.LogoPadding
	}
	return float64(perlInt(size))
}

// MarginBottom is the margin and room for the footer
func (p *CtrlO) MarginBottom() float64 { return p.Margin + 2*lineHeight(p.FontSize, 0) }

func (p *CtrlO) widthPrint() float64 { return p.Width - p.Margin*2 }

func (p *CtrlO) yStartDefault() float64 { return p.Height - p.MarginTop() }

// Y is the current position (y_position)
func (p *CtrlO) Y() float64 {
	if !p.yBuilt {
		p.y = p.yStartDefault()
		p.yBuilt = true
	}
	return p.y
}

func (p *CtrlO) setY(y float64) { p.y = y; p.yBuilt = true }

func (p *CtrlO) down(points float64) { p.setY(p.Y() - points) }

// Page is the current page, added if there is none
func (p *CtrlO) Page() *Page {
	if p.page == nil {
		p.AddPage()
	}
	return p.page
}

// AddPage adds a page (add_page)
func (p *CtrlO) AddPage() *Page {
	p.page = p.Doc.AddPage(p.Width, p.Height)
	p.setY(p.yStartDefault())
	p.isNewPage = true
	p.PageCount++
	return p.page
}

// HeadingOptions are the options of a heading
type HeadingOptions struct {
	Size      float64
	TopMargin float64
}

// Heading adds a heading (heading)
func (p *CtrlO) Heading(s string, o HeadingOptions) {
	page := p.Page()
	if p.Y() < 150 {
		page = p.AddPage()
	}
	size := o.Size
	if size == 0 {
		size = 16
	}
	switch {
	case o.TopMargin != 0:
		p.down(o.TopMargin)
	case p.isNewPage:
		p.down(size)
		p.isNewPage = false
	default:
		p.down(lineHeight(size, 1.8))
	}
	text := page.Text()
	page.Gfx()
	text.Font(p.FontBold, size)
	text.Translate(p.Margin, p.Y())
	text.Show(s)
	p.down(lineHeight(size, 0.4))
}

// perlTrue is Perl truth of a string
func perlTrue(s string) bool { return s != "" && s != "0" }

// Text adds paragraph text (text, with the "none" format of PDF::Builder's
// column): paragraphs are separated by blank lines, and wrapped to the
// width of the page. Text that cannot be placed (a word wider than the
// page, after other words) is an error, as the croak of CtrlO::PDF.
func (p *CtrlO) Text(s string, size float64) error {
	if !perlTrue(s) {
		return nil
	}
	if size == 0 {
		size = p.FontSize
	}
	page := p.Page()
	if p.Y()-lineHeight(size, 0) < p.MarginBottom() {
		page = p.AddPage()
	}
	text := page.Text()
	page.Gfx()
	// Perl quirk: the height of the column is taken before moving down by
	// the top padding, so on this page it ends below the bottom margin
	height := p.Y() - p.MarginBottom()
	text.Font(p.Font, 10)
	topPadding := lineHeight(size, 2) - size
	if p.isNewPage {
		p.isNewPage = false
	} else {
		p.down(topPadding)
	}
	col := newColumn(p.Font, size, topPadding, noneParagraphs(s))
	nextY, more := col.output(text, p.Margin, p.Y(), p.widthPrint(), p.Y()-height)
	lastUnused := ""
	for more {
		page = p.AddPage()
		text = page.Text()
		page.Gfx()
		nextY, more = col.output(text, p.Margin, p.Y(), p.widthPrint(), p.MarginBottom())
		p.isNewPage = false
		unused := col.unused()
		if unused == "" {
			break
		}
		if unused == lastUnused {
			return fmt.Errorf("Unable to print text to PDF: %s", unused)
		}
		lastUnused = unused
	}
	p.setY(nextY)
	return nil
}

// noneParagraphs is _none_hash: lines with content are joined with a
// space, blank lines end a paragraph
func noneParagraphs(s string) []string {
	s = strings.TrimSuffix(s, "\n")
	var paras []string
	inPara := false
	var line string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) == "" && strings.Trim(l, " \t\r\f\v") == "" {
			if inPara {
				paras = append(paras, line)
				inPara = false
				line = ""
			}
			continue
		}
		if inPara {
			line += " " + l
		} else {
			inPara = true
			line = l
		}
	}
	if inPara {
		paras = append(paras, line)
	}
	return paras
}

// column is the text flow of PDF::Builder's column for plain paragraphs,
// as _output_text: the first baseline is the font's ascender below the
// top, a line is the ascender less the descender and 0.125 leading, and a
// following paragraph has the paragraph spacing above it. Each line takes
// the whole phrase that is left if it fits, else the longest start of it
// that ends with a word. A word that is too wide is split (Hyphenate_basic)
// only when it is all that is left of the paragraph; otherwise the line is
// left empty, line after line, to the bottom of the column.
type column struct {
	font      *Font
	size      float64
	paraSpace float64
	paras     []string
	started   bool // text has been output (paragraph margins apply)
	cont      bool // the first paragraph continues from the last column
}

func newColumn(f *Font, size, paraSpace float64, paras []string) *column {
	return &column{font: f, size: size, paraSpace: paraSpace, paras: paras}
}

// unused is the text left, as CtrlO::PDF joins it in its error
func (c *column) unused() string {
	var parts []string
	for _, p := range c.paras {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " ")
}

// output writes as much as fits between top and minY, returning the next
// y and whether text remains
func (c *column) output(text *Content, x, top, width, minY float64) (float64, bool) {
	asc := float64(c.font.Ascender) / 1000 * c.size
	desc := float64(c.font.Descender) / 1000 * c.size
	descLeading := desc - (1.125-1.0)*c.size
	startY := top
	nextY := top
	topCol := true
	text.Font(c.font, c.size)
	for len(c.paras) > 0 {
		phrase := c.paras[0]
		if !topCol {
			// below the last line
			startY = nextY
		}
		var addY float64
		if !topCol && c.started && !c.cont {
			// margin above a following paragraph
			addY = c.paraSpace
		}
		c.cont = false
		remainder := ""
		needLine := true
		var y, cx, endx float64
		for phrase != "" {
			fullLine := false
			if needLine {
				if startY-addY-asc+desc < minY {
					// continued in the next column
					c.paras[0] = phrase + remainder
					c.cont = true
					return nextY, true
				}
				y = startY - addY - asc
				nextY = y + descLeading
				cx, endx = x, x+width
				addY = 0
				needLine = false
				fullLine = true
			}
			runes := []rune(phrase)
			cum := make([]int, len(runes)+1)
			for i, r := range runes {
				g := int(c.font.Glyph(r))
				w := c.font.missing
				if g < len(c.font.wx) {
					w = c.font.wx[g]
				}
				cum[i+1] = cum[i] + w
			}
			fits := func(k int) bool { return cx+float64(cum[k])/1000*c.size <= endx }
			// The longest candidate that fits: the phrase, then without
			// its trailing space, without its last word, and so on
			k := len(runes)
			split := false
			for k > 0 && !fits(k) {
				if isSpace(runes[k-1]) {
					for k > 0 && isSpace(runes[k-1]) {
						k--
					}
				} else {
					if fullLine && k == len(runes) && !strings.Contains(strings.TrimSpace(string(runes[:k])), " ") {
						split = true
						break
					}
					for k > 0 && !isSpace(runes[k-1]) {
						k--
					}
				}
				fullLine = false
			}
			if split {
				left, right := c.splitWord(strings.TrimSpace(phrase), endx-cx)
				if left == "" {
					// Nothing fits: the rest of the paragraph is lost
					break
				}
				remainder = right + " " + remainder
				text.Translate(cx, y)
				text.Show(left)
				c.started, topCol = true, false
				phrase = strings.TrimLeft(remainder, " \t\n\r\f\v")
				remainder = ""
				if phrase != "" {
					needLine = true
					startY = nextY
				}
				continue
			}
			if k == 0 {
				// Nothing fits on this line: try the next
				needLine = true
				startY = nextY
				phrase = phrase + remainder
				remainder = ""
				continue
			}
			fit := string(runes[:k])
			text.Translate(cx, y)
			text.Show(fit)
			c.started, topCol = true, false
			cx += float64(cum[k]) / 1000 * c.size
			phrase = strings.TrimLeft(string(runes[k:])+remainder, " \t\n\r\f\v")
			remainder = ""
			if phrase != "" {
				needLine = true
				startY = nextY
			}
		}
		c.paras = c.paras[1:]
	}
	return nextY, false
}

// splitWord splits a word that is too wide, as _output_text tries
// Hyphenate_basic: at a hyphenation point, then at a non-breaking space,
// then anywhere
func (c *column) splitWord(word string, width float64) (string, string) {
	left, right := splitWord(c.font, c.size, word, width, splitNormal)
	if left == "" {
		left, right = splitWord(c.font, c.size, word, width, splitNBSP)
		if left == "" {
			left, right = splitWord(c.font, c.size, word, width, splitAnywhere)
		}
	}
	return left, right
}

const (
	splitNormal = iota
	splitNBSP
	splitAnywhere
)

// splitWord is PDF::Builder::Content::Hyphenate_basic splitWord: the
// rightmost split of a word that fits, with a hyphen
func splitWord(f *Font, size float64, word string, width float64, mode int) (string, string) {
	minBegin, minEnd := 2, 3
	var loc []int
	var chars []rune
	i := 0
	for _, r := range word {
		if r == 0xAD {
			loc = append(loc, i-1)
			continue
		}
		chars = append(chars, r)
		i++
	}
	word = string(chars)
	isLetter := func(r rune) bool { return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' }
	isDigit := func(r rune) bool { return r >= '0' && r <= '9' }
	if mode == splitNormal {
		for i, r := range chars {
			if r == '-' {
				loc = append(loc, i)
			}
		}
		for i := 0; i < len(chars)-1; i++ {
			if strings.ContainsRune("!.?,%&:;<>)]}_~^+*/", chars[i]) || chars[i] == 8211 || chars[i] == 8212 {
				if chars[i+1] != '-' {
					loc = append(loc, i)
				}
			}
		}
		for i := 0; i < len(chars)-1; i++ {
			if isDigit(chars[i]) && !(isDigit(chars[i+1]) || chars[i+1] == '-') {
				loc = append(loc, i)
			}
		}
		for i := 0; i < len(chars)-1; i++ {
			if isLetter(chars[i]) && !(isLetter(chars[i+1]) || chars[i+1] == '-') {
				loc = append(loc, i)
			}
		}
		for i := 0; i < len(chars)-1; i++ {
			if chars[i] >= 'a' && chars[i] <= 'z' && chars[i+1] >= 'A' && chars[i+1] <= 'Z' {
				loc = append(loc, i)
			}
		}
	}
	if len(loc) == 0 && mode == splitNBSP {
		for i, r := range chars {
			if r == 0xA0 {
				loc = append(loc, i)
			}
		}
	}
	if len(loc) == 0 && mode == splitAnywhere {
		trial := ""
		i := 0
		for i = 0; i < len(chars); i++ {
			trial += string(chars[i])
			if f.Width(trial+"-", size) > width {
				break
			}
		}
		if i == 0 {
			i = 1
		}
		loc = append(loc, i-1)
		minBegin, minEnd = 1, 1
	}
	if len(loc) == 0 {
		return "", word
	}
	sortInts(loc)
	n := utf8.RuneCountInString(word)
	j := -1
	for i := range loc {
		if loc[i] >= minBegin-1 {
			break
		}
		j = i
	}
	loc = loc[j+1:]
	j = -1
	for i := len(loc) - 1; i >= 0; i-- {
		if loc[i] < n-minEnd {
			break
		}
		j = i
	}
	if j >= 0 {
		loc = loc[:j]
	}
	for len(loc) > 0 {
		at := loc[len(loc)-1]
		loc = loc[:len(loc)-1]
		trial := string(chars[:min(at+1, len(chars))])
		h := "-"
		if last := chars[min(at, len(chars)-1)]; last == 45 || last == 8211 || last == 8212 || last == 47 {
			h = ""
		}
		if f.Width(trial+h, size) > width {
			continue
		}
		return trial + h, string(chars[min(at+1, len(chars)):])
	}
	return "", word
}

func sortInts(a []int) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}

// TableData is the data of a table: rows of cells, nil for undef
type TableData [][]*string

// Table adds a table (table): it starts on a new page when its header and
// first row do not fit
func (p *CtrlO) Table(data TableData, o TableOptions) error {
	p.Page()
	if p.isNewPage {
		p.down(lineHeight(p.FontSize, 0))
	}
	o.X = p.Margin
	o.W = p.widthPrint()
	o.Y = p.Y()
	o.H = p.Y() - p.MarginBottom()
	o.NextY = p.yStartDefault()
	o.NextH = p.yStartDefault() - p.MarginBottom()
	if o.FontSize == 0 {
		o.FontSize = p.FontSize
	}
	if o.Padding == 0 {
		o.Padding = 5
	}
	if o.Font == nil {
		o.Font = p.Font
	}
	o.NewPage = p.AddPage
	o.Ink = false
	vsizes, _, _, _, err := Table(p.page, append(TableData(nil), data...), o)
	if err != nil {
		return err
	}
	first := vsizes[1]
	if len(vsizes) > 3 {
		first += vsizes[3]
	}
	if p.Y()-first < p.MarginBottom() {
		p.AddPage()
		o.Y = p.Y()
		o.H = p.Y() - p.MarginBottom()
	}
	o.Ink = true
	_, _, _, finalY, err := Table(p.page, append(TableData(nil), data...), o)
	if err != nil {
		return err
	}
	p.setY(finalY)
	p.down(lineHeight(p.FontSize, 0))
	p.isNewPage = false
	return nil
}

// Content is the PDF (content): the logo, header, footer and page number
// of each page are added
func (p *CtrlO) Content() []byte {
	count := p.Doc.Pages()
	for n := 1; n <= count; n++ {
		page := p.Doc.OpenPage(n)
		if p.logo != nil {
			gfx := page.Gfx()
			gfx.Image(p.logo, p.Width-p.Margin-p.logoWidth(), p.Height-p.Margin-p.logoHeight(), p.LogoScaling)
		}
		text := page.Text()
		text.Font(p.Font, 10)
		text.FillColor("black")
		if perlTrue(p.Header) {
			text.TextAt(math.Trunc(p.Width/2), p.Height-p.Margin, p.Header, "center")
		}
		text.TextAt(p.Width-p.Margin, p.Margin, fmt.Sprintf("Page %d of %d", n, count), "right")
		if perlTrue(p.Footer) {
			text.TextAt(p.Margin, p.Margin, p.Footer, "left")
		}
	}
	return p.Doc.Bytes()
}
