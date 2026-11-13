package appmon

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode/utf16"
)

// parsePlistKeys returns the top-level scalar string values of a plist. It
// understands both XML plists and the binary "bplist00" format without shelling
// out to plutil (a fork per bundle costs ~11 ms per app on every scan) and
// without new dependencies. Nested containers are skipped; only keys that
// resolve to a scalar are kept.
func parsePlistKeys(data []byte) (map[string]string, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, errors.New("empty plist")
	}
	if bytes.HasPrefix(data, []byte("bplist00")) {
		return parseBinaryPlist(data)
	}
	if bytes.HasPrefix(data, []byte("<?xml")) || bytes.HasPrefix(data, []byte("<plist")) || bytes.HasPrefix(data, []byte("<!DOCTYPE")) {
		return parseXMLPlist(data)
	}
	return nil, errors.New("unknown plist format")
}

// parseXMLPlist decodes a top-level <dict> of scalar values.
func parseXMLPlist(data []byte) (map[string]string, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false

	out := map[string]string{}
	var pendingKey string
	depth := 0
	for {
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			name := t.Name.Local
			if name == "key" {
				var key string
				if err := dec.DecodeElement(&key, &t); err != nil {
					return nil, err
				}
				depth--
				pendingKey = strings.TrimSpace(key)
				continue
			}
			// Value element of the top-level dict: plist > dict > value.
			if depth == 3 && pendingKey != "" {
				if val, ok := decodeXMLScalar(dec, t); ok {
					out[pendingKey] = val
				}
				depth--
				pendingKey = ""
			}
		case xml.EndElement:
			depth--
			if depth < 0 {
				depth = 0
			}
			if t.Name.Local == "key" {
				pendingKey = ""
			}
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no scalar keys in plist")
	}
	return out, nil
}

// decodeXMLScalar consumes a value element, reporting whether it was a scalar.
func decodeXMLScalar(dec *xml.Decoder, start xml.StartElement) (string, bool) {
	switch start.Name.Local {
	case "string", "integer", "real", "date", "data":
		var body string
		if err := dec.DecodeElement(&body, &start); err != nil {
			return "", false
		}
		return strings.TrimSpace(body), true
	case "true", "false":
		var body string
		_ = dec.DecodeElement(&body, &start)
		if start.Name.Local == "true" {
			return "1", true
		}
		return "0", true
	default:
		var raw string
		_ = dec.DecodeElement(&raw, &start)
		return "", false
	}
}

// Binary plist object type markers (high nibble of the marker byte).
const (
	bplistTypeNull  = 0x00
	bplistTypeInt   = 0x10
	bplistTypeReal  = 0x20
	bplistTypeDate  = 0x30
	bplistTypeData  = 0x40
	bplistTypeASCII = 0x50
	bplistTypeUTF16 = 0x60
	bplistTypeUID   = 0x80
	bplistTypeArray = 0xA0
	bplistTypeSet   = 0xC0
	bplistTypeDict  = 0xD0

	bplistTrailerSize = 32
	bplistMaxDepth    = 32
	bplistMaxObjects  = 1 << 20
)

type bplistParser struct {
	data       []byte
	offsets    []uint64
	refSize    int
	numObjects uint64
}

func parseBinaryPlist(data []byte) (map[string]string, error) {
	if len(data) < 8+bplistTrailerSize {
		return nil, errors.New("truncated binary plist")
	}
	trailer := data[len(data)-bplistTrailerSize:]
	offsetIntSize := int(trailer[6])
	objRefSize := int(trailer[7])
	numObjects := binary.BigEndian.Uint64(trailer[8:16])
	topObject := binary.BigEndian.Uint64(trailer[16:24])
	tableOffset := binary.BigEndian.Uint64(trailer[24:32])

	if offsetIntSize < 1 || offsetIntSize > 8 || objRefSize < 1 || objRefSize > 8 {
		return nil, errors.New("bad binary plist trailer")
	}
	if numObjects == 0 || numObjects > bplistMaxObjects || topObject >= numObjects {
		return nil, errors.New("bad binary plist object table")
	}
	tableEnd := tableOffset + numObjects*uint64(offsetIntSize)
	if tableEnd > uint64(len(data)-bplistTrailerSize) {
		return nil, errors.New("binary plist offset table out of range")
	}

	p := &bplistParser{data: data, refSize: objRefSize, numObjects: numObjects}
	p.offsets = make([]uint64, numObjects)
	for i := uint64(0); i < numObjects; i++ {
		start := tableOffset + i*uint64(offsetIntSize)
		off := readSizedUint(data[start : start+uint64(offsetIntSize)])
		if off >= tableOffset {
			return nil, errors.New("binary plist offset out of range")
		}
		p.offsets[i] = off
	}

	top, err := p.object(topObject, 0)
	if err != nil {
		return nil, err
	}
	dict, ok := top.(map[string]string)
	if !ok {
		return nil, errors.New("binary plist root is not a dictionary")
	}
	if len(dict) == 0 {
		return nil, errors.New("no scalar keys in plist")
	}
	return dict, nil
}

func readSizedUint(b []byte) uint64 {
	var v uint64
	for _, c := range b {
		v = v<<8 | uint64(c)
	}
	return v
}

// object decodes one object. Scalars become strings; nested containers become
// nil so callers can skip them cheaply.
func (p *bplistParser) object(ref uint64, depth int) (any, error) {
	if depth > bplistMaxDepth || ref >= p.numObjects {
		return nil, errors.New("binary plist object out of range")
	}
	off := p.offsets[ref]
	if off >= uint64(len(p.data)) {
		return nil, errors.New("binary plist object offset out of range")
	}
	marker := p.data[off]
	pos := off + 1
	hi, lo := marker&0xF0, marker&0x0F

	switch hi {
	case bplistTypeNull:
		switch marker {
		case 0x09:
			return "1", nil
		case 0x08, 0x00:
			return "0", nil
		}
		return nil, nil
	case bplistTypeInt:
		size := uint64(1) << lo
		if lo > 4 {
			return nil, nil
		}
		if pos+size > uint64(len(p.data)) {
			return nil, errors.New("binary plist int out of range")
		}
		return strconv.FormatUint(readSizedUint(p.data[pos:pos+size]), 10), nil
	case bplistTypeReal:
		size := uint64(1) << lo
		if pos+size > uint64(len(p.data)) {
			return nil, errors.New("binary plist real out of range")
		}
		switch size {
		case 4:
			bits := binary.BigEndian.Uint32(p.data[pos : pos+4])
			return strconv.FormatFloat(float64(math.Float32frombits(bits)), 'g', -1, 32), nil
		case 8:
			bits := binary.BigEndian.Uint64(p.data[pos : pos+8])
			return strconv.FormatFloat(math.Float64frombits(bits), 'g', -1, 64), nil
		}
		return nil, nil
	case bplistTypeDate, bplistTypeData, bplistTypeUID, bplistTypeArray, bplistTypeSet:
		return nil, nil
	case bplistTypeASCII, bplistTypeUTF16:
		n, body, err := p.countAndBody(pos, lo)
		if err != nil {
			return nil, err
		}
		if hi == bplistTypeASCII {
			if body+n > uint64(len(p.data)) {
				return nil, errors.New("binary plist string out of range")
			}
			return string(p.data[body : body+n]), nil
		}
		if body+2*n > uint64(len(p.data)) {
			return nil, errors.New("binary plist utf16 out of range")
		}
		units := make([]uint16, n)
		for i := uint64(0); i < n; i++ {
			units[i] = binary.BigEndian.Uint16(p.data[body+2*i : body+2*i+2])
		}
		return string(utf16.Decode(units)), nil
	case bplistTypeDict:
		n, body, err := p.countAndBody(pos, lo)
		if err != nil {
			return nil, err
		}
		refSize := uint64(p.refSize)
		if body+2*n*refSize > uint64(len(p.data)) {
			return nil, errors.New("binary plist dict out of range")
		}
		out := make(map[string]string, n)
		for i := uint64(0); i < n; i++ {
			keyPos := body + i*refSize
			valPos := body + n*refSize + i*refSize
			keyRef := readSizedUint(p.data[keyPos : keyPos+refSize])
			valRef := readSizedUint(p.data[valPos : valPos+refSize])
			keyVal, err := p.object(keyRef, depth+1)
			if err != nil {
				return nil, err
			}
			key, _ := keyVal.(string)
			if key == "" {
				continue
			}
			val, err := p.object(valRef, depth+1)
			if err != nil {
				return nil, err
			}
			if s, ok := val.(string); ok {
				out[key] = s
			}
		}
		return out, nil
	}
	return nil, nil
}

// countAndBody decodes a variable-length object's count and returns the body
// offset after any length-prefix integer.
func (p *bplistParser) countAndBody(pos uint64, lo byte) (uint64, uint64, error) {
	if lo != 0x0F {
		return uint64(lo), pos, nil
	}
	if pos >= uint64(len(p.data)) {
		return 0, 0, errors.New("binary plist length out of range")
	}
	marker := p.data[pos]
	if marker&0xF0 != bplistTypeInt || marker&0x0F > 3 {
		return 0, 0, errors.New("binary plist bad length marker")
	}
	size := uint64(1) << (marker & 0x0F)
	if pos+1+size > uint64(len(p.data)) {
		return 0, 0, errors.New("binary plist length out of range")
	}
	n := readSizedUint(p.data[pos+1 : pos+1+size])
	if n > uint64(len(p.data)) {
		return 0, 0, errors.New("binary plist length too large")
	}
	return n, pos + 1 + size, nil
}
