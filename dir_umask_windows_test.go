//go:build windows

package fs

func syscallUmask(_ int) int { return 0 }
