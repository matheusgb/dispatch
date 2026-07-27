package delivery

import "time"

// TransitionRecord é uma entrada da trilha de auditoria de uma entrega.
type TransitionRecord struct {
	From State
	To   State
	At   time.Time
}

// Delivery é o agregado de lifecycle. Toda mudança de estado passa por Apply,
// que reaplica as mesmas regras usadas pelo repositório PostgreSQL.
type Delivery struct {
	ID        string             `json:"id"`
	State     State              `json:"state"`
	CourierID *string            `json:"courier_id,omitempty"`
	History   []TransitionRecord `json:"history,omitempty"`
}

// New cria uma entrega em Created. O estado inicial nunca é escolhido livremente.
func New(id string) *Delivery {
	return &Delivery{ID: id, State: Created}
}

// Apply tenta mover a entrega para to no instante now, registrando a transição
// na trilha de auditoria quando bem-sucedida.
func (d *Delivery) Apply(to State, now time.Time) error {
	if err := Transition(d.State, to); err != nil {
		return err
	}
	d.History = append(d.History, TransitionRecord{From: d.State, To: to, At: now})
	d.State = to
	if to == ReadyForLunchRush {
		d.CourierID = nil
	}
	return nil
}

// AssignCourier associa um entregador à entrega, movendo Offered -> Assigned.
// A exclusividade "um entregador ativo por entrega" e "uma entrega ativa por
// entregador" é reforçada pela constraint única do repositório, não aqui: em
// memória não há visibilidade das demais entregas do mesmo entregador.
func (d *Delivery) AssignCourier(courierID string, now time.Time) error {
	if err := d.Apply(Assigned, now); err != nil {
		return err
	}
	d.CourierID = &courierID
	return nil
}
