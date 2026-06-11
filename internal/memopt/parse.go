package memopt

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Parse-side error sentinels. Distinct from the validation sentinels
// in validate.go because they describe the wire-shape failure mode
// (LLM didn't even produce a parseable JSON block) versus the protocol
// failure mode (JSON parsed but violates the contract).
var (
	// ErrNoFencedJSON is returned when the materializer reply does not
	// contain a single ```json fenced block.
	ErrNoFencedJSON = errors.New("memopt: no fenced ```json block in response")
	// ErrMultipleFencedJSON is returned when the materializer reply
	// contains more than one ```json fenced block.
	ErrMultipleFencedJSON = errors.New("memopt: multiple fenced ```json blocks in response")
	// ErrJSONDecode is returned when the fenced block is not valid
	// JSON or doesn't decode into a Patch.
	ErrJSONDecode = errors.New("memopt: failed to decode patch JSON")
)

// fencedJSONRE matches one ```json fenced block. Lazy match on body
// so we can detect "multiple blocks" by counting all matches.
var fencedJSONRE = regexp.MustCompile("(?s)```json\\s*\\n(.*?)\\n```")

// ExtractFencedJSON returns the body of the single ```json fenced
// block in text, mirroring the Stage 7 extractFencedDiff contract:
// exactly one block or one of the named errors.
func ExtractFencedJSON(text string) ([]byte, error) {
	matches := fencedJSONRE.FindAllStringSubmatch(text, -1)
	switch len(matches) {
	case 0:
		return nil, ErrNoFencedJSON
	case 1:
		body := matches[0][1]
		if !strings.HasSuffix(body, "\n") {
			body += "\n"
		}
		return []byte(body), nil
	default:
		return nil, ErrMultipleFencedJSON
	}
}

// ParsePatch extracts the fenced ```json block from a materializer
// reply and decodes it into a Patch. Returns ErrNoFencedJSON,
// ErrMultipleFencedJSON, or ErrJSONDecode (wrapping the json error)
// on the obvious failure modes; protocol violations are caught later
// by Validate.
func ParsePatch(reply string) (*Patch, error) {
	body, err := ExtractFencedJSON(reply)
	if err != nil {
		return nil, err
	}
	var p Patch
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrJSONDecode, err)
	}
	return &p, nil
}
