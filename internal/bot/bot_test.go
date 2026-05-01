package bot

import (
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/stretchr/testify/assert"
)

func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

func TestTruncateForTelegramShortInputUnchanged(t *testing.T) {
	in := "hello"
	assert.Equal(t, in, truncateForTelegram(in))
}

func TestTruncateForTelegramASCIIRespectsLimit(t *testing.T) {
	in := strings.Repeat("a", telegramMaxUTF16+100)
	out := truncateForTelegram(in)
	assert.LessOrEqual(t, utf16Len(out), telegramMaxUTF16)
	assert.True(t, strings.HasSuffix(out, "[... truncated]"))
}

func TestTruncateForTelegramNonBMPRespectsUTF16Limit(t *testing.T) {
	// A non-BMP emoji takes 2 UTF-16 units. A naive rune-count would have let
	// 4000 of these through (8000 units, well over the 4096 cap). Make sure
	// the UTF-16 budget is honored.
	in := strings.Repeat("\U0001F389", 4000) // 🎉
	out := truncateForTelegram(in)
	assert.LessOrEqual(t, utf16Len(out), telegramMaxUTF16)
	assert.True(t, strings.HasSuffix(out, "[... truncated]"))
}
