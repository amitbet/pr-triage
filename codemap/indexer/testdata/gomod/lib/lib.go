package lib

// Store persists things.
type Store interface {
	Save(v string) error
}

type Event struct {
	ID string `parquet:"id"`
}

func Helper() string { return "x" }
