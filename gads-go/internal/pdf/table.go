package pdf

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// PDF::Table 1.007 (table and text_block), for the settings that GADS uses
// through CtrlO::PDF: global settings, header_props, and cell_props with
// colspan. The quirks are kept: a cell is drawn on one line when the widest
// content of its column so far fits it (else wrapped by text_block, with a
// taller row), the cells past the columns of "size" are 2 points wide (and
// their text is not drawn), and the rows are shared between the sizing
// pass (ink 0) and the drawing pass, which sees the default text that the
// first one set.

// Defaults of PDF::Table
const (
	tableLeadingRatio    = 1.25
	tableMinColWidth     = 2
	tableBorderWDefault  = 1
	tableDashedRule      = 2
	tableMaxWordLen      = 20
	tableEmptyCellText   = "-"
	tableFgColorDefault  = "black"
	tableHFgColorDefault = "#000066"
	tableHBgColorDefault = "#FFFFAA"
)

// TableHeader is header_props
type TableHeader struct {
	Font     *Font
	FontSize float64
	Justify  string
	BgColor  string
	FgColor  string
	Repeat   bool
}

// TableOptions are the settings of a table
type TableOptions struct {
	X, W, Y, H   float64
	NextY, NextH float64
	Font         *Font
	FontSize     float64
	Padding      float64
	BorderW      *float64 // border_w (default 1)
	HBorderW     float64  // h_border_w (0: border_w)
	VBorderW     float64  // v_border_w (0: border_w)
	BorderC      string
	BgColorOdd   string
	BgColorEven  string
	Header       *TableHeader
	// CellColspan is the colspan of cell_props, by row and column
	CellColspan [][]int
	Size        string
	Ink         bool
	NewPage     func() *Page
}

// TableError is a die of PDF::Table (a word wider than its cell)
type TableError struct{ Msg string }

func (e *TableError) Error() string { return e.Msg }

// perlSplitWS is Perl's split(/\s+/, $s): a leading empty field is kept,
// trailing ones are not
func perlSplitWS(s string) []string {
	f := regexp.MustCompile(`\s+`).Split(s, -1)
	for len(f) > 0 && f[len(f)-1] == "" {
		f = f[:len(f)-1]
	}
	return f
}

// splitLongWords is s#(\S{20})(?=\S)#$1 #g: a space after each run of 20
// non-space characters followed by another
func splitLongWords(s string) string {
	var out strings.Builder
	runes := []rune(s)
	run := 0
	for i, r := range runes {
		out.WriteRune(r)
		if isSpace(r) {
			run = 0
			continue
		}
		run++
		if run == tableMaxWordLen && i+1 < len(runes) && !isSpace(runes[i+1]) {
			out.WriteByte(' ')
			run = 0
		}
	}
	return out.String()
}

func isSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' || r == '\v'
}

// truthy is Perl truth of a string
func truthy(s *string) bool { return s != nil && *s != "" && *s != "0" }

// setColumnWidths is PDF::Table::ColumnWidth::SetColumnWidths: the widths
// of the columns of "size" (nil for none), and the table width
func setColumnWidths(avail float64, size string, em, ex float64) ([]*float64, float64) {
	specs := strings.Fields(size)
	calc := make([]*float64, len(specs))
	float := make([]float64, len(specs))
	reNumUnit := regexp.MustCompile(`(?i)^([\d.]+)([*a-z]+)$`)
	reNum := regexp.MustCompile(`^([\d.]+)$`)
	reUnit := regexp.MustCompile(`(?i)^([*a-z]+)$`)
	reValid := regexp.MustCompile(`^(\d+|\d+\.|\d+\.\d+|\.\d+)$`)
	for i, s := range specs {
		number, unit := "1", "*"
		if m := reNumUnit.FindStringSubmatch(s); m != nil {
			number, unit = m[1], m[2]
		} else if m := reNum.FindStringSubmatch(s); m != nil {
			number, unit = m[1], "pt"
		} else if m := reUnit.FindStringSubmatch(s); m != nil {
			unit = m[1]
		}
		if !reValid.MatchString(number) {
			number = "1"
		}
		n, _ := strconv.ParseFloat(number, 64)
		w := 0.0
		float[i] = -1
		switch strings.ToLower(unit) {
		case "*":
			w = -1
			float[i] = n
		case "pt":
			w = n
		case "in":
			w = n * 72
		case "cm":
			w = n * 72 / 2.54
		case "mm":
			w = n * 72 / 25.4
		case "em":
			w = n * em
		case "ex":
			w = n * ex
		default:
			w = n * 72 / 25.4
		}
		calc[i] = &w
	}
	used, totalFloat := 0.0, 0.0
	for i := range specs {
		if float[i] > 0 {
			totalFloat += float[i]
		} else {
			used += *calc[i]
		}
	}
	if used > avail {
		avail = used
	}
	if totalFloat > 0 {
		if avail-used < 1 {
			avail += 5 * em * totalFloat
		}
		star := (avail - used) / totalFloat
		for i := range specs {
			if float[i] > 0 {
				w := star * float[i]
				calc[i] = &w
			}
		}
	}
	if totalFloat == 0 && used < avail {
		avail = used
	}
	return calc, avail
}

// Table draws a table (PDF::Table table) on a page. With Ink, it returns
// the last page, the number of pages and the final y; without, the
// heights (vsizes: total, header, 0, then each row)
func Table(page *Page, data [][]*string, o TableOptions) (vsizes []float64, last *Page, pages int, finalY float64, err error) {
	defer func() {
		if r := recover(); r != nil {
			if te, ok := r.(*TableError); ok {
				err = te
				return
			}
			panic(r)
		}
	}()
	ink := o.Ink
	xbase, ybase, width, height := o.X, o.Y, o.W, o.H
	if xbase <= 0 || ybase <= 0 || width <= 0 || height <= 0 {
		return nil, page, 0, ybase, fmt.Errorf("table geometry not defined")
	}
	pgCnt := 1
	curY := ybase
	nextY, nextH := o.NextY, o.NextH
	txt := &Content{page: page, text: true} // for measuring
	if !ink {
		vsizes = []float64{0, 0, 0}
		ybase, height = 2147000000, 2147000000
	} else {
		txt = page.Text()
	}
	doHeaders := 0
	hp := o.Header
	if hp != nil {
		doHeaders = 1
		if hp.Repeat {
			doHeaders = 2
		}
	}
	var headerRow []*string
	if doHeaders > 0 {
		headerRow = data[0]
	}
	fntSize := o.FontSize
	if fntSize == 0 {
		fntSize = 12
	}
	borderW := 1.0
	if o.BorderW != nil {
		borderW = *o.BorderW
	}
	hBorderW := o.HBorderW
	if hBorderW == 0 {
		hBorderW = borderW
	}
	vBorderW := o.VBorderW
	if vBorderW == 0 {
		vBorderW = borderW
	}
	borderC := o.BorderC
	if borderC == "" {
		borderC = tableFgColorDefault
	}
	padding := o.Padding

	colspan := func(row, col int) int {
		if row < len(o.CellColspan) && col < len(o.CellColspan[row]) {
			return o.CellColspan[row][col]
		}
		return 0
	}
	cellFont := func(header bool) (*Font, float64) {
		if header {
			f := hp.Font
			if f == nil {
				f = o.Font
			}
			size := hp.FontSize
			if size == 0 {
				size = fntSize
			}
			return f, size
		}
		return o.Font, fntSize
	}

	// Row heights and column widths
	var rowColWidths [][]float64
	var hRowWidths []float64
	var rowsHeight []float64
	var colMinWidth, colMaxContent []float64
	for ri := range data {
		var columnWidths []float64
		rowsHeight = append(rowsHeight, 0)
		for ci := range data[ri] {
			for len(colMinWidth) <= ci {
				colMinWidth = append(colMinWidth, 0)
				colMaxContent = append(colMaxContent, 0)
			}
			isHeader := ri == 0 && doHeaders > 0
			f, size := cellFont(isHeader)
			leading := size * tableLeadingRatio
			txt.Font(f, size)
			rowsHeight[ri] = math.Max(rowsHeight[ri], math.Max(leading+2*padding, 0))
			if truthy(data[ri][ci]) {
				s := splitLongWords(*data[ri][ci])
				data[ri][ci] = &s
			}
			spaceW := txt.AdvanceWidth(" ")
			maxColW, minColW := 0.0, 0.0
			if truthy(data[ri][ci]) {
				for _, w := range perlSplitWS(*data[ri][ci]) {
					ww := txt.AdvanceWidth(w)
					minColW = math.Max(minColW, ww)
					if maxColW != 0 {
						maxColW += spaceW
					}
					maxColW += ww
				}
			}
			minColW = math.Max(minColW, txt.AdvanceWidth(tableEmptyCellText))
			minColW += 2 * padding
			maxColW += 2 * padding
			maxColW = math.Max(minColW, maxColW)
			colMinWidth[ci] = math.Max(colMinWidth[ci], minColW)
			colMaxContent[ci] = math.Max(colMaxContent[ci], maxColW)
			for len(columnWidths) <= ci {
				columnWidths = append(columnWidths, 0)
			}
			columnWidths[ci] = colMaxContent[ci]
		}
		rowColWidths = append(rowColWidths, columnWidths)
		if ri == 0 && doHeaders > 0 {
			hRowWidths = append([]float64(nil), columnWidths...)
		}
	}
	em, ex := txt.AdvanceWidth("M"), txt.AdvanceWidth("x")
	calcWidths, width := setColumnWidths(width, o.Size, em, ex)
	calcWidth := func(ci int) (float64, bool) {
		if ci < len(calcWidths) && calcWidths[ci] != nil {
			return *calcWidths[ci], true
		}
		return 0, false
	}

	rowIdx := 0
	rowIsOdd := false
	headerMinRH := rowsHeight[0]
	nextTopBorder := 0
	firstRow := true
	var gfx, gfxBg *Content
	var botMargin, tableTopY float64
	data = append([][]*string(nil), data...)
	colspanned := map[string]bool{}
	actualColumnWidths := map[int]map[int]float64{}

	for len(data) > 0 {
		if pgCnt == 1 {
			tableTopY = ybase
			botMargin = tableTopY - height
			if botMargin < 0 {
				botMargin = 0
				height = tableTopY
			}
		} else {
			if o.NewPage != nil {
				page = o.NewPage()
			} else {
				page = page.doc.AddPage(page.W, page.H)
			}
			if nextY == 0 {
				nextY = page.H * 0.9
			}
			if nextH == 0 {
				nextH = page.H * 0.8
			}
			tableTopY = nextY
			botMargin = tableTopY - nextH
			if botMargin < 0 {
				botMargin = 0
				nextH = tableTopY
			}
			if doHeaders == 2 {
				data = append([][]*string{append([]*string(nil), headerRow...)}, data...)
				rowColWidths = append([][]float64{append([]float64(nil), hRowWidths...)}, rowColWidths...)
				rowsHeight = append([]float64{headerMinRH}, rowsHeight...)
				firstRow = true
				rowIdx--
			}
		}
		minHeight := rowsHeight[0]
		if (doHeaders > 0 && pgCnt == 1 || doHeaders == 2 && pgCnt > 1) && len(rowsHeight) > 1 {
			minHeight += rowsHeight[1]
		}
		if minHeight >= tableTopY-botMargin {
			delta := minHeight - (tableTopY - botMargin) + 1
			botMargin -= delta
			if pgCnt == 1 {
				height += delta
			} else {
				nextH += delta
			}
		}
		if ink {
			gfxBg = page.Gfx()
			txt = page.Text()
			gfx = page.Gfx()
			gfx.StrokeColor(borderC)
		}
		curY = tableTopY
		if ink && hBorderW != 0 {
			hline(gfx, nextTopBorder, hBorderW, xbase-vBorderW/2, curY, xbase+width+vBorderW/2)
		}

		for len(data) > 0 && curY-rowsHeight[0] > botMargin {
			dataRow := data[0]
			data = data[1:]
			columnsNumber := len(dataRow)
			currentMinRH := rowsHeight[0]
			rowsHeight = rowsHeight[1:]
			actualRowHeight := currentMinRH
			dataRowWidths := rowColWidths[0]
			rowColWidths = rowColWidths[1:]
			curX := xbase
			var leftovers []*string
			doLeftovers := false
			saveBg := make([]string, columnsNumber)
			saveVRuleW := make([]float64, columnsNumber)
			saveHRuleW := make([]float64, columnsNumber)
			if actualColumnWidths[rowIdx] == nil {
				actualColumnWidths[rowIdx] = map[int]float64{}
			}
			isHeader := firstRow && doHeaders > 0
			for ci := 0; ci < columnsNumber; ci++ {
				if colspanned[fmt.Sprint(rowIdx, "_", ci)] {
					continue
				}
				for len(leftovers) <= ci {
					leftovers = append(leftovers, nil)
				}
				leftovers[ci] = nil
				f, size := cellFont(isHeader)
				leading := size * tableLeadingRatio
				justify := "left"
				var bg, fg string
				if isHeader {
					if hp.Justify != "" {
						justify = hp.Justify
					}
					bg, fg = hp.BgColor, hp.FgColor
					if bg == "" {
						bg = tableHBgColorDefault
					}
					if fg == "" {
						fg = tableHFgColorDefault
					}
				} else if rowIsOdd {
					bg = o.BgColorOdd
				} else {
					bg = o.BgColorEven
				}
				if fg == "" {
					fg = tableFgColorDefault
				}
				textStartY := curY - padding - size
				txt.Font(f, size)
				if ink {
					txt.FillColor(fg)
				}
				if dataRow[ci] == nil {
					d := tableEmptyCellText
					dataRow[ci] = &d
				}
				cpRow := rowIdx
				if isHeader {
					cpRow = 0
				}
				thisCellWidth, _ := calcWidth(ci)
				if cs := colspan(cpRow, ci); cs > 1 {
					for off := 1; off < cs; off++ {
						if w, ok := calcWidth(ci + off); ok && w != 0 {
							thisCellWidth += w
						}
						if isHeader {
							colspanned[fmt.Sprint("0_", ci+off)] = true
						} else {
							colspanned[fmt.Sprint(rowIdx, "_", ci+off)] = true
						}
					}
				}
				thisCellWidth = math.Max(thisCellWidth, tableMinColWidth)
				actualColumnWidths[rowIdx][ci] = thisCellWidth
				content := *dataRow[ci]
				if content == "" {
					content = tableEmptyCellText
				}
				var rowWidth float64
				if ci < len(dataRowWidths) {
					rowWidth = dataRowWidths[ci]
				}
				if !reNewlineInside.MatchString(content) && rowWidth != 0 && rowWidth <= thisCellWidth {
					if ink {
						switch justify {
						case "right":
							txt.TextAt(curX+thisCellWidth-padding, textStartY, content, "right")
						case "center":
							txt.TextAt(curX+padding+(thisCellWidth-2*padding)/2, textStartY, content, "center")
						default:
							txt.TextAt(curX+padding, textStartY, content, "left")
						}
					}
				} else {
					ypos, left := textBlock(txt, ink, content, rowIdx, ci, curX+padding, textStartY,
						thisCellWidth-2*padding, curY-botMargin-2*padding, size, leading, justify)
					actualRowHeight = math.Max(actualRowHeight, curY-ypos+padding+(leading-size)*2.5)
					if left != "" && left != "0" {
						leftovers[ci] = &left
						doLeftovers = true
					}
				}
				curX += thisCellWidth
				saveBg[ci] = bg
				saveVRuleW[ci] = vBorderW
				saveHRuleW[ci] = hBorderW
			}
			if doLeftovers {
				data = append([][]*string{leftovers}, data...)
				rowColWidths = append([][]float64{dataRowWidths}, rowColWidths...)
				rowsHeight = append([]float64{currentMinRH}, rowsHeight...)
			}
			if !isHeader {
				rowIsOdd = !rowIsOdd
			}
			curX = xbase
			for ci := 0; ci < len(dataRow); ci++ {
				if ink {
					key := fmt.Sprint(rowIdx, "_", ci)
					aw := actualColumnWidths[rowIdx][ci]
					if bg := saveBg[ci]; bg != "" && bg != "transparent" && bg != "trans" && !colspanned[key] {
						gfxBg.Rect(curX, curY-actualRowHeight, aw, actualRowHeight)
						gfxBg.FillColor(bg)
						gfxBg.Fill()
					}
					if w := saveVRuleW[ci]; w != 0 && ci != 0 && !colspanned[key] {
						gfx.LineWidth(w)
						gfx.StrokeColor(borderC)
						gfx.Move(curX, curY-actualRowHeight)
						top := curY
						if rowIdx == 0 {
							top -= hBorderW / 2
						}
						gfx.Line(curX, top)
						gfx.Stroke()
					}
					if w := saveHRuleW[ci]; w != 0 && len(data) > 0 && curY-actualRowHeight-currentMinRH > botMargin {
						gfx.LineWidth(w)
						gfx.StrokeColor(borderC)
						gfx.Move(curX, curY-actualRowHeight)
						gfx.HLine(curX+aw, curY-actualRowHeight)
						gfx.Stroke()
					}
				}
				w, _ := calcWidth(ci)
				curX += w
			}
			curY -= actualRowHeight
			if !ink {
				if firstRow && doHeaders > 0 {
					vsizes[1] = actualRowHeight
				} else {
					vsizes = append(vsizes, actualRowHeight)
				}
			}
			if doLeftovers {
				rowIsOdd = !rowIsOdd
				nextTopBorder = 2
			} else {
				rowIdx++
				nextTopBorder = 1
			}
			firstRow = false
		}
		if len(data) == 0 {
			nextTopBorder = 0
		}
		if ink {
			if hBorderW != 0 {
				gfx.StrokeColor(borderC)
				hline(gfx, nextTopBorder, hBorderW, xbase-vBorderW/2, curY, xbase+width+vBorderW/2)
			}
			if vBorderW != 0 {
				gfx.LineWidth(vBorderW)
				gfx.Move(xbase, tableTopY)
				gfx.Line(xbase, curY)
				gfx.Move(xbase+width, tableTopY)
				gfx.Line(xbase+width, curY)
			}
			gfx.Stroke()
		}
		pgCnt++
	}
	if ink {
		return nil, page, pgCnt - 1, curY, nil
	}
	for i := 1; i < len(vsizes); i++ {
		vsizes[0] += vsizes[i]
	}
	return vsizes, page, 0, curY, nil
}

var reNewlineInside = regexp.MustCompile(`(?s)[^\n]\n[^\n]`)

// hline draws the top or bottom border: solid, thinner after a row, or
// dashed where a row continues on the next page
func hline(gfx *Content, kind int, w, x1, y, x2 float64) {
	switch kind {
	case 0:
		gfx.LineWidth(w)
	case 1:
		gfx.LineWidth(tableBorderWDefault)
	default:
		gfx.LineWidth(tableBorderWDefault)
		gfx.LineDash(tableDashedRule)
	}
	gfx.Move(x1, y)
	gfx.HLine(x2, y)
	gfx.Stroke()
	gfx.LineDash()
}

// textBlock is PDF::Table text_block: text wrapped in a block, returning
// the baseline of the last line and the text left over
func textBlock(txt *Content, ink bool, text string, row, col int, xbase, ybase, width, height, fontSize, leading float64, align string) (float64, string) {
	if xbase <= 0 || ybase <= 0 || width <= 0 || height <= 0 {
		// "Error: ... of Block is NOT defined!": nothing is drawn
		if ybase <= 0 {
			ybase = -1
		}
		return ybase, ""
	}
	if text == "" {
		text = " "
	}
	text = strings.ReplaceAll(text, "\r", "")
	paragraphs := strings.Split(text, "\n")
	for len(paragraphs) > 0 && paragraphs[len(paragraphs)-1] == "" {
		paragraphs = paragraphs[:len(paragraphs)-1]
	}
	lineSpace := leading
	if lineSpace <= 0 {
		lineSpace = fontSize * tableLeadingRatio
	}
	if fontSize > lineSpace {
		lineSpace = fontSize * tableLeadingRatio
	}
	shift := func() []string {
		if len(paragraphs) == 0 {
			return nil
		}
		p := paragraphs[0]
		paragraphs = paragraphs[1:]
		return strings.Fields(p)
	}
	paragraph := shift()
	ypos := ybase + lineSpace
	bottomBorder := ypos - height
	for ypos >= bottomBorder+lineSpace {
		if len(paragraph) == 0 {
			if len(paragraphs) == 0 {
				break
			}
			paragraph = shift()
			if ypos < bottomBorder {
				break
			}
		}
		ypos -= lineSpace
		var line []string
		for len(paragraph) > 0 {
			if len(line) == 0 {
				if txt.AdvanceWidth(paragraph[0]) <= width+0.01 {
					line = append(line, paragraph[0])
					paragraph = paragraph[1:]
					continue
				}
				panic(&TableError{Msg: fmt.Sprintf("!!! Error !!! first word in paragraph for row %d, col %d '%s' doesn't fit into empty line!", row, col, paragraph[0])})
			}
			if txt.AdvanceWidth(strings.Join(line, " ")+" "+paragraph[0]) <= width {
				line = append(line, paragraph[0])
				paragraph = paragraph[1:]
				continue
			}
			break
		}
		if ink {
			s := strings.Join(line, " ")
			switch align {
			case "right":
				txt.TextAt(xbase+width, ypos, s, "right")
			case "center":
				txt.TextAt(xbase+width/2, ypos, s, "center")
			default:
				txt.TextAt(xbase, ypos, s, "left")
			}
		}
	}
	if len(paragraph) > 0 {
		paragraphs = append([]string{strings.Join(paragraph, " ")}, paragraphs...)
	}
	return ypos, strings.Join(paragraphs, "\n")
}
