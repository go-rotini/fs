package fs

// archiveSniffSize is the number of leading bytes inspected to
// classify an archive. Tar's "ustar" magic lives at offset 257; we
// peek 264 bytes so any of the recognized signatures fits.
const archiveSniffSize = 264

// detectArchiveFormat identifies the container in buf.
//
//	gzip  magic 0x1F 0x8B at offset 0      → tar.gz (assumed; bare gz is rare in this package's use case)
//	zip   magic 0x50 0x4B 0x03 0x04        → zip
//	tar   "ustar" at offset 257            → tar
//
// Anything else returns [ArchiveFormatUnknown].
func detectArchiveFormat(buf []byte) ArchiveFormat {
	if len(buf) >= 4 {
		if buf[0] == 0x1F && buf[1] == 0x8B {
			return ArchiveFormatTarGz
		}
		if buf[0] == 0x50 && buf[1] == 0x4B && buf[2] == 0x03 && buf[3] == 0x04 {
			return ArchiveFormatZip
		}
	}
	if len(buf) >= 263 {
		if string(buf[257:262]) == "ustar" {
			return ArchiveFormatTar
		}
	}
	return ArchiveFormatUnknown
}
