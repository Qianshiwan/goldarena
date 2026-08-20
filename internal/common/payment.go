package common

import (
	"log"
	"sort"
	"time"
)

// Payment status values persisted in ga_payment_orders.
const (
	PaymentPending = "pending"
	PaymentPaid    = "paid"
	PaymentFailed  = "failed"
)

// PaymentOrder records a real-money recharge request. It is created when the
// user initiates payment and marked "paid" only after the payment provider's
// asynchronous callback (or, in sandbox mode, a local simulate call) confirms
// the money arrived. Durable via the same SQLite write-through as wallets.
type PaymentOrder struct {
	ID         int64      `json:"id"`
	UserID     int64      `json:"user_id"`
	OutTradeNo string     `json:"out_trade_no"`
	Channel    string     `json:"channel"` // wxpay | alipay
	AmountRMB  float64    `json:"amount_rmb"`
	GameCoins  float64    `json:"game_coins"`
	Status     string     `json:"status"`
	Provider   string     `json:"provider"`
	QRContent  string     `json:"qr_content,omitempty"`
	PayURL     string     `json:"pay_url,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	PaidAt     *time.Time `json:"paid_at,omitempty"`
}

// ---- MemoryStore fields (added alongside the other maps) ----

func (m *MemoryStore) SavePaymentOrder(o *PaymentOrder) {
	if o.ID == 0 {
		o.ID = m.paySeq.Add(1)
	}
	m.mu.Lock()
	m.paymentOrders[o.OutTradeNo] = o
	m.mu.Unlock()
	m.persistPaymentOrder(o)
}

func (m *MemoryStore) GetPaymentOrderByOutTradeNo(no string) *PaymentOrder {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if o, ok := m.paymentOrders[no]; ok {
		cp := *o
		return &cp
	}
	return nil
}

// GetPaymentOrdersByUser returns the user's payment orders, newest first.
func (m *MemoryStore) GetPaymentOrdersByUser(userID int64) []PaymentOrder {
	m.mu.RLock()
	defer m.mu.RUnlock()
	res := make([]PaymentOrder, 0, len(m.paymentOrders))
	for _, o := range m.paymentOrders {
		if o.UserID == userID {
			res = append(res, *o)
		}
	}
	sort.Slice(res, func(i, j int) bool { return res[i].CreatedAt.After(res[j].CreatedAt) })
	return res
}

// UpdatePaymentOrderStatus flips the order to paid/failed and persists it.
// Safe to call multiple times; the wallet credit is guarded by callers.
func (m *MemoryStore) UpdatePaymentOrderStatus(no string, status string, paidAt *time.Time) {
	m.mu.Lock()
	if o, ok := m.paymentOrders[no]; ok {
		o.Status = status
		o.PaidAt = paidAt
	}
	m.mu.Unlock()
	m.persistPaymentOrderStatus(no, status, paidAt)
}

func (m *MemoryStore) persistPaymentOrder(o *PaymentOrder) {
	if m.db == nil || o == nil {
		return
	}
	m.dbMu.Lock()
	defer m.dbMu.Unlock()
	_, err := m.db.Exec(`INSERT INTO ga_payment_orders
		(out_trade_no,id,user_id,channel,amount_rmb,game_coins,status,provider,qr_content,pay_url,created_at,paid_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(out_trade_no) DO UPDATE SET
		id=excluded.id,user_id=excluded.user_id,channel=excluded.channel,amount_rmb=excluded.amount_rmb,
		game_coins=excluded.game_coins,status=excluded.status,provider=excluded.provider,
		qr_content=excluded.qr_content,pay_url=excluded.pay_url,created_at=excluded.created_at,paid_at=excluded.paid_at`,
		o.OutTradeNo, o.ID, o.UserID, o.Channel, o.AmountRMB, o.GameCoins, o.Status, o.Provider,
		o.QRContent, o.PayURL, fmtTime(o.CreatedAt), nullTime(o.PaidAt))
	if err != nil {
		log.Printf("WARN persistPaymentOrder(%s): %v", o.OutTradeNo, err)
	}
}

func (m *MemoryStore) persistPaymentOrderStatus(no string, status string, paidAt *time.Time) {
	if m.db == nil {
		return
	}
	m.dbMu.Lock()
	defer m.dbMu.Unlock()
	_, err := m.db.Exec(`UPDATE ga_payment_orders SET status=?, paid_at=? WHERE out_trade_no=?`,
		status, nullTime(paidAt), no)
	if err != nil {
		log.Printf("WARN persistPaymentOrderStatus(%s): %v", no, err)
	}
}
