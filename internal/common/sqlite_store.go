package common

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "modernc.org/sqlite"
)

// openSQLite opens (creating if needed) a SQLite database file configured for
// durable, single-writer WAL operation. Returns nil (without error) when the
// path is empty so callers can gracefully fall back to memory-only mode.
func openSQLite(path string) (*sql.DB, error) {
	if path == "" {
		return nil, nil
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // serialize writers; SQLite WAL allows concurrent readers
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL: %w", err)
	}
	if _, err := db.Exec("PRAGMA synchronous=NORMAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set synchronous: %w", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set busy_timeout: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=OFF"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set foreign_keys: %w", err)
	}
	if err := migrateSQLite(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func migrateSQLite(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS ga_users (
			id INTEGER PRIMARY KEY,
			username TEXT, nickname TEXT, password_hash TEXT,
			email TEXT, phone TEXT, avatar TEXT,
			is_verified INTEGER, role TEXT, status INTEGER,
			cultivation_level INTEGER, spirit_energy INTEGER,
			created_at TEXT, updated_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS ga_wallets (
			user_id INTEGER PRIMARY KEY,
			id INTEGER, balance REAL, frozen REAL, total_recharged REAL,
			version INTEGER, created_at TEXT, updated_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS ga_wallet_txns (
			id INTEGER PRIMARY KEY,
			user_id INTEGER, type TEXT, amount REAL,
			balance_before REAL, balance_after REAL,
			reference_id TEXT, remark TEXT, created_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS ga_orders (
			id INTEGER PRIMARY KEY,
			order_no TEXT, user_id INTEGER, contest_id INTEGER,
			symbol TEXT, contract_month TEXT, direction INTEGER, order_type INTEGER,
			volume REAL, leverage INTEGER,
			price REAL, stop_loss REAL, take_profit REAL,
			status INTEGER, executed_price REAL, margin REAL, spread_cost REAL,
			created_at TEXT, updated_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS ga_positions (
			id INTEGER PRIMARY KEY,
			user_id INTEGER, contest_id INTEGER, order_no TEXT,
			symbol TEXT, contract_month TEXT, direction INTEGER,
			volume REAL, leverage INTEGER,
			open_price REAL, current_price REAL,
			stop_loss REAL, take_profit REAL,
			margin REAL, floating_pnl REAL, spread_cost REAL,
			status INTEGER, closed_at TEXT, created_at TEXT, updated_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS ga_meta (
			key TEXT PRIMARY KEY, value INTEGER
		)`,
		`CREATE TABLE IF NOT EXISTS ga_payment_orders (
			out_trade_no TEXT PRIMARY KEY,
			id INTEGER, user_id INTEGER, channel TEXT, amount_rmb REAL, game_coins REAL,
			status TEXT, provider TEXT, qr_content TEXT, pay_url TEXT, created_at TEXT, paid_at TEXT
		)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}

// ---------- nullable helpers ----------

func nullInt64(p *int64) sql.NullInt64 {
	if p == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *p, Valid: true}
}
func toInt64Ptr(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}
func nullFloat64(p *float64) sql.NullFloat64 {
	if p == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *p, Valid: true}
}
func toFloat64Ptr(n sql.NullFloat64) *float64 {
	if !n.Valid {
		return nil
	}
	v := n.Float64
	return &v
}
func fmtTime(t time.Time) string  { return t.Format(time.RFC3339) }
func nullTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *t, Valid: true}
}
func toTimePtr(n sql.NullTime) *time.Time {
	if !n.Valid {
		return nil
	}
	t := n.Time
	return &t
}
func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// parseNullTimePtr parses a sql.NullString (TEXT column holding an RFC3339
// timestamp) into *time.Time. Returns nil when the value is NULL or empty.
// Used for columns like closed_at / paid_at that modernc/sqlite returns as
// strings (sql.NullTime cannot scan a driver.Value string).
func parseNullTimePtr(s sql.NullString) *time.Time {
	if !s.Valid || s.String == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s.String)
	if err != nil {
		return nil
	}
	return &t
}

// ---------- write-through persistence ----------

// persistUser upserts a single user row. No-op when SQLite is disabled.
func (m *MemoryStore) persistUser(u *User) {
	if m.db == nil || u == nil {
		return
	}
	m.dbMu.Lock()
	defer m.dbMu.Unlock()
	_, err := m.db.Exec(`INSERT INTO ga_users
		(id,username,nickname,password_hash,email,phone,avatar,is_verified,role,status,cultivation_level,spirit_energy,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
		username=excluded.username,nickname=excluded.nickname,password_hash=excluded.password_hash,
		email=excluded.email,phone=excluded.phone,avatar=excluded.avatar,is_verified=excluded.is_verified,
		role=excluded.role,status=excluded.status,cultivation_level=excluded.cultivation_level,
		spirit_energy=excluded.spirit_energy,updated_at=excluded.updated_at`,
		u.ID, u.Username, u.Nickname, u.PasswordHash, u.Email, u.Phone, u.Avatar,
		boolToInt(u.IsVerified), u.Role, u.Status, u.CultivationLevel, u.SpiritEnergy,
		fmtTime(u.CreatedAt), fmtTime(u.UpdatedAt))
	if err != nil {
		log.Printf("WARN persistUser(%d): %v", u.ID, err)
	}
}

func (m *MemoryStore) persistUserByID(userID int64) {
	m.mu.RLock()
	u := m.users[userID]
	m.mu.RUnlock()
	if u != nil {
		m.persistUser(u)
	}
}

func (m *MemoryStore) persistWallet(userID int64) {
	if m.db == nil {
		return
	}
	m.mu.RLock()
	w := m.wallets[userID]
	m.mu.RUnlock()
	if w == nil {
		return
	}
	m.dbMu.Lock()
	defer m.dbMu.Unlock()
	_, err := m.db.Exec(`INSERT INTO ga_wallets
		(user_id,id,balance,frozen,total_recharged,version,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT(user_id) DO UPDATE SET
		id=excluded.id,balance=excluded.balance,frozen=excluded.frozen,
		total_recharged=excluded.total_recharged,version=excluded.version,updated_at=excluded.updated_at`,
		w.UserID, w.ID, w.Balance, w.Frozen, w.TotalRecharged, w.Version,
		fmtTime(w.CreatedAt), fmtTime(w.UpdatedAt))
	if err != nil {
		log.Printf("WARN persistWallet(%d): %v", userID, err)
	}
}

func (m *MemoryStore) persistWalletTxn(t *WalletTransaction) {
	if m.db == nil || t == nil {
		return
	}
	m.dbMu.Lock()
	defer m.dbMu.Unlock()
	_, err := m.db.Exec(`INSERT INTO ga_wallet_txns
		(id,user_id,type,amount,balance_before,balance_after,reference_id,remark,created_at)
		VALUES (?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
		user_id=excluded.user_id,type=excluded.type,amount=excluded.amount,
		balance_before=excluded.balance_before,balance_after=excluded.balance_after,
		reference_id=excluded.reference_id,remark=excluded.remark,created_at=excluded.created_at`,
		t.ID, t.UserID, t.Type, t.Amount, t.BalanceBefore, t.BalanceAfter, t.ReferenceID, t.Remark, fmtTime(t.CreatedAt))
	if err != nil {
		log.Printf("WARN persistWalletTxn(%d): %v", t.ID, err)
	}
}

func (m *MemoryStore) persistOrder(o *Order) {
	if m.db == nil || o == nil {
		return
	}
	m.dbMu.Lock()
	defer m.dbMu.Unlock()
	_, err := m.db.Exec(`INSERT INTO ga_orders
		(id,order_no,user_id,contest_id,symbol,contract_month,direction,order_type,volume,leverage,
		price,stop_loss,take_profit,status,executed_price,margin,spread_cost,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
		order_no=excluded.order_no,user_id=excluded.user_id,contest_id=excluded.contest_id,
		symbol=excluded.symbol,contract_month=excluded.contract_month,direction=excluded.direction,
		order_type=excluded.order_type,volume=excluded.volume,leverage=excluded.leverage,
		price=excluded.price,stop_loss=excluded.stop_loss,take_profit=excluded.take_profit,
		status=excluded.status,executed_price=excluded.executed_price,margin=excluded.margin,
		spread_cost=excluded.spread_cost,updated_at=excluded.updated_at`,
		o.ID, o.OrderNo, o.UserID, nullInt64(o.ContestID), o.Symbol, o.ContractMonth, o.Direction, o.OrderType,
		o.Volume, o.Leverage, nullFloat64(o.Price), nullFloat64(o.StopLoss), nullFloat64(o.TakeProfit),
		o.Status, nullFloat64(o.ExecutedPrice), o.Margin, o.SpreadCost, fmtTime(o.CreatedAt), fmtTime(o.UpdatedAt))
	if err != nil {
		log.Printf("WARN persistOrder(%d): %v", o.ID, err)
	}
}

func (m *MemoryStore) persistPosition(p *Position) {
	if m.db == nil || p == nil {
		return
	}
	m.dbMu.Lock()
	defer m.dbMu.Unlock()
	_, err := m.db.Exec(`INSERT INTO ga_positions
		(id,user_id,contest_id,order_no,symbol,contract_month,direction,volume,leverage,
		open_price,current_price,stop_loss,take_profit,margin,floating_pnl,spread_cost,
		status,closed_at,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
		user_id=excluded.user_id,contest_id=excluded.contest_id,order_no=excluded.order_no,
		symbol=excluded.symbol,contract_month=excluded.contract_month,direction=excluded.direction,
		volume=excluded.volume,leverage=excluded.leverage,open_price=excluded.open_price,
		current_price=excluded.current_price,stop_loss=excluded.stop_loss,take_profit=excluded.take_profit,
		margin=excluded.margin,floating_pnl=excluded.floating_pnl,spread_cost=excluded.spread_cost,
		status=excluded.status,closed_at=excluded.closed_at,updated_at=excluded.updated_at`,
		p.ID, p.UserID, nullInt64(p.ContestID), p.OrderNo, p.Symbol, p.ContractMonth, p.Direction, p.Volume, p.Leverage,
		p.OpenPrice, p.CurrentPrice, nullFloat64(p.StopLoss), nullFloat64(p.TakeProfit), p.Margin, p.FloatingPnL, p.SpreadCost,
		p.Status, nullTime(p.ClosedAt), fmtTime(p.CreatedAt), fmtTime(p.UpdatedAt))
	if err != nil {
		log.Printf("WARN persistPosition(%d): %v", p.ID, err)
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// FlushMeta persists the in-memory ID sequence counters so a restart continues
// from the correct next ID (prevents collisions after reload). It also folds the
// WAL into the main database file so a standalone copy of goldarena.db contains
// everything and read-only inspections see current data.
func (m *MemoryStore) FlushMeta() {
	if m.db == nil {
		return
	}
	m.dbMu.Lock()
	defer m.dbMu.Unlock()
	for _, kv := range [][2]string{
		{"user_seq", fmt.Sprint(m.userSeq.Load())},
		{"order_seq", fmt.Sprint(m.orderSeq.Load())},
		{"pos_seq", fmt.Sprint(m.posSeq.Load())},
		{"pay_seq", fmt.Sprint(m.paySeq.Load())},
	} {
		if _, err := m.db.Exec(`INSERT INTO ga_meta(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, kv[0], kv[1]); err != nil {
			log.Printf("WARN FlushMeta(%s): %v", kv[0], err)
		}
	}
	// Fold WAL into the main db file (TRUNCATE keeps the WAL small).
	if _, err := m.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		log.Printf("WARN wal_checkpoint: %v", err)
	}
}

// MigrateMemoryToSQLite pushes the entire in-memory state into SQLite. Used once
// to backfill legacy JSON snapshots; idempotent thanks to upsert semantics.
func (m *MemoryStore) MigrateMemoryToSQLite() error {
	if m.db == nil {
		return nil
	}
	m.mu.RLock()
	users := make([]*User, 0, len(m.users))
	for _, u := range m.users {
		users = append(users, u)
	}
	wallets := make([]*Wallet, 0, len(m.wallets))
	for _, w := range m.wallets {
		wallets = append(wallets, w)
	}
	txns := make([]WalletTransaction, 0)
	for _, ts := range m.walletTxns {
		txns = append(txns, ts...)
	}
	orders := make([]Order, len(m.orders))
	copy(orders, m.orders)
	positions := make([]Position, len(m.positions))
	copy(positions, m.positions)
	m.mu.RUnlock()

	m.dbMu.Lock()
	defer m.dbMu.Unlock()
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	for _, u := range users {
		if _, err := tx.Exec(`INSERT INTO ga_users
			(id,username,nickname,password_hash,email,phone,avatar,is_verified,role,status,cultivation_level,spirit_energy,created_at,updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET username=excluded.username,nickname=excluded.nickname,
			password_hash=excluded.password_hash,email=excluded.email,phone=excluded.phone,avatar=excluded.avatar,
			is_verified=excluded.is_verified,role=excluded.role,status=excluded.status,
			cultivation_level=excluded.cultivation_level,spirit_energy=excluded.spirit_energy,updated_at=excluded.updated_at`,
			u.ID, u.Username, u.Nickname, u.PasswordHash, u.Email, u.Phone, u.Avatar,
			boolToInt(u.IsVerified), u.Role, u.Status, u.CultivationLevel, u.SpiritEnergy,
			fmtTime(u.CreatedAt), fmtTime(u.UpdatedAt)); err != nil {
			tx.Rollback()
			return err
		}
	}
	for _, w := range wallets {
		if _, err := tx.Exec(`INSERT INTO ga_wallets
			(user_id,id,balance,frozen,total_recharged,version,created_at,updated_at)
			VALUES (?,?,?,?,?,?,?,?)
			ON CONFLICT(user_id) DO UPDATE SET id=excluded.id,balance=excluded.balance,frozen=excluded.frozen,
			total_recharged=excluded.total_recharged,version=excluded.version,updated_at=excluded.updated_at`,
			w.UserID, w.ID, w.Balance, w.Frozen, w.TotalRecharged, w.Version,
			fmtTime(w.CreatedAt), fmtTime(w.UpdatedAt)); err != nil {
			tx.Rollback()
			return err
		}
	}
	for _, t := range txns {
		if _, err := tx.Exec(`INSERT INTO ga_wallet_txns
			(id,user_id,type,amount,balance_before,balance_after,reference_id,remark,created_at)
			VALUES (?,?,?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET user_id=excluded.user_id,type=excluded.type,amount=excluded.amount,
			balance_before=excluded.balance_before,balance_after=excluded.balance_after,reference_id=excluded.reference_id,
			remark=excluded.remark,created_at=excluded.created_at`,
			t.ID, t.UserID, t.Type, t.Amount, t.BalanceBefore, t.BalanceAfter, t.ReferenceID, t.Remark, fmtTime(t.CreatedAt)); err != nil {
			tx.Rollback()
			return err
		}
	}
	for _, o := range orders {
		if _, err := tx.Exec(`INSERT INTO ga_orders
			(id,order_no,user_id,contest_id,symbol,contract_month,direction,order_type,volume,leverage,
			price,stop_loss,take_profit,status,executed_price,margin,spread_cost,created_at,updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET order_no=excluded.order_no,user_id=excluded.user_id,contest_id=excluded.contest_id,
			symbol=excluded.symbol,contract_month=excluded.contract_month,direction=excluded.direction,order_type=excluded.order_type,
			volume=excluded.volume,leverage=excluded.leverage,price=excluded.price,stop_loss=excluded.stop_loss,
			take_profit=excluded.take_profit,status=excluded.status,executed_price=excluded.executed_price,margin=excluded.margin,
			spread_cost=excluded.spread_cost,updated_at=excluded.updated_at`,
			o.ID, o.OrderNo, o.UserID, nullInt64(o.ContestID), o.Symbol, o.ContractMonth, o.Direction, o.OrderType,
			o.Volume, o.Leverage, nullFloat64(o.Price), nullFloat64(o.StopLoss), nullFloat64(o.TakeProfit),
			o.Status, nullFloat64(o.ExecutedPrice), o.Margin, o.SpreadCost, fmtTime(o.CreatedAt), fmtTime(o.UpdatedAt)); err != nil {
			tx.Rollback()
			return err
		}
	}
	for _, p := range positions {
		if _, err := tx.Exec(`INSERT INTO ga_positions
			(id,user_id,contest_id,order_no,symbol,contract_month,direction,volume,leverage,
			open_price,current_price,stop_loss,take_profit,margin,floating_pnl,spread_cost,
			status,closed_at,created_at,updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET user_id=excluded.user_id,contest_id=excluded.contest_id,order_no=excluded.order_no,
			symbol=excluded.symbol,contract_month=excluded.contract_month,direction=excluded.direction,volume=excluded.volume,
			leverage=excluded.leverage,open_price=excluded.open_price,current_price=excluded.current_price,stop_loss=excluded.stop_loss,
			take_profit=excluded.take_profit,margin=excluded.margin,floating_pnl=excluded.floating_pnl,spread_cost=excluded.spread_cost,
			status=excluded.status,closed_at=excluded.closed_at,updated_at=excluded.updated_at`,
			p.ID, p.UserID, nullInt64(p.ContestID), p.OrderNo, p.Symbol, p.ContractMonth, p.Direction, p.Volume, p.Leverage,
			p.OpenPrice, p.CurrentPrice, nullFloat64(p.StopLoss), nullFloat64(p.TakeProfit), p.Margin, p.FloatingPnL, p.SpreadCost,
			p.Status, nullTime(p.ClosedAt), fmtTime(p.CreatedAt), fmtTime(p.UpdatedAt)); err != nil {
			tx.Rollback()
			return err
		}
	}
	// Persist ID sequence counters inside the same transaction (avoids taking
	// dbMu again, which MigrateMemoryToSQLite already holds — would deadlock).
	for _, kv := range []struct {
		k string
		v int64
	}{
		{"user_seq", m.userSeq.Load()},
		{"order_seq", m.orderSeq.Load()},
		{"pos_seq", m.posSeq.Load()},
	} {
		if _, err := tx.Exec(`INSERT INTO ga_meta(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, kv.k, kv.v); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// LoadFromSQLite restores in-memory maps from SQLite. Returns the number of
// users loaded (0 means SQLite was empty / unavailable).
func (m *MemoryStore) LoadFromSQLite() (int, error) {
	if m.db == nil {
		return 0, nil
	}
	m.dbMu.Lock()
	defer m.dbMu.Unlock()

	// Users
	userRows, err := m.db.Query(`SELECT id,username,nickname,password_hash,email,phone,avatar,is_verified,role,status,cultivation_level,spirit_energy,created_at,updated_at FROM ga_users`)
	if err != nil {
		return 0, err
	}
	var maxUser int64
	for userRows.Next() {
		var u User
		var isVerified int
		var createdAt, updatedAt string
		if err := userRows.Scan(&u.ID, &u.Username, &u.Nickname, &u.PasswordHash, &u.Email, &u.Phone, &u.Avatar, &isVerified, &u.Role, &u.Status, &u.CultivationLevel, &u.SpiritEnergy, &createdAt, &updatedAt); err != nil {
			userRows.Close()
			return 0, err
		}
		u.IsVerified = isVerified != 0
		u.CreatedAt = parseTime(createdAt)
		u.UpdatedAt = parseTime(updatedAt)
		m.users[u.ID] = &u
		if u.ID > maxUser {
			maxUser = u.ID
		}
	}
	userRows.Close()

	// Wallets
	walletRows, err := m.db.Query(`SELECT user_id,id,balance,frozen,total_recharged,version,created_at,updated_at FROM ga_wallets`)
	if err != nil {
		return 0, err
	}
	for walletRows.Next() {
		var w Wallet
		var createdAt, updatedAt string
		if err := walletRows.Scan(&w.UserID, &w.ID, &w.Balance, &w.Frozen, &w.TotalRecharged, &w.Version, &createdAt, &updatedAt); err != nil {
			walletRows.Close()
			return 0, err
		}
		w.CreatedAt = parseTime(createdAt)
		w.UpdatedAt = parseTime(updatedAt)
		m.wallets[w.UserID] = &w
	}
	walletRows.Close()

	// Wallet txns
	txnRows, err := m.db.Query(`SELECT id,user_id,type,amount,balance_before,balance_after,reference_id,remark,created_at FROM ga_wallet_txns`)
	if err != nil {
		return 0, err
	}
	for txnRows.Next() {
		var t WalletTransaction
		var createdAt string
		if err := txnRows.Scan(&t.ID, &t.UserID, &t.Type, &t.Amount, &t.BalanceBefore, &t.BalanceAfter, &t.ReferenceID, &t.Remark, &createdAt); err != nil {
			txnRows.Close()
			return 0, err
		}
		t.CreatedAt = parseTime(createdAt)
		m.walletTxns[t.UserID] = append(m.walletTxns[t.UserID], t)
	}
	txnRows.Close()

	// Orders
	orderRows, err := m.db.Query(`SELECT id,order_no,user_id,contest_id,symbol,contract_month,direction,order_type,volume,leverage,price,stop_loss,take_profit,status,executed_price,margin,spread_cost,created_at,updated_at FROM ga_orders`)
	if err != nil {
		return 0, err
	}
	var maxOrder int64
	for orderRows.Next() {
		var o Order
		var contestID sql.NullInt64
		var price, stopLoss, takeProfit, executedPrice sql.NullFloat64
		var createdAt, updatedAt string
		if err := orderRows.Scan(&o.ID, &o.OrderNo, &o.UserID, &contestID, &o.Symbol, &o.ContractMonth, &o.Direction, &o.OrderType, &o.Volume, &o.Leverage, &price, &stopLoss, &takeProfit, &o.Status, &executedPrice, &o.Margin, &o.SpreadCost, &createdAt, &updatedAt); err != nil {
			orderRows.Close()
			return 0, err
		}
		o.ContestID = toInt64Ptr(contestID)
		o.Price = toFloat64Ptr(price)
		o.StopLoss = toFloat64Ptr(stopLoss)
		o.TakeProfit = toFloat64Ptr(takeProfit)
		o.ExecutedPrice = toFloat64Ptr(executedPrice)
		o.CreatedAt = parseTime(createdAt)
		o.UpdatedAt = parseTime(updatedAt)
		m.orders = append(m.orders, o)
		if o.ID > maxOrder {
			maxOrder = o.ID
		}
	}
	orderRows.Close()

	// Positions
	posRows, err := m.db.Query(`SELECT id,user_id,contest_id,order_no,symbol,contract_month,direction,volume,leverage,open_price,current_price,stop_loss,take_profit,margin,floating_pnl,spread_cost,status,closed_at,created_at,updated_at FROM ga_positions`)
	if err != nil {
		return 0, err
	}
	var maxPos, maxPay int64
	for posRows.Next() {
		var p Position
		var contestID sql.NullInt64
		var stopLoss, takeProfit sql.NullFloat64
		var closedAt sql.NullString
		var createdAt, updatedAt string
		if err := posRows.Scan(&p.ID, &p.UserID, &contestID, &p.OrderNo, &p.Symbol, &p.ContractMonth, &p.Direction, &p.Volume, &p.Leverage, &p.OpenPrice, &p.CurrentPrice, &stopLoss, &takeProfit, &p.Margin, &p.FloatingPnL, &p.SpreadCost, &p.Status, &closedAt, &createdAt, &updatedAt); err != nil {
			posRows.Close()
			return 0, err
		}
		p.ContestID = toInt64Ptr(contestID)
		p.StopLoss = toFloat64Ptr(stopLoss)
		p.TakeProfit = toFloat64Ptr(takeProfit)
		p.ClosedAt = parseNullTimePtr(closedAt)
		p.CreatedAt = parseTime(createdAt)
		p.UpdatedAt = parseTime(updatedAt)
		m.positions = append(m.positions, p)
		if p.ID > maxPos {
			maxPos = p.ID
		}
	}
	posRows.Close()

	// Payment orders
	poRows, err := m.db.Query(`SELECT out_trade_no,id,user_id,channel,amount_rmb,game_coins,status,provider,qr_content,pay_url,created_at,paid_at FROM ga_payment_orders`)
	if err == nil {
		for poRows.Next() {
			var po PaymentOrder
			var createdAt string
			var paidAt sql.NullString
			if err := poRows.Scan(&po.OutTradeNo, &po.ID, &po.UserID, &po.Channel, &po.AmountRMB, &po.GameCoins, &po.Status, &po.Provider, &po.QRContent, &po.PayURL, &createdAt, &paidAt); err != nil {
				poRows.Close()
				return 0, err
			}
			po.CreatedAt = parseTime(createdAt)
			po.PaidAt = parseNullTimePtr(paidAt)
			m.paymentOrders[po.OutTradeNo] = &po
			if po.ID > maxPay {
				maxPay = po.ID
			}
		}
		poRows.Close()
	}

	// Meta sequences
	meta := map[string]int64{}
	rows, err := m.db.Query(`SELECT key,value FROM ga_meta`)
	if err == nil {
		for rows.Next() {
			var k string
			var v int64
			if err := rows.Scan(&k, &v); err == nil {
				meta[k] = v
			}
		}
		rows.Close()
	}
	if v, ok := meta["user_seq"]; ok && v > maxUser {
		maxUser = v
	}
	if v, ok := meta["order_seq"]; ok && v > maxOrder {
		maxOrder = v
	}
	if v, ok := meta["pos_seq"]; ok && v > maxPos {
		maxPos = v
	}
	if v, ok := meta["pay_seq"]; ok && v > maxPay {
		maxPay = v
	}
	// NextUserID/NextOrderID/NextPositionID do +1 themselves, so store the max.
	m.userSeq.Store(maxUser)
	m.orderSeq.Store(maxOrder)
	m.posSeq.Store(maxPos)
	m.paySeq.Store(maxPay)

	return len(m.users), nil
}

// Close releases the SQLite handle (if open).
func (m *MemoryStore) Close() error {
	if m.db == nil {
		return nil
	}
	return m.db.Close()
}
