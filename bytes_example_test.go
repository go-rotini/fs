package fs_test

import (
	"fmt"

	"github.com/go-rotini/fs"
)

// ParseBytes uses strict SI semantics by default: bare KB / MB / GB
// / etc. are 1000-based (matching kubectl, docker, kafka). IEC
// binary units (KiB, MiB, ...) keep their 1024-based meaning.
//
// Use [fs.ParseBytesIEC] when interoperating with the legacy
// disk-vendor idiom where bare KB means 1024.
func ExampleParseBytes() {
	for _, in := range []string{"1024", "1KiB", "1.5MiB", "10GB"} {
		n, _ := fs.ParseBytes(in)
		fmt.Printf("%s -> %d\n", in, n)
	}
	// Output:
	// 1024 -> 1024
	// 1KiB -> 1024
	// 1.5MiB -> 1572864
	// 10GB -> 10000000000
}

// ParseBytesIEC keeps the legacy "bare KB / MB / GB are 1024-based"
// idiom for callers that need it. IEC-suffixed units (KiB, MiB,
// ...) are 1024-based in both variants.
func ExampleParseBytesIEC() {
	for _, in := range []string{"1KB", "1MB", "1GiB"} {
		n, _ := fs.ParseBytesIEC(in)
		fmt.Printf("%s -> %d\n", in, n)
	}
	// Output:
	// 1KB -> 1024
	// 1MB -> 1048576
	// 1GiB -> 1073741824
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
