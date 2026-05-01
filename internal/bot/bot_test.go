package bot

import (
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

func TestSplitForTelegramShortInputUnchanged(t *testing.T) {
	chunks := splitForTelegram("hello")
	assert.Equal(t, []string{"hello"}, chunks)
}

func TestSplitForTelegramASCIIRespectsLimit(t *testing.T) {
	in := strings.Repeat("a", telegramMaxUTF16+100)
	chunks := splitForTelegram(in)
	require.Greater(t, len(chunks), 1)
	for _, chunk := range chunks {
		assert.LessOrEqual(t, utf16Len(chunk), telegramMaxUTF16)
	}
}

func TestSplitForTelegramNonBMPRespectsUTF16Limit(t *testing.T) {
	// A non-BMP emoji takes 2 UTF-16 units. 4000 of them = 8000 units, well
	// over the 4096 limit — verify all chunks fit.
	in := strings.Repeat("\U0001F389", 4000) // 🎉
	chunks := splitForTelegram(in)
	require.Greater(t, len(chunks), 1)
	for _, chunk := range chunks {
		assert.LessOrEqual(t, utf16Len(chunk), telegramMaxUTF16)
	}
}

func TestSplitForTelegramPrefersDoubleNewline(t *testing.T) {
	// First paragraph fills the budget; second paragraph should be a separate chunk.
	para1 := strings.Repeat("a", telegramMaxUTF16-10)
	para2 := "second paragraph"
	in := para1 + "\n\n" + para2
	chunks := splitForTelegram(in)
	require.Equal(t, 2, len(chunks))
	assert.Equal(t, para1, chunks[0])
	assert.Equal(t, para2, chunks[1])
}

func TestSplitForTelegramFallsBackToSingleNewline(t *testing.T) {
	line1 := strings.Repeat("a", telegramMaxUTF16-10)
	line2 := "second line"
	in := line1 + "\n" + line2
	chunks := splitForTelegram(in)
	require.Equal(t, 2, len(chunks))
	assert.Equal(t, line1, chunks[0])
	assert.Equal(t, line2, chunks[1])
}

func TestTruncateToUTF8BytesASCII(t *testing.T) {
	assert.Equal(t, "hello", truncateToUTF8Bytes("hello world", 5))
}

func TestTruncateToUTF8BytesMultibyte(t *testing.T) {
	// "é" is 2 bytes in UTF-8. Truncating at 1 byte must not produce invalid UTF-8.
	s := "é"
	out := truncateToUTF8Bytes(s, 1)
	assert.Equal(t, "", out) // leading byte dropped to keep valid UTF-8
}
