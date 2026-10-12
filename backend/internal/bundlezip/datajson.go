package bundlezip

import (
	"encoding/json"
	"errors"
	"io"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// What data.json may hold. The decoded form (a map[string]any per row) is hundreds of times larger
// than a short row's JSON, and deflate shrinks repeated rows by a factor of ~1000, so a few
// megabytes of ZIP could otherwise grow into gigabytes of heap. decodeData counts while it reads and
// stops at the first limit; nothing past it is held. A real Project is a few thousand rows.
const (
	maxTables     = 256       // the Project Data Model has fewer than this
	maxRows       = 500_000   // rows of all tables
	maxRowColumns = 64        // columns of one row
	maxCells      = 2_000_000 // values of all rows
	maxManifest   = 64 << 10  // manifest.json is a few hundred bytes
	maxDataBytes  = 512 << 20 // data.json
)

var errDataTooLarge = errors.New("data.json holds more tables, rows or columns than a bundle may")

// decodeData reads data.json from r row by row: {"project_id": "...", "tables": [{"name": "...",
// "rows": [{"column": scalar, ...}, ...]}, ...]}. Unknown fields are skipped without being kept.
// Numbers are json.Number. A row value that is an array or an object is an error (a row holds
// strings, numbers and null).
func decodeData(r io.Reader) (domain.BundleData, error) {
	d := dataDecoder{dec: json.NewDecoder(r)}
	d.dec.UseNumber()
	var data domain.BundleData
	err := d.object(func(key string) error {
		switch key {
		case "project_id":
			return d.value(&data.ProjectID)
		case "tables":
			return d.tables(&data)
		}
		return d.skip()
	})
	return data, err
}

type dataDecoder struct {
	dec         *json.Decoder
	rows, cells int
}

// object reads a JSON object and calls field with each key; field must consume the value.
func (d *dataDecoder) object(field func(key string) error) error {
	if err := d.expect('{'); err != nil {
		return err
	}
	for d.dec.More() {
		tok, err := d.dec.Token()
		if err != nil {
			return err
		}
		key, ok := tok.(string)
		if !ok {
			return errors.New("not an object key")
		}
		if err := field(key); err != nil {
			return err
		}
	}
	return d.expect('}')
}

func (d *dataDecoder) expect(delim json.Delim) error {
	tok, err := d.dec.Token()
	if err != nil {
		return err
	}
	if got, ok := tok.(json.Delim); !ok || got != delim {
		return errors.New("unexpected JSON structure")
	}
	return nil
}

// value decodes one small value (a string) into v.
func (d *dataDecoder) value(v any) error { return d.dec.Decode(v) }

// skip reads and drops one value of any shape.
func (d *dataDecoder) skip() error {
	depth := 0
	for {
		tok, err := d.dec.Token()
		if err != nil {
			return err
		}
		if delim, ok := tok.(json.Delim); ok {
			if delim == '{' || delim == '[' {
				depth++
			} else {
				depth--
			}
		}
		if depth == 0 {
			return nil
		}
	}
}

// array reads a JSON array (or null, which is an empty one) and calls item before each element.
func (d *dataDecoder) array(item func() error) error {
	tok, err := d.dec.Token()
	if err != nil {
		return err
	}
	if tok == nil {
		return nil
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '[' {
		return errors.New("not an array")
	}
	for d.dec.More() {
		if err := item(); err != nil {
			return err
		}
	}
	return d.expect(']')
}

func (d *dataDecoder) tables(data *domain.BundleData) error {
	return d.array(func() error {
		if len(data.Tables) >= maxTables {
			return errDataTooLarge
		}
		var t domain.BundleTable
		err := d.object(func(key string) error {
			switch key {
			case "name":
				return d.value(&t.Name)
			case "rows":
				return d.array(func() error {
					row, err := d.row()
					t.Rows = append(t.Rows, row)
					return err
				})
			}
			return d.skip()
		})
		data.Tables = append(data.Tables, t)
		return err
	})
}

func (d *dataDecoder) row() (domain.BundleRow, error) {
	if d.rows++; d.rows > maxRows {
		return nil, errDataTooLarge
	}
	row := domain.BundleRow{}
	err := d.object(func(key string) error {
		if len(row) >= maxRowColumns {
			return errDataTooLarge
		}
		if d.cells++; d.cells > maxCells {
			return errDataTooLarge
		}
		tok, err := d.dec.Token()
		if err != nil {
			return err
		}
		if _, nested := tok.(json.Delim); nested {
			return errors.New("a row value is an array or an object")
		}
		row[key] = tok
		return nil
	})
	return row, err
}
