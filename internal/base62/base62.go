// =============================================================================
// internal/base62/base62.go — Encode Numbers as Short URL-Safe Strings
// =============================================================================
//
// WHAT DOES THIS FILE DO?
//   It converts a number (like 12345) into a short string using only the characters
//   [0-9][A-Z][a-z] — 62 characters total. That's "Base62". For example, the number
//   12345 might become "3D7". We use this to turn an internal numeric ID into the
//   "short code" you see in URLs like https://short.link/3D7.
//
// WHY BASE62 FOR URL SHORTENERS?
//   - URL-safe: no need to encode special characters (unlike Base64 which uses +, /).
//   - Short: fewer characters than decimal (e.g. 62^3 is way bigger than 10^3).
//   - No database needed for encoding: same number always gives the same string.
//
// GO CONCEPTS IN THIS FILE:
//   - const: compile-time constants (alphabet, base).
//   - uint64: unsigned 64-bit integer (no negatives; big range).
//   - Slices ([]byte): dynamic list of bytes; we append and then reverse.
//   - make([]byte, 0, 11): create a slice with length 0, capacity 11 (avoids realloc).
//   - for loop: we use a "while"-style loop (for numericID > 0) and a classic loop with indices.
//
// =============================================================================

package base62

// alphabet is the 62 characters we use: digits, then uppercase letters, then lowercase.
// In Go, a string is immutable and can be indexed: alphabet[0] is '0', alphabet[10] is 'A'.
const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// We group related constants. base = 62 (length of alphabet). We use it for division and modulo.
// mask and shift could be used for bit tricks; in this simple version we only use base.
const (
	base  = uint64(len(alphabet)) // 62
	mask  = base - 1               // 61, useful for bitwise AND in other encodings
	shift = 6                      // not used here; sometimes used for bit-shift optimizations
)

// Encode converts a positive integer into its Base62 string.
//
// How it works:
//   1. We repeatedly divide the number by 62 and take the remainder. The remainder (0–61)
//      tells us which character to pick from the alphabet.
//   2. We append those characters to a slice. Because we divide from the bottom up,
//      the digits come out in reverse order, so we reverse the slice at the end.
//   3. We convert the slice of bytes to a string and return it.
//
// Example (simplified): 62 in base-62 might give remainder 0 then 1, so "10" (1*62 + 0).
func Encode(numericID uint64) string {
	if numericID == 0 {
		return string(alphabet[0]) // Special case: 0 -> "0"
	}

	// make([]byte, 0, 11): slice with 0 length, 11 capacity. A uint64 in base62 needs at most 11 chars.
	// We append in the loop; capacity 11 avoids multiple reallocations.
	encodedDigits := make([]byte, 0, 11)
	for numericID > 0 {
		remainderAfterDivision := numericID % base  // 0 to 61
		numericID = numericID / base                 // Integer division: shrink the number
		encodedDigits = append(encodedDigits, alphabet[remainderAfterDivision])
	}

	// Reverse the slice in place. We built digits from right-to-left (smallest first), but we want
	// left-to-right for the final string. leftIndex starts at 0, rightIndex at last; we swap and move in.
	for leftIndex, rightIndex := 0, len(encodedDigits)-1; leftIndex < rightIndex; leftIndex, rightIndex = leftIndex+1, rightIndex-1 {
		encodedDigits[leftIndex], encodedDigits[rightIndex] = encodedDigits[rightIndex], encodedDigits[leftIndex]
	}

	// Convert []byte to string. In Go, string(bytes) copies the bytes into an immutable string.
	return string(encodedDigits)
}
