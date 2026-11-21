// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cloud

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/altessa-s/snctl/internal/domain"
	"github.com/altessa-s/snctl/internal/port"

	"golang.org/x/text/unicode/norm"

	sncloud "github.com/altessa-s/sncloud-go"
)

func deviceEntry(e sncloud.DeviceEntry) port.Entry {
	return port.Entry{ID: e.ID.String(), Name: e.Name, Path: e.Path, Folder: e.IsFolder,
		Size: e.Size, Hash: strings.ToLower(e.ContentHash), Modified: e.Modified}
}

// List reads the tablet's path-based tree.
func (c *Client) List(ctx context.Context, folder string, recursive bool) ([]port.Entry, error) {
	entries, err := c.tree(ctx)
	if err != nil {
		return nil, err
	}
	folder = cleanPath(folder)
	if folder != "" {
		e, err := resolve(entries, folder)
		if err != nil {
			if c.offline && errors.Is(err, port.ErrNotFound) {
				return nil, fmt.Errorf("offline: %s is missing from cached listing", folder)
			}
			return nil, err
		}
		if !e.IsFolder {
			return nil, port.ErrNotFound
		}
		folder = e.Path
	}
	out := make([]port.Entry, 0)
	for _, e := range entries {
		parent := deviceParent(e.Path)
		if parent == folder || (recursive && (folder == "" || strings.HasPrefix(e.Path, folder+"/"))) {
			out = append(out, deviceEntry(e))
		}
	}
	return out, nil
}

func cleanPath(name string) string {
	return strings.Trim(path.Clean("/"+name), "/")
}

// resolve follows the device tree one segment at a time, preferring exact bytes
// to a unique NFC-equivalent spelling. Returned entries retain the device bytes.
func resolve(entries []sncloud.DeviceEntry, name string) (sncloud.DeviceEntry, error) {
	entry, _, err := resolveWithParent(entries, name)
	return entry, err
}

// deviceParent slices an already-clean device path without cleaning or allocating.
func deviceParent(name string) string {
	if parent, _, found := strings.CutLast(name, "/"); found {
		return parent
	}
	return ""
}

func resolveWithParent(entries []sncloud.DeviceEntry, name string) (sncloud.DeviceEntry, sncloud.DeviceEntry, error) {
	name = cleanPath(name)
	var current, previous sncloud.DeviceEntry
	parent := ""
	for part := range strings.SplitSeq(name, "/") {
		next, err := resolveChild(entries, parent, part)
		if err != nil {
			if parent == "" && errors.Is(err, port.ErrNotFound) {
				var roots []string
				for _, e := range entries {
					if !strings.Contains(e.Path, "/") {
						roots = append(roots, e.Path)
					}
				}
				if len(roots) > 0 {
					return current, previous, fmt.Errorf("%w: top-level folder must match exactly (or in NFC); candidates: %s", err, strings.Join(roots, ", "))
				}
			}
			return sncloud.DeviceEntry{}, sncloud.DeviceEntry{}, err
		}
		previous, current, parent = current, next, next.Path
	}
	return current, previous, nil
}

func resolveChild(entries []sncloud.DeviceEntry, parent, name string) (sncloud.DeviceEntry, error) {
	var exact, equivalent sncloud.DeviceEntry
	var exactCount, equivalentCount int
	key := norm.NFC.String(name)
	for _, e := range entries {
		if deviceParent(e.Path) != parent {
			continue
		}
		base := path.Base(e.Path)
		if base == name {
			exact, exactCount = e, exactCount+1
		} else if norm.NFC.String(base) == key {
			equivalent, equivalentCount = e, equivalentCount+1
		}
	}
	match, count := equivalent, equivalentCount
	if exactCount > 0 {
		match, count = exact, exactCount
	}
	switch count {
	case 0:
		return sncloud.DeviceEntry{}, port.ErrNotFound
	case 1:
		return match, nil
	default:
		return sncloud.DeviceEntry{}, port.ErrConflict
	}
}

// Stat resolves a path from the real tree, never the server's aliases.
func (c *Client) Stat(ctx context.Context, name string) (port.Entry, error) {
	entries, err := c.tree(ctx)
	if err != nil {
		return port.Entry{}, err
	}
	e, parent, err := resolveWithParent(entries, name)
	out := deviceEntry(e)
	if err == nil && parent.ID != 0 {
		out.ParentID = parent.ID.String()
	}
	if c.offline && errors.Is(err, port.ErrNotFound) {
		return out, fmt.Errorf("offline: %s is missing from cached listing", name)
	}
	return out, err
}

func (c *Client) destination(ctx context.Context, name string, create bool, source sncloud.FileID) (string, error) {
	entries, err := c.tree(ctx)
	if err != nil {
		return "", c.failure("destination", name, err)
	}
	name = cleanPath(name)
	if existing, resolveErr := resolve(entries, name); resolveErr == nil && create && existing.ID != source {
		return "", port.ErrConflict
	} else if resolveErr != nil && !errors.Is(resolveErr, port.ErrNotFound) {
		return "", resolveErr
	}
	if top, _, _ := strings.Cut(name, "/"); !domain.Writable(name) && (top == "NOTE" || top == "DOCUMENT") {
		return "", fmt.Errorf("%w: use NOTE/Note, NOTE/MyStyle or DOCUMENT/Document; "+
			"writes directly below the virtual category can duplicate entries", port.ErrNotFound)
	}
	parent, err := resolve(entries, path.Dir(name))
	if err != nil {
		return "", err
	}
	if !parent.IsFolder {
		return "", port.ErrNotFound
	}
	target := path.Join(parent.Path, path.Base(name))
	// Upload may replace an exact name, but must never create a differently
	// encoded twin. Creation refuses every NFC-equal sibling, even if an exact
	// match would win during a read. A move excludes its own source entry.
	key := norm.NFC.String(path.Base(target))
	for _, e := range entries {
		if e.ID != source && deviceParent(e.Path) == parent.Path && norm.NFC.String(path.Base(e.Path)) == key && (create || e.Path != target) {
			return "", port.ErrConflict
		}
	}
	return target, nil
}

func fileID(id string) (sncloud.FileID, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil || n <= 0 {
		return 0, port.ErrNotFound
	}
	return sncloud.FileID(n), nil
}

// Download returns verified bytes from the cache or service.
func (c *Client) Download(ctx context.Context, e port.Entry, p port.Progress) (string, error) {
	file, _, err := c.DownloadSource(ctx, e, p)
	return file, err
}

// DownloadSource also reports whether the bytes came from the local cache.
func (c *Client) DownloadSource(ctx context.Context, e port.Entry, p port.Progress) (string, bool, error) {
	if c.cache != nil {
		file, found, err := c.cache.Body(ctx, e)
		if err != nil {
			if c.offline {
				return "", false, fmt.Errorf("offline: cached body for %s: %w", e.Path, err)
			}
			c.cacheDiagnostic("read body", err)
		} else if found {
			c.observe(ctx, e, file)
			return file, true, nil
		}
	}
	if c.offline {
		return "", false, fmt.Errorf("offline: cached body missing or damaged for %s (MD5 %s)", e.Path, e.Hash)
	}
	id, err := fileID(e.ID)
	if err != nil {
		return "", false, err
	}
	file, err := c.device.Download(ctx, id, nil, sncloud.Progress(p))
	err = c.failure("download", e.Path, err)
	if err == nil {
		readable := c.observe(ctx, e, file)
		if readable || c.drawingInputs[[2]string{e.ID, e.Hash}] {
			c.retainBody(ctx, e, file, "keep notebook body")
		}
	}

	return file, false, err
}

// Upload streams a local file to a full tablet path.
func (c *Client) Upload(ctx context.Context, file, destination string, p port.Progress) (port.Entry, error) {
	if err := c.invalidate(ctx); err != nil {
		return port.Entry{}, err
	}

	destination, err := c.destination(ctx, destination, false, 0)
	if err != nil {
		return port.Entry{}, err
	}
	e, err := c.device.Upload(ctx, file, destination, sncloud.Progress(p))
	if err != nil {
		mapped := c.failure("upload", destination, err)
		if definiteUploadRefusal(err) {
			return port.Entry{}, mapped
		}
		return port.Entry{}, port.ErrAmbiguousUpload
	}
	c.view.put(e, destination, false)
	return deviceEntry(c.view.normal(e, destination, false)), nil
}

// Mkdir refuses automatic collision renaming.
func (c *Client) Mkdir(ctx context.Context, name string) (port.Entry, error) {
	if err := c.invalidate(ctx); err != nil {
		return port.Entry{}, err
	}

	name, err := c.destination(ctx, name, true, 0)
	if err != nil {
		return port.Entry{}, err
	}
	e, err := c.device.CreateFolder(ctx, name, false)
	if err != nil {
		return port.Entry{}, c.failure("mkdir", name, err)
	}
	c.view.put(e, name, true)
	return deviceEntry(c.view.normal(e, name, true)), nil
}

// Move relocates an entry, including its filename.
func (c *Client) Move(ctx context.Context, e port.Entry, destination string) error {
	if err := c.invalidate(ctx); err != nil {
		return err
	}

	id, err := fileID(e.ID)
	if err != nil {
		return err
	}
	destination, err = c.destination(ctx, destination, true, id)
	if err != nil {
		return err
	}
	if destination == e.Path {
		return port.ErrConflict
	}
	if _, err = c.device.Move(ctx, id, destination, false); err != nil {
		return c.failure("move", destination, err)
	}
	c.view.move(id, destination)
	return nil
}

// Delete recycles through the web API unless permanent was explicitly requested.
func (c *Client) Delete(ctx context.Context, e port.Entry, permanent bool) error {
	if err := c.invalidate(ctx); err != nil {
		return err
	}

	id, err := fileID(e.ID)
	if err != nil {
		return err
	}
	if permanent {
		err = c.device.Delete(ctx, id)
	} else {
		// The web API recycles by parent; a Stat result carries it already.
		parentID := e.ParentID
		if parentID == "" {
			parent, statErr := c.Stat(ctx, path.Dir(e.Path))
			if statErr != nil {
				return statErr
			}
			parentID = parent.ID
		}
		pid, idErr := fileID(parentID)
		if idErr != nil {
			return idErr
		}
		err = c.web.Delete(ctx, []sncloud.FileID{id}, pid)
	}
	if err != nil {
		return c.failure("delete", e.Path, err)
	}
	c.view.remove(id)
	return nil
}

// Trash reads recycled identifiers, which can be passed to Restore.
func (c *Client) Trash(ctx context.Context) ([]port.Entry, error) {
	if c.offline {
		return nil, errors.New("offline: recycle-bin listing is not cached")
	}
	entries, err := c.web.RecycleBin(ctx, sncloud.SortTime, false)
	out := make([]port.Entry, 0, len(entries))
	for _, e := range entries {
		out = append(out, port.Entry{ID: e.ID.String(), Name: e.Name, Folder: e.IsFolder, Size: e.Size, Modified: e.Deleted})
	}
	return out, c.failure("trash", "", err)
}

// EmptyTrash permanently removes every recycled entry.
func (c *Client) EmptyTrash(ctx context.Context) error {
	if err := c.invalidate(ctx); err != nil {
		return err
	}

	return c.failure("emptytrash", "", c.web.EmptyRecycleBin(ctx))
}

// Restore restores one recycled identifier to its original parent.
func (c *Client) Restore(ctx context.Context, value string) error {
	if err := c.invalidate(ctx); err != nil {
		return err
	}

	id, err := fileID(value)
	if err != nil {
		return err
	}
	return c.failure("restore", "", c.web.Restore(ctx, []sncloud.FileID{id}))
}

func (c *Client) tree(ctx context.Context) ([]sncloud.DeviceEntry, error) {
	if c.view != nil {
		return c.view.entries(), ctx.Err()
	}
	return c.cachedTree(ctx)
}

// Snapshot gives a use case one listing for a whole invocation, kept current
// with the changes made through it. Nothing is retained across invocations.
func (c *Client) Snapshot(ctx context.Context) (port.Files, error) {
	if c.view != nil {
		return c, nil
	}
	entries, err := c.tree(ctx)
	if err != nil {
		return nil, err
	}
	view := *c
	view.view = &listing{list: entries}
	return &view, nil
}

// WithSyncLock holds the account sync lock while body runs. The library
// releases it even when ctx is canceled.
func (c *Client) WithSyncLock(ctx context.Context, body func(context.Context) error) error {
	pending := false
	if c.lockState != nil {
		var err error
		pending, err = c.lockState.Pending()
		if err != nil {
			return errors.New("cannot read sync lock state")
		}
		if err = c.lockState.Mark(); err != nil {
			return errors.New("cannot save sync lock state")
		}
	}
	var bodyErr error
	ran := false
	err := c.device.WithSyncLock(ctx, func(ctx context.Context, _ bool) error {
		ran = true
		bodyErr = body(ctx)
		// The library hides release failures behind body failures. Returning the
		// body result separately makes its return value confirm the release.
		return nil
	})
	mapped := c.failure("sync lock", "", err)
	if !ran && errors.Is(err, sncloud.ErrSyncLocked) {
		if c.lockState != nil && !pending {
			_ = c.lockState.Clear()
		}
		if pending {
			return fmt.Errorf("%w: a previous snctl run may still hold the sync lock; see docs/uploads.md for manual recovery", port.ErrBusy)
		}
		return port.ErrBusy
	}
	if err == nil && c.lockState != nil {
		mapped = c.lockState.Clear()
	}
	return errors.Join(bodyErr, mapped)
}

// listing is the tree of one invocation. Its own writes are applied to it so
// that later lookups in the same invocation see them; concurrent transfers
// share it.
type listing struct {
	mu   sync.Mutex
	list []sncloud.DeviceEntry
}

func (l *listing) entries() []sncloud.DeviceEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.list)
}

// normal fills only absent metadata; a server-selected name or path must
// remain visible to the caller. sncloud-go already expands parent paths.
func (*listing) normal(e sncloud.DeviceEntry, name string, folder bool) sncloud.DeviceEntry {
	if e.Path == "" {
		e.Path = cleanPath(name)
	}
	if e.Name == "" {
		e.Name = path.Base(e.Path)
	}
	e.IsFolder = folder
	return e
}

func (l *listing) put(e sncloud.DeviceEntry, name string, folder bool) {
	if l == nil {
		return
	}
	e = l.normal(e, name, folder)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.list = slices.DeleteFunc(l.list, func(old sncloud.DeviceEntry) bool { return old.ID == e.ID })
	l.list = append(l.list, e)
}

func (l *listing) move(id sncloud.FileID, destination string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	old := ""
	for _, e := range l.list {
		if e.ID == id {
			old = e.Path
		}
	}
	for i, e := range l.list {
		switch {
		case e.ID == id:
			l.list[i].Path, l.list[i].Name = destination, path.Base(destination)
		case old != "" && strings.HasPrefix(e.Path, old+"/"):
			l.list[i].Path = destination + strings.TrimPrefix(e.Path, old)
		}
	}
}

func (l *listing) remove(id sncloud.FileID) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	old := ""
	for _, e := range l.list {
		if e.ID == id {
			old = e.Path
		}
	}
	l.list = slices.DeleteFunc(l.list, func(e sncloud.DeviceEntry) bool {
		return e.ID == id || old != "" && strings.HasPrefix(e.Path, old+"/")
	})
}

// Refresh makes one lookup and replaces only the affected cached path, so
// concurrent transfers do not discard each other's listing updates.
func (c *Client) Refresh(ctx context.Context, name string) (port.Entry, error) {
	e, err := c.device.EntryAt(ctx, name)
	if err != nil {
		mapped := c.failure("verify upload", name, err)
		if definiteUploadRefusal(err) || ctx.Err() != nil {
			return port.Entry{}, mapped
		}
		return port.Entry{}, port.ErrAmbiguousUpload
	}
	if e == nil || e.Path != name {
		return port.Entry{}, port.ErrNotFound
	}
	if c.view != nil {
		c.view.mu.Lock()
		c.view.list = slices.DeleteFunc(c.view.list, func(old sncloud.DeviceEntry) bool { return old.Path == name })
		c.view.list = append(c.view.list, *e)
		c.view.mu.Unlock()
	}
	return deviceEntry(*e), nil
}

// ListFresh bypasses both the cache and the planning snapshot for write checks.
func (c *Client) ListFresh(ctx context.Context, folder string, recursive bool) ([]port.Entry, error) {
	if c.offline {
		return nil, errors.New("offline: cloud writes require a live listing")
	}
	// Resolve the spelling from the invocation view, then read only this
	// folder. Partial listings must never replace the complete disk cache.
	entries, err := c.tree(ctx)
	if err != nil {
		return nil, err
	}
	folder = cleanPath(folder)
	if folder != "" {
		parent, resolveErr := resolve(entries, folder)
		if errors.Is(resolveErr, port.ErrNotFound) || resolveErr == nil && !parent.IsFolder {
			return []port.Entry{}, nil
		}
		if resolveErr != nil {
			return nil, resolveErr
		}
		folder = parent.Path
	}
	entries, err = c.device.List(ctx, "/"+folder, recursive)
	if err != nil {
		return nil, c.failure("fresh listing", folder, err)
	}
	out := make([]port.Entry, 0, len(entries))
	for _, e := range entries {
		out = append(out, deviceEntry(e))
	}
	return out, nil
}

// ListParentFresh reads one folder's current identity and children. Batch
// create/upload checks use FreshSnapshot instead to bound cost by batch.
// Destructive operations use this targeted read before every write.
func (c *Client) ListParentFresh(ctx context.Context, folder string) (port.Entry, []port.Entry, error) {
	children, err := c.ListFresh(ctx, folder, false)
	if err != nil {
		return port.Entry{}, nil, err
	}
	folder = cleanPath(folder)
	if folder == "" {
		return port.Entry{Folder: true}, children, nil
	}
	entries, err := c.tree(ctx)
	if err != nil {
		return port.Entry{}, nil, err
	}
	parent, err := resolve(entries, folder)
	if errors.Is(err, port.ErrNotFound) || err == nil && !parent.IsFolder {
		return port.Entry{}, children, nil
	}
	if err != nil {
		return port.Entry{}, nil, err
	}
	current, err := c.device.EntryAt(ctx, parent.Path)
	if err != nil {
		return port.Entry{}, nil, c.failure("fresh parent", folder, err)
	}
	if current == nil {
		return port.Entry{}, children, nil
	}
	return deviceEntry(*current), children, nil
}

// FreshSnapshot gives a long-lived consumer an independent live listing for one pass.
// Its mutations cannot change another pass's planning view. The mirror selects
// which drawing inputs to retain after validating its listing.
func (c *Client) FreshSnapshot(ctx context.Context) (port.Files, error) {
	if c.offline {
		return nil, errors.New("offline: cloud writes require a live listing")
	}
	entries, err := c.device.List(ctx, "/", true)
	if err != nil {
		return nil, c.failure("list", "/", err)
	}
	c.saveListing(ctx, entries)
	fresh := *c
	fresh.view = &listing{list: entries}
	fresh.drawingInputs = nil
	return &fresh, nil
}
