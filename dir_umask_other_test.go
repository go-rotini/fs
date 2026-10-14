//go:build !unix

package fs

func syscallUmask(_ int) int { return 0 }
