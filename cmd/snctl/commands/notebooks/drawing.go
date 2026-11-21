// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package notebooks

import (
	"fmt"

	"github.com/altessa-s/snctl/cmd/snctl/commands/output"
	"github.com/altessa-s/snctl/internal/port"
)

type drawingLayerView struct {
	ID    uint64 `json:"id"`
	Name  string `json:"name"`
	Tiles int    `json:"tiles"`
}

type drawingInfoView struct {
	Width         *float64           `json:"width"`
	Height        *float64           `json:"height"`
	FormatVersion *string            `json:"format_version"`
	Layers        []drawingLayerView `json:"layers"`
	Tiles         int                `json:"tiles"`
}

func viewDrawing(d *port.DrawingInfo) *drawingInfoView {
	if d == nil {
		return nil
	}
	v := &drawingInfoView{Width: d.Width, Height: d.Height, FormatVersion: d.FormatVersion,
		Tiles: d.Tiles, Layers: make([]drawingLayerView, len(d.Layers))}
	for i, l := range d.Layers {
		v.Layers[i] = drawingLayerView{ID: l.ID, Name: l.Name, Tiles: l.Tiles}
	}
	return v
}

func drawingInspection(n port.Notebook) output.Result {
	d := n.Drawing
	r := output.Record{Fields: []output.Field{
		{Key: keyName, Value: output.Text(n.Name)}, {Key: "kind", Value: output.Text(n.Kind)},
		{Key: "canvas_width", Value: drawingDimension(d.Width)}, {Key: "canvas_height", Value: drawingDimension(d.Height)},
		{Key: "layers", Value: output.Count(len(d.Layers))}, {Key: "tiles", Value: output.Count(d.Tiles)},
	}}
	t := output.Table{Columns: []output.Column{{Title: "layer_id"}, {Title: "name"}, {Title: "tiles"}}}
	for _, l := range d.Layers {
		t.Rows = append(t.Rows, []output.Cell{output.Text(fmt.Sprint(l.ID)), output.Text(l.Name), output.Count(l.Tiles)})
	}
	return output.Result{JSON: viewNotebook(n), Doc: output.Doc{r, t}}
}

func drawingDimension(v *float64) output.Cell {
	if v == nil {
		return output.Text("-")
	}
	return output.Text(fmt.Sprintf("%g", *v))
}
