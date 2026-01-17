package cdb

type Scannable interface {
	Scan(dest ...any) error
}
