// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package clouddouble is a loopback Supernote private cloud for tests. The
// real sncloud-go client, through snctl's cloud adapter, lists, stats,
// downloads (checked against an MD5, with ranges), uploads, creates folders,
// moves, recycles, restores, deletes for good and reads capacity against it.
// Faults make a request fail, stall, lie about its checksum, drop its
// connection halfway or find its session expired. Identifiers count from 1 and
// times advance one second per change from a fixed instant, so runs repeat
// exactly. Every request is logged without credentials.
package clouddouble

import (
	"bytes"
	"crypto/md5" // #nosec G501 -- the wire protocol's checksum is MD5.
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Token is the synthetic access token the server accepts; any other is refused
// as an expired session.
const Token = "synthetic-token"

// SignedURL is a synthetic secret included in refusal messages to test redaction.
const SignedURL = "https://storage.invalid/object?signature=synthetic-secret"

// Epoch is the time of the first change; each later change is one second on.
const Epoch = 1700000000000

const (
	sizeField       = "size"
	capacity        = 1 << 30
	country         = 44
	storageLimit    = 64 << 20
	codeExpired     = "E0712"
	codeExists      = "E0322"
	codeMissing     = "E0311"
	codeLocked      = "E0078"
	entriesField    = "entriesVO"
	recycleEndpoint = "file/delete"
)

// Entry stores the bytes and location of a synthetic file or folder.
type Entry struct {
	ID, Path        string
	Folder, Deleted bool
	Data            []byte
	Modified        int64
}

// Request is one request as the server saw it: method, URL path and the tablet
// path it addressed, when it named one.
type Request struct{ Method, Path, Target string }

// Fault applies to the next Count requests whose URL path or target path is
// Path. Status answers with that HTTP status, Delay stalls (and respects
// cancellation), Expired refuses the session, Checksum announces a wrong MD5
// for a download and Drop cuts a download's body in half.
type Fault struct {
	Before       func()
	LostResponse bool
	// FinishStoredError models a committed upload whose finish response fails.
	FinishStoredError bool
	// FinishStoredRejected reproduces a committed upload with a server-error envelope.
	FinishStoredRejected    bool
	Path                    string
	Method                  string
	UntilCleared            bool
	Code                    string
	Count                   int
	Status                  int
	Delay                   time.Duration
	Drop, Checksum, Expired bool
}

// writing are the endpoints that change state on the server.
//
//nolint:gochecknoglobals // a fixed table of the protocol
var writing = map[string]bool{
	"/api/file/2/files/create_folder_v2": true, "/api/file/3/files/upload/apply": true, "/api/file/2/files/upload/finish": true,
	"/storage/upload": true, "/api/file/3/files/move_v3": true, "/api/file/3/files/copy_v3": true,
	"/api/file/3/files/delete_folder_v3": true, "/api/file/delete": true, "/api/file/recycle/revert": true,
	"/api/file/recycle/delete": true, "/api/file/recycle/clear": true,
	"/api/file/2/files/synchronous/start": true, "/api/file/2/files/synchronous/end": true,
}

// Cloud owns an isolated HTTP server and serializes all mutable protocol state.
type Cloud struct {
	server            *httptest.Server
	mu                sync.Mutex
	next              int
	clock             int64
	entries           map[string]Entry
	objects           map[string][]byte
	requests          []Request
	faults            []Fault
	lock              string
	lockUntil         time.Time
	refuseLockedMoves bool
	mode              UploadMode
	visibility        VisibilityMode
	hidden            map[string]bool
	applied           map[string]body
	parts             map[string]*formUpload
}

// New listens on loopback only and needs no login.
func New() *Cloud {
	c := &Cloud{entries: map[string]Entry{}, objects: map[string][]byte{}, clock: Epoch, applied: map[string]body{}, parts: map[string]*formUpload{}}
	c.server = httptest.NewServer(http.HandlerFunc(c.serve))
	return c
}

// URL is the private-cloud endpoint to give the real adapter.
func (c *Cloud) URL() string { return c.server.URL }

// Close releases the listener after clients finish.
func (c *Cloud) Close() { c.server.Close() }

// Seed creates or replaces one exact path, with every missing ancestor
// folder, and returns its identifier. The bytes are copied.
func (c *Cloud) Seed(name string, folder bool, data []byte) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	name = strings.Trim(name, "/")
	var missing []string
	for dir := path.Dir(name); dir != "."; dir = path.Dir(dir) {
		if _, ok := c.find(dir); !ok {
			missing = append(missing, dir)
		}
	}
	for _, dir := range slices.Backward(missing) {
		c.add(dir, true, nil)
	}
	return c.add(name, folder, data).ID
}

// Remove deletes a path and everything below it, as another device would.
func (c *Cloud) Remove(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.find(name); ok {
		c.change(e.ID, "delete")
	}
}

// File returns the bytes at a live path.
func (c *Cloud) File(name string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.find(name)
	return bytes.Clone(e.Data), ok && !e.Folder
}

// Paths lists every live path in order.
func (c *Cloud) Paths() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for _, e := range c.ordered() {
		if !e.Deleted {
			out = append(out, e.Path)
		}
	}
	return out
}

// Hold takes the account sync lock for another device, or releases it with "".
func (c *Cloud) Hold(device string) { c.mu.Lock(); defer c.mu.Unlock(); c.lock = device }

// RefuseLockedMoves makes move_v3 reject requests during any active sync session.
func (c *Cloud) RefuseLockedMoves(refuse bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.refuseLockedMoves = refuse
}

// Inject schedules a fault without racing active requests.
func (c *Cloud) Inject(f Fault) { c.mu.Lock(); defer c.mu.Unlock(); c.faults = append(c.faults, f) }

// Requests returns a copy of the request log, storage transfers included.
func (c *Cloud) Requests() []Request {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.requests)
}

// Writes is the part of the request log that changed or would change state:
// folder creation, upload, move, copy, deletion, recycling and the sync lock.
func (c *Cloud) Writes() []Request {
	var out []Request
	for _, r := range c.Requests() {
		if writing[r.Path] {
			out = append(out, r)
		}
	}
	return out
}

// Count is how many logged requests went to the URL path.
func (c *Cloud) Count(urlPath string) int {
	n := 0
	for _, r := range c.Requests() {
		if r.Path == urlPath {
			n++
		}
	}
	return n
}

func (c *Cloud) add(name string, folder bool, data []byte) Entry {
	name = strings.Trim(name, "/")
	e, ok := c.find(name)
	if !ok {
		c.next++
		e.ID = strconv.Itoa(c.next)
	}
	c.clock += int64(time.Second / time.Millisecond)
	e.Path, e.Folder, e.Data, e.Deleted, e.Modified = name, folder, bytes.Clone(data), false, c.clock
	c.entries[e.ID] = e
	return e
}

func (c *Cloud) find(name string) (Entry, bool) {
	name = strings.Trim(name, "/")
	for _, e := range c.entries {
		if e.Path == name && !e.Deleted {
			return e, true
		}
	}
	return Entry{}, false
}

// Sum is the content hash the protocol uses: lowercase hexadecimal MD5.
func Sum(data []byte) string {
	sum := md5.Sum(data) // #nosec G401 -- the protocol checksum, never authentication.
	return hex.EncodeToString(sum[:])
}

func row(e Entry) map[string]any {
	tag := "file"
	if e.Folder {
		tag = "folder"
	}
	return map[string]any{"id": e.ID, "name": path.Base(e.Path), "path_display": e.Path, "tag": tag, sizeField: len(e.Data),
		"content_hash": Sum(e.Data), "lastUpdateTime": e.Modified}
}

func reply(w http.ResponseWriter, fields map[string]any) {
	if fields == nil {
		fields = map[string]any{}
	}
	fields["success"] = true
	w.Header().Set("Content-Type", "application/json")
	_ = json.MarshalWrite(w, fields) //nolint:errcheck // the client sees a short answer
}

func refuse(w http.ResponseWriter, code string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.MarshalWrite(w, map[string]any{"success": false, "errorCode": code, "errorMsg": Token + " " + SignedURL}) //nolint:errcheck // as above
}

type body struct {
	ID        jsontext.Value   `json:"id"`
	IDs       []jsontext.Value `json:"idList"`
	Path      string           `json:"path"`
	To        string           `json:"to_path"`
	Name      string           `json:"fileName"`
	Inner     string           `json:"innerName"`
	Equipment string           `json:"equipmentNo"`
	Size      int64            `json:"size"`
	Hash      string           `json:"content_hash"`
	Page      int              `json:"pageNo"`
	PageSize  int              `json:"pageSize"`
	Recursive bool             `json:"recursive"`
}

func identifier(v jsontext.Value) string { return strings.Trim(string(v), `"`) }

func (c *Cloud) serve(w http.ResponseWriter, r *http.Request) {
	var b body
	storage := strings.HasPrefix(r.URL.Path, "/storage/") || r.URL.Path == "/api/oss/download"
	if !storage {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get("x-access-token") != Token {
			refuse(w, codeExpired)
			return
		}
		if strings.Contains(r.URL.Path, "/files/") &&
			(r.Header.Get("version") != "202407" || r.Header.Get("equipmentNo") == "" || r.Header.Get("channel") == "") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if err := json.UnmarshalRead(r.Body, &b); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
	}
	if storage {
		if r.Method == http.MethodGet && r.Header.Get("x-access-token") != "" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if r.Method == http.MethodPost && r.Header.Get("x-access-token") != Token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
	}
	fault := c.log(r, b)
	if fault.Before != nil {
		fault.Before()
	}
	if !waitFault(r, fault, storage) {
		return
	}
	if c.failStoredFinish(w, r, b, fault) {
		return
	}
	if fault.LostResponse {
		c.loseResponse(w, r, b)
		return
	}
	switch {
	case fault.Expired:
		if storage {
			w.WriteHeader(http.StatusUnauthorized)
		} else {
			refuse(w, codeExpired)
		}
	case fault.Code != "":
		refuse(w, fault.Code)
	case fault.Status != 0:
		w.WriteHeader(fault.Status)
	case r.URL.Path == "/storage/upload" || r.URL.Path == "/storage/part":
		c.receive(w, r)
	case storage:
		c.send(w, r, fault)
	default:
		c.mu.Lock()
		defer c.mu.Unlock()
		c.api(w, r, b, fault)
	}
}

// log records the request and claims the first fault that matches it.
func (c *Cloud) log(r *http.Request, b body) Fault {
	c.mu.Lock()
	defer c.mu.Unlock()
	target := strings.Trim(b.Path, "/")
	if e, ok := c.entries[identifier(b.ID)]; ok {
		target = e.Path
	}
	if e, ok := c.entries[r.URL.Query().Get("id")]; ok {
		target = e.Path
	}
	c.requests = append(c.requests, Request{r.Method, r.URL.Path, target})
	for i, f := range c.faults {
		if (f.Count > 0 || f.UntilCleared) && (f.Method == "" || f.Method == r.Method) &&
			(f.Path == r.URL.Path || f.Path == target && target != "") && (!f.Drop || storageDownload(r.URL.Path)) {
			c.faults[i].Count--
			return f
		}
	}
	return Fault{}
}

func (c *Cloud) api(w http.ResponseWriter, r *http.Request, b body, fault Fault) {
	endpoint := strings.TrimPrefix(r.URL.Path, "/api/")
	id := identifier(b.ID)
	if c.mutationLocked(endpoint) {
		refuse(w, codeLocked)
		return
	}
	switch endpoint {
	case "file/2/files/list_folder":
		c.list(w, b)
	case "file/3/files/query_v3", "file/3/files/query/by/path_v3":
		e, ok := c.entries[id]
		if b.Path != "" {
			e, ok = c.find(b.Path)
		}
		if !ok || e.Deleted || c.hidden[e.ID] {
			reply(w, map[string]any{entriesField: nil})
			return
		}
		reply(w, map[string]any{entriesField: row(e)})
	case "file/2/files/create_folder_v2":
		parent, ok := c.find(path.Dir(strings.Trim(b.Path, "/")))
		if _, exists := c.find(b.Path); exists || !ok || !parent.Folder {
			refuse(w, map[bool]string{true: codeExists, false: codeMissing}[exists])
			return
		}
		reply(w, map[string]any{"metadata": row(c.add(b.Path, true, nil))})
	case "file/3/files/download_v3":
		e, ok := c.entries[id]
		if !ok || e.Folder {
			refuse(w, codeMissing)
			return
		}
		sum := Sum(e.Data)
		if fault.Checksum {
			sum = strings.Repeat("0", hex.EncodedLen(md5.Size))
		}
		reply(w, map[string]any{"url": c.URL() + "/api/oss/download?id=" + id, sizeField: len(e.Data), "content_hash": sum})
	case "file/3/files/upload/apply":
		c.next++
		key := strconv.Itoa(c.next)
		c.applied[key] = b
		reply(w, map[string]any{"apiServer": "private_cloud", "innerName": key,
			"fullUploadUrl": c.URL() + "/storage/upload?key=" + key, "partUploadUrl": c.URL() + "/storage/part?key=" + key})
	case "file/2/files/upload/finish":
		c.finish(w, b)
	case "file/3/files/move_v3", "file/3/files/copy_v3":
		c.move(w, b, endpoint)
	case "file/3/files/delete_folder_v3":
		c.change(id, "delete")
		reply(w, nil)
	case recycleEndpoint, "file/recycle/revert", "file/recycle/delete":
		c.recycle(w, b, endpoint)
	case "file/recycle/clear":
		for key, e := range c.entries {
			if e.Deleted {
				delete(c.entries, key)
			}
		}
		reply(w, nil)
	case "file/recycle/list/query":
		c.recycled(w, b)
	case "file/path/query":
		c.location(w, id)
	case "file/2/users/get_space_usage":
		reply(w, map[string]any{"used": c.used(), "allocationVO": map[string]any{"allocated": capacity}})
	case "file/capacity/query":
		reply(w, map[string]any{"usedCapacity": c.used(), "totalCapacity": capacity})
	case "user/query":
		reply(w, map[string]any{"userId": "1", "userName": "Synthetic", "email": "test@example.invalid", "countryCode": country})
	case "file/2/files/synchronous/start", "file/2/files/synchronous/end":
		c.syncLock(w, b, strings.HasSuffix(endpoint, "/start"))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (c *Cloud) mutationLocked(endpoint string) bool {
	if c.lock == "" || !c.lockUntil.IsZero() && !lockLive(c.lockUntil) {
		return false
	}
	return endpoint == recycleEndpoint || endpoint == "file/3/files/move_v3" && c.refuseLockedMoves
}

// list answers a path listing, recursive or not, as the private cloud does:
// path_display is each entry's own full path.
func (c *Cloud) list(w http.ResponseWriter, b body) {
	folder := strings.Trim(b.Path, "/")
	if folder != "" {
		if e, ok := c.find(folder); !ok || !e.Folder {
			refuse(w, codeMissing)
			return
		}
	}
	entries := make([]map[string]any, 0)
	for _, e := range c.ordered() {
		parent := path.Dir(e.Path)
		if parent == "." {
			parent = ""
		}
		inside := folder == "" || strings.HasPrefix(e.Path, folder+"/")
		if !e.Deleted && !c.hidden[e.ID] && (parent == folder || b.Recursive && inside) {
			entry := row(e)
			// Private Cloud folder listings omit the full tree's client time.
			if !b.Recursive {
				delete(entry, "lastUpdateTime")
			}
			entries = append(entries, entry)
		}
	}
	reply(w, map[string]any{"entries": entries})
}

func (c *Cloud) finish(w http.ResponseWriter, b body) {
	data, ok := c.objects[b.Inner]
	apply, applied := c.applied[b.Inner]
	if !applied || apply.Path != b.Path || apply.Name != b.Name || apply.Size != b.Size || int64(len(data)) != b.Size || Sum(data) != b.Hash {
		refuse(w, "E0300")
		return
	}
	parent, found := c.find(strings.Trim(b.Path, "/"))
	if !ok || !found || !parent.Folder {
		refuse(w, codeMissing)
		return
	}
	delete(c.objects, b.Inner)
	delete(c.applied, b.Inner)
	if e, exists := c.find(b.Path + b.Name); exists && e.Folder {
		refuse(w, "E0335")
		return
	}
	if _, exists := c.find(b.Path + b.Name); exists && c.mode == RefuseOverwrite {
		refuse(w, "E0335")
		return
	}
	reply(w, row(c.upload(b.Path+b.Name, data)))
}

func (c *Cloud) recycled(w http.ResponseWriter, b body) {
	entries := make([]map[string]any, 0)
	for _, e := range c.ordered() {
		if e.Deleted {
			entries = append(entries, map[string]any{"fileId": e.ID, "fileName": path.Base(e.Path), "isFolder": e.Folder,
				sizeField: len(e.Data), "deleteTime": e.Modified})
		}
	}
	total := len(entries)
	pageSize := b.PageSize
	if pageSize <= 0 {
		pageSize = 100
	}
	start := min(max(b.Page-1, 0)*pageSize, total)
	entries = entries[start:min(start+pageSize, total)]
	reply(w, map[string]any{"recycleFileVOList": entries, "total": total})
}

func (c *Cloud) location(w http.ResponseWriter, id string) {
	e, ok := c.entries[id]
	if !ok {
		refuse(w, codeMissing)
		return
	}
	var names, ids []string
	parent := ""
	for part := range strings.SplitSeq(path.Dir(e.Path), "/") {
		parent = path.Join(parent, part)
		folder, found := c.find(parent)
		if !found {
			refuse(w, codeMissing)
			return
		}
		names, ids = append(names, part), append(ids, folder.ID)
	}
	// Location includes the queried entry, even when it is recycled.
	names, ids = append(names, path.Base(e.Path)), append(ids, e.ID)
	reply(w, map[string]any{"path": strings.Join(names, "/"), "idPath": strings.Join(ids, "/")})
}

func (c *Cloud) ordered() []Entry {
	entries := make([]Entry, 0, len(c.entries))
	for _, e := range c.entries {
		entries = append(entries, e)
	}
	slices.SortFunc(entries, func(a, b Entry) int { return strings.Compare(a.Path, b.Path) })
	return entries
}

func (c *Cloud) used() int64 {
	var n int64
	for _, e := range c.entries {
		n += int64(len(e.Data))
	}
	return n
}

// change applies a deletion, recycling or restoration to an entry and
// everything below it.
func (c *Cloud) change(id, mode string) {
	top, ok := c.entries[id]
	if !ok {
		return
	}
	for key, e := range c.entries {
		if key != id && !strings.HasPrefix(e.Path, top.Path+"/") {
			continue
		}
		if mode == "delete" {
			delete(c.entries, key)
			continue
		}
		e.Deleted = mode == "recycle"
		c.clock += int64(time.Second / time.Millisecond)
		e.Modified = c.clock
		c.entries[key] = e
	}
}

// receive stores an upload's bytes without holding the state lock while the
// body arrives, as a storage server would.
func (c *Cloud) receive(w http.ResponseWriter, r *http.Request) {
	reader, err := r.MultipartReader()
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	part, err := reader.NextPart()
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	data, err := io.ReadAll(io.LimitReader(part, storageLimit))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	key := r.URL.Query().Get("key")
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.applied[key]; !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if r.URL.Path == "/storage/part" {
		c.part(w, r, key, data)
		return
	}
	c.objects[key] = data
	reply(w, map[string]any{"innerName": key, "md5": Sum(data)})
}

// send serves a download, with ranges, outside the state lock.
func (c *Cloud) send(w http.ResponseWriter, r *http.Request, fault Fault) {
	c.mu.Lock()
	e, ok := c.entries[r.URL.Query().Get("id")]
	c.mu.Unlock()
	if !ok || e.Folder {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if fault.Drop {
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		conn, rw, err := hijacker.Hijack()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_, _ = fmt.Fprintf(rw, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\n\r\n", len(e.Data)) //nolint:errcheck // a broken answer on purpose
		_, _ = rw.Write(e.Data[:len(e.Data)/2])                                              //nolint:errcheck // as above
		_ = rw.Flush()                                                                       //nolint:errcheck // as above
		return
	}
	http.ServeContent(w, r, path.Base(e.Path), time.UnixMilli(e.Modified), bytes.NewReader(e.Data))
}

func (c *Cloud) move(w http.ResponseWriter, b body, endpoint string) {
	id := identifier(b.ID)
	e, ok := c.entries[id]
	if !ok || e.Deleted || c.hidden[e.ID] {
		refuse(w, codeMissing)
		return
	}
	to := strings.Trim(b.To, "/")
	if to == e.Path || strings.HasPrefix(to, e.Path+"/") {
		refuse(w, "E0337")
		return
	}
	if _, exists := c.find(to); exists {
		refuse(w, "E0337")
		return
	}
	if parent, found := c.find(path.Dir(to)); !found || !parent.Folder {
		refuse(w, codeMissing)
		return
	}
	if endpoint == "file/3/files/copy_v3" {
		e = c.add(to, e.Folder, e.Data)
	} else {
		old := e.Path
		c.clock += int64(time.Second / time.Millisecond)
		e.Path, e.Modified = to, c.clock
		c.entries[id] = e
		for key, child := range c.entries {
			if strings.HasPrefix(child.Path, old+"/") {
				child.Path = to + strings.TrimPrefix(child.Path, old)
				c.entries[key] = child
			}
		}
	}
	reply(w, map[string]any{entriesField: row(e)})
}

func (c *Cloud) recycle(w http.ResponseWriter, b body, endpoint string) {
	mode := map[string]string{recycleEndpoint: "recycle", "file/recycle/revert": "restore", "file/recycle/delete": "delete"}[endpoint]
	for _, value := range b.IDs {
		e, ok := c.entries[identifier(value)]
		if mode == "restore" {
			if _, exists := c.find(e.Path); exists {
				refuse(w, "E0338")
				return
			}
		}
		if !ok {
			refuse(w, codeMissing)
			return
		}
	}
	for _, value := range b.IDs {
		c.change(identifier(value), mode)
	}
	reply(w, nil)
}

func (c *Cloud) syncLock(w http.ResponseWriter, b body, start bool) {
	if start {
		if c.lock != "" && (c.lockUntil.IsZero() || lockLive(c.lockUntil)) {
			refuse(w, codeLocked)
			return
		}
		c.lock = b.Equipment
		reply(w, map[string]any{"synType": true})
		return
	}
	if c.lock != "" && c.lock != b.Equipment {
		refuse(w, codeLocked)
		return
	}
	c.lock = ""
	c.hidden = nil
	reply(w, nil)
}

func (c *Cloud) loseResponse(w http.ResponseWriter, r *http.Request, b body) {
	c.mu.Lock()
	c.api(httptest.NewRecorder(), r, b, Fault{})
	c.mu.Unlock()
	if hijacker, ok := w.(http.Hijacker); ok {
		conn, _, err := hijacker.Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}
}

func waitFault(r *http.Request, fault Fault, storage bool) bool {
	if fault.Delay > 0 {
		if storage && r.Method == http.MethodPost {
			data, err := io.ReadAll(io.LimitReader(r.Body, storageLimit))
			if err != nil {
				return false
			}
			_ = r.Body.Close()
			r.Body = io.NopCloser(bytes.NewReader(data))
		}
		timer := time.NewTimer(fault.Delay)
		defer timer.Stop()
		select {
		case <-r.Context().Done():
			return false
		case <-timer.C:
		}
	}
	return true
}

// failStoredFinish commits the upload before returning either observed failure shape.
func (c *Cloud) failStoredFinish(w http.ResponseWriter, r *http.Request, b body, fault Fault) bool {
	if (!fault.FinishStoredError && !fault.FinishStoredRejected) || r.URL.Path != "/api/file/2/files/upload/finish" {
		return false
	}
	c.mu.Lock()
	c.finish(httptest.NewRecorder(), b)
	if fault.FinishStoredRejected && c.visibility == HiddenUntilUnlock && c.lock != "" {
		if e, ok := c.find(path.Join(b.Path, b.Name)); ok {
			if c.hidden == nil {
				c.hidden = map[string]bool{}
			}
			c.hidden[e.ID] = true
		}
	}
	c.mu.Unlock()
	if fault.FinishStoredRejected {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":false,"errorCode":"500","errorMsg":"The system is temporarily unable to process your request. ` +
			`Please try again later. If you need any further assistance, please upload logs and send feedback."}`))
	} else {
		w.WriteHeader(http.StatusInternalServerError)
	}
	return true
}

// AssertRequests checks the exact request count since a saved log position.
// Its small reporting interface also works in benchmarks and external suites.
func (c *Cloud) AssertRequests(t interface {
	Helper()
	Fatalf(string, ...any)
}, since, want int) {
	t.Helper()
	got := c.Requests()[since:]
	if len(got) != want {
		t.Fatalf("requests: got %d, want %d: %+v", len(got), want, got)
	}
}
