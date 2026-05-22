package server

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"foreignreader_be/internal/auth"
	"foreignreader_be/internal/entitlement"
)

func registerAdminRoutes(mux *http.ServeMux, store *auth.Store, issuer *auth.TokenIssuer, entStore *entitlement.Store) {
	mux.Handle("GET /api/v1/admin/metrics", bearerAuthHandler(store, issuer, ownerOnly(entStore, handleAdminMetrics(entStore))))
}

func ownerOnly(entStore *entitlement.Store, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized", "missing authentication context")
			return
		}
		isOwner, err := entStore.IsOwner(r.Context(), u.ID)
		if err != nil {
			log.Printf("admin: owner_check user_id=%s err=%v", u.ID, err)
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not verify access")
			return
		}
		if !isOwner {
			writeAPIError(w, http.StatusForbidden, "forbidden", "owner access required")
			return
		}
		next(w, r)
	}
}

// ─── Response types ───────────────────────────────────────────────────────────

type adminMetrics struct {
	Summary      summaryMetrics  `json:"summary"`
	DauSeries    []dauPoint      `json:"dauSeries"`
	AuthAnon     []authAnonPoint `json:"authAnon"`
	Platform     []platformPoint `json:"platform"`
	Subscription subsMetrics     `json:"subscription"`
	Reading      []readingPoint  `json:"reading"`
	Translations []transPoint    `json:"translations"`
}

type summaryMetrics struct {
	TotalUsers   int `json:"totalUsers"`
	TotalDevices int `json:"totalDevices"`
	DAU          int `json:"dau"`
	WAU          int `json:"wau"`
	MAU          int `json:"mau"`
	ActivePro    int `json:"activePro"`
	TotalEvents  int `json:"totalEvents"`
}

type dauPoint struct {
	Day string `json:"day"`
	DAU int    `json:"dau"`
}

type authAnonPoint struct {
	Day  string `json:"day"`
	Auth int    `json:"auth"`
	Anon int    `json:"anon"`
}

type platformPoint struct {
	Day    string `json:"day"`
	IPhone int    `json:"iphone"`
	IPad   int    `json:"ipad"`
}

type subsMetrics struct {
	ProAccounts      int `json:"proAccounts"`
	FreeRegistered   int `json:"freeRegistered"`
	AnonymousDevices int `json:"anonymousDevices"`
}

type readingPoint struct {
	Day     string `json:"day"`
	Pages   int    `json:"pages"`
	Readers int    `json:"readers"`
}

type transPoint struct {
	Day          string `json:"day"`
	WordTrans    int    `json:"wordTrans"`
	ContextTrans int    `json:"contextTrans"`
}

// ─── Handler ──────────────────────────────────────────────────────────────────

func handleAdminMetrics(entStore *entitlement.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		db := entStore.DB
		ctx := r.Context()
		var m adminMetrics
		m.DauSeries = make([]dauPoint, 0)
		m.AuthAnon = make([]authAnonPoint, 0)
		m.Platform = make([]platformPoint, 0)
		m.Reading = make([]readingPoint, 0)
		m.Translations = make([]transPoint, 0)

		// Summary
		if err := db.QueryRowContext(ctx, `
			SELECT
				(SELECT COUNT(*) FROM users),
				(SELECT COUNT(DISTINCT anonymous_id) FROM analytics_events),
				(SELECT COUNT(DISTINCT anonymous_id) FROM analytics_events WHERE occurred_at >= CURRENT_DATE),
				(SELECT COUNT(DISTINCT anonymous_id) FROM analytics_events WHERE occurred_at >= date_trunc('week', NOW())),
				(SELECT COUNT(DISTINCT anonymous_id) FROM analytics_events WHERE occurred_at >= date_trunc('month', NOW())),
				(SELECT COUNT(*) FROM entitlements WHERE product_code = 'pro' AND status = 'active'),
				(SELECT COUNT(*) FROM analytics_events)
		`).Scan(
			&m.Summary.TotalUsers, &m.Summary.TotalDevices,
			&m.Summary.DAU, &m.Summary.WAU, &m.Summary.MAU,
			&m.Summary.ActivePro, &m.Summary.TotalEvents,
		); err != nil {
			log.Printf("admin/metrics: summary err=%v", err)
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not load metrics")
			return
		}

		// DAU series
		rows, err := db.QueryContext(ctx, `
			SELECT DATE(occurred_at), COUNT(DISTINCT anonymous_id)
			FROM analytics_events
			GROUP BY 1 ORDER BY 1
		`)
		if err != nil {
			log.Printf("admin/metrics: dau_series err=%v", err)
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not load metrics")
			return
		}
		for rows.Next() {
			var p dauPoint
			var d time.Time
			if err := rows.Scan(&d, &p.DAU); err != nil {
				_ = rows.Close()
				writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not load metrics")
				return
			}
			p.Day = d.Format("2006-01-02")
			m.DauSeries = append(m.DauSeries, p)
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not load metrics")
			return
		}

		// Auth vs Anonymous daily
		rows, err = db.QueryContext(ctx, `
			SELECT DATE(occurred_at),
				COUNT(DISTINCT CASE WHEN user_id IS NOT NULL THEN anonymous_id END),
				COUNT(DISTINCT CASE WHEN user_id IS NULL     THEN anonymous_id END)
			FROM analytics_events
			GROUP BY 1 ORDER BY 1
		`)
		if err != nil {
			log.Printf("admin/metrics: auth_anon err=%v", err)
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not load metrics")
			return
		}
		for rows.Next() {
			var p authAnonPoint
			var d time.Time
			if err := rows.Scan(&d, &p.Auth, &p.Anon); err != nil {
				_ = rows.Close()
				writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not load metrics")
				return
			}
			p.Day = d.Format("2006-01-02")
			m.AuthAnon = append(m.AuthAnon, p)
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not load metrics")
			return
		}

		// iPhone vs iPad daily
		rows, err = db.QueryContext(ctx, `
			SELECT DATE(occurred_at),
				COUNT(DISTINCT CASE WHEN platform = 'iphone' THEN anonymous_id END),
				COUNT(DISTINCT CASE WHEN platform = 'ipad'   THEN anonymous_id END)
			FROM analytics_events
			GROUP BY 1 ORDER BY 1
		`)
		if err != nil {
			log.Printf("admin/metrics: platform err=%v", err)
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not load metrics")
			return
		}
		for rows.Next() {
			var p platformPoint
			var d time.Time
			if err := rows.Scan(&d, &p.IPhone, &p.IPad); err != nil {
				_ = rows.Close()
				writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not load metrics")
				return
			}
			p.Day = d.Format("2006-01-02")
			m.Platform = append(m.Platform, p)
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not load metrics")
			return
		}

		// Subscription split (account-level for pro/free, device-level for anonymous)
		if err := db.QueryRowContext(ctx, `
			SELECT
				(SELECT COUNT(*) FROM entitlements
				   WHERE product_code = 'pro' AND status = 'active')                       AS pro_accounts,
				(SELECT COUNT(*) FROM users u
				   WHERE NOT EXISTS (
				       SELECT 1 FROM entitlements e
				       WHERE e.user_id = u.id AND e.product_code = 'pro' AND e.status = 'active'
				   ))                                                                       AS free_registered,
				(SELECT COUNT(DISTINCT anonymous_id) FROM analytics_events
				   WHERE anonymous_id NOT IN (
				       SELECT DISTINCT anonymous_id FROM analytics_events
				       WHERE user_id IS NOT NULL))                                          AS anonymous_devices
		`).Scan(&m.Subscription.ProAccounts, &m.Subscription.FreeRegistered, &m.Subscription.AnonymousDevices); err != nil {
			log.Printf("admin/metrics: subscription err=%v", err)
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not load metrics")
			return
		}

		// Reading pages daily
		rows, err = db.QueryContext(ctx, `
			SELECT DATE(occurred_at),
				SUM(ABS((properties->>'pageDelta')::int)),
				COUNT(DISTINCT anonymous_id)
			FROM analytics_events
			WHERE event_name = 'reader_page_changed'
			GROUP BY 1 ORDER BY 1
		`)
		if err != nil {
			log.Printf("admin/metrics: reading err=%v", err)
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not load metrics")
			return
		}
		for rows.Next() {
			var p readingPoint
			var d time.Time
			if err := rows.Scan(&d, &p.Pages, &p.Readers); err != nil {
				_ = rows.Close()
				writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not load metrics")
				return
			}
			p.Day = d.Format("2006-01-02")
			m.Reading = append(m.Reading, p)
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not load metrics")
			return
		}

		// Translations daily
		rows, err = db.QueryContext(ctx, `
			SELECT DATE(occurred_at),
				COUNT(*) FILTER (WHERE event_name = 'word_translation_requested'),
				COUNT(*) FILTER (WHERE event_name = 'context_translation_requested')
			FROM analytics_events
			WHERE event_name IN ('word_translation_requested', 'context_translation_requested')
			GROUP BY 1 ORDER BY 1
		`)
		if err != nil {
			log.Printf("admin/metrics: translations err=%v", err)
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not load metrics")
			return
		}
		for rows.Next() {
			var p transPoint
			var d time.Time
			if err := rows.Scan(&d, &p.WordTrans, &p.ContextTrans); err != nil {
				_ = rows.Close()
				writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not load metrics")
				return
			}
			p.Day = d.Format("2006-01-02")
			m.Translations = append(m.Translations, p)
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not load metrics")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(m)
	}
}
