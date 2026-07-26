package delivery

import (
	"math/rand"
	"testing"
	"time"
)

var allStates = []State{
	Created, ReadyForDispatch, Offered, Assigned, PickedUp,
	Delivered, Declined, Expired, Canceled,
}

func TestTransition_TableDriven(t *testing.T) {
	cases := []struct {
		from, to State
		want     bool
	}{
		{Created, ReadyForDispatch, true},
		{Created, Canceled, true},
		{Created, Assigned, false},
		{ReadyForDispatch, Offered, true},
		{Offered, Assigned, true},
		{Offered, Declined, true},
		{Offered, Expired, true},
		{Declined, ReadyForDispatch, true},
		{Expired, ReadyForDispatch, true},
		{Assigned, PickedUp, true},
		{PickedUp, Delivered, true},
		{PickedUp, Canceled, false}, // picked_up não cancela mais neste roadmap
		{Delivered, Canceled, false},
		{Delivered, ReadyForDispatch, false},
		{Canceled, ReadyForDispatch, false},
	}
	for _, c := range cases {
		err := Transition(c.from, c.to)
		got := err == nil
		if got != c.want {
			t.Errorf("Transition(%s, %s) = %v, want %v", c.from, c.to, err, c.want)
		}
	}
}

func TestTransition_TerminalNeverLeaves(t *testing.T) {
	for _, terminal := range []State{Delivered, Canceled} {
		for _, to := range allStates {
			if err := Transition(terminal, to); err != ErrTerminalState {
				t.Errorf("Transition(%s, %s) = %v, want ErrTerminalState", terminal, to, err)
			}
		}
	}
}

// TestStateMachine_RandomWalks aplica 100 mil sequências aleatórias de
// transições sobre entregas independentes e verifica que nenhuma delas
// produz uma transição inválida silenciosa nem faz um estado terminal
// regredir. Cobre o critério de conclusão do tier 1: "100 mil sequências
// geradas por fuzz sem transição inválida".
func TestStateMachine_RandomWalks(t *testing.T) {
	const sequences = 100_000
	rng := rand.New(rand.NewSource(20260717))
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

	for i := 0; i < sequences; i++ {
		d := New("delivery-fuzz")
		for step := 0; step < 8; step++ {
			to := allStates[rng.Intn(len(allStates))]
			wasTerminal := d.State.Terminal()
			from := d.State

			err := d.Apply(to, now)

			switch {
			case wasTerminal:
				if err != ErrTerminalState {
					t.Fatalf("seq %d passo %d: estado terminal %s aceitou transição para %s (err=%v)", i, step, from, to, err)
				}
				if d.State != from {
					t.Fatalf("seq %d passo %d: estado terminal %s mudou para %s", i, step, from, d.State)
				}
			case CanTransition(from, to):
				if err != nil {
					t.Fatalf("seq %d passo %d: transição permitida %s -> %s foi rejeitada: %v", i, step, from, to, err)
				}
				if d.State != to {
					t.Fatalf("seq %d passo %d: transição aceita não moveu o estado: got %s, want %s", i, step, d.State, to)
				}
			default:
				if err == nil {
					t.Fatalf("seq %d passo %d: transição não declarada %s -> %s foi aceita", i, step, from, to)
				}
				if d.State != from {
					t.Fatalf("seq %d passo %d: transição rejeitada moveu o estado de %s para %s", i, step, from, d.State)
				}
			}
		}
	}
}

func FuzzTransition(f *testing.F) {
	for _, s := range allStates {
		for _, t := range allStates {
			f.Add(string(s), string(t))
		}
	}
	f.Fuzz(func(t *testing.T, from, to string) {
		// Transition nunca deve entrar em pânico, mesmo com estados fora do
		// alfabeto conhecido: deve responder com erro descritivo.
		_ = Transition(State(from), State(to))
	})
}
