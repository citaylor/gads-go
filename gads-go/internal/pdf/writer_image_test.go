package pdf

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"strings"
	"testing"
)

// An image too large to hold is refused from its header, before its
// pixels are decoded
func TestAddImageTooLarge(t *testing.T) {
	// A PNG header of 100000 x 100000 pixels (the data does not matter)
	var b bytes.Buffer
	b.WriteString("\x89PNG\r\n\x1a\n")
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], 100000)
	binary.BigEndian.PutUint32(ihdr[4:], 100000)
	ihdr[8], ihdr[9] = 8, 6
	binary.Write(&b, binary.BigEndian, uint32(len(ihdr)))
	chunk := append([]byte("IHDR"), ihdr...)
	b.Write(chunk)
	binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	_, err := NewDocument().AddImage(b.Bytes())
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Errorf("error %v", err)
	}
}
