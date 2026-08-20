package common

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// MemoryStore provides in-memory fast cache. When a SQLite path is supplied it
// also persists every mutation durably (ACID + WAL), surviving restarts. The
// PostgreSQL path (pkg/db) remains the preferred store when a real PG is
// reachable; SQLite is the durable fallback that actually runs in this sandbox.
type MemoryStore struct {
	mu     sync.RWMutex
	users  map[int64]*User
	wallets map[int64]*Wallet
	walletTxns map[int64][]WalletTransaction
	orders  []Order
	positions []Position
	paymentOrders map[string]*PaymentOrder

	userSeq   atomic.Int64
	orderSeq  atomic.Int64
	posSeq    atomic.Int64
	paySeq    atomic.Int64

	db   *sql.DB // optional SQLite durable backend (nil = memory-only)
	dbMu sync.Mutex
}

func NewMemoryStore(sqlitePath string) *MemoryStore {
	ms := &MemoryStore{
		users:       make(map[int64]*User),
		wallets:     make(map[int64]*Wallet),
		walletTxns:  make(map[int64][]WalletTransaction),
		orders:      make([]Order, 0),
		positions:   make([]Position, 0),
		paymentOrders: make(map[string]*PaymentOrder),
	}
	if sqlitePath != "" {
		db, err := openSQLite(sqlitePath)
		if err != nil {
			log.Printf("WARNING: SQLite unavailable (%v) — running memory-only (data lost on restart)", err)
		} else {
			ms.db = db
			log.Printf("SQLite durable store opened: %s", sqlitePath)
		}
	}
	return ms
}

func (m *MemoryStore) NextUserID() int64 {
	return m.userSeq.Add(1)
}

func (m *MemoryStore) NextOrderID() int64 {
	return m.orderSeq.Add(1)
}

func (m *MemoryStore) NextPositionID() int64 {
	return m.posSeq.Add(1)
}

func (m *MemoryStore) SaveUser(u *User) {
	m.mu.Lock()
	m.users[u.ID] = u
	m.mu.Unlock()
	m.persistUser(u)
}

func (m *MemoryStore) GetUserByUsername(username string) *User {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, u := range m.users {
		if u.Username == username {
			cp := *u
			return &cp
		}
	}
	return nil
}

// GetUserByEmail returns a copy of the user with the given (verified) email, or nil.
func (m *MemoryStore) GetUserByEmail(email string) *User {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, u := range m.users {
		if u.Email == email {
			cp := *u
			return &cp
		}
	}
	return nil
}

// UpdateUserPassword replaces the stored password hash for userID and persists
// it to the durable SQLite store (so a reset survives restarts).
func (m *MemoryStore) UpdateUserPassword(userID int64, hash string) {
	m.mu.Lock()
	if u, ok := m.users[userID]; ok {
		u.PasswordHash = hash
	}
	m.mu.Unlock()
	m.persistUserByID(userID)
}

func (m *MemoryStore) GetUserByID(id int64) *User {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if u, ok := m.users[id]; ok {
		cp := *u
		return &cp
	}
	return nil
}

// UpdateUserCultivation updates a user's cultivation level and spirit energy
func (m *MemoryStore) UpdateUserCultivation(userID int64, level int, energy int64) {
	m.mu.Lock()
	if u, ok := m.users[userID]; ok {
		u.CultivationLevel = level
		u.SpiritEnergy = energy
	}
	m.mu.Unlock()
	m.persistUserByID(userID)
}

// GetAllUsers returns all users (for ranking)
func (m *MemoryStore) GetAllUsers() []*User {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*User, 0, len(m.users))
	for _, u := range m.users {
		cp := *u
		result = append(result, &cp)
	}
	return result
}

// GetAllPositions returns every OPEN position across all users (admin view).
func (m *MemoryStore) GetAllPositions() []Position {
	m.mu.RLock()
	defer m.mu.RUnlock()
	res := make([]Position, 0, len(m.positions))
	for _, p := range m.positions {
		if p.Status == 1 {
			res = append(res, p)
		}
	}
	return res
}

// GetAllOrders returns every order (admin audit view), newest first.
func (m *MemoryStore) GetAllOrders() []Order {
	m.mu.RLock()
	defer m.mu.RUnlock()
	res := make([]Order, len(m.orders))
	copy(res, m.orders)
	sort.Slice(res, func(i, j int) bool { return res[i].CreatedAt.After(res[j].CreatedAt) })
	return res
}

// GetAllPaymentOrders returns every payment order (admin view), newest first.
func (m *MemoryStore) GetAllPaymentOrders() []PaymentOrder {
	m.mu.RLock()
	defer m.mu.RUnlock()
	res := make([]PaymentOrder, 0, len(m.paymentOrders))
	for _, o := range m.paymentOrders {
		res = append(res, *o)
	}
	sort.Slice(res, func(i, j int) bool { return res[i].CreatedAt.After(res[j].CreatedAt) })
	return res
}

// UpdateUserStatus freezes (0) or enables (1) an account and persists it.
func (m *MemoryStore) UpdateUserStatus(userID int64, status int) {
	m.mu.Lock()
	if u, ok := m.users[userID]; ok {
		u.Status = status
		u.UpdatedAt = time.Now()
	}
	m.mu.Unlock()
	m.persistUserByID(userID)
}

func (m *MemoryStore) SaveWallet(w *Wallet) {
	m.mu.Lock()
	m.wallets[w.UserID] = w
	m.mu.Unlock()
	m.persistWallet(w.UserID)
}

func (m *MemoryStore) GetWallet(userID int64) *Wallet {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if w, ok := m.wallets[userID]; ok {
		cp := *w
		return &cp
	}
	return nil
}

func (m *MemoryStore) UpdateWalletBalance(userID int64, newBalance, newFrozen float64) {
	m.mu.Lock()
	if w, ok := m.wallets[userID]; ok {
		w.Balance = newBalance
		w.Frozen = newFrozen
		w.Version++
	}
	m.mu.Unlock()
	m.persistWallet(userID)
}

func (m *MemoryStore) SaveWalletTransaction(userID int64, txn *WalletTransaction) {
	m.mu.Lock()
	m.walletTxns[userID] = append(m.walletTxns[userID], *txn)
	m.mu.Unlock()
	m.persistWalletTxn(txn)
}

func (m *MemoryStore) GetWalletTransactions(userID int64) []WalletTransaction {
	m.mu.RLock()
	defer m.mu.RUnlock()
	txns := m.walletTxns[userID]
	result := make([]WalletTransaction, len(txns))
	copy(result, txns)
	return result
}

func (m *MemoryStore) SaveOrder(o *Order) {
	m.mu.Lock()
	m.orders = append(m.orders, *o)
	m.mu.Unlock()
	m.persistOrder(o)
}

func (m *MemoryStore) UpdateOrder(o *Order) {
	m.mu.Lock()
	for i, ord := range m.orders {
		if ord.ID == o.ID {
			m.orders[i] = *o
			m.mu.Unlock()
			m.persistOrder(o)
			return
		}
	}
	m.mu.Unlock()
}

func (m *MemoryStore) SavePosition(p *Position) {
	m.mu.Lock()
	m.positions = append(m.positions, *p)
	m.mu.Unlock()
	m.persistPosition(p)
}

func (m *MemoryStore) GetPositions(userID int64, contestID *int64) []Position {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []Position
	for _, p := range m.positions {
		if p.UserID == userID && p.Status == 1 {
			cp := p
			result = append(result, cp)
		}
	}
	return result
}

func (m *MemoryStore) GetPositionByID(positionID int64) *Position {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for i := len(m.positions) - 1; i >= 0; i-- {
		if m.positions[i].ID == positionID {
			cp := m.positions[i]
			return &cp
		}
	}
	return nil
}

func (m *MemoryStore) UpdatePosition(pos *Position) {
	m.mu.Lock()
	for i, p := range m.positions {
		if p.ID == pos.ID {
			m.positions[i] = *pos
			m.mu.Unlock()
			m.persistPosition(pos)
			return
		}
	}
	m.mu.Unlock()
}

func (m *MemoryStore) GetClosedPositions(userID int64) []Position {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []Position
	for _, p := range m.positions {
		if p.UserID == userID && p.Status == 2 {
			cp := p
			result = append(result, cp)
		}
	}
	return result
}

// ========== Snapshot persistence (survives restarts in memory mode) ==========

type walletTxnGroup struct {
	UserID int64               `json:"user_id"`
	Txns   []WalletTransaction `json:"txns"`
}

type memSnapshot struct {
	Users      []User            `json:"users"`
	Wallets    []Wallet          `json:"wallets"`
	WalletTxns []walletTxnGroup  `json:"wallet_txns"`
	Orders     []Order           `json:"orders"`
	Positions  []Position        `json:"positions"`
	PayOrders  []PaymentOrder    `json:"payment_orders"`
}

// SaveSnapshot writes the whole in-memory store to disk as JSON (atomic via temp+rename).
func (m *MemoryStore) SaveSnapshot(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o754); err != nil {
		return err
	}
	m.mu.RLock()
	snap := memSnapshot{}
	for _, u := range m.users {
		snap.Users = append(snap.Users, *u)
	}
	for _, w := range m.wallets {
		snap.Wallets = append(snap.Wallets, *w)
	}
	for uid, txns := range m.walletTxns {
		snap.WalletTxns = append(snap.WalletTxns, walletTxnGroup{UserID: uid, Txns: txns})
	}
	snap.Orders = append(snap.Orders, m.orders...)
	snap.Positions = append(snap.Positions, m.positions...)
	for _, po := range m.paymentOrders {
		snap.PayOrders = append(snap.PayOrders, *po)
	}
	m.mu.RUnlock()

	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// LoadSnapshot restores the in-memory store from disk. Call once at startup (memory mode only).
func (m *MemoryStore) LoadSnapshot(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // fresh start
		}
		return err
	}
	var snap memSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	var maxUser, maxOrder, maxPos, maxPay int64
	for i := range snap.Users {
		u := snap.Users[i]
		m.users[u.ID] = &u
		if u.ID > maxUser {
			maxUser = u.ID
		}
	}
	for i := range snap.Wallets {
		w := snap.Wallets[i]
		m.wallets[w.UserID] = &w
	}
	for _, g := range snap.WalletTxns {
		m.walletTxns[g.UserID] = g.Txns
	}
	for i := range snap.Orders {
		o := snap.Orders[i]
		m.orders = append(m.orders, o)
		if o.ID > maxOrder {
			maxOrder = o.ID
		}
	}
	for i := range snap.Positions {
		p := snap.Positions[i]
		m.positions = append(m.positions, p)
		if p.ID > maxPos {
			maxPos = p.ID
		}
	}
	for i := range snap.PayOrders {
		po := snap.PayOrders[i]
		m.paymentOrders[po.OutTradeNo] = &po
		if po.ID > maxPay {
			maxPay = po.ID
		}
	}
	// Restore sequence counters so new IDs don't collide with restored ones.
	// NextUserID/NextOrderID/NextPositionID themselves do +1, so store the max.
	m.userSeq.Store(maxUser)
	m.orderSeq.Store(maxOrder)
	m.posSeq.Store(maxPos)
	m.paySeq.Store(maxPay)
	return nil
}

// persistLoop periodically saves a snapshot (memory mode only). Flushes on shutdown.
func (m *MemoryStore) PersistLoop(ctx context.Context, path string) {
	// immediate first save so a quick restart still persists current state
	_ = m.SaveSnapshot(path)
	m.FlushMeta()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = m.SaveSnapshot(path)
			m.FlushMeta()
			return
		case <-ticker.C:
			_ = m.SaveSnapshot(path)
			m.FlushMeta()
		}
	}
}
