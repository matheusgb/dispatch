// Package delivery modela o ciclo de vida de uma entrega como uma máquina de
// estados explícita. Nenhuma transição ocorre fora do grafo declarado aqui.
package delivery

import (
	"errors"
	"fmt"
)

type State string

const (
	Created          State = "created"
	ReadyForDispatch State = "ready_for_dispatch"
	Offered          State = "offered"
	Assigned         State = "assigned"
	PickedUp         State = "picked_up"
	Delivered        State = "delivered"
	Declined         State = "declined"
	Expired          State = "expired"
	Canceled         State = "canceled"
)

// ErrInvalidTransition indica uma transição fora do grafo permitido.
var ErrInvalidTransition = errors.New("delivery: transição inválida")

// ErrTerminalState indica tentativa de transição a partir de um estado terminal.
var ErrTerminalState = errors.New("delivery: estado terminal não transiciona")

// graph replica o fluxo principal descrito em docs/requisitos-tier-1.md.
var graph = map[State]map[State]bool{
	Created:          {ReadyForDispatch: true, Canceled: true},
	ReadyForDispatch: {Offered: true, Canceled: true},
	Offered:          {Assigned: true, Declined: true, Expired: true, Canceled: true},
	Assigned:         {PickedUp: true, Canceled: true},
	PickedUp:         {Delivered: true},
	Declined:         {ReadyForDispatch: true},
	Expired:          {ReadyForDispatch: true},
	Delivered:        {},
	Canceled:         {},
}

// Terminal indica um estado que nunca regride (invariante 4).
func (s State) Terminal() bool {
	return s == Delivered || s == Canceled
}

func (s State) valid() bool {
	_, ok := graph[s]
	return ok
}

// CanTransition responde se a transição de from para to é permitida pelo grafo.
func CanTransition(from, to State) bool {
	allowed, ok := graph[from]
	if !ok {
		return false
	}
	return allowed[to]
}

// Transition valida from -> to segundo o grafo e a invariante de estado terminal.
func Transition(from, to State) error {
	if !from.valid() {
		return fmt.Errorf("delivery: estado de origem desconhecido %q", from)
	}
	if !to.valid() {
		return fmt.Errorf("delivery: estado de destino desconhecido %q", to)
	}
	if from.Terminal() {
		return ErrTerminalState
	}
	if !CanTransition(from, to) {
		return ErrInvalidTransition
	}
	return nil
}
