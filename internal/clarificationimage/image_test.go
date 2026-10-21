package clarificationimage

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestClarificationImageTypeAndDimensionValidation(t *testing.T) {
	var b bytes.Buffer
	im := image.NewRGBA(image.Rect(0, 0, 2, 2))
	im.Set(0, 0, color.White)
	if e := png.Encode(&b, im); e != nil {
		t.Fatal(e)
	}
	if media, e := Validate(b.Bytes()); e != nil || media != "image/png" {
		t.Fatal(media, e)
	}
	for _, bad := range [][]byte{[]byte("<svg><script>alert(1)</script></svg>"), []byte("package main\nfunc main(){}"), []byte("-----BEGIN PRIVATE KEY-----"), b.Bytes()[:20], append(append([]byte{}, b.Bytes()...), []byte("package main")...), make([]byte, MaxBytes+1)} {
		if _, e := Validate(bad); e == nil {
			t.Fatal("CLARIFICATION_IMAGE_CONTENT_ACCEPTED")
		}
	}
}
