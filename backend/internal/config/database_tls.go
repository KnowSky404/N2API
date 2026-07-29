package config

import (
	"crypto/tls"

	"github.com/jackc/pgx/v5"
)

type databaseTransportRisks struct {
	plaintext     bool
	unverifiedTLS bool
}

func classifyDatabaseTransport(config *pgx.ConnConfig) databaseTransportRisks {
	if config == nil {
		return databaseTransportRisks{plaintext: true}
	}
	risk := databaseTransportRisks{}
	classify := func(tlsConfig *tls.Config) {
		if tlsConfig == nil {
			risk.plaintext = true
			return
		}
		if tlsConfig.InsecureSkipVerify || tlsConfig.ServerName == "" {
			risk.unverifiedTLS = true
		}
	}
	classify(config.TLSConfig)
	for _, fallback := range config.Fallbacks {
		classify(fallback.TLSConfig)
	}
	return risk
}
