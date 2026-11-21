// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package clouddouble

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
)

func call(t *testing.T, c *Cloud, endpoint, body string) map[string]any {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, c.URL()+"/api/"+endpoint, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("x-access-token", Token)
	req.Header.Set("version", "202407")
	req.Header.Set("equipmentNo", "synthetic-device")
	req.Header.Set("channel", "synthetic-channel")
	res, err := c.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	if err = json.UnmarshalRead(res.Body, &out); err != nil {
		t.Fatal(endpoint, err)
	}
	return out
}

func TestProtocol(t *testing.T) {
	c := New()
	defer c.Close()
	id := c.Seed("NOTE/Note/a.note", false, []byte("abcdef"))
	c.Seed("NOTE/Note/a.note", false, []byte("abcdef"))
	for _, tc := range []struct {
		endpoint, body string
		success        bool
	}{
		{"file/3/files/query_v3", `{"id":` + id + `}`, true},
		{"file/3/files/query/by/path_v3", `{"path":"/NOTE/Note/a.note"}`, true},
		{"file/2/files/create_folder_v2", `{"path":"NOTE/Note"}`, false},
		{"file/2/files/create_folder_v2", `{"path":"NOTE/Missing/x"}`, false},
		{"file/3/files/download_v3", `{"id":999}`, false},
		{"file/3/files/move_v3", `{"id":999}`, false},
		{"file/3/files/move_v3", `{"id":` + id + `,"to_path":"NOTE/Note"}`, false},
		{"file/3/files/move_v3", `{"id":` + id + `,"to_path":"NOTE/Missing/x"}`, false},
		{"file/3/files/copy_v3", `{"id":` + id + `,"to_path":"NOTE/Note/copy.note"}`, true},
		{"file/3/files/move_v3", `{"id":2,"to_path":"NOTE/Renamed"}`, true},
		{"file/2/files/list_folder", `{"path":"/NOTE/Missing"}`, false},
		{"file/2/files/upload/finish", `{"path":"NOTE/Renamed/","fileName":"x","innerName":"none"}`, false},
		{"file/recycle/delete", `{"idList":[999]}`, false},
		{"file/recycle/delete", `{"idList":[` + id + `]}`, true},
		{"file/3/files/delete_folder_v3", `{"id":999}`, true},
		{"file/2/users/get_space_usage", `{}`, true},
		{"file/capacity/query", `{}`, true},
		{"user/query", `{}`, true},
		{"file/2/files/synchronous/start", `{"equipmentNo":"one"}`, true},
		{"file/2/files/synchronous/start", `{"equipmentNo":"two"}`, false},
		{"file/2/files/synchronous/end", `{"equipmentNo":"two"}`, false},
		{"file/2/files/synchronous/end", `{"equipmentNo":"one"}`, true},
	} {
		if out := call(t, c, tc.endpoint, tc.body); out["success"] != tc.success {
			t.Fatalf("%s %s: %v", tc.endpoint, tc.body, out)
		}
	}
	if out := call(t, c, "file/3/files/query_v3", `{"id":999}`); out["entriesVO"] != nil {
		t.Fatal(out)
	}
	// A listing of a folder, recursive or not.
	c.Seed("NOTE/Renamed/sub/deep.note", false, []byte("d"))
	for recursive, want := range map[string]int{"false": 2, "true": 3} {
		out := call(t, c, "file/2/files/list_folder", `{"path":"/NOTE/Renamed","recursive":`+recursive+`}`)
		if entries, _ := out["entries"].([]any); len(entries) != want {
			t.Fatal(recursive, out)
		}
	}
	id = c.Seed("NOTE/Note/range", false, []byte("abcdef"))
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, c.URL()+"/storage/download?id="+id, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Range", "bytes=1-3")
	res, err := c.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if err != nil || res.StatusCode != http.StatusPartialContent || string(data) != "bcd" {
		t.Fatal(string(data), err)
	}
	for _, tc := range []struct {
		method, path, body, token string
		status                    int
	}{
		{"GET", "/api/unknown", "", Token, 405},
		{"POST", "/api/unknown", "{}", Token, 404},
		{"POST", "/api/unknown", "{", Token, 400},
		{"POST", "/api/unknown", "{}", "", 200},
		{"GET", "/storage/download?id=999", "", "", 404},
		{"POST", "/storage/upload", "", "", 401},
	} {
		req, err := http.NewRequestWithContext(t.Context(), tc.method, c.URL()+tc.path, bytes.NewBufferString(tc.body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("x-access-token", tc.token)
		res, err := c.server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		if res.StatusCode != tc.status {
			t.Fatal(tc, res.StatusCode)
		}
	}
	// A multipart body without a part is refused.
	var form bytes.Buffer
	mw := multipart.NewWriter(&form)
	_ = mw.Close()
	req, err = http.NewRequestWithContext(t.Context(), http.MethodPost, c.URL()+"/storage/upload?key=1", &form)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("x-access-token", Token)
	res, err = c.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatal(res.StatusCode)
	}
}

func TestStateHelpers(t *testing.T) {
	c := New()
	defer c.Close()
	c.Seed("DOCUMENT/Document/a.pdf", false, []byte("a"))
	if data, ok := c.File("DOCUMENT/Document/a.pdf"); !ok || string(data) != "a" {
		t.Fatal(data, ok)
	}
	if _, ok := c.File("DOCUMENT/Document"); ok {
		t.Fatal("folder read as a file")
	}
	c.Remove("DOCUMENT/Document")
	if len(c.Paths()) != 1 {
		t.Fatal(c.Paths())
	}
	c.Remove("absent")
	c.Hold("tablet")
	if out := call(t, c, "file/2/files/synchronous/start", `{"equipmentNo":"one"}`); out["success"] != false {
		t.Fatal(out)
	}
	if len(c.Writes()) != 1 || c.Count("/api/file/2/files/synchronous/start") != 1 {
		t.Fatal(c.Requests())
	}
}

func TestMutationsDuringSyncSession(t *testing.T) {
	for _, refuseMoves := range []bool{false, true} {
		for _, owner := range []string{"synthetic-device", "tablet"} {
			t.Run(fmt.Sprintf("refuse-moves=%t/owner=%s", refuseMoves, owner), func(t *testing.T) {
				c := New()
				defer c.Close()
				c.RefuseLockedMoves(refuseMoves)
				id := c.Seed("DOCUMENT/Document/a.pdf", false, []byte("a"))
				c.Hold(owner)
				out := call(t, c, "file/delete", `{"idList":[`+id+`]}`)
				if out["success"] != false || out["errorCode"] != codeLocked {
					t.Fatal("recycle accepted under sync lock", out)
				}
				out = call(t, c, "file/3/files/move_v3", `{"id":`+id+`,"to_path":"DOCUMENT/Document/b.pdf"}`)
				if out["success"] != !refuseMoves || refuseMoves && out["errorCode"] != codeLocked {
					t.Fatal("incorrect move policy", out)
				}
				c.Hold("")
				if out = call(t, c, "file/delete", `{"idList":[`+id+`]}`); out["success"] != true {
					t.Fatal("recycle refused after unlock", out)
				}
			})
		}
	}
}

func TestRecycledLocationIncludesEntry(t *testing.T) {
	c := New()
	defer c.Close()
	root := c.Seed("DOCUMENT", true, nil)
	parent := c.Seed("DOCUMENT/rtest", true, nil)
	id := c.Seed("DOCUMENT/rtest/Report.pdf", false, []byte("document"))
	if out := call(t, c, recycleEndpoint, `{"idList":[`+id+`]}`); out["success"] != true {
		t.Fatal("recycle failed", out)
	}
	out := call(t, c, "file/path/query", `{"id":`+id+`}`)
	if out["path"] != "DOCUMENT/rtest/Report.pdf" || out["idPath"] != root+"/"+parent+"/"+id {
		t.Fatalf("recycled location = %v, want path and IDs ending with the entry", out)
	}
}
