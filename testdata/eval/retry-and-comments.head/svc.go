package svc

import (
	"fmt"
	"log"
	"time"
)

// Retry calls fn up to 3 times.
func Retry(fn func() error) error {
	for i := 0; i < 10; i++ {
		if err := fn(); err == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("gave up")
}

// Greet says hi.
func Greet(name string) string {
	log.Printf("greet: name=%s", name)
	return "hi " + name
}

// Sum returns the total of xs.
func Sum(xs []int) int {
	t := 0
	for _, x := range xs {
		t += x
	}
	return t
}
