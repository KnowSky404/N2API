package encryptionrotation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/encryptioninventory"
	"github.com/KnowSky404/N2API/backend/internal/secret"
)

func TestCheckReportsReadyOnlyWhenEveryGatePasses(t *testing.T) {
	now := time.Date(2026, 7, 23, 16, 0, 0, 0, time.UTC)
	lease := &fakeLease{values: []encryptioninventory.EncryptedValue{
		{Table: "provider_account_credentials", Type: secret.SecretKindOAuthAccessToken, RowID: 7, Ciphertext: mustEncrypt(t, testKeyring(t), secret.SecretKindOAuthAccessToken, "access-token-canary")},
	}}
	result, err := Check(context.Background(), fakeRepository{lease: lease, acquired: true}, testKeyring(t), Options{
		CurrentKeyExplicit: true,
		PreviousKeyCount:   1,
		BackupIdentifier:   "restore-record-20260723-01",
		BackupCreatedAt:    now.Add(-2 * time.Hour),
		BackupRestoredAt:   now.Add(-time.Hour),
	}, func() time.Time { return now })
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if result.Status != StatusReady || !result.DryRun || result.InventoryStatus != encryptioninventory.StatusOK {
		t.Fatalf("result = %+v", result)
	}
	if !lease.closed {
		t.Fatal("lease was not closed")
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	if strings.Contains(string(encoded), "access-token-canary") {
		t.Fatalf("result leaked plaintext canary: %s", encoded)
	}
	for _, name := range []string{
		CheckCurrentKeyConfigured,
		CheckPreviousKeyringConfigured,
		CheckBackupConfirmationValid,
		CheckExclusiveLockAcquired,
		CheckInventoryDryRunConfirmed,
		CheckRequiredSecretsReadable,
	} {
		assertCheck(t, result, name, CheckPassed)
	}
}

func TestCheckRejectsEachPreconditionWithoutMutatingData(t *testing.T) {
	now := time.Date(2026, 7, 23, 16, 0, 0, 0, time.UTC)
	base := Options{
		CurrentKeyExplicit: true,
		PreviousKeyCount:   1,
		BackupIdentifier:   "restore-record-20260723-01",
		BackupCreatedAt:    now.Add(-2 * time.Hour),
		BackupRestoredAt:   now.Add(-time.Hour),
	}
	tests := map[string]struct {
		options Options
		values  []encryptioninventory.EncryptedValue
		check   string
		reason  string
	}{
		"implicit current key":      {options: withOptions(base, func(value *Options) { value.CurrentKeyExplicit = false }), check: CheckCurrentKeyConfigured, reason: ReasonCurrentKeyNotExplicit},
		"missing previous key":      {options: withOptions(base, func(value *Options) { value.PreviousKeyCount = 0 }), check: CheckPreviousKeyringConfigured, reason: ReasonPreviousKeyringMissing},
		"invalid backup identifier": {options: withOptions(base, func(value *Options) { value.BackupIdentifier = "https://storage.example.test/dump" }), check: CheckBackupConfirmationValid, reason: ReasonBackupIdentifierInvalid},
		"backup path":               {options: withOptions(base, func(value *Options) { value.BackupIdentifier = "/backups/operator.dump" }), check: CheckBackupConfirmationValid, reason: ReasonBackupIdentifierInvalid},
		"future backup":             {options: withOptions(base, func(value *Options) { value.BackupCreatedAt = now.Add(time.Minute) }), check: CheckBackupConfirmationValid, reason: ReasonBackupTimeInvalid},
		"restore before backup":     {options: withOptions(base, func(value *Options) { value.BackupRestoredAt = now.Add(-3 * time.Hour) }), check: CheckBackupConfirmationValid, reason: ReasonBackupTimeInvalid},
		"stale restore": {options: withOptions(base, func(value *Options) {
			value.BackupCreatedAt = now.Add(-25 * time.Hour)
			value.BackupRestoredAt = now.Add(-25 * time.Hour)
		}), check: CheckBackupConfirmationValid, reason: ReasonBackupConfirmationStale},
		"required unreadable": {
			options: base,
			values:  []encryptioninventory.EncryptedValue{{Table: "provider_account_credentials", Type: secret.SecretKindOAuthRefreshToken, RowID: 9, Ciphertext: "corrupt-ciphertext-canary"}},
			check:   CheckRequiredSecretsReadable, reason: ReasonRequiredSecretsUnreadable,
		},
		"unknown lifecycle": {
			options: base,
			values:  []encryptioninventory.EncryptedValue{{Table: "oauth_states", Type: secret.SecretKindOAuthCodeVerifier, RowID: 10, Ciphertext: "corrupt-ciphertext-canary"}},
			check:   CheckInventoryDryRunConfirmed, reason: ReasonInventoryBlocking,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			lease := &fakeLease{values: test.values}
			result, err := Check(context.Background(), fakeRepository{lease: lease, acquired: true}, testKeyring(t), test.options, func() time.Time { return now })
			if err != nil {
				t.Fatalf("Check returned error: %v", err)
			}
			if result.Status != StatusBlocked || !result.DryRun || lease.listCalls != 1 {
				t.Fatalf("result=%+v listCalls=%d", result, lease.listCalls)
			}
			got := findCheck(t, result, test.check)
			if got.Status != CheckFailed || got.ReasonCode != test.reason {
				t.Fatalf("check = %+v", got)
			}
			if (name == "invalid backup identifier" || name == "backup path") && result.BackupIdentifier != "" {
				t.Fatalf("invalid backup identifier was echoed: %q", result.BackupIdentifier)
			}
		})
	}
}

func TestCheckMapsContentionAndInfrastructureFailures(t *testing.T) {
	now := time.Date(2026, 7, 23, 16, 0, 0, 0, time.UTC)
	options := Options{CurrentKeyExplicit: true, PreviousKeyCount: 1, BackupIdentifier: "restore-01", BackupCreatedAt: now.Add(-2 * time.Hour), BackupRestoredAt: now.Add(-time.Hour)}
	result, err := Check(context.Background(), fakeRepository{acquired: false}, testKeyring(t), options, func() time.Time { return now })
	if err != nil || result.Status != StatusContended {
		t.Fatalf("contended result=%+v err=%v", result, err)
	}
	assertCheck(t, result, CheckExclusiveLockAcquired, CheckFailed)
	assertCheck(t, result, CheckInventoryDryRunConfirmed, CheckNotChecked)

	canary := errors.New("postgres-password-canary")
	if _, err := Check(context.Background(), fakeRepository{err: canary}, testKeyring(t), options, func() time.Time { return now }); !errors.Is(err, ErrGateFailed) || strings.Contains(err.Error(), "canary") {
		t.Fatalf("infrastructure error = %v", err)
	}
}

func TestCheckClosesLeaseAfterCancellationAndCloseFailure(t *testing.T) {
	now := time.Date(2026, 7, 23, 16, 0, 0, 0, time.UTC)
	options := Options{CurrentKeyExplicit: true, PreviousKeyCount: 1, BackupIdentifier: "restore-01", BackupCreatedAt: now.Add(-2 * time.Hour), BackupRestoredAt: now.Add(-time.Hour)}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	lease := &fakeLease{err: context.Canceled}
	if _, err := Check(canceled, fakeRepository{lease: lease, acquired: true}, testKeyring(t), options, func() time.Time { return now }); !errors.Is(err, ErrGateFailed) || !lease.closed {
		t.Fatalf("canceled Check err=%v closed=%v", err, lease.closed)
	}

	lease = &fakeLease{closeErr: errors.New("unlock-canary")}
	if _, err := Check(context.Background(), fakeRepository{lease: lease, acquired: true}, testKeyring(t), options, func() time.Time { return now }); !errors.Is(err, ErrGateFailed) || strings.Contains(err.Error(), "canary") {
		t.Fatalf("close failure = %v", err)
	}
}

type fakeRepository struct {
	lease    Lease
	acquired bool
	err      error
}

func (repository fakeRepository) TryAcquire(context.Context) (Lease, bool, error) {
	return repository.lease, repository.acquired, repository.err
}

type fakeLease struct {
	values    []encryptioninventory.EncryptedValue
	err       error
	closeErr  error
	closed    bool
	listCalls int
}

func (lease *fakeLease) ListEncryptedValues(context.Context) ([]encryptioninventory.EncryptedValue, error) {
	lease.listCalls++
	return lease.values, lease.err
}

func (lease *fakeLease) Close() error {
	lease.closed = true
	return lease.closeErr
}

func assertCheck(t *testing.T, result Result, name, status string) {
	t.Helper()
	check := findCheck(t, result, name)
	if check.Status != status {
		t.Fatalf("check %s = %+v, want status %s", name, check, status)
	}
}

func findCheck(t *testing.T, result Result, name string) GateCheck {
	t.Helper()
	for _, check := range result.Checks {
		if check.Name == name {
			return check
		}
	}
	t.Fatalf("missing check %s", name)
	return GateCheck{}
}

func withOptions(options Options, update func(*Options)) Options {
	update(&options)
	return options
}

func testKeyring(t *testing.T) *secret.Keyring {
	t.Helper()
	keyring, err := secret.NewKeyring(
		secret.EncryptionKey{ID: "current", Secret: "current-encryption-secret-at-least-32-bytes"},
		[]secret.EncryptionKey{{ID: "previous", Secret: "previous-encryption-secret-at-least-32-bytes"}},
	)
	if err != nil {
		t.Fatalf("NewKeyring returned error: %v", err)
	}
	return keyring
}

func mustEncrypt(t *testing.T, keyring *secret.Keyring, kind secret.SecretKind, plaintext string) string {
	t.Helper()
	value, err := keyring.EncryptStringFor(kind, plaintext)
	if err != nil {
		t.Fatalf("EncryptStringFor returned error: %v", err)
	}
	return value
}
