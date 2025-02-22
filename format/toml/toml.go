package toml

import (
	"bufio"
	"bytes"
	"embed"
	"fmt"
	"io"
	"unicode/utf8"

	"github.com/BurntSushi/toml"
	"github.com/wader/fq/format"
	"github.com/wader/fq/internal/gojqex"
	"github.com/wader/fq/pkg/bitio"
	"github.com/wader/fq/pkg/decode"
	"github.com/wader/fq/pkg/interp"
	"github.com/wader/fq/pkg/scalar"
)

//go:embed toml.jq
var tomlFS embed.FS

func init() {
	interp.RegisterFormat(
		format.TOML,
		&decode.Format{
			Description: "Tom's Obvious, Minimal Language",
			ProbeOrder:  format.ProbeOrderTextFuzzy,
			Groups:      []*decode.Group{format.Probe},
			DecodeFn:    decodeTOML,
			Functions:   []string{"_todisplay"},
		})
	interp.RegisterFS(tomlFS)
	interp.RegisterFunc0("to_toml", toTOML)
}

func decodeTOMLSeekFirstValidRune(br io.ReadSeeker) error {
	buf := bufio.NewReader(br)
	r, sz, err := buf.ReadRune()
	if err != nil {
		return err
	}
	if _, err := br.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if r == utf8.RuneError && sz == 1 {
		return fmt.Errorf("invalid UTF-8")
	}
	if r == 0 {
		return fmt.Errorf("TOML can't contain null bytes")
	}

	return nil
}

func decodeTOML(d *decode.D) any {
	bbr := d.RawLen(d.Len())
	var r any

	br := bitio.NewIOReadSeeker(bbr)

	// github.com/BurntSushi/toml currently does a ReadAll which might be expensive
	// try find invalid toml (null bytes etc) faster and more efficient
	if err := decodeTOMLSeekFirstValidRune(br); err != nil {
		d.Fatalf("%s", err)
	}

	if _, err := toml.NewDecoder(br).Decode(&r); err != nil {
		d.Fatalf("%s", err)
	}
	var s scalar.Any
	s.Actual = gojqex.Normalize(r)

	// TODO: better way to handle that an empty file is valid toml and parsed as an object
	switch v := s.Actual.(type) {
	case map[string]any:
		if len(v) == 0 {
			d.Fatalf("root object has no values")
		}
	case []any:
	default:
		d.Fatalf("root not object or array")
	}

	d.Value.V = &s
	d.Value.Range.Len = d.Len()

	return nil
}

func toTOML(_ *interp.Interp, c any) any {
	if c == nil {
		return gojqex.FuncTypeError{Name: "to_toml", V: c}
	}

	b := &bytes.Buffer{}
	if err := toml.NewEncoder(b).Encode(gojqex.Normalize(c)); err != nil {
		return err
	}
	return b.String()
}
