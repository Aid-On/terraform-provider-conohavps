// ボリュームのアタッチのリソースの単体テストを提供する.
// 偽の ConoHa API にサーバーとボリュームを用意し、アタッチ・インポート・付け替え・外部でのデタッチ・デタッチを検証する.

package resource_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
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

// アタッチの状態を持つ偽の API.
type volumeAttachFake struct {
	*fakeapi.Server
	mu          sync.Mutex
	servers     map[string]bool
	volumes     map[string]string            // ボリューム ID → ステータス
	attachments map[string]map[string]string // サーバー ID → ボリューム ID → デバイス
	bodies      []map[string]any             // アタッチのリクエストの本文
	detached    []string                     // デタッチしたボリューム ID
}

func newVolumeAttachFake(t *testing.T) *volumeAttachFake {
	f := &volumeAttachFake{
		Server:      fakeapi.New(t),
		servers:     map[string]bool{"srv-1": true},
		volumes:     map[string]string{"vol-1": "available", "vol-2": "available"},
		attachments: map[string]map[string]string{"srv-1": {}},
	}
	attachment := func(sid, vid, device string) map[string]any {
		return map[string]any{"volumeAttachment": map[string]any{"id": vid, "serverId": sid, "volumeId": vid, "device": device}}
	}

	f.Mux.HandleFunc("POST /compute/v2.1/servers/{sid}/os-volume_attachments", func(w http.ResponseWriter, r *http.Request) {
		if !fakeapi.Authorized(w, r) {
			return
		}
		var body map[string]any
		if err := fakeapi.ReadJSON(r, &body); err != nil {
			fakeapi.WriteJSON(w, http.StatusBadRequest, map[string]any{"badRequest": map[string]any{"message": err.Error()}})
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.bodies = append(f.bodies, body)
		sid := r.PathValue("sid")
		if !f.servers[sid] {
			fakeapi.WriteJSON(w, http.StatusNotFound, map[string]any{"itemNotFound": map[string]any{"message": "server not found"}})
			return
		}
		vid, _ := body["volumeAttachment"].(map[string]any)["volumeId"].(string)
		if f.volumes[vid] != "available" {
			fakeapi.WriteJSON(w, http.StatusBadRequest, map[string]any{"badRequest": map[string]any{"message": "volume is not available"}})
			return
		}
		// 追加ストレージ用のボリュームは1つまで
		if len(f.attachments[sid]) > 0 {
			fakeapi.WriteJSON(w, http.StatusForbidden, map[string]any{"forbidden": map[string]any{"message": "only one volume can be attached"}})
			return
		}
		f.attachments[sid][vid] = "/dev/vdb"
		f.volumes[vid] = "in-use"
		fakeapi.WriteJSON(w, http.StatusOK, attachment(sid, vid, "/dev/vdb"))
	})

	f.Mux.HandleFunc("GET /compute/v2.1/servers/{sid}/os-volume_attachments/{vid}", func(w http.ResponseWriter, r *http.Request) {
		if !fakeapi.Authorized(w, r) {
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		sid, vid := r.PathValue("sid"), r.PathValue("vid")
		device, ok := f.attachments[sid][vid]
		if !ok {
			fakeapi.WriteJSON(w, http.StatusNotFound, map[string]any{"itemNotFound": map[string]any{"message": "not attached"}})
			return
		}
		fakeapi.WriteJSON(w, http.StatusOK, attachment(sid, vid, device))
	})

	f.Mux.HandleFunc("DELETE /compute/v2.1/servers/{sid}/os-volume_attachments/{vid}", func(w http.ResponseWriter, r *http.Request) {
		if !fakeapi.Authorized(w, r) {
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		sid, vid := r.PathValue("sid"), r.PathValue("vid")
		if _, ok := f.attachments[sid][vid]; !ok {
			fakeapi.WriteJSON(w, http.StatusNotFound, map[string]any{"itemNotFound": map[string]any{"message": "not attached"}})
			return
		}
		delete(f.attachments[sid], vid)
		f.volumes[vid] = "available"
		f.detached = append(f.detached, vid)
		w.WriteHeader(http.StatusAccepted)
	})

	f.Mux.HandleFunc("GET /block-storage/v3/tenant/volumes/{vid}", func(w http.ResponseWriter, r *http.Request) {
		if !fakeapi.Authorized(w, r) {
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		vid := r.PathValue("vid")
		status, ok := f.volumes[vid]
		if !ok {
			fakeapi.WriteJSON(w, http.StatusNotFound, map[string]any{"itemNotFound": map[string]any{"message": "volume not found"}})
			return
		}
		fakeapi.WriteJSON(w, http.StatusOK, map[string]any{"volume": map[string]any{
			"id": vid, "status": status, "size": 200, "volume_type": "c3j1-ds02-add",
			"created_at": "2024-01-01T00:00:00.000000", "updated_at": "2024-01-01T00:00:00.000000",
		}})
	})
	return f
}

func (f *volumeAttachFake) config(volumeID string) string {
	return f.ProviderConfig() + fmt.Sprintf(`
resource "conohavps_volume_attachment" "test" {
  instance_id = "srv-1"
  volume_id   = %q
}
`, volumeID)
}

// 最後のアタッチのリクエストの本文が、API 仕様（NovaAttachVolumeReq）どおり volumeAttachment.volumeId だけであることを確かめる.
func (f *volumeAttachFake) checkLastBody(volumeID string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		if len(f.bodies) == 0 {
			return fmt.Errorf("no attach request was sent")
		}
		want := map[string]any{"volumeAttachment": map[string]any{"volumeId": volumeID}}
		if got := f.bodies[len(f.bodies)-1]; !reflect.DeepEqual(got, want) {
			g, _ := json.Marshal(got)
			return fmt.Errorf("attach request body = %s, want volumeAttachment.volumeId only", g)
		}
		return nil
	}
}

func (f *volumeAttachFake) checkVolumeStatus(volumeID, status string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		if got := f.volumes[volumeID]; got != status {
			return fmt.Errorf("volume %s status = %q, want %q", volumeID, got, status)
		}
		return nil
	}
}

func TestVolumeAttachmentResource_Unit(t *testing.T) {
	f := newVolumeAttachFake(t)
	const addr = "conohavps_volume_attachment.test"

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		CheckDestroy: func(*terraform.State) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			if n := len(f.attachments["srv-1"]); n != 0 {
				return fmt.Errorf("%d volumes are still attached", n)
			}
			return nil
		},
		Steps: []resource.TestStep{
			// アタッチ
			{
				Config: f.config("vol-1"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionCreate)},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("id"), knownvalue.StringExact("srv-1/vol-1")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("device"), knownvalue.StringExact("/dev/vdb")),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					f.checkLastBody("vol-1"),
					f.checkVolumeStatus("vol-1", "in-use"),
				),
			},
			// インポート
			{
				ResourceName:      addr,
				ImportState:       true,
				ImportStateVerify: true,
			},
			// ボリュームを替えると付け替える（先にデタッチしてからアタッチする）
			{
				Config: f.config("vol-2"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionDestroyBeforeCreate)},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("id"), knownvalue.StringExact("srv-1/vol-2")),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					f.checkLastBody("vol-2"),
					f.checkVolumeStatus("vol-1", "available"),
					f.checkVolumeStatus("vol-2", "in-use"),
				),
			},
			// Terraform の外でデタッチされたら、State から外してアタッチし直す
			{
				PreConfig: func() {
					f.mu.Lock()
					defer f.mu.Unlock()
					delete(f.attachments["srv-1"], "vol-2")
					f.volumes["vol-2"] = "available"
				},
				Config: f.config("vol-2"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionCreate)},
				},
				Check: f.checkVolumeStatus("vol-2", "in-use"),
			},
		},
	})
}

func TestVolumeAttachmentResource_InvalidImportID(t *testing.T) {
	f := newVolumeAttachFake(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		Steps: []resource.TestStep{{
			Config:        f.config("vol-1"),
			ResourceName:  "conohavps_volume_attachment.test",
			ImportState:   true,
			ImportStateId: "vol-1",
			ExpectError:   regexp.MustCompile(regexp.QuoteMeta("<instance_id>/<volume_id>")),
		}},
	})
}
