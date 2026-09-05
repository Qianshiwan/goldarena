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

	// 金龟子 (Jinguizi) simulated coin wallet — isolated from the main wallet.
	jinguiziWallets map[int64]*JinguiziWallet
	jinguiziTxns    map[int64][]JinguiziTransaction
	// 金龟子选拔赛报名记录（与钱包平行，独立子系统）
	jinguiziEnrollments map[int64]*JinguiziEnrollment

	// 应用内留言（平台与用户双向）
	messages map[int64][]Message

	userSeq   atomic.Int64
	orderSeq  atomic.Int64
	posSeq    atomic.Int64
	paySeq    atomic.Int64
	jinguiziSeq atomic.Int64
	msgSeq    atomic.Int64

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
		jinguiziWallets: make(map[int64]*JinguiziWallet),
		jinguiziTxns:    make(map[int64][]JinguiziTransaction),
		jinguiziEnrollments: make(map[int64]*JinguiziEnrollment),
		messages:        make(map[int64][]Message),
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

// ========== 金龟子 (Jinguizi) simulated coin wallet helpers ==========

func (m *MemoryStore) GetJinguiziWallet(userID int64) *JinguiziWallet {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if w, ok := m.jinguiziWallets[userID]; ok {
		cp := *w
		return &cp
	}
	return nil
}

// EnsureJinguiziWallet returns the user's 金龟子 wallet, creating a zero-balance
// one on first access so callers never have to NULL-check.
func (m *MemoryStore) EnsureJinguiziWallet(userID int64) *JinguiziWallet {
	if w := m.GetJinguiziWallet(userID); w != nil {
		return w
	}
	now := time.Now()
	w := &JinguiziWallet{
		UserID: userID, ID: userID, Balance: 0, Frozen: 0, TotalRecharged: 0,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	m.SaveJinguiziWallet(w)
	return m.GetJinguiziWallet(userID)
}

func (m *MemoryStore) SaveJinguiziWallet(w *JinguiziWallet) {
	m.mu.Lock()
	m.jinguiziWallets[w.UserID] = w
	m.mu.Unlock()
	m.persistJinguiziWallet(w.UserID)
}

func (m *MemoryStore) UpdateJinguiziBalance(userID int64, newBalance, newFrozen float64) {
	m.mu.Lock()
	if w, ok := m.jinguiziWallets[userID]; ok {
		w.Balance = newBalance
		w.Frozen = newFrozen
		w.Version++
	}
	m.mu.Unlock()
	m.persistJinguiziWallet(userID)
}

func (m *MemoryStore) AddJinguiziRecharged(userID int64, amount float64) {
	m.mu.Lock()
	if w, ok := m.jinguiziWallets[userID]; ok {
		w.TotalRecharged += amount
	}
	m.mu.Unlock()
	m.persistJinguiziWallet(userID)
}

func (m *MemoryStore) NextJinguiziTxnID() int64 {
	return m.jinguiziSeq.Add(1)
}

func (m *MemoryStore) SaveJinguiziTransaction(userID int64, txn *JinguiziTransaction) {
	if txn.ID == 0 {
		txn.ID = m.NextJinguiziTxnID()
	}
	m.mu.Lock()
	m.jinguiziTxns[userID] = append(m.jinguiziTxns[userID], *txn)
	m.mu.Unlock()
	m.persistJinguiziTxn(txn)
}

func (m *MemoryStore) GetJinguiziTransactions(userID int64) []JinguiziTransaction {
	m.mu.RLock()
	defer m.mu.RUnlock()
	txns := m.jinguiziTxns[userID]
	result := make([]JinguiziTransaction, len(txns))
	copy(result, txns)
	return result
}

func (m *MemoryStore) GetAllJinguiziWallets() []*JinguiziWallet {
	m.mu.RLock()
	defer m.mu.RUnlock()
	res := make([]*JinguiziWallet, 0, len(m.jinguiziWallets))
	for _, w := range m.jinguiziWallets {
		cp := *w
		res = append(res, &cp)
	}
	return res
}

// ========== 金龟子 (Jinguizi) contest enrollment helpers ==========

func (m *MemoryStore) GetJinguiziEnrollment(userID int64) *JinguiziEnrollment {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if e, ok := m.jinguiziEnrollments[userID]; ok {
		cp := *e
		return &cp
	}
	return nil
}

// GetActiveEnrollment returns the user's enrollment only if it is currently active
// (status == "active"). Used by the trading layer to decide whether a user's
// margin should be drawn from the 金龟子 contest wallet instead of the main wallet.
func (m *MemoryStore) GetActiveEnrollment(userID int64) *JinguiziEnrollment {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if e, ok := m.jinguiziEnrollments[userID]; ok && e.Status == "active" {
		cp := *e
		return &cp
	}
	return nil
}

func (m *MemoryStore) SaveJinguiziEnrollment(e *JinguiziEnrollment) {
	m.mu.Lock()
	m.jinguiziEnrollments[e.UserID] = e
	m.mu.Unlock()
	m.persistJinguiziEnrollment(e.UserID)
}

func (m *MemoryStore) GetAllJinguiziEnrollments() []*JinguiziEnrollment {
	m.mu.RLock()
	defer m.mu.RUnlock()
	res := make([]*JinguiziEnrollment, 0, len(m.jinguiziEnrollments))
	for _, e := range m.jinguiziEnrollments {
		cp := *e
		res = append(res, &cp)
	}
	return res
}

// GetAllActiveEnrollments returns every enrollment whose status is "active".
func (m *MemoryStore) GetAllActiveEnrollments() []*JinguiziEnrollment {
	m.mu.RLock()
	defer m.mu.RUnlock()
	res := make([]*JinguiziEnrollment, 0)
	for _, e := range m.jinguiziEnrollments {
		if e.Status == "active" {
			cp := *e
			res = append(res, &cp)
		}
	}
	return res
}

// ========== In-app Message helpers ==========

func (m *MemoryStore) NextMessageID() int64 {
	return m.msgSeq.Add(1)
}

// SaveMessage appends a message to the user's thread and persists it.
func (m *MemoryStore) SaveMessage(msg *Message) {
	if msg.ID == 0 {
		msg.ID = m.NextMessageID()
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now()
	}
	m.mu.Lock()
	m.messages[msg.UserID] = append(m.messages[msg.UserID], *msg)
	m.mu.Unlock()
	m.persistMessage(msg)
}

// GetMessages returns the full conversation for a user, oldest first.
func (m *MemoryStore) GetMessages(userID int64) []Message {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if msgs, ok := m.messages[userID]; ok {
		res := make([]Message, len(msgs))
		copy(res, msgs)
		return res
	}
	return nil
}

// MarkMessagesRead marks unread messages from the opposite side as read.
// When a user opens the chat, mark all "platform" messages as read.
// When the admin opens the chat, mark all "user" messages as read.
func (m *MemoryStore) MarkMessagesRead(userID int64, reader string) {
	m.mu.Lock()
	if msgs, ok := m.messages[userID]; ok {
		for i := range msgs {
			if !msgs[i].Read && msgs[i].Sender != reader {
				msgs[i].Read = true
			}
		}
		m.messages[userID] = msgs
	}
	m.mu.Unlock()
	m.persistMessagesRead(userID)
}

// GetUnreadMessageCounts returns a map[userID]count of unread user messages
// (from the platform's perspective, i.e. sender == "user" and read == false).
func (m *MemoryStore) GetUnreadMessageCounts() map[int64]int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	counts := make(map[int64]int)
	for uid, msgs := range m.messages {
		for _, msg := range msgs {
			if msg.Sender == "user" && !msg.Read {
				counts[uid]++
			}
		}
	}
	return counts
}

// GetMessageConversationUserIDs returns all user IDs that have at least one
// message in the platform, sorted by their latest message time descending.
func (m *MemoryStore) GetMessageConversationUserIDs() []int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	type pair struct {
		uid int64
		at  time.Time
	}
	pairs := make([]pair, 0, len(m.messages))
	for uid, msgs := range m.messages {
		if len(msgs) == 0 {
			continue
		}
		last := msgs[len(msgs)-1]
		pairs = append(pairs, pair{uid: uid, at: last.CreatedAt})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].at.After(pairs[j].at) })
	res := make([]int64, len(pairs))
	for i, p := range pairs {
		res[i] = p.uid
	}
	return res
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
	JinguiziWallets []JinguiziWallet       `json:"jinguizi_wallets"`
	JinguiziTxns    []jinguiziTxnGroup     `json:"jinguizi_txns"`
	JinguiziEnrollments []JinguiziEnrollment `json:"jinguizi_enrollments"`
	Messages   []messageGroup     `json:"messages"`
}

type jinguiziTxnGroup struct {
	UserID int64                `json:"user_id"`
	Txns   []JinguiziTransaction `json:"txns"`
}

type messageGroup struct {
	UserID int64     `json:"user_id"`
	Msgs   []Message `json:"msgs"`
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
	for _, w := range m.jinguiziWallets {
		snap.JinguiziWallets = append(snap.JinguiziWallets, *w)
	}
	for uid, txns := range m.jinguiziTxns {
		snap.JinguiziTxns = append(snap.JinguiziTxns, jinguiziTxnGroup{UserID: uid, Txns: txns})
	}
	for _, e := range m.jinguiziEnrollments {
		snap.JinguiziEnrollments = append(snap.JinguiziEnrollments, *e)
	}
	for uid, msgs := range m.messages {
		snap.Messages = append(snap.Messages, messageGroup{UserID: uid, Msgs: msgs})
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
	for i := range snap.JinguiziWallets {
		w := snap.JinguiziWallets[i]
		m.jinguiziWallets[w.UserID] = &w
	}
	for i := range snap.JinguiziEnrollments {
		e := snap.JinguiziEnrollments[i]
		m.jinguiziEnrollments[e.UserID] = &e
	}
	var maxJi int64
	for _, g := range snap.JinguiziTxns {
		m.jinguiziTxns[g.UserID] = g.Txns
		for _, t := range g.Txns {
			if t.ID > maxJi {
				maxJi = t.ID
			}
		}
	}
	var maxMsg int64
	for _, g := range snap.Messages {
		m.messages[g.UserID] = g.Msgs
		for _, msg := range g.Msgs {
			if msg.ID > maxMsg {
				maxMsg = msg.ID
			}
		}
	}
	// Restore sequence counters so new IDs don't collide with restored ones.
	// NextUserID/NextOrderID/NextPositionID themselves do +1, so store the max.
	m.userSeq.Store(maxUser)
	m.orderSeq.Store(maxOrder)
	m.posSeq.Store(maxPos)
	m.paySeq.Store(maxPay)
	m.jinguiziSeq.Store(maxJi)
	m.msgSeq.Store(maxMsg)
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
