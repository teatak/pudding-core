package attachment

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

const (
	ModelImageMaxDimension  = 2048
	ModelImageMaxSourceArea = 64 * 1024 * 1024
	modelImageMaxBytes      = 4 << 20
	modelImageJPEGQuality   = 85
	modelImageCacheVersion  = "v1-2048"
	modelImageCacheDir      = ".model"
)

var ErrModelImageTooLarge = errors.New("attachment model image has too many pixels")

type ModelImage struct {
	Data     []byte
	MIME     string
	Width    int
	Height   int
	Original bool
}

// ModelImageForProvider returns the original image when it is already bounded,
// otherwise it creates and caches a model-only derivative next to the blob.
func (s *Service) ModelImageForProvider(sessionID, key, mimeType string) (ModelImage, error) {
	path, ok, err := s.Path(sessionID, key)
	if err != nil {
		return ModelImage{}, err
	}
	if !ok {
		return ModelImage{}, os.ErrNotExist
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ModelImage{}, err
	}
	if len(data) == 0 {
		return ModelImage{}, errors.New("attachment model image is empty")
	}
	mimeType = normalizeMIME(mimeType)
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return ModelImage{Data: data, MIME: mimeType, Original: true}, nil
	}
	if int64(config.Width)*int64(config.Height) > ModelImageMaxSourceArea {
		return ModelImage{}, ErrModelImageTooLarge
	}
	if config.Width <= ModelImageMaxDimension && config.Height <= ModelImageMaxDimension && len(data) <= modelImageMaxBytes {
		return ModelImage{Data: data, MIME: resolvedImageMIME(mimeType, format), Width: config.Width, Height: config.Height, Original: true}, nil
	}

	cacheBase := modelImageCacheBase(path)
	if cached, ok := readCachedModelImage(cacheBase); ok {
		return cached, nil
	}
	decoded, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return ModelImage{Data: data, MIME: mimeType, Width: config.Width, Height: config.Height, Original: true}, nil
	}
	targetWidth, targetHeight := modelImageDimensions(config.Width, config.Height)
	modelImage := decoded
	if targetWidth != config.Width || targetHeight != config.Height {
		modelImage = resizeModelImage(decoded, targetWidth, targetHeight)
	}
	encoded, outputMIME, extension, err := encodeModelImage(modelImage, format)
	if err != nil {
		return ModelImage{}, err
	}
	if targetWidth == config.Width && targetHeight == config.Height && len(encoded) >= len(data) {
		return ModelImage{Data: data, MIME: resolvedImageMIME(mimeType, format), Width: config.Width, Height: config.Height, Original: true}, nil
	}
	if err := writeCachedModelImage(cacheBase+extension, encoded); err != nil {
		return ModelImage{Data: encoded, MIME: outputMIME, Width: targetWidth, Height: targetHeight}, nil
	}
	return ModelImage{Data: encoded, MIME: outputMIME, Width: targetWidth, Height: targetHeight}, nil
}

func modelImageDimensions(width, height int) (int, int) {
	if width <= ModelImageMaxDimension && height <= ModelImageMaxDimension {
		return width, height
	}
	if width >= height {
		return ModelImageMaxDimension, max(1, int(math.Round(float64(height)*ModelImageMaxDimension/float64(width))))
	}
	return max(1, int(math.Round(float64(width)*ModelImageMaxDimension/float64(height)))), ModelImageMaxDimension
}

func resizeModelImage(src image.Image, width, height int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, width, height))
	bounds := src.Bounds()
	sourceWidth := bounds.Dx()
	sourceHeight := bounds.Dy()
	for y := 0; y < height; y++ {
		y0, y1, yWeight := interpolationAxis(y, height, sourceHeight)
		for x := 0; x < width; x++ {
			x0, x1, xWeight := interpolationAxis(x, width, sourceWidth)
			c00 := color.NRGBAModel.Convert(src.At(bounds.Min.X+x0, bounds.Min.Y+y0)).(color.NRGBA)
			c10 := color.NRGBAModel.Convert(src.At(bounds.Min.X+x1, bounds.Min.Y+y0)).(color.NRGBA)
			c01 := color.NRGBAModel.Convert(src.At(bounds.Min.X+x0, bounds.Min.Y+y1)).(color.NRGBA)
			c11 := color.NRGBAModel.Convert(src.At(bounds.Min.X+x1, bounds.Min.Y+y1)).(color.NRGBA)
			dst.SetNRGBA(x, y, color.NRGBA{
				R: interpolateChannel(c00.R, c10.R, c01.R, c11.R, xWeight, yWeight),
				G: interpolateChannel(c00.G, c10.G, c01.G, c11.G, xWeight, yWeight),
				B: interpolateChannel(c00.B, c10.B, c01.B, c11.B, xWeight, yWeight),
				A: interpolateChannel(c00.A, c10.A, c01.A, c11.A, xWeight, yWeight),
			})
		}
	}
	return dst
}

func interpolationAxis(position, targetSize, sourceSize int) (int, int, float64) {
	value := (float64(position)+0.5)*float64(sourceSize)/float64(targetSize) - 0.5
	if value <= 0 {
		return 0, min(1, sourceSize-1), 0
	}
	left := int(math.Floor(value))
	if left >= sourceSize-1 {
		return sourceSize - 1, sourceSize - 1, 0
	}
	return left, left + 1, value - float64(left)
}

func interpolateChannel(c00, c10, c01, c11 uint8, xWeight, yWeight float64) uint8 {
	top := float64(c00)*(1-xWeight) + float64(c10)*xWeight
	bottom := float64(c01)*(1-xWeight) + float64(c11)*xWeight
	return uint8(math.Round(top*(1-yWeight) + bottom*yWeight))
}

func encodeModelImage(img image.Image, sourceFormat string) ([]byte, string, string, error) {
	if sourceFormat == "jpeg" {
		data, err := encodeJPEG(img, modelImageJPEGQuality)
		return data, "image/jpeg", ".jpg", err
	}
	var output bytes.Buffer
	if err := png.Encode(&output, img); err != nil {
		return nil, "", "", err
	}
	pngData := output.Bytes()
	if len(pngData) <= modelImageMaxBytes || !modelImageOpaque(img) {
		return pngData, "image/png", ".png", nil
	}
	jpegData, err := encodeJPEG(img, 90)
	if err == nil && len(jpegData) < len(pngData) {
		return jpegData, "image/jpeg", ".jpg", nil
	}
	return pngData, "image/png", ".png", nil
}

func encodeJPEG(img image.Image, quality int) ([]byte, error) {
	var output bytes.Buffer
	if err := jpeg.Encode(&output, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func modelImageOpaque(img image.Image) bool {
	if opaque, ok := img.(interface{ Opaque() bool }); ok {
		return opaque.Opaque()
	}
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			_, _, _, alpha := img.At(x, y).RGBA()
			if alpha != 0xffff {
				return false
			}
		}
	}
	return true
}

func resolvedImageMIME(raw, format string) string {
	switch format {
	case "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "gif":
		return "image/gif"
	default:
		return raw
	}
}

func modelImageCacheBase(originalPath string) string {
	return filepath.Join(filepath.Dir(originalPath), modelImageCacheDir, filepath.Base(originalPath)+"."+modelImageCacheVersion)
}

func readCachedModelImage(base string) (ModelImage, bool) {
	for _, candidate := range []struct {
		extension string
		mime      string
	}{{".png", "image/png"}, {".jpg", "image/jpeg"}} {
		data, err := os.ReadFile(base + candidate.extension)
		if err != nil || len(data) == 0 {
			continue
		}
		config, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			_ = os.Remove(base + candidate.extension)
			continue
		}
		return ModelImage{Data: data, MIME: candidate.mime, Width: config.Width, Height: config.Height}, true
	}
	return ModelImage{}, false
}

func writeCachedModelImage(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".model-image-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		if _, statErr := os.Stat(path); statErr == nil {
			return nil
		}
		return err
	}
	return nil
}

func removeModelImageCache(originalPath string) error {
	base := modelImageCacheBase(originalPath)
	var firstErr error
	for _, extension := range []string{".png", ".jpg"} {
		if err := os.Remove(base + extension); err != nil && !errors.Is(err, os.ErrNotExist) && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
