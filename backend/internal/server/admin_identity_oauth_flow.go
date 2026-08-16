package server

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	adminOAuthFlowTTL           = 10 * time.Minute
	adminOAuthExchangeTTL       = time.Minute
	adminOAuthStateCookiePrefix = "tokenhub_admin_oauth_state_"
	adminOAuthStateCookiePath   = "/api/admin/auth/oauth/callback"
)

type adminOAuthFlow struct {
	State         string
	BrowserNonce  string
	ProviderID    string
	ReturnURL     string
	RedirectURI   string
	CodeChallenge string
	CookieSecure  bool
	CreatedAt     time.Time
}

type adminOAuthFlowRecord struct {
	ID               string `gorm:"primaryKey"`
	StateHash        string `gorm:"uniqueIndex"`
	BrowserNonceHash string
	ProviderID       string
	ReturnURL        string
	RedirectURI      string
	CodeChallenge    string
	CookieSecure     bool
	CreatedAt        time.Time
	ExpiresAt        time.Time `gorm:"index"`
}

type adminOAuthExchange struct {
	Code          string
	CodeChallenge string
	UserID        string
	CreatedAt     time.Time
}

type adminOAuthExchangeRecord struct {
	ID            string `gorm:"primaryKey"`
	CodeHash      string `gorm:"uniqueIndex"`
	CodeChallenge string
	UserID        string `gorm:"index"`
	CreatedAt     time.Time
	ExpiresAt     time.Time `gorm:"index"`
}

func (s *GormStore) SaveAdminOAuthFlow(flow adminOAuthFlow) error {
	if strings.TrimSpace(flow.State) == "" || strings.TrimSpace(flow.BrowserNonce) == "" ||
		strings.TrimSpace(flow.ProviderID) == "" || strings.TrimSpace(flow.ReturnURL) == "" ||
		strings.TrimSpace(flow.RedirectURI) == "" || !validAdminOAuthCodeChallenge(flow.CodeChallenge) {
		return fmt.Errorf("admin OAuth flow is incomplete")
	}
	now := time.Now().UTC()
	if flow.CreatedAt.IsZero() {
		flow.CreatedAt = now
	}
	record := adminOAuthFlowRecord{
		ID:               NewID("oauth_flow"),
		StateHash:        HashSecret(flow.State),
		BrowserNonceHash: HashSecret(flow.BrowserNonce),
		ProviderID:       flow.ProviderID,
		ReturnURL:        flow.ReturnURL,
		RedirectURI:      flow.RedirectURI,
		CodeChallenge:    flow.CodeChallenge,
		CookieSecure:     flow.CookieSecure,
		CreatedAt:        flow.CreatedAt,
		ExpiresAt:        flow.CreatedAt.Add(adminOAuthFlowTTL),
	}
	_ = s.db.Where("expires_at <= ?", now).Delete(&adminOAuthFlowRecord{}).Error
	return s.db.Create(&record).Error
}

func (s *GormStore) ConsumeAdminOAuthFlow(state string, browserNonce string) (adminOAuthFlow, bool, error) {
	state = strings.TrimSpace(state)
	browserNonce = strings.TrimSpace(browserNonce)
	if state == "" || browserNonce == "" {
		return adminOAuthFlow{}, false, nil
	}
	stateHash := HashSecret(state)
	browserNonceHash := HashSecret(browserNonce)
	var record adminOAuthFlowRecord
	consumed := false
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.lockScopeForUpdate(tx, "admin_oauth_flow", stateHash); err != nil {
			return err
		}
		query := tx
		if s.dbDriver == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		now := time.Now().UTC()
		if err := query.First(&record, "state_hash = ? AND browser_nonce_hash = ? AND expires_at > ?", stateHash, browserNonceHash, now).Error; err != nil {
			return err
		}
		result := tx.Where("id = ? AND state_hash = ? AND browser_nonce_hash = ?", record.ID, stateHash, browserNonceHash).Delete(&adminOAuthFlowRecord{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		consumed = true
		return nil
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return adminOAuthFlow{}, false, nil
		}
		return adminOAuthFlow{}, false, err
	}
	return adminOAuthFlow{
		State:         state,
		BrowserNonce:  browserNonce,
		ProviderID:    record.ProviderID,
		ReturnURL:     record.ReturnURL,
		RedirectURI:   record.RedirectURI,
		CodeChallenge: record.CodeChallenge,
		CookieSecure:  record.CookieSecure,
		CreatedAt:     record.CreatedAt,
	}, consumed, nil
}

func (s *GormStore) SaveAdminOAuthExchange(exchange adminOAuthExchange) error {
	if strings.TrimSpace(exchange.Code) == "" || !validAdminOAuthCodeChallenge(exchange.CodeChallenge) || strings.TrimSpace(exchange.UserID) == "" {
		return fmt.Errorf("admin OAuth exchange is incomplete")
	}
	now := time.Now().UTC()
	if exchange.CreatedAt.IsZero() {
		exchange.CreatedAt = now
	}
	record := adminOAuthExchangeRecord{
		ID:            NewID("oauth_exchange"),
		CodeHash:      HashSecret(exchange.Code),
		CodeChallenge: exchange.CodeChallenge,
		UserID:        exchange.UserID,
		CreatedAt:     exchange.CreatedAt,
		ExpiresAt:     exchange.CreatedAt.Add(adminOAuthExchangeTTL),
	}
	_ = s.db.Where("expires_at <= ?", now).Delete(&adminOAuthExchangeRecord{}).Error
	return s.db.Create(&record).Error
}

func (s *GormStore) ConsumeAdminOAuthExchange(code string, codeVerifier string) (adminOAuthExchange, bool, error) {
	code = strings.TrimSpace(code)
	codeChallenge, valid := adminOAuthCodeChallenge(codeVerifier)
	if code == "" || !valid {
		return adminOAuthExchange{}, false, nil
	}
	codeHash := HashSecret(code)
	var record adminOAuthExchangeRecord
	consumed := false
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.lockScopeForUpdate(tx, "admin_oauth_exchange", codeHash); err != nil {
			return err
		}
		query := tx
		if s.dbDriver == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		now := time.Now().UTC()
		if err := query.First(&record, "code_hash = ? AND code_challenge = ? AND expires_at > ?", codeHash, codeChallenge, now).Error; err != nil {
			return err
		}
		result := tx.Where("id = ? AND code_hash = ? AND code_challenge = ?", record.ID, codeHash, codeChallenge).Delete(&adminOAuthExchangeRecord{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		consumed = true
		return nil
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return adminOAuthExchange{}, false, nil
		}
		return adminOAuthExchange{}, false, err
	}
	return adminOAuthExchange{
		Code:          code,
		CodeChallenge: record.CodeChallenge,
		UserID:        record.UserID,
		CreatedAt:     record.CreatedAt,
	}, consumed, nil
}

func adminOAuthStateCookieName(state string) string {
	return adminOAuthStateCookiePrefix + HashSecret(strings.TrimSpace(state))[:24]
}

func setAdminOAuthBindingCookie(w http.ResponseWriter, name string, value string, path string, secure bool, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     path,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(ttl / time.Second),
		Expires:  time.Now().UTC().Add(ttl),
	})
}

func validateAdminOAuthPKCE(codeChallenge string, method string) (string, error) {
	codeChallenge = strings.TrimSpace(codeChallenge)
	if method != "S256" || !validAdminOAuthCodeChallenge(codeChallenge) {
		return "", NewHTTPError(http.StatusBadRequest, "invalid_oauth_code_challenge", "OAuth code challenge must use S256")
	}
	return codeChallenge, nil
}

func validAdminOAuthCodeChallenge(codeChallenge string) bool {
	if len(codeChallenge) != 43 {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(codeChallenge)
	return err == nil && len(decoded) == sha256.Size
}

func adminOAuthCodeChallenge(codeVerifier string) (string, bool) {
	codeVerifier = strings.TrimSpace(codeVerifier)
	if len(codeVerifier) < 43 || len(codeVerifier) > 128 {
		return "", false
	}
	for _, char := range codeVerifier {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '-' || char == '.' || char == '_' || char == '~' {
			continue
		}
		return "", false
	}
	sum := sha256.Sum256([]byte(codeVerifier))
	return base64.RawURLEncoding.EncodeToString(sum[:]), true
}

func clearAdminOAuthBindingCookie(w http.ResponseWriter, name string, path string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     path,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(1, 0).UTC(),
	})
}
