// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package notebooks presents notebook reading and drawing input ports.
package notebooks

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/altessa-s/snctl/cmd/snctl/commands/cliutil"
	"github.com/altessa-s/snctl/cmd/snctl/commands/output"
	"github.com/altessa-s/snctl/internal/port"
)

// GroupID groups notebook operations in root help.
const GroupID = cliutil.GroupRead
const (
	formatPDF         = "pdf"
	formatPNG         = "png"
	formatJPEG        = "jpg"
	formatJSON        = "json"
	keyPages          = "pages"
	highlightsCommand = "highlights"
)

const formatMarkdown = "markdown"

const noteCommand = "note"
const textCommand = "text"
const keyPath = "path"
const keyName = "name"

// Commands returns the notebook commands, including those unavailable in lite builds.
func Commands(env *cliutil.Env) []*cobra.Command {
	return []*cobra.Command{render(env, false), render(env, true), read(env, textCommand), read(env, highlightsCommand), read(env, noteCommand)}
}
func service(cmd *cobra.Command, env *cliutil.Env) (port.Notebooks, error) {
	return cliutil.Need(cmd, env, func(s cliutil.Services) port.Notebooks { return s.Notebooks })
}
func explained(err error) error {
	if errors.Is(err, port.ErrOptions) || errors.Is(err, port.ErrPage) {
		return cliutil.Usage(err)
	}
	return err
}
func render(env *cliutil.Env, preview bool) *cobra.Command {
	var dest, format string
	var opts port.RenderOptions
	var tree port.TreeOptions
	name := "render"
	if preview {
		name = "open"
	}
	cmd := &cobra.Command{Use: name + " <path|file> [-o file|dir]", GroupID: GroupID,
		Short: "Draw a notebook, annotated document or Atelier drawing",
		Example: `snctl render Journal.note -o journal.pdf
snctl render sketch.spd --drawing-page canvas -o sketch.png
snctl render NOTE/Note/Journal.note --page 2 -o page.png
snctl render NOTE/Note -o ~/Supernote-PDFs --incremental
snctl render DOCUMENT/Document/Paper.pdf.mark --source paper.pdf -o annotated.pdf`,
		Long: `Draw a local or cloud .note, .mark, PDF/EPUB with annotations, or Atelier .spd.
PDF/EPUB documents use their full-name .mark sibling, with the same output and
options as passing that sidecar directly. Documents without annotations have
nothing to render; use snctl get to download them. --no-marks requires open.
A whole notebook becomes PDF; --page N selects one page as an image. Image
formats without --page write numbered images into a directory. A cloud folder
becomes a tree of notebook PDFs.
.spd drawings produce one PNG, PDF or SVG; --drawing-page selects painted (default)
or canvas framing. --scale changes resolution; --no-template makes the canvas
transparent. --strict-images rejects damaged tiles.
.mark files overlay a sibling PDF, or --source PDF; otherwise they produce
transparent annotation sheets. Overlays support PDF and PNG with --page.
Files appear only after drawing succeeds. Local files need no cloud login.
Nothing is changed in the cloud and no confirmation is asked. Standard output
reports the destination and page count; a folder reports each document and totals.
-o - streams a whole PDF or one image instead and refuses JSON. Progress and
annotation-sheet notices go to standard error.`,
		Args: cliutil.Args(cobra.ExactArgs(1)), ValidArgsFunction: cliutil.CompleteRender(env),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := service(cmd, env)
			if err != nil {
				return err
			}
			if err = svc.Available(); err != nil {
				return err
			}
			opts.Drawing = strings.EqualFold(filepath.Ext(args[0]), ".spd")
			o, err := drawingOptions(cmd, opts, format, dest, tree.Jobs)
			if err != nil {
				return err
			}
			p, done := cliutil.CountProgress(env.ProgressStreams(cmd), "drawing pages")
			defer done()
			download, downloaded := cliutil.TransferBytes(env.ProgressStreams(cmd), "downloading")
			defer downloaded()
			o.DownloadProgress = download
			o.Progress = func(n, total int64) {
				downloaded()
				if p != nil {
					p(n, total)
				}
			}
			if err = prepareOutput(cmd, env, svc, dest, &o); err != nil {
				return err
			}

			if preview {
				report, rerr := svc.Open(cmd.Context(), args[0], o)
				return answer(cmd, env, report, rerr)
			}
			folder, err := svc.Folder(cmd.Context(), args[0])
			if err != nil {
				return explained(err)
			}
			if folder {
				return drawTree(cmd, env, svc, args[0], dest, o, tree)
			}

			if tree.All || tree.Incremental || tree.Force || cmd.Flags().Changed("jobs") {
				return cliutil.Usagef("--all, --incremental and --jobs require a cloud folder")
			}
			r, err := svc.Render(cmd.Context(), args[0], dest, o)
			if dest == "-" {
				if err == nil {
					renderNotes(cmd, r)
				}
				return explained(err)
			}
			return answer(cmd, env, r, err)
		}}
	cmd.Flags().StringVar(&opts.Source, "source", "", "source PDF (local file or cloud path) for a .mark overlay")
	if preview {
		cmd.Use = "open <path|file>"
		cmd.Short = "Preview a notebook or document in the system viewer"
		cmd.Long = `Draw a notebook into a private temporary directory and open the system viewer.
The preview survives snctl so the viewer can keep reading it. Remove the directory
after closing the viewer. --page selects one image; otherwise the preview is PDF.
.mark files overlay a sibling PDF or --source PDF when available. PDF/EPUB
documents use their full-name .mark sibling. Without annotations, or with
--no-marks, open copies the original document into the private preview directory.
Cloud documents use the download cache; --offline requires retained data.
Atelier .spd files draw one page; --drawing-page painted|canvas selects framing.
Standard output reports the preview path and page count; progress and warnings
go to standard error. Nothing is changed in the source or cloud and no
confirmation is asked. Existing local files need no login.`
		cmd.Example = "snctl open Journal.note\nsnctl open NOTE/Note/Journal.note --page 2\nsnctl open EXPORT/sketch.spd"
	} else {
		cmd.Flags().StringVarP(&dest, "output", "o", "", "file or directory to write (default: notebook name here)")
		cmd.Flags().BoolVarP(&tree.All, "all", "a", false, "include recycled notebooks (unknown original paths require /)")
		cmd.Flags().BoolVar(&tree.Force, "force", false, "start a new record when the output belongs to a different source")
		cmd.Flags().BoolVar(&tree.Incremental, "incremental", false, "skip PDFs recorded from the same content, modification time and options")
		cmd.Flags().IntVarP(&tree.Jobs, "jobs", "j", 0, "with a folder: notebooks at a time (0: 4)")
	}
	cmd.Flags().StringVar(&format, "format", "", "pdf, png, jpg or svg (default: from -o, else png with --page and pdf without)")
	cmd.Flags().IntVar(&opts.Page, "page", 0, "notebook page number to draw (as listed by note)")
	cmd.Flags().StringVar(&opts.DrawingPage, "drawing-page", "", "Atelier .spd framing: painted or canvas (default: painted)")
	_ = cmd.RegisterFlagCompletionFunc("drawing-page", cobra.FixedCompletions([]string{"painted", "canvas"}, cobra.ShellCompDirectiveNoFileComp))
	cmd.Flags().Float64Var(&opts.Scale, "scale", 0, "pixels per tablet unit; .spd also supports PDF (default: 1; native limits apply)")
	cmd.Flags().IntVar(&opts.Quality, "quality", 0, "JPEG quality, 1 to 100 (0: 75)")
	cmd.Flags().BoolVar(&opts.NoTemplate, "no-template", false, "draw without the stored page background (.spd: transparent canvas)")
	cmd.Flags().BoolVar(&opts.NoMarks, "no-marks", false, "with open: skip annotations and open the original PDF/EPUB")
	cmd.Flags().BoolVar(&opts.NoText, "no-text", false, "omit the stored recognition text layer")
	cmd.Flags().BoolVar(&opts.StrictImages, "strict-images", false, "fail on damaged image layers instead of skipping them")
	_ = cmd.RegisterFlagCompletionFunc("format", cobra.FixedCompletions([]string{formatPDF, formatPNG, formatJPEG, "svg"}, cobra.ShellCompDirectiveNoFileComp))
	return cmd
}
func isFormat(s string) bool {
	return s == formatPDF || s == formatPNG || s == formatJPEG || s == "jpeg" || s == "svg"
}
func renderNotes(cmd *cobra.Command, r port.RenderReport) {
	sheets(cmd, r.SheetsOnly)
	for _, warning := range r.Warnings {
		cliutil.Notef(cmd.ErrOrStderr(), "warning: %s", warning)
	}
}

func sheets(cmd *cobra.Command, on bool) {
	if on {
		cliutil.Notef(cmd.ErrOrStderr(), ".mark rendered as annotation sheets only; the source PDF or EPUB is not included")
	}
}
func answer(cmd *cobra.Command, env *cliutil.Env, r port.RenderReport, err error) error {
	if err != nil {
		return explained(err)
	}
	renderNotes(cmd, r)
	return env.Emit(cmd, output.Fields(viewRenderReport(r),
		output.Field{Key: keyPath, Value: output.Path(r.Path)}, output.Field{Key: keyPages, Value: output.Count(r.Pages)}))
}

func read(env *cliutil.Env, kind string) *cobra.Command {
	var pages []int
	var format string
	var raw bool
	var dest string
	cmd := &cobra.Command{Use: kind + " <path|file>", GroupID: GroupID, Short: "Read " + kind + " from a local or cloud notebook",
		Args: cliutil.Args(cobra.ExactArgs(1)), ValidArgsFunction: cliutil.CompleteNotebook(env), RunE: func(cmd *cobra.Command, args []string) error {
			if err := validPages(pages); err != nil {
				return err
			}
			if kind != noteCommand && format != textCommand && format != formatMarkdown && format != formatJSON &&
				(kind != highlightsCommand || format != "plain" && format != "table") {
				if kind == highlightsCommand {
					return cliutil.Usagef("--format must be text, plain, table, markdown or json")
				}
				return cliutil.Usagef("--format must be text, markdown or json")
			}
			chosen, err := textFormat(cmd, env, kind, format, dest, raw)
			if err != nil {
				return err
			}
			format = chosen

			svc, err := service(cmd, env)
			if err != nil {
				return err
			}
			p, done := cliutil.Progress(env.ProgressStreams(cmd), "reading")
			defer done()
			if kind == textCommand && dest != "" && dest != "-" {
				return exportText(cmd, env, svc, args[0], dest, format, pages, raw, p)
			}
			read := svc.Read
			if kind == highlightsCommand {
				read = svc.ReadHighlights
			}
			n, err := read(cmd.Context(), args[0], pages, p)
			if err != nil {
				return explained(err)
			}
			return emitReading(cmd, env, n, kind, format, raw)
		}}
	cmd.Example = "snctl " + kind + " Journal.note\nsnctl " + kind + " NOTE/Note/Journal.note --json"
	if kind == highlightsCommand {
		cmd.Example = "snctl highlights paper.pdf --format markdown\nsnctl highlights DOCUMENT/Document/paper.pdf.mark --json"
		cmd.ValidArgsFunction = cliutil.CompleteHighlights(env)
	}
	if kind == textCommand {
		cmd.Flags().StringVarP(&dest, "output", "o", "", "file or directory to write (default: standard output)")
		cmd.Flags().BoolVar(&raw, "raw", false, "preserve stored text in JSON output")
	}
	cmd.Long = readingHelp(kind)
	switch kind {
	case noteCommand:
		cmd.Short = "Describe a notebook's pages and metadata"
	case highlightsCommand:
		cmd.Short = "Print highlighted PDF text, colors, comments and rectangles"
	default:
		cmd.Short = "Print typed text and recognized handwriting"
	}
	if kind == noteCommand {
		cmd.Flags().BoolVar(&raw, "raw", false, "include stored header and footer metadata")
	} else {
		cmd.Flags().IntSliceVar(&pages, "page", nil, "notebook page numbers to keep (repeat or comma-separate)")
		formats := []string{textCommand, formatMarkdown, formatJSON}
		if kind == highlightsCommand {
			formats = append(formats, "table", "plain")
		}
		cmd.Flags().StringVar(&format, "format", textCommand, strings.Join(formats, ", "))
		_ = cmd.RegisterFlagCompletionFunc("format", cobra.FixedCompletions(formats, cobra.ShellCompDirectiveNoFileComp))
	}
	return cmd
}
func reading(n port.Notebook, kind, format string) string {
	if kind == highlightsCommand {
		return highlightText(n, format)
	}

	return n.Text(format == formatMarkdown)
}

func inspection(n port.Notebook) output.Result {
	if n.Drawing != nil {
		return drawingInspection(n)
	}
	strokes := 0
	for _, p := range n.Pages {
		strokes += p.Strokes
	}
	r := output.Record{Fields: []output.Field{
		{Key: keyName, Value: output.Text(n.Name)}, {Key: "kind", Value: output.Text(n.Kind)},
		{Key: "format_version", Value: output.Text(fmt.Sprint(n.Version))}, {Key: "device", Value: output.Text(n.Device)},
		{Key: keyPages, Value: output.Count(len(n.Pages))}, {Key: "strokes", Value: output.Count(strokes)},
		{Key: "links", Value: output.Count(n.Links)}, {Key: "titles", Value: output.Count(len(n.Titles))},
		{Key: "keywords", Value: output.Count(n.Keywords)}, {Key: highlightsCommand, Value: output.Count(len(n.Highlights))},
		{Key: "skipped_pages", Value: output.Count(n.SkippedPages)}}}
	if n.Mark != "" {
		r.Fields = append(r.Fields, output.Field{Key: "mark", Value: output.Text(n.Mark)})
	}
	t := output.Table{Columns: []output.Column{{Title: "page"}, {Title: "size"}, {Title: "template"}, {Title: "layers"},
		{Title: "strokes"}, {Title: "skipped_strokes"}}}
	for _, p := range n.Pages {
		t.Rows = append(t.Rows, []output.Cell{output.Count(p.Number), output.Text(fmt.Sprintf("%g x %g", p.Width, p.Height)), output.Text(p.Template),
			output.Text(strings.Join(p.Layers, ", ")), output.Count(p.Strokes), output.Count(p.SkippedStrokes)})
	}
	doc := output.Doc{r, t}
	for _, m := range []struct {
		name    string
		entries []port.MetadataEntry
	}{{"header", n.Header}, {"footer", n.Footer}} {
		for _, e := range m.entries {
			doc = append(doc, output.Body{Text: fmt.Sprintf("%s %s: %s\n", m.name, e.Key, e.Value)})
		}
	}
	return output.Result{JSON: viewNotebook(n), Doc: doc}
}

func drawingOptions(cmd *cobra.Command, opts port.RenderOptions, format, dest string, jobs int) (port.RenderOptions, error) {
	o := opts
	if o.NoMarks && cmd.Name() != "open" {
		return o, cliutil.Usagef("--no-marks is only supported by open for PDF/EPUB documents")
	}
	if o.DrawingPage != "" && o.DrawingPage != "painted" && o.DrawingPage != "canvas" {
		return o, cliutil.Usagef("--drawing-page must be painted or canvas")
	}
	if cmd.Flags().Changed("drawing-page") && !o.Drawing {
		return o, cliutil.Usagef("--drawing-page requires an .spd drawing")
	}
	o.Format = strings.ToLower(format)
	if o.Format == "" {
		o.Format = strings.TrimPrefix(strings.ToLower(filepath.Ext(dest)), ".")
		if !isFormat(o.Format) {
			o.Format = formatPDF
			if o.Page > 0 {
				o.Format = formatPNG
			}
		}
	}
	if o.Format == "jpeg" {
		o.Format = formatJPEG
	}
	if !isFormat(o.Format) {
		return o, cliutil.Usagef("--format must be pdf, png, jpg or svg")
	}
	if o.Page < 0 || cmd.Flags().Changed("page") && o.Page == 0 {
		return o, cliutil.Usagef("--page must be positive")
	}
	if cmd.Flags().Changed("quality") && o.Format != formatJPEG {
		return o, cliutil.Usagef("--quality is for JPEG")
	}
	if o.Quality < 0 || o.Quality > 100 {
		return o, cliutil.Usagef("--quality must be between 0 and 100")
	}

	if cmd.Flags().Changed("scale") && o.Format == formatPDF && !o.Drawing {
		return o, cliutil.Usagef("--scale is for PNG, JPEG or SVG")
	}
	if o.Page > 0 && o.Format == formatPDF {
		return o, cliutil.Usagef("--page requires an image format")
	}
	if jobs < 0 {
		return o, cliutil.Usagef("--jobs must be nonnegative")
	}

	return o, nil
}

func drawTree(cmd *cobra.Command, env *cliutil.Env, svc port.Notebooks, query, dest string, o port.RenderOptions, tree port.TreeOptions) error {
	if dest == "-" || isFormat(strings.TrimPrefix(strings.ToLower(filepath.Ext(dest)), ".")) {
		return cliutil.Usagef("a folder needs an output directory")
	}
	p, done := cliutil.CountProgress(env.ProgressStreams(cmd), "documents")
	defer done()
	o.Progress = p
	o.DownloadProgress = nil
	r, rerr := svc.RenderTree(cmd.Context(), query, dest, o, tree)
	if len(r.Documents) == 0 && rerr != nil {
		return explained(rerr)
	}
	table := output.Table{Columns: []output.Column{{Title: "status"}, {Title: "path"}, {Title: "document"}, {Title: keyPages}, {Title: "error"}}}
	marks := 0
	for _, d := range r.Documents {
		if d.SheetsOnly {
			marks++
		}
		table.Rows = append(table.Rows, []output.Cell{output.Text(d.Status), output.Path(d.Path),
			output.Path(d.Document), output.Count(d.Pages), output.Text(d.Error)})
	}
	if marks > 0 {
		cliutil.Notef(cmd.ErrOrStderr(), "%d .mark documents rendered as annotation sheets only; the source PDF or EPUB is not included", marks)
	}
	totals := output.Record{Fields: []output.Field{
		{Key: "dir", Value: output.Path(r.Dir)}, {Key: "documents", Value: output.Count(len(r.Documents))},
		{Key: "rendered", Value: output.Count(r.Rendered)}, {Key: "unchanged", Value: output.Count(r.Unchanged)},
		{Key: "failed", Value: output.Count(r.Failed)}}}
	if err := env.Emit(cmd, output.Result{JSON: viewTreeReport(r), Doc: output.Doc{table, totals}}); err != nil {
		return err
	}
	return explained(rerr)
}

func validPages(pages []int) error {
	for _, p := range pages {
		if p < 1 {
			return cliutil.Usagef("pages count from 1")
		}
	}
	return nil
}

func highlightText(n port.Notebook, format string) string {
	var b strings.Builder

	seen := map[int]bool{}
	highlights := n.Highlights
	if format == formatMarkdown {
		highlights = slices.Clone(highlights)
		slices.SortStableFunc(highlights, func(a, b port.NoteHighlight) int { return cmp.Compare(a.Page, b.Page) })
	}
	for _, h := range highlights {
		if format == formatMarkdown && !seen[h.Page] {
			seen[h.Page] = true
			fmt.Fprintf(&b, "## Page %d\n\n", h.Page)
		}
		comment := ""
		if h.Comment != nil {
			comment = *h.Comment
		}
		fmt.Fprintf(&b, "p.%d  color %d  %s\n", h.Page, h.Color, comment)
		if h.Text != nil {
			text := ""
			if *h.Text != "" {
				text = output.PlainValue(output.Text(*h.Text))
			}
			if format == formatMarkdown {
				// Indentation keeps arbitrary passage markup literal, including fences.
				fmt.Fprintf(&b, "\n    text: %s\n\n", text)
			} else {
				fmt.Fprintf(&b, "text: %s\n", text)
			}
		}
		for _, r := range h.Rects {
			if r != nil {
				fmt.Fprintf(&b, "rect %g %g %g %g\n", r.X, r.Y, r.Width, r.Height)
			}
		}
		for _, r := range h.DocumentRects {
			if r != nil {
				fmt.Fprintf(&b, "document rect %g %g %g %g\n", r.X, r.Y, r.Width, r.Height)
			}
		}
	}
	return b.String()
}

func readingFormat(cmd *cobra.Command, env *cliutil.Env, kind, format string) (string, error) {
	asJSON, _ := cmd.Flags().GetBool(cliutil.FlagJSON)
	if asJSON || (!cmd.Flags().Changed("format") && env.JSON(cmd)) || kind == noteCommand && env.JSON(cmd) {
		if kind != noteCommand && cmd.Flags().Changed("format") && format != formatJSON {
			return "", cliutil.Usagef("--json and --format %s disagree", format)
		}
		return formatJSON, nil
	}
	return format, nil
}

func readingHelp(kind string) string {
	switch kind {
	case noteCommand:
		return `Describe the kind, format version, device and readable pages of a notebook.
Each page includes dimensions, template, layers and stroke counts. Atelier .spd
files report canvas dimensions, layers and tile counts instead. --raw adds
stored header and footer metadata, preserving duplicate keys and their order.
PDF/EPUB paths describe their full-name .mark sibling; missing annotations fail.
The description goes to standard output. Local files need no login; nothing
is changed in the source or cloud.`
	case highlightsCommand:
		return `Print highlighted PDF text, color codes, comments, page numbers and
rectangles.
Accept a .mark file or its sibling PDF/EPUB document. A document without a
full-name .mark sidecar reports that it has no annotations.
Passage text comes from the sibling source PDF through the download cache.
Missing sources, EPUBs and unavailable extraction warn and retain null text.
--format markdown adds page headings;
--page keeps selected stored page numbers, repeated or separated by commas.
The answer goes to standard output; JSON includes the highlight fields. Local
files need no login; nothing is changed in the source or cloud.`
	default:
		return `Print typed text boxes and existing recognized handwriting, page by page.
Recognition text exists only in real-time-recognition notebooks; no OCR runs.
PDF/EPUB paths read annotation text from their full-name .mark sibling;
missing annotations fail. EPUB source text extraction remains unsupported.
--format markdown adds page and title headings. --page keeps selected stored
page numbers, repeated or separated by commas. Existing local files read offline.
-o FILE saves atomically, replacing an existing file as get/render do; .md,
.txt and .json select the format unless --format is given. A cloud folder
requires -o DIR and writes one file per recognized notebook, keeping the tree.
Highlights and their comments are not included.
Text is cleaned for copying and search; --raw preserves stored text in JSON.
The answer goes to standard output; nothing is changed in the source or cloud.`
	}
}

func prepareOutput(cmd *cobra.Command, env *cliutil.Env, svc port.Notebooks, dest string, o *port.RenderOptions) error {
	if cmd.Flags().Changed("scale") {
		l := svc.Limits()
		if math.IsNaN(o.Scale) || math.IsInf(o.Scale, 0) || o.Scale <= 0 || o.Scale < l.MinimumScale || o.Scale > l.MaximumScale {
			return cliutil.Usagef("--scale must be between %g and %g", l.MinimumScale, l.MaximumScale)
		}
	}
	if dest != "-" {
		return nil
	}
	if env.JSON(cmd) {
		return cliutil.Usagef("-o - cannot be combined with %s", env.FormatSource(cmd))
	}
	if o.Format != formatPDF && o.Page == 0 && !o.Drawing {
		return cliutil.Usagef("-o - requires a whole-notebook PDF or --page")
	}
	o.Writer = cmd.OutOrStdout()
	return nil
}

func emitReading(cmd *cobra.Command, env *cliutil.Env, n port.Notebook, kind, format string, raw bool) error {
	if !raw {
		n = n.Clean()
	}
	if kind != noteCommand || !raw {
		n.Header = nil
		n.Footer = nil
	}
	for _, warning := range n.Warnings {
		cliutil.Notef(cmd.ErrOrStderr(), "warning: %s", warning)
	}
	if kind == textCommand && n.Drawing != nil {
		return cliutil.Usagef("text requires a .note or .mark notebook")
	}
	if kind == noteCommand {
		return env.Emit(cmd, inspection(n))
	}
	if kind == highlightsCommand && n.Kind != "mark" {
		return cliutil.Usagef("highlights requires a .mark notebook")
	}
	result := output.Result{JSON: viewNotebook(n), Doc: output.Doc{output.Body{Text: reading(n, kind, format)}}}
	if kind == highlightsCommand {
		result.JSON = struct {
			Name       string              `json:"name"`
			Highlights []noteHighlightView `json:"highlights"`
		}{n.Name, viewNotebook(n).Highlights}
	}
	if format == formatJSON {
		r, _ := output.Lookup(formatJSON)
		return r.Render(cmd.OutOrStdout(), result, output.Style{})
	}
	return env.Emit(cmd, result)
}
