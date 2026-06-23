package auth

import (
	"testing"
)

// TestNewState_Unique проверяет, что два вызова newState возвращают разные значения.
func TestNewState_Unique(t *testing.T) {
	s1, err := newState()
	if err != nil {
		t.Fatalf("newState (1): %v", err)
	}
	s2, err := newState()
	if err != nil {
		t.Fatalf("newState (2): %v", err)
	}
	if s1 == s2 {
		t.Error("newState должен возвращать разные значения при каждом вызове")
	}
}

// TestNewState_Length проверяет, что state достаточной длины (base64url из 32 байт ≥ 40 символов).
func TestNewState_Length(t *testing.T) {
	s, err := newState()
	if err != nil {
		t.Fatalf("newState: %v", err)
	}
	// 32 байта → 43 символа base64url без padding (43 = ceil(32*4/3))
	if len(s) < 40 {
		t.Errorf("state слишком короткий: %d символов, ожидается >= 40", len(s))
	}
}

// TestCompareState_Equal проверяет, что одинаковые значения принимаются.
func TestCompareState_Equal(t *testing.T) {
	s, err := newState()
	if err != nil {
		t.Fatalf("newState: %v", err)
	}
	if !compareState(s, s) {
		t.Error("compareState должен возвращать true для одинаковых значений")
	}
}

// TestCompareState_Different проверяет, что разные значения отклоняются.
func TestCompareState_Different(t *testing.T) {
	s1, _ := newState()
	s2, _ := newState()
	if compareState(s1, s2) {
		t.Error("compareState должен возвращать false для разных значений")
	}
}

// TestCompareState_Empty проверяет, что пустой state отклоняется.
func TestCompareState_Empty(t *testing.T) {
	s, _ := newState()
	if compareState("", s) {
		t.Error("compareState должен возвращать false при пустом a")
	}
	if compareState(s, "") {
		t.Error("compareState должен возвращать false при пустом b")
	}
	if compareState("", "") {
		t.Error("compareState должен возвращать false при обоих пустых")
	}
}
