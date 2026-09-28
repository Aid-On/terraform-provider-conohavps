// イメージ保存容量のリソースと使用量のデータソースの単体テストを提供する.
// 偽の API を相手に、設定・変更・インポート・削除（50GB に戻す）と、50GB＋500GB 単位（550GB 以上）の検証、
// 使用量を下回る変更で API のエラーが返ること、外での変更を差分として検出することを検証する.

package resource_test

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/fakeapi"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

const imageQuotaAddr = "conohavps_image_quota.test"

// 偽のイメージ保存容量の API.
type fakeImageQuota struct {
	*fakeapi.Server
	mu        sync.Mutex
	sizeGB    int64    // イメージ保存容量（GB）
	usedBytes int64    // 使用量（byte）
	puts      []string // PUT で受けた本文の image_size
	badPuts   []string // 形がドキュメントと違った PUT の理由
}

func newFakeImageQuota(t *testing.T) *fakeImageQuota {
	f := &fakeImageQuota{Server: fakeapi.New(t), sizeGB: 50}

	f.Mux.HandleFunc("GET /image-service/v2/quota", func(w http.ResponseWriter, r *http.Request) {
		if !fakeapi.Authorized(w, r) {
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		fakeapi.WriteJSON(w, http.StatusOK, map[string]any{"quota": map[string]any{"image_size": fmt.Sprintf("%dGB", f.sizeGB)}})
	})

	f.Mux.HandleFunc("PUT /image-service/v2/quota", func(w http.ResponseWriter, r *http.Request) {
		if !fakeapi.Authorized(w, r) {
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			f.badPuts = append(f.badPuts, "Content-Type "+ct)
		}
		var body struct {
			Quota struct {
				ImageSize string `json:"image_size"`
			} `json:"quota"`
		}
		if err := fakeapi.ReadJSON(r, &body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.puts = append(f.puts, body.Quota.ImageSize)
		gb, err := strconv.ParseInt(strings.TrimSuffix(body.Quota.ImageSize, "GB"), 10, 64)
		if err != nil || !strings.HasSuffix(body.Quota.ImageSize, "GB") || gb < 50 || (gb-50)%500 != 0 {
			http.Error(w, "invalid image_size", http.StatusBadRequest)
			return
		}
		if gb*gib < f.usedBytes {
			http.Error(w, "image_size cannot be less than the usage", http.StatusBadRequest)
			return
		}
		f.sizeGB = gb
		fakeapi.WriteJSON(w, http.StatusOK, map[string]any{"quota": map[string]any{"image_size": body.Quota.ImageSize}})
	})

	f.Mux.HandleFunc("GET /image-service/v2/images/total", func(w http.ResponseWriter, r *http.Request) {
		if !fakeapi.Authorized(w, r) {
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		fakeapi.WriteJSON(w, http.StatusOK, map[string]any{"images": map[string]any{"size": f.usedBytes}})
	})

	return f
}

func (f *fakeImageQuota) with(fn func(f *fakeImageQuota)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

// 最後の PUT の image_size が want であることを確かめる.
func (f *fakeImageQuota) expectLastPut(want string) error {
	var err error
	f.with(func(f *fakeImageQuota) {
		switch {
		case len(f.badPuts) > 0:
			err = fmt.Errorf("PUT requests did not match the docs: %v", f.badPuts)
		case len(f.puts) == 0:
			err = fmt.Errorf("no PUT /v2/quota request was sent")
		case f.puts[len(f.puts)-1] != want:
			err = fmt.Errorf("the last PUT /v2/quota has image_size %q, want %q", f.puts[len(f.puts)-1], want)
		}
	})
	return err
}

func imageQuotaConfig(f *fakeImageQuota, gb int) string {
	return f.ProviderConfig() + fmt.Sprintf(`
resource "conohavps_image_quota" "test" {
  image_size_gb = %d
}
`, gb)
}

func TestImageQuota_Lifecycle(t *testing.T) {
	f := newFakeImageQuota(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		// 削除は既定の 50GB に戻す
		CheckDestroy: func(_ *terraform.State) error {
			if err := f.expectLastPut("50GB"); err != nil {
				return err
			}
			var err error
			f.with(func(f *fakeImageQuota) {
				if f.sizeGB != 50 {
					err = fmt.Errorf("image quota is %d GB after destroy, want 50", f.sizeGB)
				}
			})
			return err
		},
		Steps: []resource.TestStep{
			{
				Config: imageQuotaConfig(f, 550),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(imageQuotaAddr, tfjsonpath.New("id"), knownvalue.StringExact(fakeapi.TenantID)),
					statecheck.ExpectKnownValue(imageQuotaAddr, tfjsonpath.New("image_size_gb"), knownvalue.Int64Exact(550)),
				},
				Check: func(_ *terraform.State) error { return f.expectLastPut("550GB") },
			},
			{
				Config: imageQuotaConfig(f, 1050),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(imageQuotaAddr, plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(imageQuotaAddr, tfjsonpath.New("image_size_gb"), knownvalue.Int64Exact(1050)),
				},
				Check: func(_ *terraform.State) error { return f.expectLastPut("1050GB") },
			},
			{
				ResourceName:      imageQuotaAddr,
				ImportState:       true,
				ImportStateId:     fakeapi.TenantID,
				ImportStateVerify: true,
			},
			{
				ResourceName:  imageQuotaAddr,
				ImportState:   true,
				ImportStateId: "other-tenant",
				ExpectError:   regexp.MustCompile(`imported by the tenant ID of the provider`),
			},
			// 使用量を下回る縮小は API が拒否し、そのエラーを返す
			{
				PreConfig: func() {
					f.with(func(f *fakeImageQuota) { f.usedBytes = 600 * gib })
				},
				Config:      imageQuotaConfig(f, 550),
				ExpectError: regexp.MustCompile(`(?s)image_size cannot be less\s+than the usage`),
			},
			// 使用量はデータソースで読める. 縮小に失敗した State は変わらない
			{
				Config: imageQuotaConfig(f, 1050) + `data "conohavps_image_usage" "u" {}`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("data.conohavps_image_usage.u", tfjsonpath.New("size_bytes"), knownvalue.Int64Exact(600*gib)),
					statecheck.ExpectKnownValue("data.conohavps_image_usage.u", tfjsonpath.New("id"), knownvalue.StringExact(fakeapi.TenantID)),
					statecheck.ExpectKnownValue(imageQuotaAddr, tfjsonpath.New("image_size_gb"), knownvalue.Int64Exact(1050)),
				},
			},
			// 使用量が 50GB を超えていれば、削除（50GB に戻す）も API が拒否する
			{
				Config:      imageQuotaConfig(f, 1050),
				Destroy:     true,
				ExpectError: regexp.MustCompile(`(?s)back to 50\s+GB.*image_size cannot be less\s+than the usage`),
			},
			{
				PreConfig: func() {
					f.with(func(f *fakeImageQuota) { f.usedBytes = 1 * gib })
				},
				Config: imageQuotaConfig(f, 1050),
			},
		},
	})
}

// 外で容量が変えられたら、差分として検出する.
func TestImageQuota_DriftOutside(t *testing.T) {
	f := newFakeImageQuota(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		Steps: []resource.TestStep{
			{Config: imageQuotaConfig(f, 550)},
			// 外で増やされた容量は、その場で戻す
			{
				PreConfig: func() { f.with(func(f *fakeImageQuota) { f.sizeGB = 1050 }) },
				Config:    imageQuotaConfig(f, 550),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(imageQuotaAddr, plancheck.ResourceActionUpdate),
					},
				},
				Check: func(_ *terraform.State) error { return f.expectLastPut("550GB") },
			},
			// 無料の 50GB に戻されていれば、足した容量が無いものとして作り直す
			{
				PreConfig: func() { f.with(func(f *fakeImageQuota) { f.sizeGB = 50 }) },
				Config:    imageQuotaConfig(f, 550),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(imageQuotaAddr, plancheck.ResourceActionCreate),
					},
				},
				Check: func(_ *terraform.State) error { return f.expectLastPut("550GB") },
			},
		},
	})
}

// 無料の 50GB のままのアカウントには、インポートする容量が無い.
func TestImageQuota_ImportDefault(t *testing.T) {
	f := newFakeImageQuota(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		Steps: []resource.TestStep{{
			Config:        imageQuotaConfig(f, 550),
			ResourceName:  imageQuotaAddr,
			ImportState:   true,
			ImportStateId: fakeapi.TenantID,
			ExpectError:   regexp.MustCompile(`Cannot import non-existent remote object`),
		}},
	})
}

// 50GB＋500GB 単位でない容量と、無料分と同じ 50GB 以下の容量は、API を呼ぶ前に弾く.
func TestImageQuota_InvalidSize(t *testing.T) {
	f := newFakeImageQuota(t)

	for _, gb := range []int{0, 40, 50, 100, 500, 600} {
		t.Run(strconv.Itoa(gb), func(t *testing.T) {
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: fakeapi.Factories,
				Steps: []resource.TestStep{{
					Config:      imageQuotaConfig(f, gb),
					ExpectError: regexp.MustCompile(fmt.Sprintf(`must be 50 plus a multiple of 500 \(at least 550\), got: %d`, gb)),
				}},
			})
		})
	}

	if err := f.expectLastPut(""); err == nil || !strings.Contains(err.Error(), "no PUT") {
		t.Errorf("an invalid image quota reached the API: %v", err)
	}
}
