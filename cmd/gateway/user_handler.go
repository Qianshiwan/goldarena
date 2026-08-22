package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/goldarena/goldarena/internal/common"
	"github.com/goldarena/goldarena/pkg/captcha"
	"github.com/goldarena/goldarena/pkg/db"
	"github.com/goldarena/goldarena/pkg/errs"
	"github.com/goldarena/goldarena/pkg/jwt"
	"github.com/goldarena/goldarena/pkg/mail"
	"github.com/goldarena/goldarena/pkg/ratelimit"
	"github.com/goldarena/goldarena/pkg/redis"
	"github.com/goldarena/goldarena/pkg/verify"
	"golang.org/x/crypto/bcrypt"
)

// AuthKit bundles the resources needed for email-verified, rate-limited registration.
type AuthKit struct {
	Verify        *verify.Store
	Cap           *captcha.Store
	Mailer        *mail.Sender
	SendEmailLim  *ratelimit.Limiter // per-email resend interval
	SendIPLim     *ratelimit.Limiter // per-IP send-code limit
	RegIPLim      *ratelimit.Limiter // per-IP register limit
	CodeTTL       time.Duration
	MaxAttempts   int
	VerifiedBonus float64
}

type UserService struct {
	pg     *db.Postgres
	rdb    *redis.Redis
	jwtMgr *jwt.Manager
	mem    *common.MemoryStore
	kit    *AuthKit
}

func NewUserService(pg *db.Postgres, rdb *redis.Redis, jwtMgr *jwt.Manager, mem *common.MemoryStore, kit *AuthKit) *UserService {
	return &UserService{pg: pg, rdb: rdb, jwtMgr: jwtMgr, mem: mem, kit: kit}
}

func (s *UserService) isMemoryMode() bool {
	return s.pg == nil || s.pg.Pool == nil
}

// ========== Auth Handlers ==========

type RegisterReq struct {
	Username string `json:"username" binding:"required,min=3,max=50"`
	Password string `json:"password" binding:"required,min=6,max=100"`
	Nickname string `json:"nickname"`
	Email    string `json:"email" binding:"required,email"`
	Code     string `json:"code" binding:"required,len=6"`
}

// SendCode generates and sends an email verification code. It is rate-limited
// both per email address (resend interval) and per IP (hourly cap) to stop
// bots from harvesting codes or spamming inboxes.
func (s *UserService) SendCode(c *gin.Context) {
	var req struct {
		Email         string `json:"email" binding:"required,email"`
		CaptchaTicket string `json:"captcha_ticket" binding:"required"`
	}
	if err := common.BindJSON(c, &req); err != nil {
		return
	}

	// Slider-captcha gate (anti-bot): a solved, unexpired, single-use ticket is required.
	if !s.kit.Cap.UseTicket(req.CaptchaTicket) {
		common.Error(c, errs.InvalidParam, "滑块验证已失效，请重新完成拼图验证")
		return
	}

	ip := c.ClientIP()
	if !s.kit.SendIPLim.Allow(ip) {
		common.Error(c, errs.TooManyRequests, "发送过于频繁，请稍后再试")
		return
	}
	// Per-email resend interval (limit=1 over resend window)
	if !s.kit.SendEmailLim.Allow(req.Email) {
		common.Error(c, errs.TooManyRequests, "验证码发送过于频繁，请60秒后再试")
		return
	}

	code := s.kit.Verify.Generate(req.Email, "register", s.kit.CodeTTL)
	devCode, err := s.kit.Mailer.SendCode(req.Email, code)
	if err != nil {
		log.Printf("[mail] failed to send code to %s: %v", req.Email, err)
		common.Error(c, errs.Internal, "邮件发送失败，请稍后再试")
		return
	}

	resp := gin.H{"sent": true}
	if devCode != "" {
		// Dev-only convenience: SMTP not configured. Never set DevPrintCode in prod.
		resp["dev_code"] = devCode
	}
	common.Success(c, resp)
}

func (s *UserService) Register(c *gin.Context) {
	var req RegisterReq
	if err := common.BindJSON(c, &req); err != nil {
		return
	}

	// Per-IP registration throttle (anti-bot / high-frequency guard)
	if !s.kit.RegIPLim.Allow(c.ClientIP()) {
		common.Error(c, errs.TooManyRequests, "注册过于频繁，请稍后再试")
		return
	}

	// Verify the email code before doing any DB work
	ok, reason := s.kit.Verify.Check(req.Email, "register", req.Code, s.kit.MaxAttempts)
	if !ok {
		common.Error(c, errs.InvalidParam, reason)
		return
	}

	// Memory mode fallback
	if s.isMemoryMode() {
		s.registerMem(c, &req)
		return
	}

	// Check if username exists
	var exists bool
	err := s.pg.Pool.QueryRow(context.Background(),
		"SELECT EXISTS(SELECT 1 FROM users WHERE username=$1)", req.Username).Scan(&exists)
	if err != nil {
		common.Error(c, errs.Internal, "database error")
		return
	}
	if exists {
		common.Error(c, errs.UserExists, "username already taken")
		return
	}

	// One verified email per account (strong anti-bot measure)
	var emailExists bool
	if err := s.pg.Pool.QueryRow(context.Background(),
		"SELECT EXISTS(SELECT 1 FROM users WHERE email=$1)", req.Email).Scan(&emailExists); err != nil {
		common.Error(c, errs.Internal, "database error")
		return
	}
	if emailExists {
		common.Error(c, errs.InvalidParam, "该邮箱已被注册")
		return
	}

	// Hash password
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		common.Error(c, errs.Internal, "password hash failed")
		return
	}

	nickname := req.Nickname
	if nickname == "" {
		nickname = req.Username
	}

	bonus := s.kit.VerifiedBonus

	// Create user + wallet in transaction
	tx, err := s.pg.Pool.Begin(context.Background())
	if err != nil {
		common.Error(c, errs.Internal, "tx begin failed")
		return
	}
	defer tx.Rollback(context.Background())

	var userID int64
	err = tx.QueryRow(context.Background(),
		`INSERT INTO users (username, nickname, password_hash, email, is_verified)
		 VALUES ($1, $2, $3, $4, true) RETURNING id`, req.Username, nickname, string(hash), req.Email).Scan(&userID)
	if err != nil {
		common.Error(c, errs.Internal, "create user failed")
		return
	}

	// Verified-registration bonus
	_, err = tx.Exec(context.Background(),
		`INSERT INTO wallets (user_id, balance, frozen) VALUES ($1, $2, 0)`, userID, bonus)
	if err != nil {
		common.Error(c, errs.Internal, "create wallet failed")
		return
	}

	// Record bonus transaction
	_, err = tx.Exec(context.Background(),
		`INSERT INTO wallet_transactions (user_id, type, amount, balance_before, balance_after, remark)
		 VALUES ($1, 'bonus', $2, 0, $2, '邮箱验证注册奖励')`, userID, bonus)
	if err != nil {
		common.Error(c, errs.Internal, "bonus record failed")
		return
	}

	if err := tx.Commit(context.Background()); err != nil {
		common.Error(c, errs.Internal, "tx commit failed")
		return
	}

	// Generate tokens
	accessToken, _ := s.jwtMgr.GenerateAccessToken(userID, req.Username, nickname)
	refreshToken, _ := s.jwtMgr.GenerateRefreshToken(userID, req.Username, nickname)

	common.Success(c, gin.H{
		"user_id":       userID,
		"username":      req.Username,
		"nickname":      nickname,
		"email":         req.Email,
		"is_verified":   true,
		"role":          "user",
		"access_token":  accessToken,
		"refresh_token": refreshToken,
	})
}

type LoginReq struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

func (s *UserService) Login(c *gin.Context) {
	var req LoginReq
	if err := common.BindJSON(c, &req); err != nil {
		return
	}

	// Memory mode fallback
	if s.isMemoryMode() {
		s.loginMem(c, &req)
		return
	}

	// Find user
	var user common.User
	err := s.pg.Pool.QueryRow(context.Background(),
		`SELECT id, username, nickname, password_hash, is_verified, role, status
		 FROM users WHERE username=$1`, req.Username).Scan(
		&user.ID, &user.Username, &user.Nickname, &user.PasswordHash,
		&user.IsVerified, &user.Role, &user.Status)
	if err != nil {
		common.Error(c, errs.UserNotFound, "user not found")
		return
	}
	if user.Status != 1 {
		common.Error(c, errs.Forbidden, "account disabled")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		common.Error(c, errs.PasswordMismatch, "wrong password")
		return
	}

	// Generate tokens
	accessToken, _ := s.jwtMgr.GenerateAccessToken(user.ID, user.Username, user.Nickname)
	refreshToken, _ := s.jwtMgr.GenerateRefreshToken(user.ID, user.Username, user.Nickname)

	// Store refresh token in Redis
	s.rdb.CacheSet(context.Background(), fmt.Sprintf("refresh:%d", user.ID), refreshToken, time.Hour*24*7)

	common.Success(c, gin.H{
		"user_id":       user.ID,
		"username":      user.Username,
		"nickname":      user.Nickname,
		"role":          user.Role,
		"access_token":  accessToken,
		"refresh_token": refreshToken,
	})
}

func (s *UserService) RefreshToken(c *gin.Context) {
	var req struct {
		RefreshToken string `json:"refresh_token" binding:"required"`
	}
	if err := common.BindJSON(c, &req); err != nil {
		return
	}
	claims, err := s.jwtMgr.ParseToken(req.RefreshToken)
	if err != nil || claims.TokenType != "refresh" {
		common.Error(c, errs.InvalidToken, "invalid refresh token")
		return
	}

	accessToken, _ := s.jwtMgr.GenerateAccessToken(claims.UserID, claims.Username, claims.Nickname)
	refreshToken, _ := s.jwtMgr.GenerateRefreshToken(claims.UserID, claims.Username, claims.Nickname)

	common.Success(c, gin.H{
		"access_token":  accessToken,
		"refresh_token": refreshToken,
	})
}

// ========== Password Recovery (找回账号 / 找回密码) ==========

type SendResetCodeReq struct {
	Email         string `json:"email" binding:"required,email"`
	CaptchaTicket string `json:"captcha_ticket" binding:"required"`
}

// SendResetCode issues a password-reset verification code to the given email.
// It reuses the same slider-captcha + rate-limit gates as registration so it
// cannot be abused to harvest codes. To avoid account enumeration it always
// responds success; the code is only generated/sent when the email is actually
// bound to an account.
func (s *UserService) SendResetCode(c *gin.Context) {
	var req SendResetCodeReq
	if err := common.BindJSON(c, &req); err != nil {
		return
	}

	if !s.kit.Cap.UseTicket(req.CaptchaTicket) {
		common.Error(c, errs.InvalidParam, "滑块验证已失效，请重新完成拼图验证")
		return
	}

	ip := c.ClientIP()
	if !s.kit.SendIPLim.Allow(ip) {
		common.Error(c, errs.TooManyRequests, "发送过于频繁，请稍后再试")
		return
	}
	if !s.kit.SendEmailLim.Allow(req.Email) {
		common.Error(c, errs.TooManyRequests, "验证码发送过于频繁，请60秒后再试")
		return
	}

	// Only generate/send a code when the email is bound to an account.
	if s.userExistsByEmail(req.Email) {
		code := s.kit.Verify.Generate(req.Email, "reset", s.kit.CodeTTL)
		devCode, err := s.kit.Mailer.SendResetCode(req.Email, code)
		if err != nil {
			log.Printf("[mail] failed to send reset code to %s: %v", req.Email, err)
			common.Error(c, errs.Internal, "邮件发送失败，请稍后再试")
			return
		}
		resp := gin.H{"sent": true}
		if devCode != "" {
			resp["dev_code"] = devCode
		}
		common.Success(c, resp)
		return
	}

	common.Success(c, gin.H{"sent": true})
}

type ResetPasswordReq struct {
	Email       string `json:"email" binding:"required,email"`
	Code        string `json:"code" binding:"required,len=6"`
	NewPassword string `json:"new_password" binding:"required,min=6,max=100"`
}

// ResetPassword verifies the reset code and sets a new password. On success it
// returns the account username/nickname so the user also recovers the login
// name they forgot.
func (s *UserService) ResetPassword(c *gin.Context) {
	var req ResetPasswordReq
	if err := common.BindJSON(c, &req); err != nil {
		return
	}

	user := s.userByEmail(req.Email)
	if user == nil {
		common.Error(c, errs.InvalidParam, "该邮箱未注册任何账号")
		return
	}

	ok, reason := s.kit.Verify.Check(req.Email, "reset", req.Code, s.kit.MaxAttempts)
	if !ok {
		common.Error(c, errs.InvalidParam, reason)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		common.Error(c, errs.Internal, "密码加密失败")
		return
	}

	if s.isMemoryMode() {
		s.mem.UpdateUserPassword(user.ID, string(hash))
	} else {
		if _, err := s.pg.Pool.Exec(context.Background(),
			`UPDATE users SET password_hash=$1 WHERE id=$2`, string(hash), user.ID); err != nil {
			common.Error(c, errs.Internal, "密码更新失败")
			return
		}
	}

	common.Success(c, gin.H{
		"reset":   true,
		"username": user.Username,
		"nickname": user.Nickname,
	})
}

// userByEmail looks up a user by email in either storage mode.
func (s *UserService) userByEmail(email string) *common.User {
	if s.isMemoryMode() {
		return s.mem.GetUserByEmail(email)
	}
	var u common.User
	err := s.pg.Pool.QueryRow(context.Background(),
		`SELECT id, username, nickname, password_hash, is_verified, role, status
		 FROM users WHERE email=$1`, email).Scan(
		&u.ID, &u.Username, &u.Nickname, &u.PasswordHash,
		&u.IsVerified, &u.Role, &u.Status)
	if err != nil {
		return nil
	}
	return &u
}

// userExistsByEmail reports whether any account is bound to the email.
func (s *UserService) userExistsByEmail(email string) bool {
	return s.userByEmail(email) != nil
}

// ========== Middleware ==========

func (s *UserService) AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Support both Authorization header (HTTP) and ?token= query param (WebSocket)
		tokenStr := ""
		authHeader := c.GetHeader("Authorization")
		if authHeader != "" {
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) == 2 && parts[0] == "Bearer" {
				tokenStr = parts[1]
			}
		}
		if tokenStr == "" {
			tokenStr = c.Query("token")
		}
		if tokenStr == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"code": errs.Unauthorized, "message": "missing token"})
			c.Abort()
			return
		}
		claims, err := s.jwtMgr.ParseToken(tokenStr)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": errs.InvalidToken, "message": err.Error()})
			c.Abort()
			return
		}
		if claims.TokenType != "access" {
			c.JSON(http.StatusUnauthorized, gin.H{"code": errs.InvalidToken, "message": "wrong token type"})
			c.Abort()
			return
		}
		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)
		c.Next()
	}
}

// ========== Profile Handlers ==========

func (s *UserService) GetProfile(c *gin.Context) {
	userID := c.GetInt64("user_id")

	// Memory mode fallback
	if s.isMemoryMode() {
		user := s.mem.GetUserByID(userID)
		if user == nil {
			common.Error(c, errs.UserNotFound, "")
			return
		}
		common.Success(c, user)
		return
	}

	var user common.User
	err := s.pg.Pool.QueryRow(context.Background(),
		`SELECT id, username, nickname, email, phone, avatar, is_verified, role, status, created_at
		 FROM users WHERE id=$1`, userID).Scan(
		&user.ID, &user.Username, &user.Nickname, &user.Email, &user.Phone,
		&user.Avatar, &user.IsVerified, &user.Role, &user.Status, &user.CreatedAt)
	if err != nil {
		common.Error(c, errs.UserNotFound, "")
		return
	}
	common.Success(c, user)
}

type UpdateProfileReq struct {
	Nickname string `json:"nickname"`
	Email    string `json:"email"`
	Phone    string `json:"phone"`
	Avatar   string `json:"avatar"`
}

func (s *UserService) UpdateProfile(c *gin.Context) {
	userID := c.GetInt64("user_id")
	var req UpdateProfileReq
	if err := common.BindJSON(c, &req); err != nil {
		return
	}
	_, err := s.pg.Pool.Exec(context.Background(),
		`UPDATE users SET nickname=COALESCE(NULLIF($1,''),nickname),
		 email=COALESCE(NULLIF($2,''),email), phone=COALESCE(NULLIF($3,''),phone),
		 avatar=COALESCE(NULLIF($4,''),avatar), updated_at=NOW()
		 WHERE id=$5`, req.Nickname, req.Email, req.Phone, req.Avatar, userID)
	if err != nil {
		common.Error(c, errs.Internal, "update failed")
		return
	}
	common.Success(c, gin.H{"ok": true})
}

// ========== Wallet Handlers ==========

func (s *UserService) GetWallet(c *gin.Context) {
	userID := c.GetInt64("user_id")

	// Memory mode fallback
	if s.isMemoryMode() {
		wallet := s.mem.GetWallet(userID)
		if wallet == nil {
			common.Error(c, errs.Internal, "wallet not found")
			return
		}
		common.Success(c, wallet)
		return
	}

	var wallet common.Wallet
	err := s.pg.Pool.QueryRow(context.Background(),
		`SELECT id, user_id, balance, frozen, total_recharged, created_at
		 FROM wallets WHERE user_id=$1`, userID).Scan(
		&wallet.ID, &wallet.UserID, &wallet.Balance, &wallet.Frozen,
		&wallet.TotalRecharged, &wallet.CreatedAt)
	if err != nil {
		common.Error(c, errs.Internal, "wallet not found")
		return
	}
	common.Success(c, wallet)
}

type RechargeReq struct {
	Amount float64 `json:"amount" binding:"required,min=10"`
}

func (s *UserService) RechargeWallet(c *gin.Context) {
	userID := c.GetInt64("user_id")
	var req RechargeReq
	if err := common.BindJSON(c, &req); err != nil {
		return
	}

	// Memory mode fallback
	if s.isMemoryMode() {
		s.rechargeMem(c, userID, &req)
		return
	}

	// 10 RMB = 10,000 game coins (prices in USD)
	gameCoins := req.Amount * 1000 // 10元 = 10000游戏币

	tx, err := s.pg.Pool.Begin(context.Background())
	if err != nil {
		common.Error(c, errs.Internal, "tx begin failed")
		return
	}
	defer tx.Rollback(context.Background())

	// Fetch wallet with lock
	var walletID int64
	var balance float64
	var version int64
	err = tx.QueryRow(context.Background(),
		`SELECT id, balance, version FROM wallets WHERE user_id=$1 FOR UPDATE`, userID).Scan(&walletID, &balance, &version)
	if err != nil {
		common.Error(c, errs.Internal, "wallet error")
		return
	}

	newBalance := balance + gameCoins
	_, err = tx.Exec(context.Background(),
		`UPDATE wallets SET balance=$1, total_recharged=total_recharged+$2, version=version+1, updated_at=NOW()
		 WHERE id=$3 AND version=$4`, newBalance, gameCoins, walletID, version)
	if err != nil {
		common.Error(c, errs.Internal, "update wallet failed")
		return
	}

	// Record transaction
	_, err = tx.Exec(context.Background(),
		`INSERT INTO wallet_transactions (user_id, type, amount, balance_before, balance_after, remark)
		 VALUES ($1, 'recharge', $2, $3, $4, $5)`,
		userID, gameCoins, balance, newBalance, fmt.Sprintf("充值%.0f元→%.0f游戏币", req.Amount, gameCoins))
	if err != nil {
		common.Error(c, errs.Internal, "txn record failed")
		return
	}

	if err := tx.Commit(context.Background()); err != nil {
		common.Error(c, errs.Internal, "commit failed")
		return
	}

	common.Success(c, gin.H{
		"amount_rmb":    req.Amount,
		"game_coins":    gameCoins,
		"balance_after": newBalance,
	})
}

func (s *UserService) GetWalletTransactions(c *gin.Context) {
	userID := c.GetInt64("user_id")
	page, pageSize := parsePagination(c)

	// Memory mode fallback
	if s.isMemoryMode() {
		all := s.mem.GetWalletTransactions(userID)
		// 按时间从近到远排序
		sort.Slice(all, func(i, j int) bool {
			return all[i].CreatedAt.After(all[j].CreatedAt)
		})
		total := len(all)
		start := (page - 1) * pageSize
		if start > total {
			start = total
		}
		end := start + pageSize
		if end > total {
			end = total
		}
		list := all[start:end]
		if list == nil {
			list = []common.WalletTransaction{}
		}
		common.Success(c, gin.H{
			"list":      list,
			"total":     total,
			"page":      page,
			"page_size": pageSize,
		})
		return
	}

	offset := (page - 1) * pageSize
	var total int64
	_ = s.pg.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM wallet_transactions WHERE user_id=$1`, userID).Scan(&total)

	rows, err := s.pg.Pool.Query(context.Background(),
		`SELECT id, type, amount, balance_before, balance_after, reference_id, remark, created_at
		 FROM wallet_transactions WHERE user_id=$1
		 ORDER BY created_at DESC LIMIT $2 OFFSET $3`, userID, pageSize, offset)
	if err != nil {
		common.Error(c, errs.Internal, "query failed")
		return
	}
	defer rows.Close()

	var txns []common.WalletTransaction
	for rows.Next() {
		var t common.WalletTransaction
		if err := rows.Scan(&t.ID, &t.Type, &t.Amount, &t.BalanceBefore, &t.BalanceAfter,
			&t.ReferenceID, &t.Remark, &t.CreatedAt); err != nil {
			continue
		}
		t.UserID = userID
		txns = append(txns, t)
	}
	if txns == nil {
		txns = []common.WalletTransaction{}
	}

	common.Success(c, gin.H{
		"list":      txns,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// ========== Memory Mode Implementations ==========

func (s *UserService) registerMem(c *gin.Context, req *RegisterReq) {
	// Check if username exists
	if existing := s.mem.GetUserByUsername(req.Username); existing != nil {
		common.Error(c, errs.UserExists, "username already taken")
		return
	}
	// One verified email per account
	if existing := s.mem.GetUserByEmail(req.Email); existing != nil {
		common.Error(c, errs.InvalidParam, "该邮箱已被注册")
		return
	}

	// Hash password
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		common.Error(c, errs.Internal, "password hash failed")
		return
	}

	nickname := req.Nickname
	if nickname == "" {
		nickname = req.Username
	}

	bonus := s.kit.VerifiedBonus
	userID := s.mem.NextUserID()
	now := time.Now()
	user := &common.User{
		ID:               userID,
		Username:         req.Username,
		Nickname:         nickname,
		PasswordHash:     string(hash),
		Email:            req.Email,
		IsVerified:       true,
		Role:             "user",
		Status:           1,
		CultivationLevel: 1, // 练气期
		SpiritEnergy:     0,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	s.mem.SaveUser(user)

	// Verified-registration bonus
	wallet := &common.Wallet{
		ID:             userID,
		UserID:         userID,
		Balance:        bonus,
		Frozen:         0,
		TotalRecharged: 0,
		Version:        1,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	s.mem.SaveWallet(wallet)

	// Record bonus transaction
	s.mem.SaveWalletTransaction(userID, &common.WalletTransaction{
		ID:            time.Now().UnixNano(),
		UserID:        userID,
		Type:          "bonus",
		Amount:        bonus,
		BalanceBefore: 0,
		BalanceAfter:  bonus,
		Remark:        "邮箱验证注册奖励",
		CreatedAt:     now,
	})

	// Generate tokens
	accessToken, _ := s.jwtMgr.GenerateAccessToken(userID, req.Username, nickname)
	refreshToken, _ := s.jwtMgr.GenerateRefreshToken(userID, req.Username, nickname)

	common.Success(c, gin.H{
		"user_id":       userID,
		"username":      req.Username,
		"nickname":      nickname,
		"email":         req.Email,
		"is_verified":   true,
		"access_token":  accessToken,
		"refresh_token": refreshToken,
	})
}

func (s *UserService) loginMem(c *gin.Context, req *LoginReq) {
	user := s.mem.GetUserByUsername(req.Username)
	if user == nil {
		common.Error(c, errs.UserNotFound, "user not found")
		return
	}
	if user.Status != 1 {
		common.Error(c, errs.Forbidden, "account disabled")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		common.Error(c, errs.PasswordMismatch, "wrong password")
		return
	}

	accessToken, _ := s.jwtMgr.GenerateAccessToken(user.ID, user.Username, user.Nickname)
	refreshToken, _ := s.jwtMgr.GenerateRefreshToken(user.ID, user.Username, user.Nickname)

	common.Success(c, gin.H{
		"user_id":       user.ID,
		"username":      user.Username,
		"nickname":      user.Nickname,
		"role":          user.Role,
		"access_token":  accessToken,
		"refresh_token": refreshToken,
	})
}

func (s *UserService) rechargeMem(c *gin.Context, userID int64, req *RechargeReq) {
	wallet := s.mem.GetWallet(userID)
	if wallet == nil {
		common.Error(c, errs.Internal, "wallet not found")
		return
	}

	gameCoins := req.Amount * 1000
	balanceBefore := wallet.Balance
	newBalance := balanceBefore + gameCoins

	s.mem.UpdateWalletBalance(userID, newBalance, wallet.Frozen)

	s.mem.SaveWalletTransaction(userID, &common.WalletTransaction{
		ID:            time.Now().UnixNano(),
		UserID:        userID,
		Type:          "recharge",
		Amount:        gameCoins,
		BalanceBefore: balanceBefore,
		BalanceAfter:  newBalance,
		Remark:        fmt.Sprintf("充值%.0f元→%.0f游戏币", req.Amount, gameCoins),
		CreatedAt:     time.Now(),
	})

	common.Success(c, gin.H{
		"amount_rmb":    req.Amount,
		"game_coins":    gameCoins,
		"balance_after": newBalance,
	})
}
