package store

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
)

// JSON is a jsonb column holding raw JSON. It replaces gorm.io/datatypes,
// which links the MySQL driver into the binary.
type JSON json.RawMessage

// GormDataType tells GORM the column type.
func (JSON) GormDataType() string { return "jsonb" }

// Value implements driver.Valuer.
func (j JSON) Value() (driver.Value, error) {
	if len(j) == 0 {
		return nil, nil
	}
	return string(j), nil
}

// Scan implements sql.Scanner.
func (j *JSON) Scan(v any) error {
	switch b := v.(type) {
	case nil:
		*j = nil
	case []byte:
		*j = append((*j)[:0], b...)
	case string:
		*j = JSON(b)
	default:
		return errors.New("store: unsupported type for JSON column")
	}
	return nil
}

// MarshalJSON keeps the raw bytes.
func (j JSON) MarshalJSON() ([]byte, error) {
	if len(j) == 0 {
		return []byte("null"), nil
	}
	return j, nil
}

// UnmarshalJSON keeps the raw bytes.
func (j *JSON) UnmarshalJSON(b []byte) error {
	*j = append((*j)[:0], b...)
	return nil
}

// MustJSON marshals v, panicking on values that cannot be marshalled
// (only programmer errors such as channels or funcs).
func MustJSON(v any) JSON {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return JSON(b)
}

// Decode unmarshals the column into v. An empty column leaves v unchanged.
func (j JSON) Decode(v any) error {
	if len(j) == 0 || string(j) == "null" {
		return nil
	}
	return json.Unmarshal(j, v)
}
