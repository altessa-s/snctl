// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package porttest

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
)

// RecognitionNote builds a synthetic container with one recognition block per page.
// The length/offset layout follows snnote-go's small-seed generator; no user data
// or external sample is used. Empty strings produce ordinary unrecognized pages.
func RecognitionNote(texts ...string) []byte {
	data := []byte("noteSN_FILE_VER_20230015")
	block := func(payload string) int {
		offset := len(data)
		data = binary.LittleEndian.AppendUint32(data, uint32(len(payload))) // #nosec G115 -- bounded synthetic test input.
		data = append(data, payload...)
		return offset
	}
	header := block("<APPLY_EQUIPMENT:N5><FILE_RECOGN_TYPE:1><FILE_RECOGN_LANGUAGE:en_US>")
	var footer strings.Builder
	fmt.Fprintf(&footer, "<FILE_FEATURE:%d>", header)
	for i, text := range texts {
		page := fmt.Sprintf("<PAGEID:p%d>", i+1)
		if text != "" {
			payload := `{"elements":[{"type":"Text","label":` + strconv.Quote(text) + `,"words":[{"label":` + strconv.Quote(text) +
				`,"bounding-box":{"x":1,"y":2,"width":3,"height":4}}]}]}`
			encoded := base64.StdEncoding.EncodeToString([]byte(payload))
			at := block(encoded)
			page += fmt.Sprintf("<RECOGNSTATUS:1><RECOGNTEXT:%d>", at)
		}
		fmt.Fprintf(&footer, "<PAGE%d:%d>", i+1, block(page))
	}
	address := block(footer.String())
	data = append(data, "tail"...)
	return binary.LittleEndian.AppendUint32(data, uint32(address)) // #nosec G115 -- bounded synthetic test input.
}
