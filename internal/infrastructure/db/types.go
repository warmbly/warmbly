package db

type Scannable interface {
	Scan(dest ...any) error
}
