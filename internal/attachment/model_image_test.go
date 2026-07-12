package attachment

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"testing"
)

func TestModelImageForProviderKeepsBoundedOriginal(t *testing.T) {
	home := t.TempDir()
	service := NewService(home)
	source := testPNG(t, 320, 180)
	stored, err := service.StoreReader("s1", "small.png", "image/png", bytes.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}

	got, err := service.ModelImageForProvider("s1", stored.AttachmentKey, stored.MIME)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Original || got.Width != 320 || got.Height != 180 || got.MIME != "image/png" {
		t.Fatalf("unexpected bounded model image: %+v", got)
	}
	if !bytes.Equal(got.Data, source) {
		t.Fatal("bounded image bytes changed")
	}
}

func TestModelImageForProviderResizesCachesAndDeletesDerivative(t *testing.T) {
	home := t.TempDir()
	service := NewService(home)
	source := testPNG(t, 2400, 120)
	stored, err := service.StoreReader("s1", "wide.png", "image/png", bytes.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	originalPath, ok, err := service.Path("s1", stored.AttachmentKey)
	if err != nil || !ok {
		t.Fatalf("resolve original: ok=%v err=%v", ok, err)
	}

	got, err := service.ModelImageForProvider("s1", stored.AttachmentKey, stored.MIME)
	if err != nil {
		t.Fatal(err)
	}
	if got.Original || got.Width != ModelImageMaxDimension || got.Height != 102 || got.MIME != "image/png" {
		t.Fatalf("unexpected resized model image: %+v", got)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(got.Data))
	if err != nil || format != "png" || config.Width != got.Width || config.Height != got.Height {
		t.Fatalf("decode derivative: config=%+v format=%q err=%v", config, format, err)
	}
	cachePath := modelImageCacheBase(originalPath) + ".png"
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("model image cache missing: %v", err)
	}

	cached, err := service.ModelImageForProvider("s1", stored.AttachmentKey, stored.MIME)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(cached.Data, got.Data) || cached.Width != got.Width || cached.Height != got.Height {
		t.Fatalf("cache result changed: first=%+v cached=%+v", got, cached)
	}
	original, err := os.ReadFile(originalPath)
	if err != nil || !bytes.Equal(original, source) {
		t.Fatal("original image changed")
	}

	if err := service.Delete("s1", stored.AttachmentKey); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		t.Fatalf("model image cache survived delete: %v", err)
	}
}

func TestModelImageForProviderFallsBackForUndecodableImage(t *testing.T) {
	home := t.TempDir()
	service := NewService(home)
	source := []byte("not really a png")
	stored, err := service.StoreReader("s1", "invalid.png", "image/png", bytes.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.ModelImageForProvider("s1", stored.AttachmentKey, stored.MIME)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Original || !bytes.Equal(got.Data, source) {
		t.Fatalf("invalid image should preserve prior behavior: %+v", got)
	}
}

func TestModelImageForProviderResizesJPEG(t *testing.T) {
	home := t.TempDir()
	service := NewService(home)
	img := testImage(2400, 120)
	var source bytes.Buffer
	if err := jpeg.Encode(&source, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	stored, err := service.StoreReader("s1", "wide.jpg", "image/jpeg", bytes.NewReader(source.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.ModelImageForProvider("s1", stored.AttachmentKey, stored.MIME)
	if err != nil {
		t.Fatal(err)
	}
	if got.Original || got.MIME != "image/jpeg" || got.Width != ModelImageMaxDimension || got.Height != 102 {
		t.Fatalf("unexpected JPEG derivative: %+v", got)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(got.Data))
	if err != nil || format != "jpeg" || config.Width != got.Width || config.Height != got.Height {
		t.Fatalf("decode JPEG derivative: config=%+v format=%q err=%v", config, format, err)
	}
}

func testPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := testImage(width, height)
	var output bytes.Buffer
	if err := png.Encode(&output, img); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func testImage(width, height int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x % 251), G: uint8(y % 241), B: uint8((x + y) % 239), A: 0xff})
		}
	}
	return img
}
