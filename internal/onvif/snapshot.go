package onvif

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"sync/atomic"
	"time"

	"github.com/smford/onvif-camera-simulator/internal/telemetry"
)

var frameCounter uint64

// GenerateSnapshot renders an in-memory dynamic test pattern JPEG with live timestamps,
// PTZ crosshair, and analytics bounding boxes.
func GenerateSnapshot(
	cameraID string,
	width, height int,
	pan, tilt float64,
	ev telemetry.CameraEventTelemetry,
) ([]byte, error) {
	if width <= 0 {
		width = 1280
	}
	if height <= 0 {
		height = 720
	}

	frameIdx := atomic.AddUint64(&frameCounter, 1)
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	// 1. Draw background gradient / test pattern
	bgDark := color.RGBA{R: 24, G: 28, B: 36, A: 255}
	bgGrid := color.RGBA{R: 40, G: 46, B: 58, A: 255}
	draw.Draw(img, img.Bounds(), &image.Uniform{C: bgDark}, image.Point{}, draw.Src)

	// Draw grid lines every 64 pixels
	gridStep := width / 20
	if gridStep < 32 {
		gridStep = 32
	}
	for x := 0; x < width; x += gridStep {
		for y := 0; y < height; y++ {
			img.Set(x, y, bgGrid)
		}
	}
	for y := 0; y < height; y += gridStep {
		for x := 0; x < width; x++ {
			img.Set(x, y, bgGrid)
		}
	}

	// 2. Draw SMPTE style color bars at the bottom (10% height)
	barHeight := height / 10
	barY := height - barHeight
	colors := []color.RGBA{
		{200, 200, 200, 255}, // White/Gray
		{200, 200, 0, 255},   // Yellow
		{0, 200, 200, 255},   // Cyan
		{0, 200, 0, 255},     // Green
		{200, 0, 200, 255},   // Magenta
		{200, 0, 0, 255},     // Red
		{0, 0, 200, 255},     // Blue
		{20, 20, 20, 255},    // Black
	}
	barWidth := width / len(colors)
	for i, c := range colors {
		x0 := i * barWidth
		x1 := x0 + barWidth
		if i == len(colors)-1 {
			x1 = width
		}
		for bx := x0; bx < x1; bx++ {
			for by := barY; by < height; by++ {
				img.Set(bx, by, c)
			}
		}
	}

	// 3. Draw PTZ Crosshair
	// Map normalized pan [-1, 1] and tilt [-1, 1] to coordinates
	cx := int((pan + 1.0) / 2.0 * float64(width))
	cy := int((1.0 - (tilt+1.0)/2.0) * float64(height))
	crossColor := color.RGBA{R: 0, G: 220, B: 255, A: 255} // Cyan
	crosshairSize := 24
	for dx := -crosshairSize; dx <= crosshairSize; dx++ {
		px := cx + dx
		if px >= 0 && px < width && cy >= 0 && cy < height {
			img.Set(px, cy, crossColor)
			if cy+1 < height {
				img.Set(px, cy+1, crossColor)
			}
		}
	}
	for dy := -crosshairSize; dy <= crosshairSize; dy++ {
		py := cy + dy
		if py >= 0 && py < height && cx >= 0 && cx < width {
			img.Set(cx, py, crossColor)
			if cx+1 < width {
				img.Set(cx+1, py, crossColor)
			}
		}
	}

	// 4. Draw Event Bounding Boxes
	if ev.HumanActive {
		// Human Bounding Box (bright green)
		boxColor := color.RGBA{R: 0, G: 255, B: 100, A: 255}
		drawBox(img, ev.HumanBBox, width, height, boxColor, 3)
	}

	if ev.VehicleActive {
		// Vehicle Bounding Box (bright orange)
		boxColor := color.RGBA{R: 255, G: 140, B: 0, A: 255}
		drawBox(img, ev.VehicleBBox, width, height, boxColor, 3)
	}

	if ev.MotionActive {
		// Draw motion detection indicator banner in upper right
		bannerColor := color.RGBA{R: 255, G: 40, B: 40, A: 255}
		fillBox(img, width-220, 20, 200, 36, bannerColor)
	}

	// 5. Draw text overlay boxes for camera info and UTC timestamp
	textColor := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	fillBox(img, 20, 20, 320, 60, color.RGBA{R: 10, G: 12, B: 18, A: 220})
	nowUTC := time.Now().UTC().Format("2006-01-02 15:04:05.000 MST")
	infoStr := fmt.Sprintf("%s | Frame #%d", cameraID, frameIdx)
	ptzStr := fmt.Sprintf("PTZ: [%.2f, %.2f]", pan, tilt)

	drawSimpleText(img, 30, 32, infoStr, textColor)
	drawSimpleText(img, 30, 48, nowUTC, color.RGBA{R: 160, G: 220, B: 255, A: 255})
	drawSimpleText(img, 30, 64, ptzStr, color.RGBA{R: 255, G: 200, B: 80, A: 255})

	// Encode to JPEG
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		return nil, fmt.Errorf("jpeg encoding failed: %w", err)
	}

	return buf.Bytes(), nil
}

func drawBox(img *image.RGBA, bbox [4]float64, imgW, imgH int, c color.RGBA, thickness int) {
	bx := int(bbox[0] * float64(imgW))
	by := int(bbox[1] * float64(imgH))
	bw := int(bbox[2] * float64(imgW))
	bh := int(bbox[3] * float64(imgH))

	if bx < 0 {
		bx = 0
	}
	if by < 0 {
		by = 0
	}
	if bx+bw > imgW {
		bw = imgW - bx
	}
	if by+bh > imgH {
		bh = imgH - by
	}

	for t := 0; t < thickness; t++ {
		// Top and bottom horizontal edges
		for x := bx; x < bx+bw; x++ {
			if by+t < imgH {
				img.Set(x, by+t, c)
			}
			if by+bh-1-t >= 0 {
				img.Set(x, by+bh-1-t, c)
			}
		}
		// Left and right vertical edges
		for y := by; y < by+bh; y++ {
			if bx+t < imgW {
				img.Set(bx+t, y, c)
			}
			if bx+bw-1-t >= 0 {
				img.Set(bx+bw-1-t, y, c)
			}
		}
	}
}

func fillBox(img *image.RGBA, x, y, w, h int, c color.RGBA) {
	for px := x; px < x+w && px < img.Bounds().Dx(); px++ {
		for py := y; py < y+h && py < img.Bounds().Dy(); py++ {
			if px >= 0 && py >= 0 {
				img.Set(px, py, c)
			}
		}
	}
}

// 5x7 dot-matrix font renderer for ASCII characters (pure Go, zero external assets)
var font5x7 = map[byte][5]byte{
	'0': {0x3e, 0x51, 0x49, 0x45, 0x3e},
	'1': {0x00, 0x42, 0x7f, 0x40, 0x00},
	'2': {0x42, 0x61, 0x51, 0x49, 0x46},
	'3': {0x21, 0x41, 0x45, 0x4b, 0x31},
	'4': {0x18, 0x14, 0x12, 0x7f, 0x10},
	'5': {0x27, 0x45, 0x45, 0x45, 0x39},
	'6': {0x3c, 0x4a, 0x49, 0x49, 0x30},
	'7': {0x01, 0x71, 0x09, 0x05, 0x03},
	'8': {0x36, 0x49, 0x49, 0x49, 0x36},
	'9': {0x06, 0x49, 0x49, 0x29, 0x1e},
	'A': {0x7e, 0x11, 0x11, 0x11, 0x7e},
	'B': {0x7f, 0x49, 0x49, 0x49, 0x36},
	'C': {0x3e, 0x41, 0x41, 0x41, 0x22},
	'D': {0x7f, 0x41, 0x41, 0x22, 0x1c},
	'E': {0x7f, 0x49, 0x49, 0x49, 0x41},
	'F': {0x7f, 0x09, 0x09, 0x09, 0x01},
	'G': {0x3e, 0x41, 0x49, 0x49, 0x7a},
	'H': {0x7f, 0x08, 0x08, 0x08, 0x7f},
	'I': {0x00, 0x41, 0x7f, 0x41, 0x00},
	'J': {0x20, 0x40, 0x41, 0x3f, 0x01},
	'K': {0x7f, 0x08, 0x14, 0x22, 0x41},
	'L': {0x7f, 0x40, 0x40, 0x40, 0x40},
	'M': {0x7f, 0x02, 0x0c, 0x02, 0x7f},
	'N': {0x7f, 0x04, 0x08, 0x10, 0x7f},
	'O': {0x3e, 0x41, 0x41, 0x41, 0x3e},
	'P': {0x7f, 0x09, 0x09, 0x09, 0x06},
	'Q': {0x3e, 0x41, 0x51, 0x21, 0x5e},
	'R': {0x7f, 0x09, 0x19, 0x29, 0x46},
	'S': {0x46, 0x49, 0x49, 0x49, 0x31},
	'T': {0x01, 0x01, 0x7f, 0x01, 0x01},
	'U': {0x3f, 0x40, 0x40, 0x40, 0x3f},
	'V': {0x1f, 0x20, 0x40, 0x20, 0x1f},
	'W': {0x7f, 0x20, 0x18, 0x20, 0x7f},
	'X': {0x63, 0x14, 0x08, 0x14, 0x63},
	'Y': {0x07, 0x08, 0x70, 0x08, 0x07},
	'Z': {0x61, 0x51, 0x49, 0x45, 0x43},
	'a': {0x20, 0x54, 0x54, 0x54, 0x78},
	'b': {0x7f, 0x48, 0x44, 0x44, 0x38},
	'c': {0x38, 0x44, 0x44, 0x44, 0x20},
	'd': {0x38, 0x44, 0x44, 0x48, 0x7f},
	'e': {0x38, 0x54, 0x54, 0x54, 0x18},
	'f': {0x08, 0x7e, 0x09, 0x01, 0x02},
	'g': {0x08, 0x14, 0x54, 0x54, 0x3c},
	'h': {0x7f, 0x08, 0x04, 0x04, 0x78},
	'i': {0x00, 0x44, 0x7d, 0x40, 0x00},
	'j': {0x20, 0x40, 0x44, 0x3d, 0x00},
	'k': {0x7f, 0x10, 0x28, 0x44, 0x00},
	'l': {0x00, 0x41, 0x7f, 0x40, 0x00},
	'm': {0x7c, 0x04, 0x18, 0x04, 0x78},
	'n': {0x7c, 0x08, 0x04, 0x04, 0x78},
	'o': {0x38, 0x44, 0x44, 0x44, 0x38},
	'p': {0x7c, 0x14, 0x14, 0x14, 0x08},
	'q': {0x08, 0x14, 0x14, 0x18, 0x7c},
	'r': {0x7c, 0x08, 0x04, 0x04, 0x08},
	's': {0x48, 0x54, 0x54, 0x54, 0x20},
	't': {0x04, 0x3f, 0x44, 0x40, 0x20},
	'u': {0x3c, 0x40, 0x40, 0x20, 0x7c},
	'v': {0x1c, 0x20, 0x40, 0x20, 0x1c},
	'w': {0x3c, 0x40, 0x30, 0x40, 0x3c},
	'x': {0x44, 0x28, 0x10, 0x28, 0x44},
	'y': {0x0c, 0x50, 0x50, 0x50, 0x3c},
	'z': {0x44, 0x64, 0x54, 0x4c, 0x44},
	' ': {0x00, 0x00, 0x00, 0x00, 0x00},
	'-': {0x08, 0x08, 0x08, 0x08, 0x08},
	':': {0x00, 0x36, 0x36, 0x00, 0x00},
	'.': {0x00, 0x60, 0x60, 0x00, 0x00},
	'[': {0x00, 0x7f, 0x41, 0x41, 0x00},
	']': {0x00, 0x41, 0x41, 0x7f, 0x00},
	',': {0x00, 0x50, 0x30, 0x00, 0x00},
	'#': {0x14, 0x7f, 0x14, 0x7f, 0x14},
	'|': {0x00, 0x00, 0x7f, 0x00, 0x00},
}

func drawSimpleText(img *image.RGBA, startX, startY int, text string, c color.RGBA) {
	curX := startX
	for i := 0; i < len(text); i++ {
		char := text[i]
		glyph, ok := font5x7[char]
		if !ok {
			glyph = font5x7[' ']
		}
		for col := 0; col < 5; col++ {
			bits := glyph[col]
			for row := 0; row < 7; row++ {
				if (bits & (1 << row)) != 0 {
					img.Set(curX+col, startY+row, c)
				}
			}
		}
		curX += 7
	}
}
