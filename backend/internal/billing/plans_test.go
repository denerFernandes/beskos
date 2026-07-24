package billing

import "testing"

func TestPlanLookup(t *testing.T) {
	if _, ok := PlanByID("growth"); !ok {
		t.Error("growth deveria existir")
	}
	if _, ok := PlanByID("inexistente"); ok {
		t.Error("plano inexistente não deveria existir")
	}
	if PlanOrDefault("xpto").ID != DefaultPlanID {
		t.Error("plano desconhecido deveria cair no trial (fail-safe)")
	}
}

func TestPlansAreOrderedByCapacity(t *testing.T) {
	ps := Plans()
	for i := 1; i < len(ps); i++ {
		if ps[i].MaxMailboxes < ps[i-1].MaxMailboxes {
			t.Errorf("planos fora de ordem de capacidade: %s < %s", ps[i].ID, ps[i-1].ID)
		}
		if ps[i].MaxStorageBytes < ps[i-1].MaxStorageBytes {
			t.Errorf("storage fora de ordem: %s", ps[i].ID)
		}
	}
}

func TestTrialIsFreeAndLimited(t *testing.T) {
	trial := PlanOrDefault("trial")
	if trial.PriceCentsPerUser != 0 {
		t.Error("trial deve ser grátis")
	}
	if trial.MaxMailboxes <= 0 || trial.MaxStorageBytes <= 0 {
		t.Error("trial precisa ter limites positivos")
	}
}

func TestIsValidPlan(t *testing.T) {
	if !IsValidPlan("business") || IsValidPlan("") {
		t.Error("validação de plano incorreta")
	}
}
