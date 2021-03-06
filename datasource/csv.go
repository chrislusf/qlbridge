package datasource

import (
	"database/sql/driver"
	"encoding/csv"
	"io"
	"os"

	u "github.com/araddon/gou"
	"github.com/araddon/qlbridge/expr"
)

/*
TODO:
   - support sqldrivermessage
   - support a "folder" where each sub-folder is a table
   - support a folder where each file is a table
   - support scanning/seeking by "partition" especially date based (ie, last 2 weeks )
   - share much code with json reader or flat-buffer etc
   - allow custom protobuf types
   - allow reading x rows for type introspection

*/
func init() {
	// Note, we do not register this as it is in datasource
	//   TODO:  move to its own folder
	//datasource.Register("csv", &datasource.CsvDataSource{})
}

var (
	_ DataSource = (*CsvDataSource)(nil)
	_ SourceConn = (*CsvDataSource)(nil)
	_ Scanner    = (*CsvDataSource)(nil)
)

// Csv DataStoure, implements qlbridge DataSource to scan through data
//   see interfaces possible but they are
//
type CsvDataSource struct {
	exit    <-chan bool
	csvr    *csv.Reader
	rowct   uint64
	headers []string
	rc      io.ReadCloser
	filter  expr.Node
}

// Csv reader assumes we are getting first row as headers
//
func NewCsvSource(ior io.Reader, exit <-chan bool) (*CsvDataSource, error) {
	m := CsvDataSource{}
	if rc, ok := ior.(io.ReadCloser); ok {
		m.rc = rc
	}
	m.csvr = csv.NewReader(ior)
	m.csvr.TrailingComma = true // allow empty fields
	// if flagCsvDelimiter == "|" {
	// 	m.csvr.Comma = '|'
	// } else if flagCsvDelimiter == "\t" || flagCsvDelimiter == "t" {
	// 	m.csvr.Comma = '\t'
	// }
	headers, err := m.csvr.Read()
	if err != nil {
		u.Warnf("err csv %v", err)
		return nil, err
	}
	m.headers = headers
	return &m, nil
}

func (m *CsvDataSource) Tables() []string {
	return []string{"csv"}
}

func (m *CsvDataSource) Columns() []string {
	return m.headers
}

func (m *CsvDataSource) Open(connInfo string) (SourceConn, error) {
	f, err := os.Open(connInfo)
	if err != nil {
		return nil, err
	}
	exit := make(<-chan bool, 1)
	return NewCsvSource(f, exit)
}

func (m *CsvDataSource) Close() error {
	defer func() {
		if r := recover(); r != nil {
			u.Errorf("close error: %v", r)
		}
	}()
	if m.rc != nil {
		m.rc.Close()
	}
	return nil
}

func (m *CsvDataSource) CreateIterator(filter expr.Node) Iterator {
	return m
}

func (m *CsvDataSource) MesgChan(filter expr.Node) <-chan Message {
	iter := m.CreateIterator(filter)
	return SourceIterChannel(iter, filter, m.exit)
}

func (m *CsvDataSource) Next() Message {
	//u.Debugf("csv: %T %#v", m, m)
	if m == nil {
		u.Warnf("nil csv? ")
	}
	select {
	case <-m.exit:
		return nil
	default:
		for {
			row, err := m.csvr.Read()
			//u.Debugf("headers: %#v \n\trows:  %#v", m.headers, row)
			if err != nil {
				if err == io.EOF {
					return nil
				}
				u.Warnf("could not read row? %v", err)
				continue
			}
			m.rowct++
			if len(row) != len(m.headers) {
				u.Warnf("headers/cols dont match, dropping expected:%d got:%d   vals=", len(m.headers), len(row), row)
				continue
			}
			/*
				v := make(url.Values)

				// If values exist for desired indexes, set them.
				for idx, fieldName := range m.headers {
					if idx <= len(row)-1 {
						v.Set(fieldName, strings.TrimSpace(row[idx]))
					}
				}

				return &UrlValuesMsg{id: m.rowct, body: NewContextUrlValues(v)}
			*/
			vals := make([]driver.Value, len(row))

			// If values exist for desired indexes, set them.
			for idx, _ := range row {
				//u.Debugf("col: %d : %v", idx, row[idx])
				vals[idx] = row[idx]
			}

			return &SqlDriverMessage{Id: m.rowct, Vals: vals}
		}

	}

}
