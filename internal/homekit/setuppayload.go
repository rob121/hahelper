package homekit

import (
	"fmt"
	"strconv"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

const (
	CategoryBridge = 2
	FlagIP         = 2
)

// SetupURI builds an X-HM:// pairing URI for QR codes (HAP setup payload).
func SetupURI(pin string, category int, setupID string) (string, error) {
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, pin)
	if len(digits) != 8 {
		return "", fmt.Errorf("pin must be 8 digits")
	}
	code, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return "", err
	}
	setupID = normalizeSetupID(setupID)

	var payload uint64
	payload |= uint64(category&0xFF) << 31
	payload |= uint64(FlagIP&0xF) << 27
	payload |= uint64(code) & 0x7FFFFFFF

	base36 := toBase36(payload, 9)
	return "X-HM://" + base36 + setupID, nil
}

func normalizeSetupID(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') {
			b.WriteRune(r)
		}
	}
	out := b.String()
	for len(out) < 4 {
		out += "0"
	}
	return out[:4]
}

func toBase36(v uint64, width int) string {
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	buf := make([]byte, width)
	for i := width - 1; i >= 0; i-- {
		buf[i] = alphabet[v%36]
		v /= 36
	}
	return string(buf)
}

// QRPNG returns a PNG image encoding the setup URI.
func QRPNG(setupURI string, size int) ([]byte, error) {
	if size <= 0 {
		size = 256
	}
	return qrcode.Encode(setupURI, qrcode.Medium, size)
}

// FormatPIN formats an 8-digit pin as XXX-XX-XXX.
func FormatPIN(pin string) string {
	d := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, pin)
	if len(d) != 8 {
		return pin
	}
	return d[0:3] + "-" + d[3:5] + "-" + d[5:8]
}
