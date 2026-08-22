package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/goldarena/goldarena/internal/common"
	"github.com/goldarena/goldarena/pkg/captcha"
	"github.com/goldarena/goldarena/pkg/db"
	"github.com/goldarena/goldarena/pkg/jwt"
	"github.com/goldarena/goldarena/pkg/mail"
	"github.com/goldarena/goldarena/pkg/payment"
	"github.com/goldarena/goldarena/pkg/ratelimit"
	"github.com/goldarena/goldarena/pkg/redis"
	"github.com/goldarena/goldarena/pkg/verify"
	ws "github.com/goldarena/goldarena/pkg/websocket"
	"github.com/spf13/viper"
)

// smtpPassword returns the SMTP password, preferring the GOLDARENA_SMTP_PASS
// environment variable so the secret never has to live in a committed config
// file. Falls back to the value in config.yaml (mail.smtp.password) when the
// env var is unset (convenient for local dev only — do not commit real secrets).
func smtpPassword() string {
	if v := os.Getenv("GOLDARENA_SMTP_PASS"); v != "" {
		return v
	}
	return viper.GetString("mail.smtp.password")
}

func main() {
	configPath := "configs/config.yaml"
	// Prefer the local config.yaml (gitignored, holds real secrets). Fall back
	// to the example config shipped in the repo so a fresh clone still starts.
	if _, statErr := os.Stat(configPath); os.IsNotExist(statErr) {
		if _, exErr := os.Stat("configs/config.example.yaml"); exErr == nil {
			configPath = "configs/config.example.yaml"
		}
	}
	viper.SetConfigFile(configPath)
	if err := viper.ReadInConfig(); err != nil {
		log.Fatalf("Config error: %v", err)
	}

	// Database
	ctx := context.Background()
	pg, err := db.NewPostgres(ctx, db.Config{
		Host:     viper.GetString("database.postgres.host"),
		Port:     viper.GetInt("database.postgres.port"),
		User:     viper.GetString("database.postgres.user"),
		Password: viper.GetString("database.postgres.password"),
		DBName:   viper.GetString("database.postgres.dbname"),
		MaxConns: viper.GetInt("database.postgres.max_open_conns"),
		MinConns: viper.GetInt("database.postgres.max_idle_conns"),
	})
	if err != nil {
		log.Printf("WARNING: Postgres unavailable: %v (running without DB)", err)
	} else {
		defer pg.Close()
		log.Println("Postgres connected")
	}

	// Redis
	rdb, err := redis.NewRedis(redis.Config{
		Host:     viper.GetString("database.redis.host"),
		Port:     viper.GetInt("database.redis.port"),
		Password: viper.GetString("database.redis.password"),
		DB:       viper.GetInt("database.redis.db"),
	})
	if err != nil {
		log.Printf("WARNING: Redis init error: %v (running without cache)", err)
		rdb = nil
	} else if rdb.Client == nil {
		log.Println("WARNING: Redis unavailable — using in-process cache fallback (running without Redis)")
	} else {
		defer rdb.Close()
		log.Println("Redis connected")
	}

	// WebSocket Hub
	hub := ws.NewHub()
	go hub.Run()

	// JWT Manager
	jwtMgr := jwt.NewManager(
		viper.GetString("jwt.secret"),
		viper.GetDuration("jwt.access_expire"),
		viper.GetDuration("jwt.refresh_expire"),
	)

	// Memory Store (fast cache) + SQLite durable backend (survives restarts).
	// PostgreSQL remains the preferred store when a real PG is reachable; SQLite
	// is the durable fallback that actually runs in this sandbox.
	memStore := common.NewMemoryStore(viper.GetString("database.sqlite.path"))
	var memStorePath string
	var persistCancel context.CancelFunc

	// Prefer SQLite as the durable source of truth
	n, lerr := memStore.LoadFromSQLite()
	if lerr != nil {
		log.Printf("WARNING: failed to load from SQLite: %v", lerr)
	}
	if n == 0 {
		// SQLite empty: backfill from legacy JSON snapshot if present
		memStorePath = "data/memstore.json"
		if err := memStore.LoadSnapshot(memStorePath); err != nil {
			log.Printf("WARNING: failed to load memory snapshot: %v", err)
		} else {
			log.Println("Restored legacy JSON snapshot (users/wallets/positions)")
		}
		if err := memStore.MigrateMemoryToSQLite(); err != nil {
			log.Printf("WARNING: failed to migrate snapshot into SQLite: %v", err)
		} else {
			log.Println("Migrated legacy JSON snapshot into SQLite (durable)")
		}
	} else {
		log.Printf("Restored %d users from SQLite durable store", n)
	}
	memStorePath = "data/memstore.json"
	persistCtx, cancel := context.WithCancel(ctx)
	persistCancel = cancel
	go memStore.PersistLoop(persistCtx, memStorePath)

	// Services
	authKit := &AuthKit{
		Verify:        verify.NewStore(),
		Cap:           captcha.NewStore(),
		Mailer: mail.NewSender(mail.Config{
			SMTPHost:    viper.GetString("mail.smtp.host"),
			SMTPPort:    viper.GetInt("mail.smtp.port"),
			SMTPUser:    viper.GetString("mail.smtp.username"),
			SMTPPass:    smtpPassword(),
			From:        viper.GetString("mail.smtp.from"),
			UseSSL:      viper.GetBool("mail.smtp.use_ssl"),
			DevPrintCode: viper.GetBool("mail.dev_print_code"),
		}),
		SendEmailLim: ratelimit.New(1, viper.GetDuration("register.resend_interval")),
		SendIPLim:    ratelimit.New(viper.GetInt("register.ip_send_limit"), time.Hour),
		RegIPLim:     ratelimit.New(viper.GetInt("register.ip_register_limit"), time.Hour),
		CodeTTL:      viper.GetDuration("register.code_ttl"),
		MaxAttempts:  viper.GetInt("register.max_code_attempts"),
		VerifiedBonus: viper.GetFloat64("gamecoin.verified_bonus"),
	}
	userSvc := NewUserService(pg, rdb, jwtMgr, memStore, authKit)
	marketBasePrice := viper.GetFloat64("market.base_price")
	log.Printf("Config: market.base_price = %.2f", marketBasePrice)
	marketSvc := NewMarketService(rdb, hub, marketBasePrice)
	tradeSvc := NewTradeService(pg, rdb, memStore, marketSvc)
	marketSvc.tradeSvc = tradeSvc // back-ref for tick-level order matching
	cultivationSvc := NewCultivationService(pg, rdb, memStore)

	// Admin console (management backend). Seed a default admin on first boot.
	adminSvc := NewAdminService(pg, memStore, jwtMgr, marketSvc, tradeSvc)
	seedAdminIfNeeded(memStore, viper.GetString("admin.seed_username"), viper.GetString("admin.seed_password"))

	// Payment (real-money recharge).
	//   provider: "wechatpay"  — 微信支付 v3 Native 扫码支付（需自有商户号）
	//   provider: "aggregator" — 易支付/码付风格聚合网关（无需商户号）
	//   sandbox: true 时跳过真实网关与验签，用本地模拟回调验证入账流程
	payProvider := viper.GetString("payment.provider")
	paySandbox := viper.GetBool("payment.sandbox")

	// notify_base_url: payment async callback must be reachable from the internet.
	// Read from payment.notify_base_url, fall back to aggregator.notify_base_url
	// (backward compat), then default to localhost.
	payNotifyBase := viper.GetString("payment.notify_base_url")
	if payNotifyBase == "" {
		payNotifyBase = viper.GetString("payment.aggregator.notify_base_url")
	}
	if payNotifyBase == "" {
		payNotifyBase = "http://localhost:8080"
	}

	var payProv payment.Provider
	var payGateway, payPID, payKey string

	if payProvider == "aggregator" && !paySandbox {
		payProv = payment.NewAggregatorProvider()
		payGateway = viper.GetString("payment.aggregator.gateway")
		payPID = viper.GetString("payment.aggregator.pid")
		payKey = viper.GetString("payment.aggregator.key")

	} else if payProvider == "wechatpay" && !paySandbox {
		mchID := viper.GetString("payment.wechatpay.mchid")
		appID := viper.GetString("payment.wechatpay.appid")
		serialNo := viper.GetString("payment.wechatpay.serial_no")
		keyPath := viper.GetString("payment.wechatpay.private_key_path")
		apiV3Key := viper.GetString("payment.wechatpay.api_v3_key")

		// APIv3 key can also be injected via env var for security
		if envKey := os.Getenv("WECHATPAY_API_V3_KEY"); envKey != "" {
			apiV3Key = envKey
		}

		if mchID == "" || appID == "" || serialNo == "" || keyPath == "" || apiV3Key == "" {
			log.Println("WARNING: wechatpay config incomplete (need mchid/appid/serial_no/private_key_path/api_v3_key) — payment disabled")
		} else {
			keyBytes, err := os.ReadFile(keyPath)
			if err != nil {
				log.Printf("WARNING: wechatpay private key read failed (%s): %v — payment disabled", keyPath, err)
			} else {
				wp, err := payment.NewWeChatPayProvider(mchID, appID, serialNo, string(keyBytes), apiV3Key)
				if err != nil {
					log.Printf("WARNING: wechatpay init failed: %v — payment disabled", err)
				} else {
					payProv = wp
					log.Println("WeChat Pay v3 provider initialized (mchid=" + mchID + ")")
				}
			}
		}
	} else if payProvider == "wechatpay_v2" && !paySandbox {
		mchID := viper.GetString("payment.wechatpay.mchid")
		appID := viper.GetString("payment.wechatpay.appid")
		v2Key := viper.GetString("payment.wechatpay.v2_key")
		subMchID := viper.GetString("payment.wechatpay.sub_mchid")

		if envKey := os.Getenv("WECHATPAY_V2_KEY"); envKey != "" {
			v2Key = envKey
		}

		if mchID == "" || appID == "" || v2Key == "" {
			log.Println("WARNING: wechatpay_v2 config incomplete (need mchid/appid/v2_key) — payment disabled")
		} else {
			payProv = payment.NewWeChatPayV2Provider(mchID, appID, v2Key, subMchID)
			payKey = v2Key
			mode := "direct"
			if subMchID != "" {
				mode = "sub-merchant (sub_mchid=" + subMchID + ")"
			}
			log.Printf("WeChat Pay v2 provider initialized (mchid=%s, appid=%s, mode=%s)", mchID, appID, mode)
		}
	}

	// 游戏币兑换率：¥1 = 1000 游戏币
	paySvc := NewPaymentService(memStore, payProv, paySandbox, 1000.0, payNotifyBase, payGateway, payPID, payKey)

	// Start market data polling
	go marketSvc.Start(ctx)

	// Router
	router := gin.Default()
	router.Use(common.CORS())

	// ==========================================
	// Public Routes (no auth required)
	// ==========================================
	router.GET("/health", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok", "service": "GoldArena Gateway"}) })

	api := router.Group("/api/v1")
	{
		// Auth
		auth := api.Group("/auth")
		{
			auth.GET("/captcha", userSvc.GetCaptcha)
			auth.POST("/captcha/verify", userSvc.VerifyCaptcha)
			auth.POST("/send-code", userSvc.SendCode)
			auth.POST("/register", userSvc.Register)
			auth.POST("/send-reset-code", userSvc.SendResetCode)
			auth.POST("/reset-password", userSvc.ResetPassword)
			auth.POST("/login", userSvc.Login)
			auth.POST("/refresh", userSvc.RefreshToken)
		}

		// Market (public read)
		market := api.Group("/market")
		{
			market.GET("/quote", marketSvc.GetQuote)
			market.GET("/klines", marketSvc.GetKLines)
			market.GET("/symbols", marketSvc.GetSymbols)
		}

		// Payment (public): QR image generator + provider async callback (no auth)
		api.GET("/payment/qr", paySvc.QRCode)
		api.POST("/payment/notify", paySvc.Notify)
	}

	// ==========================================
	// Authenticated Routes
	// ==========================================
	authed := api.Group("")
	authed.Use(userSvc.AuthMiddleware())
	{
		// User & Wallet
		authed.GET("/user/profile", userSvc.GetProfile)
		authed.PUT("/user/profile", userSvc.UpdateProfile)
		authed.GET("/user/wallet", userSvc.GetWallet)
		authed.POST("/user/wallet/recharge", userSvc.RechargeWallet)
		authed.POST("/payment/create", paySvc.CreateOrder)
		authed.GET("/payment/orders", paySvc.ListOrders)
		authed.POST("/payment/simulate", paySvc.SimulatePaid)
		authed.GET("/user/wallet/transactions", userSvc.GetWalletTransactions)

		// Trading
		authed.POST("/trade/order", tradeSvc.PlaceOrder)
		authed.GET("/trade/positions", tradeSvc.GetPositions)
		authed.POST("/trade/close", tradeSvc.ClosePosition)
		authed.POST("/trade/cancel", tradeSvc.CancelOrder)
		authed.GET("/trade/pending", tradeSvc.GetPendingOrders)
		authed.POST("/trade/sltp", tradeSvc.UpdateOrderSLTP)
		authed.GET("/trade/pnl", tradeSvc.GetTradePnL)
		authed.GET("/trade/closed", tradeSvc.GetClosedPositionsPage)

		// Cultivation (修仙等级)
		authed.GET("/cultivation/progress", cultivationSvc.GetCultivationProgress)
		authed.GET("/cultivation/levels", cultivationSvc.GetAllLevels)
		authed.GET("/cultivation/rank", cultivationSvc.GetCultivationRank)
		authed.POST("/cultivation/breakthrough", cultivationSvc.Breakthrough)
		authed.POST("/cultivation/refresh", cultivationSvc.RefreshSpiritEnergy)

		// WebSocket (trading data feed)
		authed.GET("/ws", marketSvc.WebSocketHandler)
	}

	// ==========================================
	// Admin Console (role=admin required)
	// ==========================================
	admin := api.Group("/admin")
	admin.Use(adminSvc.AdminMiddleware())
	{
		admin.GET("/dashboard", adminSvc.Dashboard)
		admin.GET("/users", adminSvc.ListUsers)
		admin.GET("/users/:id", adminSvc.GetUser)
		admin.POST("/users/:id/balance", adminSvc.AdjustBalance)
		admin.POST("/users/:id/status", adminSvc.SetUserStatus)
		admin.GET("/positions", adminSvc.ListPositions)
		admin.POST("/positions/:id/close", adminSvc.ForceClosePosition)
		admin.GET("/orders", adminSvc.ListOrders)
		admin.GET("/payments", adminSvc.ListPayments)
		admin.POST("/payments/:no/credit", adminSvc.CreditPayment)
	}

	// Serve the built SPA (web/dist) so the whole app is reachable from one URL.
	// Any non-API GET is served as a static file, falling back to index.html for
	// client-side routes (SPA history mode).
	distDir := "web/dist"
	if _, statErr := os.Stat(distDir); statErr == nil {
		router.NoRoute(func(c *gin.Context) {
			reqPath := c.Request.URL.Path
			if strings.HasPrefix(reqPath, "/api") {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "not found"})
				return
			}
			// Disable caching so the browser always picks up a freshly built
			// SPA bundle (otherwise stale JS is served and new chart periods
			// like 5m/30m never show up after a redeploy).
			c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
			c.Header("Pragma", "no-cache")
			c.Header("Expires", "0")
			file := filepath.Join(distDir, filepath.Clean(reqPath))
			if info, err := os.Stat(file); err == nil && !info.IsDir() {
				c.File(file)
				return
			}
			c.File(filepath.Join(distDir, "index.html"))
		})
		log.Printf("SPA static serving enabled: %s", distDir)
	}

	// ==========================================
	// Start Server
	// ==========================================
	port := viper.GetString("server.gateway.port")
	if port == "" {
		port = "8080"
	}

	srv := &http.Server{Addr: ":" + port, Handler: router}

	go func() {
		log.Printf("🚀 GoldArena Gateway running on http://localhost:%s", port)
		log.Printf("   REST API: http://localhost:%s/api/v1", port)
		log.Printf("   WebSocket: ws://localhost:%s/api/v1/ws", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Listen error: %v", err)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down gracefully...")
	// Flush the latest in-memory snapshot + SQLite sequences to disk so a
	// clean restart loses essentially nothing.
	if persistCancel != nil {
		persistCancel()
	}
	if err := memStore.SaveSnapshot(memStorePath); err != nil {
		log.Printf("WARNING: final snapshot flush failed: %v", err)
	} else {
		log.Println("Final memory snapshot flushed to disk")
	}
	memStore.FlushMeta()
	if err := memStore.Close(); err != nil {
		log.Printf("WARNING: closing SQLite store: %v", err)
	} else {
		log.Println("SQLite store closed (durable)")
	}
	ctxShut, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv.Shutdown(ctxShut)
	log.Println("Server stopped")
}
