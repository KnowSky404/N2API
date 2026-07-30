package config

import (
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

const ConfigValidationSchemaVersion = "n2api.config-validation/v1"

const (
	ConfigValidationStatusValid   = "valid"
	ConfigValidationStatusInvalid = "invalid"
)

const (
	ConfigValidationReasonValid                    = "configuration_valid"
	ConfigValidationReasonInvalid                  = "configuration_invalid"
	ConfigValidationReasonPostgresPasswordMismatch = "postgres_password_mismatch"
	ConfigValidationReasonPostgresPasswordSource   = "postgres_password_source_invalid"
)

const (
	PostgresPasswordCheckMatched       = "matched"
	PostgresPasswordCheckNotApplicable = "not_applicable"
	PostgresPasswordCheckNotEvaluated  = "not_evaluated"
	PostgresPasswordCheckMismatch      = "mismatch"
	PostgresPasswordCheckSourceInvalid = "source_invalid"
)

type ConfigValidationReport struct {
	SchemaVersion         string `json:"schema_version"`
	Status                string `json:"status"`
	ReasonCode            string `json:"reason_code"`
	PostgresPasswordCheck string `json:"postgres_password_check"`
}

// Validate checks application configuration without connecting to external services.
func Validate(lookup func(string) string) ConfigValidationReport {
	report := ConfigValidationReport{
		SchemaVersion:         ConfigValidationSchemaVersion,
		Status:                ConfigValidationStatusInvalid,
		ReasonCode:            ConfigValidationReasonInvalid,
		PostgresPasswordCheck: PostgresPasswordCheckNotEvaluated,
	}

	cfg, err := Load(lookup)
	if err != nil {
		return report
	}

	postgresPassword, postgresPasswordAvailable, err := resolvePostgresPassword(lookup)
	if err != nil {
		report.ReasonCode = ConfigValidationReasonPostgresPasswordSource
		report.PostgresPasswordCheck = PostgresPasswordCheckSourceInvalid
		return report
	}
	if !postgresPasswordAvailable {
		report.Status = ConfigValidationStatusValid
		report.ReasonCode = ConfigValidationReasonValid
		report.PostgresPasswordCheck = PostgresPasswordCheckNotApplicable
		return report
	}

	poolConfig, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return report
	}
	if poolConfig.ConnConfig.Password != postgresPassword {
		report.ReasonCode = ConfigValidationReasonPostgresPasswordMismatch
		report.PostgresPasswordCheck = PostgresPasswordCheckMismatch
		return report
	}

	report.Status = ConfigValidationStatusValid
	report.ReasonCode = ConfigValidationReasonValid
	report.PostgresPasswordCheck = PostgresPasswordCheckMatched
	return report
}

func resolvePostgresPassword(lookup func(string) string) (string, bool, error) {
	directValue := lookup("POSTGRES_PASSWORD")
	filePath := lookup("POSTGRES_PASSWORD_FILE")
	if directValue != "" && filePath != "" {
		return "", false, errors.New("POSTGRES_PASSWORD and POSTGRES_PASSWORD_FILE cannot both be set")
	}
	if directValue != "" {
		return directValue, true, nil
	}
	if filePath == "" {
		return "", false, nil
	}
	fileValue, err := readSecretFile("POSTGRES_PASSWORD_FILE", filePath)
	if err != nil {
		return "", false, err
	}
	return fileValue, true, nil
}
