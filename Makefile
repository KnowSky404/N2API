SHELL := /usr/bin/env bash

.PHONY: test test-ops ops-describe ops-doctor test-go-quality test-critical-race test-control-connections test-postgres-faults test-request-log-profile test-management-list-profile test-production-deploy test-e2e test-contracts test-playwright playwright-install \
	disk-check disk-check-heavy clean-dev-artifacts clean-dev-artifacts-deep \
	test-dev-artifacts test-restore-backup backup-dev

test:
	dev/testing/run.sh unit

test-ops:
	ops/tests/test-ops.sh

ops-describe:
	./ops/n2api describe --format json

ops-doctor:
	./ops/n2api doctor --format json

test-go-quality:
	dev/testing/run.sh go-quality

test-critical-race:
	dev/testing/run.sh critical-race

test-control-connections:
	dev/testing/run.sh control-connections

test-postgres-faults:
	dev/testing/run.sh postgres-faults

test-request-log-profile:
	dev/testing/run.sh request-log-profile

test-management-list-profile:
	dev/testing/run.sh management-list-profile

test-production-deploy:
	dev/ci/test-production-deploy.sh

test-e2e:
	dev/testing/run.sh gateway-e2e

test-contracts:
	dev/testing/run.sh contracts

test-playwright:
	dev/testing/run.sh playwright $(PLAYWRIGHT_ARGS)

playwright-install:
	dev/testing/run.sh playwright-install $(PLAYWRIGHT_INSTALL_ARGS)

disk-check:
	dev/maintenance/disk-check.sh

disk-check-heavy:
	dev/maintenance/disk-check.sh --heavy

clean-dev-artifacts:
	dev/maintenance/clean-dev-artifacts.sh

clean-dev-artifacts-deep:
	dev/maintenance/clean-dev-artifacts.sh --deep

test-dev-artifacts:
	dev/ci/test-dev-artifacts.sh
	dev/ci/test-postgres-backup.sh

test-restore-backup:
	dev/verification/test-restore-backup.sh

backup-dev:
	docker compose -f deploy/compose.yaml exec --no-TTY postgres-backup \
		/usr/local/bin/n2api-postgres-backup once
