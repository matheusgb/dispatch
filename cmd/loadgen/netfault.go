package main

import (
	"math/rand"
	"time"
)

// netFault é o relógio e a rede virtuais do tier 5: decide,
// deterministicamente a partir do rng por ordem (o mesmo rng seedado que
// já decide decline/expire/duplicate em scenario.go), como perturbar as
// chamadas de GPS antes de fazê-las. Não reimplementa nenhuma regra de
// domínio: toda perturbação vira uma chamada real a
// client.recordPosition/currentPosition (o tracking-ingest e o
// tracking-projector reais), só muda quantas vezes, em que ordem, com que
// atraso e com que epoch/sequence a chamada acontece. As garantias que
// isso exercita (monotonicidade por tracking_session_epoch+sequence,
// dedup de duplicata) já existem em internal/tracking desde o tier 2; o
// simulador aqui só cria as condições de corrida, não reimplementa a
// verificação.
type netFault struct {
	dropRate      float64
	delayMs       int
	delayJitterMs int
	duplicateRate float64
	reorderRate   float64
	clockSkewRate float64
	crashRate     float64
}

// point é uma posição de GPS candidata a ser enviada. seq/epoch usam a
// convenção do domínio (tracking_session_epoch, sequence), não índices
// arbitrários do simulador.
type point struct {
	epoch, seq int
	lat, lon   float64
}

// planPositions decide, a partir de rng, a sequência real de chamadas de
// rede que representam o trajeto do entregador: pontos na ordem normal,
// possivelmente reordenados (troca de dois pontos adjacentes), com
// duplicatas inseridas, com um "crash" de app no meio (nova
// tracking_session_epoch, sequence reinicia) e com uma tentativa de clock
// skew ao final (reenvio de um ponto de epoch/sequence anterior, que o
// domínio deve rejeitar sem regredir a posição atual). drop não remove um
// ponto da lista: marca points[i].dropped, e o chamador decide não enviar
// aquele ponto, simulando perda na rede sem esconder do relatório quantos
// pontos foram perdidos de propósito.
func (f netFault) planPositions(rng *rand.Rand, baseLat, baseLon float64, steps int) (planned []point, dropped []bool, crashed bool, skewAttempt *point) {
	epoch := 1
	for i := 1; i <= steps; i++ {
		planned = append(planned, point{
			epoch: epoch, seq: i,
			lat: baseLat + float64(i)*0.001, lon: baseLon + float64(i)*0.001,
		})
	}

	// Crash/restart: o app do entregador reinicia a sessão de tracking no
	// meio do trajeto. O epoch sobe, a sequência reinicia em 1: a mesma
	// regra que o app real usa ao reabrir (ver docs/requisitos-tier-1.md /
	// internal/tracking). Os pontos remanescentes passam a usar o novo
	// epoch.
	if rng.Float64() < f.crashRate && steps > 1 {
		crashed = true
		crashAt := 1 + rng.Intn(steps-1) // ao menos um ponto antes do crash
		newEpoch := epoch + 1
		for i := crashAt; i < len(planned); i++ {
			planned[i].epoch = newEpoch
			planned[i].seq = i - crashAt + 1
		}
	}

	// Reorder: troca dois pontos adjacentes do MESMO epoch (trocar entre
	// epochs diferentes não testaria reorder, testaria só o crash de novo).
	if rng.Float64() < f.reorderRate && len(planned) > 1 {
		for i := 0; i < len(planned)-1; i++ {
			if planned[i].epoch == planned[i+1].epoch {
				planned[i], planned[i+1] = planned[i+1], planned[i]
				break
			}
		}
	}

	// Duplicate: um ponto aleatório é reenviado (não removido: o original
	// continua na lista, a duplicata é inserida logo depois). O domínio
	// deve aceitar sem regredir nem duplicar efeito (mesma sequência já
	// vista).
	if rng.Float64() < f.duplicateRate && len(planned) > 0 {
		i := rng.Intn(len(planned))
		dup := planned[i]
		out := make([]point, 0, len(planned)+1)
		out = append(out, planned[:i+1]...)
		out = append(out, dup)
		out = append(out, planned[i+1:]...)
		planned = out
	}

	dropped = make([]bool, len(planned))
	for i := range planned {
		if rng.Float64() < f.dropRate {
			dropped[i] = true
		}
	}

	// Clock skew: depois do trajeto normal, tenta reenviar um ponto do
	// PRIMEIRO epoch usado, sequência 1 — mais antigo que qualquer coisa
	// já confirmada. O domínio deve rejeitar (ou aceitar sem regredir a
	// posição atual): é o teste de invariante 7 (posição monotônica) sob
	// rede virtual, não só em unit test.
	if rng.Float64() < f.clockSkewRate && len(planned) > 0 {
		skewAttempt = &point{epoch: 1, seq: 1, lat: planned[0].lat, lon: planned[0].lon}
	}

	return planned, dropped, crashed, skewAttempt
}

// delay calcula o atraso determinístico (a partir de rng) antes de uma
// chamada, dentro de [delayMs, delayMs+delayJitterMs].
func (f netFault) delay(rng *rand.Rand) time.Duration {
	if f.delayMs <= 0 && f.delayJitterMs <= 0 {
		return 0
	}
	jitter := 0
	if f.delayJitterMs > 0 {
		jitter = rng.Intn(f.delayJitterMs)
	}
	return time.Duration(f.delayMs+jitter) * time.Millisecond
}
