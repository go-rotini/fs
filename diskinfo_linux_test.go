//go:build linux

package fs

import (
	"strings"
	"testing"
)

func TestParseMountInfo_UnescapesMountPoint(t *testing.T) {
	t.Parallel()
	in := "36 35 98:0 / / rw - ext4 /dev/root rw\n" +
		`37 36 0:40 / /mnt/My\040Drive rw,nosuid - cifs //srv/share rw` + "\n" +
		`38 36 0:41 / /mnt/back\134slash rw - nfs4 srv:/x rw` + "\n"
	entries, err := parseMountInfo(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	want := []mountInfoEntry{{"/", "ext4"}, {"/mnt/My Drive", "cifs"}, {`/mnt/back\slash`, "nfs4"}}
	if len(entries) != len(want) {
		t.Fatalf("entries = %v; want %v", entries, want)
	}
	for i := range want {
		if entries[i] != want[i] {
			t.Errorf("entry %d = %v; want %v", i, entries[i], want[i])
		}
	}
}
