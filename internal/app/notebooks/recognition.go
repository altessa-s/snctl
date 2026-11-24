// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package notebooks

import (
	"context"
	"crypto/md5" // #nosec G501 -- Supernote content identifiers.
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/altessa-s/snctl/internal/app/outdir"
	"github.com/altessa-s/snctl/internal/domain"
	"github.com/altessa-s/snctl/internal/port"

	"golang.org/x/text/unicode/norm"
)

var _ port.NotebookText = Service{}

func (s Service) parse(ctx context.Context, data []byte) (port.Notebook, error) {
	s.Index = port.ContextRecognitionIndex(ctx, s.Index)
	n, err := s.Reader.Parse(data)
	if err == nil && s.Index != nil && n.Kind == "note" {
		hash := md5.Sum(data) // #nosec G401 -- index key, not a security check.
		_ = s.Index.RememberRecognition(ctx, hex.EncodeToString(hash[:]), n.HasRecognition())
	}
	return n, err
}

// ObserveBody indexes readable notebook downloads without changing transfer success.
// Damaged bodies and builds without notebook reading leave the state unknown.
func (s Service) ObserveBody(ctx context.Context, e port.Entry, file string) bool {
	s.Index = port.ContextRecognitionIndex(ctx, s.Index)
	if s.Index == nil || !recognizable(e) || s.Reader.Available() != nil {
		return false
	}
	if s.Index.Recognition(e.Hash) != nil {
		return true
	}
	r, err := s.Files.Open(file)
	if err != nil {
		return false
	}
	data, err := io.ReadAll(r)
	err = errors.Join(err, r.Close())
	if err != nil {
		return false
	}
	n, err := s.parse(ctx, data)
	// A scan retains even damaged verified notebook bodies under the normal budget,
	// so later scans can retry parsing without downloading the same bytes again.
	batch, batched := s.Index.(*recognitionBatch)
	return batched && batch.scan || err == nil && n.Kind == "note"
}

// ObserveCached fills missing metadata for bodies retained by an earlier snctl version.
func (s Service) ObserveCached(ctx context.Context, e port.Entry) {
	cache, ok := s.Index.(port.FileCache)
	if !ok || !recognizable(e) || port.ContextRecognitionIndex(ctx, s.Index).Recognition(e.Hash) != nil || s.Reader.Available() != nil {
		return
	}
	file, found, err := cache.Body(ctx, e)
	if err != nil || !found {
		return
	}
	defer func() { _ = s.Files.Remove(file) }()
	s.ObserveBody(ctx, e, file)
}

func recognizable(e port.Entry) bool {
	return !e.Folder && notebookExtension(e.Name) == noteExtension
}

// Recognition decorates entries from the local index; scan reads only unknown notebooks.
func (s Service) Recognition(ctx context.Context, entries []port.Entry, scan bool, progress port.Progress) (result []port.Entry, resultErr error) {
	entries = slices.Clone(entries)
	batch := s.newRecognitionBatch(scan)
	ctx = port.WithRecognitionIndex(ctx, batch)
	if scan {
		defer batch.flush(ctx)
	}
	for i := range entries {
		if recognizable(entries[i]) {
			entries[i].Recognition = batch.Recognition(entries[i].Hash)
		}
	}
	if scan {
		if err := s.recognitionBudget(entries); err != nil {
			return nil, err
		}
	}
	for i := range entries {
		e := &entries[i]
		if !scan || !recognizable(*e) || e.Recognition != nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			return entries, errors.Join(resultErr, err)
		}
		if value := batch.Recognition(e.Hash); value != nil {
			e.Recognition = value
			continue
		}
		n, err := s.Read(ctx, "/"+strings.TrimPrefix(e.Path, "/"), nil, progress)
		if err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("warning: %s: %w", e.Path, err))
			continue
		}
		value := n.HasRecognition()
		e.Recognition = &value
		if s.Index != nil {
			_ = batch.RememberRecognition(ctx, e.Hash, value)
		}
	}
	return entries, errors.Join(resultErr, ctx.Err())
}

// BatchRecognition shares a single index merge across readers and download observers.
func (s Service) BatchRecognition(ctx context.Context) (context.Context, func()) {
	if s.Index == nil || port.ContextRecognitionIndex(ctx, nil) != nil {
		return ctx, func() {}
	}
	b := s.newRecognitionBatch(false)
	return port.WithRecognitionIndex(ctx, b), func() { b.flush(ctx) }
}

func (s Service) newRecognitionBatch(scan bool) *recognitionBatch {
	b := &recognitionBatch{base: s.Index, scan: scan, values: map[string]bool{}, pending: map[string]bool{}}
	if index, ok := s.Index.(port.RecognitionBatchIndex); ok {
		b.snapshot = true
		maps.Copy(b.values, index.RecognitionSnapshot())
	}
	return b
}

// recognitionBatch also catches metadata observations made by Fetch.Read.
// No notebook text is retained; flushing remains best effort, like body caching.
type recognitionBatch struct {
	mu              sync.Mutex
	snapshot, scan  bool
	base            port.RecognitionIndex
	values, pending map[string]bool
}

func (b *recognitionBatch) Recognition(hash string) *bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	v, ok := b.values[hash]
	if !ok && !b.snapshot && b.base != nil {
		if value := b.base.Recognition(hash); value != nil {
			v, ok = *value, true
			b.values[hash] = v
		}
	}
	if !ok {
		return nil
	}
	return &v
}

func (b *recognitionBatch) RememberRecognition(_ context.Context, hash string, value bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.values[hash], b.pending[hash] = value, value
	return nil
}

// Keep partial observations on cancellation, but bound the wait for a busy
// cache lock. The next successful read can recreate any missing metadata.
const recognitionFlushTimeout = 500 * time.Millisecond

func (b *recognitionBatch) flush(ctx context.Context) {
	if len(b.pending) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), recognitionFlushTimeout)
	defer cancel()
	if index, ok := b.base.(port.RecognitionBatchIndex); ok {
		_ = index.RememberRecognitions(ctx, b.pending)
		return
	}
	if b.base != nil {
		for hash, value := range b.pending {
			if err := b.base.RememberRecognition(ctx, hash, value); err != nil {
				return
			}
		}
	}
}

// FindText matches all whitespace-separated words across a notebook, ignoring case.
// Existing local directories are walked without following symbolic links.
func (s Service) FindText(ctx context.Context, query, words string, progress port.Progress, raw bool) ([]port.TextMatch, error) {
	terms := strings.Fields(norm.NFC.String(strings.ToLower(domain.CleanText(words))))
	if len(terms) == 0 {
		return nil, fmt.Errorf("%w: --text needs at least one word", port.ErrOptions)
	}
	if err := s.Reader.Available(); err != nil {
		return nil, err
	}
	ctx, flush := s.BatchRecognition(ctx)
	defer flush()
	entries, local, err := s.textEntries(ctx, query)
	if err != nil {
		return nil, err
	}
	entries = port.FoldMarks(entries, raw)
	found := []port.TextMatch{}
	for _, e := range entries {
		source, readable := textSource(e, raw)
		if !readable {
			continue
		}
		input := "/" + strings.TrimPrefix(source.Path, "/")
		if local {
			input = source.Path
		}
		n, err := s.Read(ctx, input, nil, progress)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Path, err)
		}

		pages := []int{}
		var all strings.Builder
		for _, p := range n.Clean().Pages {
			text := norm.NFC.String(strings.ToLower(strings.Join(append(slices.Clone(p.Text), p.Recognition), "\n")))
			all.WriteString(text)
			all.WriteByte('\n')
			for _, term := range terms {
				if strings.Contains(text, term) {
					pages = append(pages, p.Number)
					break
				}
			}
		}
		if !allWords(all.String(), terms) {
			continue
		}
		if len(pages) == 0 {
			continue
		}
		if recognizable(e) {
			v := n.HasRecognition()
			e.Recognition = &v
		}
		found = append(found, port.TextMatch{Entry: e, Pages: pages})
	}
	return found, ctx.Err()
}

func allWords(text string, terms []string) bool {
	for _, word := range terms {
		if !strings.Contains(text, word) {
			return false
		}
	}
	return true
}

func (s Service) textEntries(ctx context.Context, query string) ([]port.Entry, bool, error) {
	if query != "/" && s.Files.IsDir(query) {
		disk, ok := s.Files.(port.TreeFiles)
		if !ok {
			return nil, true, port.ErrOptions
		}
		files, err := disk.Walk(query)
		entries := []port.Entry{}
		for _, f := range files {
			if f.Regular {
				name := filepath.Join(query, filepath.FromSlash(f.Rel))
				info, infoErr := disk.Info(name)
				if infoErr != nil {
					return nil, true, infoErr
				}
				entries = append(entries, port.Entry{Name: path.Base(f.Rel), Path: name, Size: &info.Size, Modified: &info.Modified})
			}
		}
		return entries, true, err
	}
	local, err := s.Files.LocalInput(query)
	if err != nil {
		return nil, local, err
	}
	if local {
		e := port.Entry{Name: filepath.Base(query), Path: query}
		return s.singleTextEntry(ctx, query, e, true)
	}
	top, err := s.Fetch.Resolve(ctx, query)
	if err != nil {
		return nil, false, err
	}
	if !top.Folder {
		return s.singleTextEntry(ctx, query, top, false)
	}
	entries, err := s.Cloud.List(ctx, query, true)
	return entries, false, err
}

func (s Service) singleTextEntry(ctx context.Context, query string, e port.Entry, local bool) ([]port.Entry, bool, error) {
	if !domain.DocumentName(e.Name) {
		return []port.Entry{e}, local, nil
	}
	mark, err := s.annotationPath(ctx, query)
	if err != nil {
		return nil, local, err
	}
	var sidecar port.Entry
	if local {
		sidecar = port.Entry{Name: filepath.Base(mark), Path: mark}
	} else {
		sidecar, err = s.Fetch.Resolve(ctx, mark)
		if err != nil {
			return nil, local, err
		}
	}
	return []port.Entry{e, sidecar}, local, nil
}

// ExportText writes a single notebook, or recognized notebooks below a cloud folder.
// Like render/get, completed files replace destinations atomically without a prompt.
func (s Service) ExportText(ctx context.Context, query, dest string, o port.TextOptions) ([]string, error) {
	if o.Encode == nil || !slices.Contains([]string{"txt", "md", "json"}, o.Extension) || dest == "" || dest == "-" {
		return nil, port.ErrOptions
	}
	if err := s.Reader.Available(); err != nil {
		return nil, err
	}
	local, err := s.Files.LocalInput(query)
	if err != nil {
		return nil, err
	}
	folder := false
	var top port.Entry
	if !local {
		top, err = s.Fetch.Resolve(ctx, query)
		if err != nil {
			return nil, err
		}
		folder = top.Folder
	}
	if !folder {
		return s.exportNotebookText(ctx, query, dest, o)
	}

	if len(o.Pages) > 0 {
		return nil, fmt.Errorf("%w: --page requires a notebook", port.ErrOptions)
	}
	ctx, flush := s.BatchRecognition(ctx)
	defer flush()
	entries, err := s.Cloud.List(ctx, query, true)
	if err != nil {
		return nil, err
	}
	entries = slices.DeleteFunc(entries, func(e port.Entry) bool { return !recognizable(e) })
	slices.SortFunc(entries, func(a, b port.Entry) int { return strings.Compare(a.Path+"\x00"+a.ID, b.Path+"\x00"+b.ID) })
	paths := outdir.Paths(entries, top.Path, nil, func(name string) string { return strings.TrimSuffix(name, path.Ext(name)) + "." + o.Extension })
	original := s.Files
	disk, err := outdir.Open(original, dest)
	if err != nil {
		return nil, err
	}
	defer func() { _ = disk.Close() }()
	outputs := []string{}
	for i, e := range entries {
		n, err := s.Read(ctx, "/"+strings.TrimPrefix(e.Path, "/"), nil, o.Progress)
		if err != nil {
			return outputs, err
		}
		if !n.HasRecognition() {
			continue
		}
		target := filepath.Join(dest, filepath.FromSlash(paths[i]))
		if err = disk.MkdirAll(filepath.Dir(target)); err != nil {
			return outputs, err
		}
		writer := s
		writer.Files = disk
		if err = writer.writeText(ctx, target, n, o); err != nil {
			return outputs, err
		}
		outputs = append(outputs, target)
	}
	return outputs, ctx.Err()
}

// Destination returns dest unchanged for a file. An appended basename means
// that same observation selected a directory, which must be pinned for writing.
func (s *Service) bindDestination(dest, name string) (string, func(), error) {
	target, err := s.Files.Destination(dest, name)
	if err != nil || target == dest {
		return target, func() {}, err
	}
	if dest == "" {
		dest = "."
	}
	disk, err := outdir.Open(s.Files, dest)
	if err != nil {
		return "", nil, err
	}
	s.Files = disk
	return target, func() { _ = disk.Close() }, nil
}

func (s Service) exportNotebookText(ctx context.Context, query, dest string, o port.TextOptions) ([]string, error) {
	n, readErr := s.Read(ctx, query, o.Pages, o.Progress)
	if readErr != nil {
		return nil, readErr
	}
	name := safeName(strings.TrimSuffix(n.Name, path.Ext(n.Name))) + "." + o.Extension
	target, closeOutput, err := s.bindDestination(dest, name)
	if err != nil {
		return nil, err
	}
	defer closeOutput()
	err = s.writeText(ctx, target, n, o)
	if err != nil {
		return nil, err
	}
	return []string{target}, nil
}

func (s Service) writeText(ctx context.Context, target string, n port.Notebook, o port.TextOptions) error {
	if n.Drawing != nil {
		return fmt.Errorf("%w: text requires a .note or .mark notebook", port.ErrOptions)
	}
	if !o.Raw {
		n = n.Clean()
	}
	data, err := o.Encode(n)
	if err != nil {
		return err
	}
	return s.Files.Produce(ctx, target, func(w io.Writer) error { _, err := w.Write(data); return err })
}

// Discovery plans the unknown set before making any download, like sync --files.
func (s Service) recognitionBudget(entries []port.Entry) error {
	cache, ok := s.Index.(port.FileCache)
	if !ok {
		return nil
	}
	status, err := cache.Status()
	if err != nil {
		return err
	}
	if status.Budget == 0 {
		return nil
	}
	seen := map[string]bool{}
	var total int64
	for _, e := range entries {
		if !recognizable(e) || e.Recognition != nil || seen[e.Hash] {
			continue
		}
		if e.Size == nil || *e.Size < 0 || e.Hash == "" {
			return fmt.Errorf("%s: missing MD5 or size", e.Path)
		}
		seen[e.Hash] = true
		if *e.Size > status.Budget-total {
			return fmt.Errorf("recognition discovery exceeds cache budget %d; raise the budget or use --no-cache; no bodies downloaded", status.Budget)
		}
		total += *e.Size
	}
	return nil
}

// textSource keeps PDFs out of the parser while reporting a folded mark as its document.
func textSource(e port.Entry, raw bool) (port.Entry, bool) {
	if e.Folder {
		return e, false
	}
	if notebookName(e.Name) {
		return e, true
	}
	if !raw && e.Mark != nil {
		return *e.Mark, true
	}
	return e, false
}
