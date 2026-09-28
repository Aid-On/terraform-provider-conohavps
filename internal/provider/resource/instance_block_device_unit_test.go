package resource

import (
	"reflect"
	"testing"
)

// サーバーに付いているボリュームのうち、設定で渡したものだけを block_device に写す.
// conohavps_volume_attachment で後から付けた追加 SSD を写すと、block_device の差分でサーバーが作り直しになる.
func TestKeepDeclaredBlockDevices(t *testing.T) {
	cases := []struct {
		name               string
		declared, attached []string
		want               []string
	}{
		{"attached data volume is not added", []string{"boot"}, []string{"boot", "data"}, []string{"boot"}},
		{"detached declared volume is dropped", []string{"boot", "old"}, []string{"boot"}, []string{"boot"}},
		{"state order is kept", []string{"b", "a"}, []string{"a", "b", "c"}, []string{"b", "a"}},
		{"nothing declared (import) takes every attached volume", nil, []string{"boot", "data"}, []string{"boot", "data"}},
		{"nothing attached", []string{"boot"}, nil, []string{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := keepDeclaredBlockDevices(c.declared, c.attached); !reflect.DeepEqual(got, c.want) {
				t.Errorf("keepDeclaredBlockDevices(%v, %v) = %v, want %v", c.declared, c.attached, got, c.want)
			}
		})
	}
}
