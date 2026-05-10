package fs_test

import (
	"fmt"

	"github.com/go-rotini/fs"
)

// ParseBytes accepts both IEC units (KiB, MiB, ...) and SI units
// (KB, MB, ...). Both default to powers of 1024 — pass through
// [fs.ParseBytesStrict] for true 1000-base SI.
func ExampleParseBytes() {
	for _, in := range []string{"1024", "1KiB", "1.5MiB", "10GB"} {
		n, _ := fs.ParseBytes(in)
		fmt.Printf("%s -> %d\n", in, n)
	}
	// Output:
	// 1024 -> 1024
	// 1KiB -> 1024
	// 1.5MiB -> 1572864
	// 10GB -> 10737418240
}

// FormatBytes renders an integer byte count as an IEC string.
func ExampleFormatBytes() {
	for _, n := range []int64{999, 1024, 1500, 5 << 20, 3 << 30} {
		fmt.Printf("%d -> %s\n", n, fs.FormatBytes(n))
	}
	// Output:
	// 999 -> 999 B
	// 1024 -> 1 KiB
	// 1500 -> 1.5 KiB
	// 5242880 -> 5 MiB
	// 3221225472 -> 3 GiB
}
