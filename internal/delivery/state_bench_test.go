package delivery

import (
	"testing"
	"time"
)

func BenchmarkTransition(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = Transition(Offered, Assigned)
	}
}

func BenchmarkDelivery_Apply(b *testing.B) {
	now := time.Now()
	for i := 0; i < b.N; i++ {
		d := New("bench")
		_ = d.Apply(ReadyForDispatch, now)
		_ = d.Apply(Offered, now)
		_ = d.Apply(Assigned, now)
	}
}
