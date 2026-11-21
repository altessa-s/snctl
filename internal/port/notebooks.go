// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package port

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

// Notebook failures distinguish unavailable capabilities from damaged input.
var (
	ErrNotebookUnavailable = errors.New("this build cannot read notebooks: rebuild with cgo and a C23 compiler")
	ErrRenderUnavailable   = errors.New("this build cannot render notebooks: install a full build with cgo and without norender")
	ErrNotebook            = errors.New("not a readable Supernote notebook")
	ErrPage                = errors.New("page out of range")
	ErrOptions             = errors.New("invalid notebook options")
	ErrFileRequired        = errors.New("get requires a file, not a folder")
)

// Fetch shares cloud resolution and verified downloads among file consumers.
type Fetch interface {
	Resolve(context.Context, string) (Entry, error)
	Read(context.Context, string, Progress) (Entry, []byte, error)
	Get(context.Context, string, string, io.Writer, Progress) (string, error)
}

// Rect retains the coordinate space of the field that contains it.
type Rect struct {
	X      float64
	Y      float64
	Width  float64
	Height float64
}

// MetadataEntry preserves duplicate keys and their stored order.
type MetadataEntry struct {
	Key   string
	Value string
}

// NoteTitle describes a heading region; handwriting has no guaranteed text label.
type NoteTitle struct {
	Page  int
	Level int
	Text  string
}

// NoteHighlight has stored geometry and comments, and optional source PDF text.
type NoteHighlight struct {
	Text          *string
	Page          int
	Color         int64
	Comment       *string
	Rects         []*Rect
	DocumentRects []*Rect
}

// NotePage is the portable reading and inspection view of one page.
type NotePage struct {
	Number         int
	Width          float64
	Height         float64
	Orientation    string
	Template       string
	Layers         []string
	Strokes        int
	SkippedStrokes int
	Text           []string
	Recognition    string
}

// Notebook owns the data needed by text, highlights and note inspection.
type Notebook struct {
	Drawing  *DrawingInfo
	Warnings []string
	Name     string
	// Mark names the annotation sidecar when a document was requested.
	Mark         string
	Kind         string
	Version      int64
	Device       string
	Pages        []NotePage
	Titles       []NoteTitle
	Highlights   []NoteHighlight
	Links        int
	Keywords     int
	SkippedPages int
	Header       []MetadataEntry
	Footer       []MetadataEntry
}

// DrawingLayer describes a stored Atelier layer without inferring visibility.
type DrawingLayer struct {
	ID    uint64
	Name  string
	Tiles int
}

// DrawingInfo retains canvas presence and stored layer/tile counts.
type DrawingInfo struct {
	Width, Height *float64
	FormatVersion *string
	Layers        []DrawingLayer
	Tiles         int
}

// DrawingReader adds Atelier inspection to the notebook reader.
type DrawingReader interface {
	ParseDrawing([]byte) (Notebook, error)
}

// NoteReader isolates the native parser from its consumers.
type NoteReader interface {
	Available() error
	Parse([]byte) (Notebook, error)
}

// RenderOptions selects stored notebook page numbers; zero selects the whole notebook.
// The use case translates a selected number to a sheet position at the renderer boundary.
type RenderOptions struct {
	// NoMarks opens an original PDF/EPUB without resolving annotations.
	NoMarks bool
	// Source is an explicit local or cloud source PDF for a .mark.
	Source string
	// Drawing selects the Atelier renderer; DrawingPage chooses painted or canvas framing.
	Drawing     bool
	DrawingPage string
	// Diagnostics is replaced by the renderer after an overlay call.
	Diagnostics      *OverlayDiagnostics
	Format           string
	Page             int
	Scale            float64
	Quality          int
	NoTemplate       bool
	NoText           bool
	StrictImages     bool
	Progress         Progress
	DownloadProgress Progress
	Writer           io.Writer
}

// OverlayDiagnostics reports omissions without turning a completed drawing into a failure.
type OverlayDiagnostics struct {
	SheetsDropped, HighlightsDropped    int
	AllAnnotationsDropped, NoPairsAgree bool
	SignatureFields                     int
}

// Warnings describes potentially incomplete or signature-bearing overlays.
func (d OverlayDiagnostics) Warnings() []string {
	var warnings []string
	if d.SheetsDropped != 0 || d.HighlightsDropped != 0 {
		warnings = append(warnings, fmt.Sprintf("overlay dropped %d sheets and %d highlights", d.SheetsDropped, d.HighlightsDropped))
	}
	if d.AllAnnotationsDropped {
		warnings = append(warnings, "overlay dropped all annotations")
	}
	if d.NoPairsAgree {
		warnings = append(warnings, "overlay source geometry does not agree with any annotation pairs")
	}
	if d.SignatureFields != 0 {
		warnings = append(warnings, "overlay changes follow source PDF signatures; signatures are not validated")
	}
	return warnings
}

// RenderLimits carries the linked library defaults to its consumers.
type RenderLimits struct {
	MinimumScale, MaximumScale float64
	MaximumPixels              int
}

// Renderer owns native drawing lifetimes.
type Renderer interface {
	PageText(context.Context, []byte, int, []*Rect) (string, error)
	Limits() RenderLimits
	RenderPages(context.Context, []byte, RenderOptions, func(int, func(io.Writer) error) error) error
	Available() error
	Render(context.Context, []byte, []byte, RenderOptions, io.Writer) (int, error)
}

// NotebookFiles provides atomic output and filesystem observations to use cases.
type NotebookFiles interface {
	OpenRoot(string) (OutputDirectory, error)
	LocalInput(string) (bool, error)
	IsDir(string) bool
	LocalFiles
	Produce(context.Context, string, func(io.Writer) error) error
	Destination(string, string) (string, error)
	MkdirAll(string) error
	ModTime(string) (time.Time, error)
	SetModTime(string, time.Time) error
	PreviewDir() (string, error)
}

// Opener hands a completed preview to the system viewer.
type Opener interface {
	Open(context.Context, string) error
}

// RenderReport describes the completed outputs, including annotation-only marks.
type RenderReport struct {
	Warnings     []string
	Path         string
	Pages        int
	SheetsOnly   bool
	SkippedPages int
}

// TreeOptions controls recursive PDF export.
type TreeOptions struct {
	All, Incremental, Force bool
	Jobs                    int
}

// RenderedDocument retains failures so other documents can finish.
type RenderedDocument struct {
	Document string
	Status   string
	RenderReport
	Error string
}

// TreeReport records the deterministic order of a folder's output.
type TreeReport struct {
	Dir       string
	Documents []RenderedDocument
	Rendered  int
	Unchanged int
	Failed    int
}

// Notebooks is the input boundary for notebook commands.
type Notebooks interface {
	Available() error
	Limits() RenderLimits
	Read(context.Context, string, []int, Progress) (Notebook, error)
	ReadHighlights(context.Context, string, []int, Progress) (Notebook, error)
	Render(context.Context, string, string, RenderOptions) (RenderReport, error)
	Folder(context.Context, string) (bool, error)
	RenderTree(context.Context, string, string, RenderOptions, TreeOptions) (TreeReport, error)
	Open(context.Context, string, RenderOptions) (RenderReport, error)
}
