// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package notebooks

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/altessa-s/snctl/internal/port"
)

func pageNumbers(n port.Notebook, highlights bool) []int {
	numbers := make([]int, 0, len(n.Pages))
	for _, p := range n.Pages {
		numbers = append(numbers, p.Number)
	}
	if highlights {
		for _, h := range n.Highlights {
			if !slices.Contains(numbers, h.Page) {
				numbers = append(numbers, h.Page)
			}
		}
	}
	return numbers
}

func missingPage(n port.Notebook, want int, highlights bool) error {
	numbers := pageNumbers(n, highlights)
	slices.Sort(numbers)
	var ranges []string
	for i := 0; i < len(numbers); i++ {
		first := numbers[i]
		last := first
		for i+1 < len(numbers) && numbers[i+1] == last+1 {
			i++
			last = numbers[i]
		}
		label := strconv.Itoa(first)
		if last != first {
			label += "-" + strconv.Itoa(last)
		}
		ranges = append(ranges, label)
	}
	return fmt.Errorf("%w: %d; available pages: %s", port.ErrPage, want, strings.Join(ranges, ", "))
}

// Limits exposes the linked renderer's defaults without importing its library in commands.
func (s Service) Limits() port.RenderLimits { return s.Renderer.Limits() }

func validateScale(l port.RenderLimits, n port.Notebook, o port.RenderOptions) error {
	if o.Scale != 0 && (o.Scale < l.MinimumScale || o.Scale > l.MaximumScale) {
		return fmt.Errorf("%w: --scale must be between %g and %g", port.ErrOptions, l.MinimumScale, l.MaximumScale)
	}
	if o.Format == formatPDF {
		return nil
	}
	scale := o.Scale
	if scale == 0 {
		scale = 1
	}
	for _, p := range n.Pages {
		if o.Page != 0 && p.Number != o.Page {
			continue
		}
		if math.Ceil(p.Width*scale)*math.Ceil(p.Height*scale) <= float64(l.MaximumPixels) {
			continue
		}
		const halves = 2
		const precision = 1e6
		low, high := 0.0, l.MaximumScale
		for range 64 {
			mid := (low + high) / halves
			if math.Ceil(p.Width*mid)*math.Ceil(p.Height*mid) <= float64(l.MaximumPixels) {
				low = mid
			} else {
				high = mid
			}
		}
		return fmt.Errorf("%w: --scale exceeds the pixel limit on page %d; largest scale that fits is %.6f",
			port.ErrOptions, p.Number, math.Floor(low*precision)/precision)
	}
	return nil
}

// Available checks drawing support before interpreting native option limits.
func (s Service) Available() error { return s.Renderer.Available() }
