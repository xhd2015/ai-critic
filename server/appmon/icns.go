package appmon

import (
	"bytes"
	"encoding/binary"
	"errors"
)

// icnsChunkPreference is the order we look for an embedded PNG. The UI renders
// a 34 px tile, so a 64 px image is enough at 2x DPI; 128 px is the next step up
// and still small on the wire. Anything larger only wastes bandwidth, and the
// legacy RLE chunks (icp4/icp5/ic04/ic05) are skipped because browsers cannot
// decode them without a transcode.
var icnsChunkPreference = []string{
	"ic12", // 32x32@2x = 64px
	"ic07", // 128px
	"ic08", // 256px
	"ic13", // 128@2x = 256px
	"ic11", // 16x16@2x = 32px
	"ic09", // 512px
	"ic14", // 256@2x = 512px
	"ic10", // 512@2x = 1024px
	"ic06", // 64px (legacy, usually RLE)
	"icp6", // 64px (legacy, usually RLE)
}

var pngMagic = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

// pngFromICNS returns the preferred embedded PNG payload of an .icns file, or
// nil when the icon carries no PNG representation.
func pngFromICNS(data []byte) []byte {
	chunks, err := parseICNS(data)
	if err != nil {
		return nil
	}
	for _, kind := range icnsChunkPreference {
		if payload, ok := chunks[kind]; ok && bytes.HasPrefix(payload, pngMagic) {
			return payload
		}
	}
	// Unknown-but-modern chunk types: accept any PNG chunk before giving up.
	for _, payload := range chunks {
		if bytes.HasPrefix(payload, pngMagic) {
			return payload
		}
	}
	return nil
}

// parseICNS maps chunk type to payload for a well-formed .icns container.
func parseICNS(data []byte) (map[string][]byte, error) {
	if len(data) < 8 || string(data[:4]) != "icns" {
		return nil, errors.New("not an icns file")
	}
	declared := uint64(binary.BigEndian.Uint32(data[4:8]))
	if declared < 8 || declared > uint64(len(data)) {
		declared = uint64(len(data))
	}
	out := map[string][]byte{}
	for pos := uint64(8); pos+8 <= declared; {
		kind := string(data[pos : pos+4])
		size := uint64(binary.BigEndian.Uint32(data[pos+4 : pos+8]))
		if size < 8 || pos+size > declared {
			return nil, errors.New("malformed icns chunk")
		}
		if _, seen := out[kind]; !seen {
			out[kind] = data[pos+8 : pos+size]
		}
		pos += size
	}
	if len(out) == 0 {
		return nil, errors.New("empty icns")
	}
	return out, nil
}
