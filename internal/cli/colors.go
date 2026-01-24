package cli

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

// ANSI Attribute Codes
const (
	ResetCode     = "\033[0m"
	BoldCode      = "\033[1m"
	DimCode       = "\033[2m"
	ItalicCode    = "\033[3m"
	UnderlineCode = "\033[4m"
	BlinkCode     = "\033[5m"
	ReverseCode   = "\033[7m"
	HiddenCode    = "\033[8m"
)

// Standard Colors
type Color string

const (
	Black   Color = "\033[30m"
	Red     Color = "\033[31m"
	Green   Color = "\033[32m"
	Yellow  Color = "\033[33m"
	Blue    Color = "\033[34m"
	Purple  Color = "\033[35m"
	Cyan    Color = "\033[36m"
	White   Color = "\033[37m"
	Default Color = "\033[39m"

	BgBlack   Color = "\033[40m"
	BgRed     Color = "\033[41m"
	BgGreen   Color = "\033[42m"
	BgYellow  Color = "\033[43m"
	BgBlue    Color = "\033[44m"
	BgPurple  Color = "\033[45m"
	BgCyan    Color = "\033[46m"
	BgWhite   Color = "\033[47m"
	BgDefault Color = "\033[49m"
)

// RGB represents a TrueColor (24-bit)
type RGB struct {
	R, G, B float64
}

var (
	BrandBlue   = RGB{0, 120, 255}  // #0078FF
	BrandPurple = RGB{189, 52, 235} // #BD34EB
	BrandError  = RGB{255, 87, 87}  // #FF5757
	BrandWarn   = RGB{255, 204, 0}  // #FFCC00
	BrandInfo   = RGB{52, 152, 219} // #3498DB
	BrandGreen  = RGB{52, 219, 158} // #34db9e
)

var (
	noColor     bool
	noColorOnce sync.Once
)

// Enabled returns true if color output is enabled
func Enabled() bool {
	noColorOnce.Do(func() {
		_, exists := os.LookupEnv("NO_COLOR")
		noColor = exists
	})
	return !noColor
}

// TextStyler is a fluent builder for styling text
type TextStyler struct {
	codes []string
}

// NewStyle creates a new TextStyler
func NewStyle() *TextStyler {
	return &TextStyler{
		codes: make([]string, 0, 4),
	}
}

func (s *TextStyler) Bold() *TextStyler {
	s.codes = append(s.codes, BoldCode)
	return s
}

func (s *TextStyler) Dim() *TextStyler {
	s.codes = append(s.codes, DimCode)
	return s
}

func (s *TextStyler) Italic() *TextStyler {
	s.codes = append(s.codes, ItalicCode)
	return s
}

func (s *TextStyler) Underline() *TextStyler {
	s.codes = append(s.codes, UnderlineCode)
	return s
}

func (s *TextStyler) Foreground(c Color) *TextStyler {
	s.codes = append(s.codes, string(c))
	return s
}

func (s *TextStyler) Background(c Color) *TextStyler {
	s.codes = append(s.codes, string(c))
	return s
}

// FgRGB adds a TrueColor foreground
func (s *TextStyler) FgRGB(c RGB) *TextStyler {
	code := fmt.Sprintf("\033[38;2;%d;%d;%dm", int(c.R), int(c.G), int(c.B))
	s.codes = append(s.codes, code)
	return s
}

// BgRGB adds a TrueColor background
func (s *TextStyler) BgRGB(c RGB) *TextStyler {
	code := fmt.Sprintf("\033[48;2;%d;%d;%dm", int(c.R), int(c.G), int(c.B))
	s.codes = append(s.codes, code)
	return s
}

// Render applies the styles to the text
func (s *TextStyler) Render(text string) string {
	if !Enabled() {
		return text
	}
	// Combine all codes
	prefix := strings.Join(s.codes, "")
	return prefix + text + ResetCode
}

// Shortcut functions for quick styling

func Stylize(text string, color Color) string {
	return NewStyle().Foreground(color).Render(text)
}

func BoldText(text string) string {
	return NewStyle().Bold().Render(text)
}

// Gradient returns the text colored with a linear interpolation across multiple color stops.
// The progress (0.0 to 1.0) determines the position within the range of colors provided.
func Gradient(text string, progress float64, colors ...RGB) string {
	if !Enabled() || len(colors) == 0 {
		return text
	}

	// if only one color is provided, there is no gradient to calculate.
	if len(colors) == 1 {
		return NewStyle().FgRGB(colors[0]).Render(text)
	}

	// clamp between 0 and 1
	if progress < 0 {
		progress = 0
	} else if progress > 1 {
		progress = 1
	}

	// see which segment the progress falls into.
	// for N colors, there are N-1 segments.
	n := float64(len(colors) - 1)
	segment := int(progress * n)

	// handle the edge case where progress is exactly 1.0
	if segment > len(colors)-2 {
		segment = len(colors) - 2
	}

	// calculate the interp factor within that specific segment [0.0, 1.0]
	localProgress := (progress * n) - float64(segment)

	start := colors[segment]
	end := colors[segment+1]

	// lerp  R, G, and B
	r := start.R + (end.R-start.R)*localProgress
	g := start.G + (end.G-start.G)*localProgress
	b := start.B + (end.B-start.B)*localProgress

	return NewStyle().FgRGB(RGB{r, g, b}).Render(text)
}

func CheckMark() string {
	return NewStyle().Foreground(Green).Bold().Render("✔")
}

func CrossMark() string {
	return NewStyle().Foreground(Red).Bold().Render("✘")
}

func WarningSign() string {
	return NewStyle().Foreground(Yellow).Bold().Render("⚠")
}

func InfoBadge() string {
	return NewStyle().BgRGB(BrandBlue).Foreground(White).Bold().Render(" INFO ")
}

func ErrorBadge() string {
	return NewStyle().BgRGB(BrandError).Foreground(White).Bold().Render(" ERROR ")
}

func WarnBadge() string {
	return NewStyle().BgRGB(BrandWarn).Foreground(Black).Bold().Render(" WARN ")
}

// Legacy helpers for compatibility (can be deprecated or kept)
func Style(text string, colorCode string) string {
	if !Enabled() {
		return text
	}
	return fmt.Sprintf("%s%s%s", colorCode, text, ResetCode)
}
