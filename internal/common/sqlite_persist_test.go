package common

import (
	"path/filepath"
	"testing"
	"time"
)

// TestSQLitePersistenceRoundTrip proves the durable contract end-to-end using the
// real store code the HTTP handlers call: mutate in one process, close (commit),
// reopen a fresh store against the same DB file, and confirm everything —
// including the json:"-" PasswordHash and the ID sequence counters — survives.
func TestSQLitePersistenceRoundTrip(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "goldarena_test.db")
	now := time.Now()

	// --- Process 1: mutate ---
	s1 := NewMemoryStore(dbPath)
	u := &User{
		ID: 1, Username: "alice", Nickname: "Alice", PasswordHash: "super-secret-hash",
		Email: "a@example.com", IsVerified: true, Role: "user", Status: 1,
		CultivationLevel: 3, SpiritEnergy: 42, CreatedAt: now, UpdatedAt: now,
	}
	s1.SaveUser(u)
	s1.SaveWallet(&Wallet{ID: 1, UserID: 1, Balance: 1000, Frozen: 0, TotalRecharged: 500, Version: 1, CreatedAt: now, UpdatedAt: now})
	s1.SaveWalletTransaction(1, &WalletTransaction{ID: 111, UserID: 1, Type: "bonus", Amount: 1000, BalanceBefore: 0, BalanceAfter: 1000, CreatedAt: now})
	s1.SaveOrder(&Order{ID: 10, OrderNo: "O1", UserID: 1, Symbol: "GC", Direction: 1, OrderType: 1, Volume: 1, Leverage: 100, Status: 2, CreatedAt: now, UpdatedAt: now})
	s1.SavePosition(&Position{ID: 20, UserID: 1, OrderNo: "O1", Symbol: "GC", Direction: 1, Volume: 1, Leverage: 100, OpenPrice: 4100, CurrentPrice: 4110, Margin: 100, FloatingPnL: 10, Status: 1, CreatedAt: now, UpdatedAt: now})
	s1.UpdateUserCultivation(1, 5, 999) // exercises persistUserByID write-through
	s1.FlushMeta()
	if err := s1.Close(); err != nil {
		t.Fatalf("close s1: %v", err)
	}

	// --- Process 2: reopen & reload ---
	s2 := NewMemoryStore(dbPath)
	n, err := s2.LoadFromSQLite()
	if err != nil {
		t.Fatalf("LoadFromSQLite: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 user, got %d", n)
	}
	lu := s2.GetUserByID(1)
	if lu == nil {
		t.Fatal("user not reloaded")
	}
	if lu.PasswordHash != "super-secret-hash" {
		t.Fatalf("PasswordHash lost across restart: %q", lu.PasswordHash)
	}
	if lu.Username != "alice" || lu.Email != "a@example.com" {
		t.Fatalf("user fields lost: %+v", lu)
	}
	if lu.CultivationLevel != 5 || lu.SpiritEnergy != 999 {
		t.Fatalf("cultivation write-through lost: lvl=%d energy=%d", lu.CultivationLevel, lu.SpiritEnergy)
	}
	lw := s2.GetWallet(1)
	if lw == nil || lw.Balance != 1000 || lw.TotalRecharged != 500 {
		t.Fatalf("wallet lost: %+v", lw)
	}
	if txns := s2.GetWalletTransactions(1); len(txns) != 1 {
		t.Fatalf("expected 1 wallet txn, got %d", len(txns))
	}
	if pos := s2.GetPositions(1, nil); len(pos) != 1 {
		t.Fatalf("expected 1 position, got %d", len(pos))
	}
	if s2.NextUserID() != 2 {
		t.Fatalf("user seq not restored: %d", s2.userSeq.Load())
	}
	if s2.NextOrderID() != 11 {
		t.Fatalf("order seq not restored: %d", s2.orderSeq.Load())
	}
	if s2.NextPositionID() != 21 {
		t.Fatalf("pos seq not restored: %d", s2.posSeq.Load())
	}
	if err := s2.Close(); err != nil {
		t.Fatalf("close s2: %v", err)
	}
	t.Log("SQLite durability round-trip OK: users, wallets, txns, positions, password hash, cultivation, and ID sequences all survived a process restart")
}
