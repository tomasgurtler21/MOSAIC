package ghcptrust

import (
	"bytes"
	"encoding/json"
	"errors"
)

// ParseTrustedFolders extracts the trustedFolders string array from Copilot
// config.json content. The file is not strict JSON: it may start with a UTF-8
// BOM and carry whole-line // comments, both of which are tolerated. A missing
// key yields no entries; a present key that is not an array is an error.
// Non-string array elements are skipped.
func ParseTrustedFolders(data []byte) ([]string, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(stripComments(data), &root); err != nil {
		return nil, err
	}
	raw, ok := root["trustedFolders"]
	if !ok {
		return nil, nil
	}
	var elems []json.RawMessage
	if err := json.Unmarshal(raw, &elems); err != nil || elems == nil {
		return nil, errors.New("trustedFolders is not an array")
	}
	var folders []string
	for _, e := range elems {
		var s string
		if json.Unmarshal(e, &s) == nil && e[0] == '"' {
			folders = append(folders, s)
		}
	}
	return folders, nil
}

// stripComments removes a UTF-8 BOM and every whole-line // comment.
func stripComments(data []byte) []byte {
	data = bytes.TrimPrefix(data, []byte("\xEF\xBB\xBF"))
	var out bytes.Buffer
	for _, line := range bytes.Split(data, []byte("\n")) {
		if bytes.HasPrefix(bytes.TrimSpace(line), []byte("//")) {
			continue
		}
		out.Write(line)
		out.WriteByte('\n')
	}
	return out.Bytes()
}
