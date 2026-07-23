package encryptionrotation

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/encryptioninventory"
	"github.com/KnowSky404/N2API/backend/internal/secret"
)

var ErrGateFailed = errors.New("encryption rotation gate failed")

const (
	StatusReady     = "ready"
	StatusBlocked   = "blocked"
	StatusContended = "contended"

	CheckPassed     = "passed"
	CheckFailed     = "failed"
	CheckNotChecked = "not_checked"

	CheckCurrentKeyConfigured      = "current_key_configured"
	CheckPreviousKeyringConfigured = "previous_keyring_configured"
	CheckBackupConfirmationValid   = "backup_confirmation_valid"
	CheckExclusiveLockAcquired     = "exclusive_lock_acquired"
	CheckInventoryDryRunConfirmed  = "inventory_dry_run_confirmed"
	CheckRequiredSecretsReadable   = "required_secrets_readable"

	ReasonCurrentKeyExplicit        = "current_key_explicit"
	ReasonCurrentKeyNotExplicit     = "current_key_not_explicit"
	ReasonPreviousKeyringConfigured = "previous_keyring_configured"
	ReasonPreviousKeyringMissing    = "previous_keyring_missing"
	ReasonBackupConfirmationValid   = "backup_confirmation_valid"
	ReasonBackupIdentifierInvalid   = "backup_identifier_invalid"
	ReasonBackupTimeInvalid         = "backup_time_invalid"
	ReasonBackupConfirmationStale   = "backup_confirmation_stale"
	ReasonExclusiveLockAcquired     = "exclusive_lock_acquired"
	ReasonExclusiveLockContended    = "exclusive_lock_contended"
	ReasonInventoryConfirmed        = "inventory_confirmed"
	ReasonInventoryBlocking         = "inventory_blocking"
	ReasonRequiredSecretsReadable   = "required_secrets_readable"
	ReasonRequiredSecretsUnreadable = "required_secrets_unreadable"
	ReasonNotChecked                = "not_checked"

	MaxBackupConfirmationAge = 24 * time.Hour
	maxBackupIdentifierBytes = 128
)

type Options struct {
	CurrentKeyExplicit bool
	PreviousKeyCount   int
	BackupIdentifier   string
	BackupCreatedAt    time.Time
	BackupRestoredAt   time.Time
}

type GateCheck struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	ReasonCode string `json:"reasonCode"`
}

type Result struct {
	Status                   string      `json:"status"`
	DryRun                   bool        `json:"dryRun"`
	CheckedAt                time.Time   `json:"checkedAt"`
	BackupIdentifier         string      `json:"backupIdentifier"`
	BackupCreatedAt          time.Time   `json:"backupCreatedAt"`
	BackupRestoredAt         time.Time   `json:"backupRestoredAt"`
	MaxBackupAgeHours        int         `json:"maxBackupAgeHours"`
	InventoryStatus          string      `json:"inventoryStatus,omitempty"`
	RequiredUnreadableCount  int         `json:"requiredUnreadableCount"`
	UnknownUnreadableCount   int         `json:"unknownUnreadableCount"`
	PurgeableUnreadableCount int         `json:"purgeableUnreadableCount"`
	Checks                   []GateCheck `json:"checks"`
}

type Lease interface {
	encryptioninventory.Repository
	Close() error
}

type Repository interface {
	TryAcquire(ctx context.Context) (Lease, bool, error)
}

func Check(ctx context.Context, repo Repository, keyring *secret.Keyring, options Options, now func() time.Time) (result Result, err error) {
	if repo == nil || keyring == nil || now == nil {
		return Result{}, ErrGateFailed
	}
	checkedAt := now().UTC()
	backupIdentifier := ""
	if validBackupIdentifier(options.BackupIdentifier) {
		backupIdentifier = options.BackupIdentifier
	}
	result = Result{
		Status:            StatusBlocked,
		DryRun:            true,
		CheckedAt:         checkedAt,
		BackupIdentifier:  backupIdentifier,
		BackupCreatedAt:   options.BackupCreatedAt.UTC(),
		BackupRestoredAt:  options.BackupRestoredAt.UTC(),
		MaxBackupAgeHours: int(MaxBackupConfirmationAge / time.Hour),
		Checks: []GateCheck{
			checkCurrentKey(options.CurrentKeyExplicit),
			checkPreviousKeyring(options.PreviousKeyCount, keyring.PreviousKeyCount()),
			checkBackupConfirmation(options, checkedAt),
			{Name: CheckExclusiveLockAcquired, Status: CheckNotChecked, ReasonCode: ReasonNotChecked},
			{Name: CheckInventoryDryRunConfirmed, Status: CheckNotChecked, ReasonCode: ReasonNotChecked},
			{Name: CheckRequiredSecretsReadable, Status: CheckNotChecked, ReasonCode: ReasonNotChecked},
		},
	}

	lease, acquired, acquireErr := repo.TryAcquire(ctx)
	if acquireErr != nil {
		return Result{}, ErrGateFailed
	}
	if !acquired {
		setCheck(&result, CheckExclusiveLockAcquired, CheckFailed, ReasonExclusiveLockContended)
		result.Status = StatusContended
		return result, nil
	}
	if lease == nil {
		return Result{}, ErrGateFailed
	}
	defer func() {
		if closeErr := lease.Close(); err == nil && closeErr != nil {
			result = Result{}
			err = ErrGateFailed
		}
	}()
	setCheck(&result, CheckExclusiveLockAcquired, CheckPassed, ReasonExclusiveLockAcquired)

	inventory, inventoryErr := encryptioninventory.VerifyAt(ctx, lease, keyring, checkedAt)
	if inventoryErr != nil {
		return Result{}, ErrGateFailed
	}
	result.InventoryStatus = inventory.Status
	result.RequiredUnreadableCount = lifecycleCount(inventory, encryptioninventory.LifecycleUnreadableRequired)
	result.UnknownUnreadableCount = lifecycleCount(inventory, encryptioninventory.LifecycleUnreadableUnknown)
	result.PurgeableUnreadableCount = lifecycleCount(inventory, encryptioninventory.LifecycleUnreadableExpiredPurgeable)
	if inventory.Status == encryptioninventory.StatusFailed {
		setCheck(&result, CheckInventoryDryRunConfirmed, CheckFailed, ReasonInventoryBlocking)
	} else {
		setCheck(&result, CheckInventoryDryRunConfirmed, CheckPassed, ReasonInventoryConfirmed)
	}
	if result.RequiredUnreadableCount > 0 {
		setCheck(&result, CheckRequiredSecretsReadable, CheckFailed, ReasonRequiredSecretsUnreadable)
	} else {
		setCheck(&result, CheckRequiredSecretsReadable, CheckPassed, ReasonRequiredSecretsReadable)
	}
	if allChecksPassed(result.Checks) {
		result.Status = StatusReady
	}
	return result, nil
}

func checkCurrentKey(explicit bool) GateCheck {
	if explicit {
		return GateCheck{Name: CheckCurrentKeyConfigured, Status: CheckPassed, ReasonCode: ReasonCurrentKeyExplicit}
	}
	return GateCheck{Name: CheckCurrentKeyConfigured, Status: CheckFailed, ReasonCode: ReasonCurrentKeyNotExplicit}
}

func checkPreviousKeyring(configured, actual int) GateCheck {
	if configured > 0 && configured == actual {
		return GateCheck{Name: CheckPreviousKeyringConfigured, Status: CheckPassed, ReasonCode: ReasonPreviousKeyringConfigured}
	}
	return GateCheck{Name: CheckPreviousKeyringConfigured, Status: CheckFailed, ReasonCode: ReasonPreviousKeyringMissing}
}

func checkBackupConfirmation(options Options, now time.Time) GateCheck {
	check := GateCheck{Name: CheckBackupConfirmationValid, Status: CheckFailed}
	if !validBackupIdentifier(options.BackupIdentifier) {
		check.ReasonCode = ReasonBackupIdentifierInvalid
		return check
	}
	createdAt := options.BackupCreatedAt.UTC()
	restoredAt := options.BackupRestoredAt.UTC()
	if createdAt.IsZero() || restoredAt.IsZero() || createdAt.After(now) || restoredAt.After(now) || restoredAt.Before(createdAt) {
		check.ReasonCode = ReasonBackupTimeInvalid
		return check
	}
	if now.Sub(createdAt) > MaxBackupConfirmationAge || now.Sub(restoredAt) > MaxBackupConfirmationAge {
		check.ReasonCode = ReasonBackupConfirmationStale
		return check
	}
	check.Status = CheckPassed
	check.ReasonCode = ReasonBackupConfirmationValid
	return check
}

func validBackupIdentifier(value string) bool {
	if len(value) == 0 || len(value) > maxBackupIdentifierBytes || !asciiLetterOrDigit(rune(value[0])) {
		return false
	}
	for _, char := range value {
		if asciiLetterOrDigit(char) || strings.ContainsRune("._-", char) {
			continue
		}
		return false
	}
	return true
}

func asciiLetterOrDigit(char rune) bool {
	return (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9')
}

func lifecycleCount(report encryptioninventory.Report, status encryptioninventory.LifecycleStatus) int {
	for _, count := range report.LifecycleCounts {
		if count.Status == status {
			return count.Count
		}
	}
	return 0
}

func setCheck(result *Result, name, status, reason string) {
	for index := range result.Checks {
		if result.Checks[index].Name == name {
			result.Checks[index].Status = status
			result.Checks[index].ReasonCode = reason
			return
		}
	}
}

func allChecksPassed(checks []GateCheck) bool {
	for _, check := range checks {
		if check.Status != CheckPassed {
			return false
		}
	}
	return true
}
