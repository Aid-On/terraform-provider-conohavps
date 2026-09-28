// オブジェクトストレージのリソースの単体テストで使う、偽のオブジェクトストレージ API を提供する.
// ドキュメントのリクエスト（ヘッダーで設定し本文を持たない）とレスポンス（ヘッダーで値を返す）を
// メモリ上の状態で再現し、受けたリクエストを記録して検証できるようにする.

package resource_test

import (
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"testing"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/fakeapi"
)

const objectStorageBase = "/object-storage/v1/AUTH_" + fakeapi.TenantID

// 偽の API が受けたリクエスト.
type recordedRequest struct {
	Method string
	Path   string
	Header http.Header
}

type fakeContainer struct {
	objects          int64
	bytes            int64
	versionsLocation string
	containerRead    string
}

// 偽のオブジェクトストレージ.
type fakeObjectStorage struct {
	*fakeapi.Server
	mu         sync.Mutex
	quotaBytes int64 // x-account-meta-quota-bytes（1GB = 1024^3 byte として返す）
	usedBytes  int64 // コンテナに属さない使用量（テストで使用量を作るため）
	containers map[string]*fakeContainer
	requests   []recordedRequest
}

const gib = int64(1) << 30

func newFakeObjectStorage(t *testing.T) *fakeObjectStorage {
	f := &fakeObjectStorage{Server: fakeapi.New(t), containers: map[string]*fakeContainer{}}

	f.handle("HEAD "+objectStorageBase, func(w http.ResponseWriter, _ *http.Request) {
		var objects, bytes int64
		for _, c := range f.containers {
			objects += c.objects
			bytes += c.bytes
		}
		h := w.Header()
		h.Set("X-Account-Container-Count", strconv.Itoa(len(f.containers)))
		h.Set("X-Account-Object-Count", strconv.FormatInt(objects, 10))
		h.Set("X-Account-Bytes-Used", strconv.FormatInt(bytes+f.usedBytes, 10))
		h.Set("X-Account-Meta-Quota-Bytes", strconv.FormatInt(f.quotaBytes, 10))
		w.WriteHeader(http.StatusNoContent)
	})

	f.handle("POST "+objectStorageBase, func(w http.ResponseWriter, r *http.Request) {
		v, ok := r.Header["X-Account-Meta-Quota-Giga-Bytes"]
		if !ok {
			http.Error(w, "missing X-Account-Meta-Quota-Giga-Bytes", http.StatusBadRequest)
			return
		}
		gb, err := strconv.ParseInt(v[0], 10, 64)
		if err != nil || gb < 0 || gb%100 != 0 {
			http.Error(w, "quota must be specified in units of 100GB", http.StatusBadRequest)
			return
		}
		if gb*gib < f.accountUsed() {
			http.Error(w, "quota cannot be less than the usage", http.StatusBadRequest)
			return
		}
		f.quotaBytes = gb * gib
		w.WriteHeader(http.StatusNoContent)
	})

	f.handle("PUT "+objectStorageBase+"/{container}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("container")
		if _, ok := f.containers[name]; ok {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		f.containers[name] = &fakeContainer{}
		w.WriteHeader(http.StatusCreated)
	})

	f.handle("HEAD "+objectStorageBase+"/{container}", func(w http.ResponseWriter, r *http.Request) {
		c, ok := f.containers[r.PathValue("container")]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		h := w.Header()
		h.Set("X-Container-Object-Count", strconv.FormatInt(c.objects, 10))
		h.Set("X-Container-Bytes-Used", strconv.FormatInt(c.bytes, 10))
		h.Set("X-Storage-Policy", "default-placement")
		h.Set("X-Storage-Class", "STANDARD")
		if c.versionsLocation != "" {
			h.Set("X-Versions-Location", c.versionsLocation)
		}
		if c.containerRead != "" {
			h.Set("X-Container-Read", c.containerRead)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	f.handle("POST "+objectStorageBase+"/{container}", func(w http.ResponseWriter, r *http.Request) {
		c, ok := f.containers[r.PathValue("container")]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if v, ok := r.Header["X-Versions-Location"]; ok {
			if _, exists := f.containers[v[0]]; !exists {
				http.Error(w, "versions location container does not exist", http.StatusBadRequest)
				return
			}
			c.versionsLocation = v[0]
		}
		if _, ok := r.Header["X-Remove-Versions-Location"]; ok {
			c.versionsLocation = ""
		}
		if v, ok := r.Header["X-Container-Read"]; ok {
			c.containerRead = v[0]
		}
		w.WriteHeader(http.StatusNoContent)
	})

	f.handle("DELETE "+objectStorageBase+"/{container}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("container")
		c, ok := f.containers[name]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if c.objects > 0 {
			http.Error(w, "There was a conflict when trying to complete your request.", http.StatusConflict)
			return
		}
		delete(f.containers, name)
		w.WriteHeader(http.StatusNoContent)
	})

	return f
}

// 認証を確かめ、リクエストを記録してから、状態をロックしてハンドラを呼ぶ.
func (f *fakeObjectStorage) handle(pattern string, h http.HandlerFunc) {
	f.Mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		if !fakeapi.Authorized(w, r) {
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.requests = append(f.requests, recordedRequest{Method: r.Method, Path: r.URL.EscapedPath(), Header: r.Header.Clone()})
		h(w, r)
	})
}

func (f *fakeObjectStorage) accountUsed() int64 {
	used := f.usedBytes
	for _, c := range f.containers {
		used += c.bytes
	}
	return used
}

// 条件に合う記録済みリクエストを返す.
func (f *fakeObjectStorage) find(method, path string) []recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	var found []recordedRequest
	for _, r := range f.requests {
		if r.Method == method && r.Path == path {
			found = append(found, r)
		}
	}
	return found
}

// 状態を変える（テストの PreConfig から使う）.
func (f *fakeObjectStorage) with(fn func(f *fakeObjectStorage)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

// 最後の該当リクエストが、ヘッダー key を値 want で持つかを確かめる.
// want が空なら、値が空のヘッダーが送られたこと（ドキュメントの解除の形）を確かめる.
func (f *fakeObjectStorage) expectHeader(method, path, key, want string) error {
	found := f.find(method, path)
	if len(found) == 0 {
		return fmt.Errorf("no %s %s request was sent", method, path)
	}
	v, ok := found[len(found)-1].Header[http.CanonicalHeaderKey(key)]
	if !ok {
		return fmt.Errorf("the last %s %s request has no %s header", method, path, key)
	}
	if v[0] != want {
		return fmt.Errorf("the last %s %s request has %s: %q, want %q", method, path, key, v[0], want)
	}
	return nil
}
