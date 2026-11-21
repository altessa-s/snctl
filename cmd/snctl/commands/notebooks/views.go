// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package notebooks

import "github.com/altessa-s/snctl/internal/port"

type rectView struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

func viewRect(v port.Rect) rectView {
	out := rectView{}
	out.X = v.X
	out.Y = v.Y
	out.Width = v.Width
	out.Height = v.Height
	return out
}

type metadataEntryView struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func viewMetadataEntry(v port.MetadataEntry) metadataEntryView {
	out := metadataEntryView{}
	out.Key = v.Key
	out.Value = v.Value
	return out
}

type noteTitleView struct {
	Page  int    `json:"page"`
	Level int    `json:"level"`
	Text  string `json:"text"`
}

func viewNoteTitle(v port.NoteTitle) noteTitleView {
	out := noteTitleView{}
	out.Page = v.Page
	out.Level = v.Level
	out.Text = v.Text
	return out
}

type noteHighlightView struct {
	Text          *string     `json:"text"`
	Page          int         `json:"page"`
	Color         int64       `json:"color"`
	Comment       *string     `json:"comment"`
	Rects         []*rectView `json:"rects"`
	DocumentRects []*rectView `json:"document_rects"`
}

func viewNoteHighlight(v port.NoteHighlight) noteHighlightView {
	out := noteHighlightView{}
	out.Page = v.Page
	out.Color = v.Color
	out.Comment = v.Comment
	out.Text = v.Text
	if v.Rects != nil {
		out.Rects = make([]*rectView, len(v.Rects))
	}
	for i, item := range v.Rects {
		if item != nil {
			value := viewRect(*item)
			out.Rects[i] = &value
		}
	}
	if v.DocumentRects != nil {
		out.DocumentRects = make([]*rectView, len(v.DocumentRects))
	}
	for i, item := range v.DocumentRects {
		if item != nil {
			value := viewRect(*item)
			out.DocumentRects[i] = &value
		}
	}
	return out
}

type notePageView struct {
	Number         int      `json:"number"`
	Width          float64  `json:"width"`
	Height         float64  `json:"height"`
	Orientation    string   `json:"orientation"`
	Template       string   `json:"template"`
	Layers         []string `json:"layers"`
	Strokes        int      `json:"strokes"`
	SkippedStrokes int      `json:"skipped_strokes"`
	Text           []string `json:"text"`
	Recognition    string   `json:"recognition"`
}

func viewNotePage(v port.NotePage) notePageView {
	out := notePageView{}
	out.Number = v.Number
	out.Width = v.Width
	out.Height = v.Height
	out.Orientation = v.Orientation
	out.Template = v.Template
	out.Layers = v.Layers
	out.Strokes = v.Strokes
	out.SkippedStrokes = v.SkippedStrokes
	out.Text = v.Text
	out.Recognition = v.Recognition
	return out
}

type notebookView struct {
	Drawing      *drawingInfoView    `json:"drawing,omitempty"`
	Name         string              `json:"name"`
	Mark         string              `json:"mark,omitempty"`
	Kind         string              `json:"kind"`
	Version      int64               `json:"format_version"`
	Device       string              `json:"device"`
	Pages        []notePageView      `json:"pages"`
	Titles       []noteTitleView     `json:"titles"`
	Highlights   []noteHighlightView `json:"highlights"`
	Links        int                 `json:"links"`
	Keywords     int                 `json:"keywords"`
	SkippedPages int                 `json:"skipped_pages"`
	Header       []metadataEntryView `json:"header,omitempty"`
	Footer       []metadataEntryView `json:"footer,omitempty"`
}

func viewNotebook(v port.Notebook) notebookView {
	out := notebookView{}
	out.Name = v.Name
	out.Mark = v.Mark
	out.Drawing = viewDrawing(v.Drawing)
	out.Kind = v.Kind
	out.Version = v.Version
	out.Device = v.Device
	if v.Pages != nil {
		out.Pages = make([]notePageView, len(v.Pages))
	}
	for i, item := range v.Pages {
		out.Pages[i] = viewNotePage(item)
	}
	if v.Titles != nil {
		out.Titles = make([]noteTitleView, len(v.Titles))
	}
	for i, item := range v.Titles {
		out.Titles[i] = viewNoteTitle(item)
	}
	if v.Highlights != nil {
		out.Highlights = make([]noteHighlightView, len(v.Highlights))
	}
	for i, item := range v.Highlights {
		out.Highlights[i] = viewNoteHighlight(item)
	}
	out.Links = v.Links
	out.Keywords = v.Keywords
	out.SkippedPages = v.SkippedPages
	if v.Header != nil {
		out.Header = make([]metadataEntryView, len(v.Header))
	}
	for i, item := range v.Header {
		out.Header[i] = viewMetadataEntry(item)
	}
	if v.Footer != nil {
		out.Footer = make([]metadataEntryView, len(v.Footer))
	}
	for i, item := range v.Footer {
		out.Footer[i] = viewMetadataEntry(item)
	}
	return out
}

type renderReportView struct {
	Warnings     []string `json:"warnings,omitempty"`
	Path         string   `json:"path"`
	Pages        int      `json:"pages"`
	SheetsOnly   bool     `json:"sheets_only"`
	SkippedPages int      `json:"skipped_pages"`
}

func viewRenderReport(v port.RenderReport) renderReportView {
	out := renderReportView{}
	out.Warnings = v.Warnings
	out.Path = v.Path
	out.Pages = v.Pages
	out.SheetsOnly = v.SheetsOnly
	out.SkippedPages = v.SkippedPages
	return out
}

type renderedDocumentView struct {
	Document string `json:"document"`
	Status   string `json:"status"`
	renderReportView
	Error string `json:"error,omitempty"`
}

func viewRenderedDocument(v port.RenderedDocument) renderedDocumentView {
	out := renderedDocumentView{renderReportView: viewRenderReport(v.RenderReport)}
	out.Document = v.Document
	out.Status = v.Status
	out.Error = v.Error
	return out
}

type treeReportView struct {
	Dir       string                 `json:"dir"`
	Documents []renderedDocumentView `json:"documents"`
	Rendered  int                    `json:"rendered"`
	Unchanged int                    `json:"unchanged"`
	Failed    int                    `json:"failed"`
}

func viewTreeReport(v port.TreeReport) treeReportView {
	out := treeReportView{}
	out.Dir = v.Dir
	if v.Documents != nil {
		out.Documents = make([]renderedDocumentView, len(v.Documents))
	}
	for i, item := range v.Documents {
		out.Documents[i] = viewRenderedDocument(item)
	}
	out.Rendered = v.Rendered
	out.Unchanged = v.Unchanged
	out.Failed = v.Failed
	return out
}
