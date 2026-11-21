// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package clouddouble

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"testing"
)

func store(t *testing.T, c *Cloud, key, token string, data []byte) int {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "file.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, c.URL()+"/storage/upload?key="+key, &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("x-access-token", token)
	response, err := c.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	return response.StatusCode
}

func TestUploadValidation(t *testing.T) {
	c := New()
	defer c.Close()
	c.Seed("EXPORT", true, nil)
	if status := store(t, c, "unknown", Token, []byte("bytes")); status != http.StatusNotFound {
		t.Fatal(status)
	}
	applied := call(t, c, "file/3/files/upload/apply", `{"path":"EXPORT/","fileName":"x.pdf","size":5}`)
	key := applied["innerName"].(string)
	if status := store(t, c, key, "", []byte("bytes")); status != http.StatusUnauthorized {
		t.Fatal(status)
	}
	if status := store(t, c, key, Token, []byte("bytes")); status != http.StatusOK {
		t.Fatal(status)
	}
	for _, hash := range []string{"", Sum([]byte("wrong"))} {
		result := call(t, c, "file/2/files/upload/finish", fmt.Sprintf(`{"path":"EXPORT/","fileName":"x.pdf","size":5,"innerName":%q,"content_hash":%q}`, key, hash))
		if result["success"] != false {
			t.Fatal(result)
		}
	}
	result := call(t, c, "file/2/files/upload/finish", fmt.Sprintf(`{"path":"EXPORT/","fileName":"x.pdf","size":4,"innerName":%q,"content_hash":%q}`, key, Sum([]byte("bytes"))))
	if result["success"] != false {
		t.Fatal(result)
	}
	result = call(t, c, "file/2/files/upload/finish", fmt.Sprintf(`{"path":"EXPORT/","fileName":"x.pdf","size":5,"innerName":%q,"content_hash":%q}`, key, Sum([]byte("bytes"))))
	if result["success"] != true {
		t.Fatal(result)
	}
}

func TestStorageAuthenticationAndFaultMatching(t *testing.T) {
	c := New()
	defer c.Close()
	a := c.Seed("EXPORT/a.pdf", false, []byte("a"))
	b := c.Seed("EXPORT/b.pdf", false, []byte("b"))
	get := func(id, token string) int {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, c.URL()+"/api/oss/download?id="+id, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("x-access-token", token)
		response, err := c.server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		return response.StatusCode
	}
	if status := get(a, Token); status != http.StatusForbidden {
		t.Fatal(status)
	}
	c.Inject(Fault{Method: "GET", Path: "EXPORT/a.pdf", UntilCleared: true, Expired: true})
	if status := get(b, ""); status != http.StatusOK {
		t.Fatal(status)
	}
	for range 2 {
		if status := get(a, ""); status != http.StatusUnauthorized {
			t.Fatal(status)
		}
	}
	c.ClearFaults()
	if status := get(a, ""); status != http.StatusOK {
		t.Fatal(status)
	}
}

func TestRecyclePagingAndCollision(t *testing.T) {
	c := New()
	defer c.Close()
	id := c.Seed("EXPORT/a", false, []byte("a"))
	call(t, c, "file/delete", `{"idList":["`+id+`"]}`)
	c.Seed("EXPORT/a", false, []byte("new"))
	if out := call(t, c, "file/recycle/revert", `{"idList":["`+id+`"]}`); out["errorCode"] != "E0338" {
		t.Fatal(out)
	}
	other := c.Seed("EXPORT/b", false, []byte("b"))
	call(t, c, "file/delete", `{"idList":["`+other+`"]}`)
	for _, page := range []int{1, 2, 3} {
		out := call(t, c, "file/recycle/list/query", fmt.Sprintf(`{"pageNo":%d,"pageSize":1}`, page))
		rows := out["recycleFileVOList"].([]any)
		want := 1
		if page == 3 {
			want = 0
		}
		if len(rows) != want {
			t.Fatal(out)
		}
	}
}

func TestRequiredDeviceHeaders(t *testing.T) {
	c := New()
	defer c.Close()
	for _, missing := range []string{"version", "equipmentNo", "channel"} {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, c.URL()+"/api/file/2/files/list_folder", bytes.NewBufferString(`{"path":"/"}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("x-access-token", Token)
		req.Header.Set("version", "202407")
		req.Header.Set("equipmentNo", "synthetic")
		req.Header.Set("channel", "synthetic")
		req.Header.Del(missing)
		response, err := c.server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatal(missing, response.StatusCode)
		}
	}
}
