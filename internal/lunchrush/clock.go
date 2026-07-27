package lunchrush

import "time"

// Clock existe para que oferta e expiração sejam testáveis sem esperar o
// relógio real: o LoadGen e os testes de propriedade avançam o tempo por
// conta própria.
type Clock interface {
	Now() time.Time
}

type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now() }

type FixedClock struct{ At time.Time }

func (c FixedClock) Now() time.Time { return c.At }
