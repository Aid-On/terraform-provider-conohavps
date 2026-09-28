// イメージ保存容量の値の形のテストを提供する.

package service

import (
	"strings"
	"testing"
)

func TestImageSize(t *testing.T) {
	for _, s := range []string{"50GB", "550GB", " 1050GB "} {
		gb, err := ParseImageSize(s)
		if err != nil || FormatImageSize(gb) != strings.TrimSpace(s) {
			t.Errorf("ParseImageSize(%q) = %d, %v", s, gb, err)
		}
	}
	for _, s := range []string{"lots", "550", "0.5TB", "550GiB"} {
		if _, err := ParseImageSize(s); err == nil {
			t.Errorf("ParseImageSize accepted %q, which is not a whole number of GB", s)
		}
	}
	for gb, want := range map[int64]bool{50: true, 550: true, 1050: true, 0: false, 100: false, 500: false, 600: false} {
		if ValidImageQuota(gb) != want {
			t.Errorf("ValidImageQuota(%d) = %v, want %v", gb, !want, want)
		}
	}
}
