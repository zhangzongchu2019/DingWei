// Package clarificationimage validates the private mobile screenshot contract.
package clarificationimage

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
)

const MaxBytes = 5 << 20

// No caller extension or Content-Type can turn text/source/HTML/SVG into an
// image. JPEG/PNG are fully decoded under a bounded dimension/pixel budget.
func Validate(b []byte) (string, error) {
	if len(b) == 0 || len(b) > MaxBytes {
		return "", errors.New("image size invalid")
	}
	kind := http.DetectContentType(b)
	switch kind {
	case "image/png", "image/jpeg":
		if kind == "image/png" && (len(b) < 12 || !bytes.Equal(b[len(b)-8:len(b)-4], []byte("IEND"))) {
			return "", errors.New("PNG trailing or missing data")
		}
		if kind == "image/jpeg" && (len(b) < 2 || b[len(b)-2] != 0xff || b[len(b)-1] != 0xd9) {
			return "", errors.New("JPEG trailing or missing data")
		}
		cfg, _, e := image.DecodeConfig(bytes.NewReader(b))
		if e != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 8192 || cfg.Height > 8192 || int64(cfg.Width)*int64(cfg.Height) > 16<<20 {
			return "", errors.New("image dimensions invalid")
		}
		if _, _, e = image.Decode(bytes.NewReader(b)); e != nil {
			return "", errors.New("image content invalid")
		}
	case "image/webp":
		// WebP has no decoder in Go's standard library. Validate the complete RIFF
		// framing and VP8/VP8L/VP8X dimension header; reject animation and metadata.
		if len(b) < 30 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WEBP" || uint64(binary.LittleEndian.Uint32(b[4:8]))+8 != uint64(len(b)) {
			return "", errors.New("invalid WebP")
		}
		var w, h, canvasW, canvasH uint32
		frames := 0
		for offset := 12; offset < len(b); {
			if offset+8 > len(b) {
				return "", errors.New("invalid WebP chunk")
			}
			n := uint64(binary.LittleEndian.Uint32(b[offset+4 : offset+8]))
			end := uint64(offset) + 8 + n
			if end > uint64(len(b)) {
				return "", errors.New("invalid WebP size")
			}
			p := b[offset+8 : end]
			tag := string(b[offset : offset+4])
			switch tag {
			case "VP8 ":
				if len(p) < 10 || p[0]&1 != 0 || !bytes.Equal(p[3:6], []byte{0x9d, 0x01, 0x2a}) {
					return "", errors.New("invalid VP8")
				}
				w = uint32(binary.LittleEndian.Uint16(p[6:8]) & 0x3fff)
				h = uint32(binary.LittleEndian.Uint16(p[8:10]) & 0x3fff)
				frames++
			case "VP8L":
				if len(p) < 5 || p[0] != 0x2f {
					return "", errors.New("invalid VP8L")
				}
				v := binary.LittleEndian.Uint32(p[1:5])
				w = (v & 0x3fff) + 1
				h = ((v >> 14) & 0x3fff) + 1
				frames++
			case "VP8X":
				if offset != 12 || canvasW != 0 {
					return "", errors.New("invalid WebP canvas ordering")
				}
				if len(p) != 10 || p[0]&^byte(0x10) != 0 || p[1] != 0 || p[2] != 0 || p[3] != 0 {
					return "", errors.New("WebP metadata or animation unsupported")
				}
				canvasW = (uint32(p[4]) | uint32(p[5])<<8 | uint32(p[6])<<16) + 1
				canvasH = (uint32(p[7]) | uint32(p[8])<<8 | uint32(p[9])<<16) + 1
				if canvasW > 8192 || canvasH > 8192 || uint64(canvasW)*uint64(canvasH) > 16<<20 {
					return "", errors.New("WebP canvas too large")
				}

			case "ALPH":
			default:
				return "", errors.New("WebP non-image chunk unsupported")
			}
			if end+n%2 > uint64(len(b)) {
				return "", errors.New("WebP padding missing")
			}
			offset = int(end + n%2)
		}
		if canvasW != 0 && (canvasW != w || canvasH != h) {
			return "", errors.New("WebP canvas mismatch")
		}
		if frames != 1 || w == 0 || h == 0 || w > 8192 || h > 8192 || uint64(w)*uint64(h) > 16<<20 {
			return "", errors.New("invalid WebP dimensions")
		}
	default:
		return "", errors.New("only PNG JPEG WebP allowed")
	}
	return kind, nil
}

// File contains only opaque IDs and bounded metadata, never a public URL.
type File struct {
	ID    string `json:"file_id"`
	Media string `json:"media_type"`
	SHA   string `json:"sha256"`
	Size  int64  `json:"size"`
}
