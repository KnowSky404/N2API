package store

import (
	"context"
	"fmt"

	"github.com/KnowSky404/N2API/backend/internal/encryptioninventory"
	"github.com/KnowSky404/N2API/backend/internal/secret"
	"github.com/jackc/pgx/v5/pgxpool"
)

type EncryptionInventoryRepository struct {
	pool *pgxpool.Pool
}

func NewEncryptionInventoryRepository(pool *pgxpool.Pool) *EncryptionInventoryRepository {
	return &EncryptionInventoryRepository{pool: pool}
}

const encryptionInventoryQuery = `
	SELECT table_name, secret_type, row_id, ciphertext
	FROM (
		SELECT
			1 AS class_order,
			'oauth_states'::text AS table_name,
			'oauth-code-verifier'::text AS secret_type,
			id AS row_id,
			encrypted_code_verifier AS ciphertext
		FROM oauth_states
		WHERE encrypted_code_verifier <> ''

		UNION ALL

		SELECT
			credential.class_order,
			'provider_account_credentials'::text AS table_name,
			credential.secret_type,
			account_id AS row_id,
			credential.ciphertext
		FROM provider_account_credentials
		CROSS JOIN LATERAL (VALUES
			(2, 'oauth-access-token'::text, encrypted_access_token),
			(3, 'oauth-refresh-token'::text, encrypted_refresh_token),
			(4, 'oauth-id-token'::text, encrypted_id_token),
			(5, 'provider-api-key'::text, encrypted_api_key),
			(6, 'provider-proxy-url'::text, encrypted_proxy_url)
		) AS credential(class_order, secret_type, ciphertext)
		WHERE credential.ciphertext <> ''

		UNION ALL

		SELECT
			7 AS class_order,
			'client_api_keys'::text AS table_name,
			'client-api-key'::text AS secret_type,
			id AS row_id,
			encrypted_secret AS ciphertext
		FROM client_api_keys
		WHERE encrypted_secret <> ''

		UNION ALL

		SELECT
			8 AS class_order,
			'alert_actions'::text AS table_name,
			'alert-action-destination'::text AS secret_type,
			id AS row_id,
			encrypted_destination AS ciphertext
		FROM alert_actions
		WHERE encrypted_destination <> ''
	) AS encrypted_values
	ORDER BY class_order, row_id
`

func (r *EncryptionInventoryRepository) ListEncryptedValues(ctx context.Context) ([]encryptioninventory.EncryptedValue, error) {
	if r == nil || r.pool == nil {
		return nil, fmt.Errorf("encryption inventory repository is not configured")
	}
	rows, err := r.pool.Query(ctx, encryptionInventoryQuery)
	if err != nil {
		return nil, fmt.Errorf("query encryption inventory: %w", err)
	}
	defer rows.Close()

	values := make([]encryptioninventory.EncryptedValue, 0)
	for rows.Next() {
		var value encryptioninventory.EncryptedValue
		var kind string
		if err := rows.Scan(&value.Table, &kind, &value.RowID, &value.Ciphertext); err != nil {
			return nil, fmt.Errorf("scan encryption inventory: %w", err)
		}
		value.Type = secret.SecretKind(kind)
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate encryption inventory: %w", err)
	}
	return values, nil
}
