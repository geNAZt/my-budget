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
			return c.Redirect(http.StatusFound, "/dashboard?error=auth_failed")
		}

		// State format: integrationID:randomUUID
		parts := strings.Split(state, ":")
		if len(parts) == 0 {
			return c.Redirect(http.StatusFound, "/dashboard?error=invalid_state")
		}
		integrationID := parts[0]

		integration, err := integrationRepo.GetByIDGlobal(integrationID)
		if err != nil || integration == nil {
			return c.Redirect(http.StatusFound, "/dashboard?error=integration_not_found")
		}

		// We need the MasterKey to decrypt the config. In this anonymous callback,
		// it must be present in the SyncService memory cache (put there during the WS session).
		masterKey, err := syncService.GetMasterKey(integration.UserID, integration.ID)
		if err != nil {
			return c.Redirect(http.StatusFound, "/dashboard?error=key_not_available")
		}

		ciphertext, _ := base64.StdEncoding.DecodeString(integration.EncryptedConfig)
		configBytes, err := cryptoService.Decrypt(masterKey, ciphertext)
		if err != nil {
			return c.Redirect(http.StatusFound, "/dashboard?error=decryption_failed")
		}

		var config struct {
			ApplicationID string   `json:"application_id"`
			PrivateKey    string   `json:"private_key"`
			SessionID     string   `json:"session_id"`
			AccountIDs    []string `json:"account_ids"`
		}
		if err := json.Unmarshal(configBytes, &config); err != nil {
			return c.Redirect(http.StatusFound, "/dashboard?error=invalid_config")
		}

		token, err := ebService.CreateJWT(config.ApplicationID, config.PrivateKey)
		if err != nil {
			return c.Redirect(http.StatusFound, "/dashboard?error=jwt_failed")
		}

		sessionID, accountIDs, err := ebService.CreateSession(c.Request().Context(), token, code)
		if err != nil {
			return c.Redirect(http.StatusFound, "/dashboard?error=create_session_failed")
		}

		config.SessionID = sessionID
		config.AccountIDs = accountIDs

		updatedConfigBytes, _ := json.Marshal(config)
		newCiphertext, _ := cryptoService.Encrypt(masterKey, updatedConfigBytes)
		integration.EncryptedConfig = base64.StdEncoding.EncodeToString(newCiphertext)
		integration.Status = "ACTIVE"

		if err := integrationRepo.Save(integration.UserID, integration); err != nil {
			return c.Redirect(http.StatusFound, "/dashboard?error=save_failed")
		}

		return c.Redirect(http.StatusFound, "/dashboard?sync=true")
	}
}
