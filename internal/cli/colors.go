package cli

import (
	"fmt"
	"os"
)

const (
	Reset  = "\033[0m"
	Bold   = "\033[1m"
	Red    = "\033[31m"
	Green  = "\033[32m"
	Yellow = "\033[33m"
	Blue   = "\033[34m"
	Purple = "\033[35m"
	Cyan   = "\033[36m"
	White  = "\033[37m"
)

// RGB represents a TrueColor
type RGB struct {
	R, G, B float64
}

var (
	BrandBlue   = RGB{0, 120, 255}  // Blue
	BrandPurple = RGB{189, 52, 235} // Purple
)

// disableColor is a cached check for the environment variable
var disableColor = checkNoColor()

func checkNoColor() bool {
	_, exists := os.LookupEnv("NO_COLOR")
	return exists
}

// Style wraps text in a specific color code
func Style(text string, colorCode string) string {
	if disableColor {
		return text
	}
	return fmt.Sprintf("%s%s%s", colorCode, text, Reset)
}

// ColorizeRGB returns text wrapped in ANSI TrueColor escape codes
func ColorizeRGB(text string, c RGB) string {
	if disableColor {
		return text
	}
	return fmt.Sprintf("\033[38;2;%d;%d;%dm%s\033[0m", int(c.R), int(c.G), int(c.B), text)
}

// Gradient returns the text colored with a linear interpolation between start and end colors
// based on the progress (0.0 to 1.0)
func Gradient(text string, start, end RGB, progress float64) string {
	if disableColor {
		return text
	}
	r := start.R + (end.R-start.R)*progress
	g := start.G + (end.G-start.G)*progress
	b := start.B + (end.B-start.B)*progress

	return ColorizeRGB(text, RGB{r, g, b})
}

func CheckMark() string {
	return Style("✔", Green)
}

func Arrow() string {
	return Style("➜", Blue)
}

func CrossMark() string {
	return Style("✘", Red)
}
