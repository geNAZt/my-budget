package api

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/genazt/my-budget-script/backend/internal/crypto"
	"github.com/genazt/my-budget-script/backend/internal/repository"
	"github.com/genazt/my-budget-script/backend/internal/service"
	"github.com/labstack/echo/v4"
)

// HandleEnableBankingCallback HTTP Handler for OAuth2 callbacks
func HandleEnableBankingCallback(
	integrationRepo *repository.IntegrationRepository,
	syncService *service.SyncService,
	cryptoService *crypto.CryptoService,
	ebService *service.EnableBankingService,
) echo.HandlerFunc {
	return func(c echo.Context) error {
		code := c.QueryParam("code")
		state := c.QueryParam("state")
		errorParam := c.QueryParam("error")

		if errorParam != "" || code == "" || state == "" {
			return c.Redirect(http.StatusFound, "/realtime?error=auth_failed")
		}

		// State format: integrationID:randomUUID
		parts := strings.Split(state, ":")
		if len(parts) == 0 {
			return c.Redirect(http.StatusFound, "/realtime?error=invalid_state")
		}
		integrationID := parts[0]

		integration, err := integrationRepo.GetByIDGlobal(integrationID)
		if err != nil || integration == nil {
			return c.Redirect(http.StatusFound, "/realtime?error=integration_not_found")
		}

		masterKey, err := syncService.GetMasterKey(integration.UserID, integration.ID)
		if err != nil {
			return c.Redirect(http.StatusFound, "/realtime?error=key_not_available")
		}

		ciphertext, _ := base64.StdEncoding.DecodeString(integration.EncryptedConfig)
		configBytes, err := cryptoService.Decrypt(masterKey, ciphertext)
		if err != nil {
			return c.Redirect(http.StatusFound, "/realtime?error=decryption_failed")
		}

		var config struct {
			ApplicationID string `json:"application_id"`
			PrivateKey    string `json:"private_key"`
		}
		if err := json.Unmarshal(configBytes, &config); err != nil {
			return c.Redirect(http.StatusFound, "/realtime?error=invalid_config")
		}

		token, err := ebService.CreateJWT(config.ApplicationID, config.PrivateKey)
		if err != nil {
			return c.Redirect(http.StatusFound, "/realtime?error=jwt_failed")
		}

		sessionID, accountIDs, err := ebService.CreateSession(c.Request().Context(), token, code)
		if err != nil {
			return c.Redirect(http.StatusFound, "/realtime?error=create_session_failed")
		}

		if err := syncService.ApplyEnableBankingSession(c.Request().Context(), integration, sessionID, accountIDs, masterKey); err != nil {
			return c.Redirect(http.StatusFound, "/realtime?error=apply_session_failed")
		}

		return c.Redirect(http.StatusFound, "/realtime?sync=true")
	}
}
