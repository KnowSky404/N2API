package provider

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/systemevent"
)

func (s *Service) SelectAccountForModel(ctx context.Context, model string, excludedAccountIDs ...int64) (SelectedAccount, error) {
	return s.SelectAccountForModelAndEndpoint(ctx, model, "", excludedAccountIDs...)
}

func (s *Service) SelectAccountForModelAndEndpoint(ctx context.Context, model, endpoint string, excludedAccountIDs ...int64) (SelectedAccount, error) {
	endpoint, err := NormalizeEndpoint(endpoint)
	if err != nil {
		return SelectedAccount{}, err
	}
	if !s.Configured() {
		return SelectedAccount{}, ErrNotConfigured
	}

	accounts, hasEnabled, notFoundErr, err := s.selectionCandidatesForEndpoint(ctx, model, endpoint, excludedAccountIDs)
	if err != nil {
		return SelectedAccount{}, err
	}
	return s.selectFromCandidates(ctx, accounts, hasEnabled, notFoundErr)
}

func (s *Service) SelectAccountForModelAndSession(ctx context.Context, model, sessionID string, excludedAccountIDs ...int64) (SelectedAccount, error) {
	return s.SelectAccountForModelAndSessionAndEndpoint(ctx, model, "", sessionID, excludedAccountIDs...)
}

func (s *Service) SelectAccountForModelAndSessionAndEndpoint(ctx context.Context, model, endpoint, sessionID string, excludedAccountIDs ...int64) (SelectedAccount, error) {
	endpoint, err := NormalizeEndpoint(endpoint)
	if err != nil {
		return SelectedAccount{}, err
	}
	model = strings.TrimSpace(model)
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return s.SelectAccountForModelAndEndpoint(ctx, model, endpoint, excludedAccountIDs...)
	}
	if !s.Configured() {
		return SelectedAccount{}, ErrNotConfigured
	}

	accounts, hasEnabled, notFoundErr, err := s.selectionCandidatesForEndpoint(ctx, model, endpoint, excludedAccountIDs)
	if err != nil {
		return SelectedAccount{}, err
	}
	accounts, _, err = s.stickySessionCandidates(ctx, accounts, model, sessionID)
	if err != nil {
		return SelectedAccount{}, err
	}
	selected, err := s.selectFromCandidates(ctx, accounts, hasEnabled, notFoundErr)
	if err != nil {
		return SelectedAccount{}, err
	}
	if err := s.repo.UpsertSessionBinding(ctx, s.cfg.Provider, model, sessionID, selected.AccountID); err != nil {
		return SelectedAccount{}, fmt.Errorf("upsert provider session binding: %w", err)
	}
	return selected, nil
}

func (s *Service) SelectAccountForModelInRoutingPool(ctx context.Context, routingPoolID int64, model string, excludedAccountIDs ...int64) (SelectedAccount, error) {
	return s.SelectAccountForModelInRoutingPoolAndEndpoint(ctx, routingPoolID, model, "", excludedAccountIDs...)
}

func (s *Service) SelectAccountForModelInRoutingPoolAndEndpoint(ctx context.Context, routingPoolID int64, model, endpoint string, excludedAccountIDs ...int64) (SelectedAccount, error) {
	endpoint, err := NormalizeEndpoint(endpoint)
	if err != nil {
		return SelectedAccount{}, err
	}
	if routingPoolID <= 0 {
		return s.SelectAccountForModelAndEndpoint(ctx, model, endpoint, excludedAccountIDs...)
	}
	if !s.Configured() {
		return SelectedAccount{}, ErrNotConfigured
	}
	accounts, hasEnabled, notFoundErr, err := s.selectionCandidatesForRoutingPoolForEndpoint(ctx, routingPoolID, model, endpoint, excludedAccountIDs)
	if err != nil {
		return SelectedAccount{}, err
	}
	return s.selectFromCandidates(ctx, accounts, hasEnabled, notFoundErr)
}

func (s *Service) SelectAccountForModelInRoutingPoolChain(ctx context.Context, primaryPoolID int64, model string, excludedAccountIDs ...int64) (SelectedAccount, error) {
	return s.SelectAccountForModelInRoutingPoolChainAndEndpoint(ctx, primaryPoolID, model, "", excludedAccountIDs...)
}

func (s *Service) SelectAccountForModelInRoutingPoolChainAndEndpoint(ctx context.Context, primaryPoolID int64, model, endpoint string, excludedAccountIDs ...int64) (SelectedAccount, error) {
	endpoint, err := NormalizeEndpoint(endpoint)
	if err != nil {
		return SelectedAccount{}, err
	}
	if primaryPoolID <= 0 {
		return s.SelectAccountForModelAndEndpoint(ctx, model, endpoint, excludedAccountIDs...)
	}
	if !s.Configured() {
		return SelectedAccount{}, ErrNotConfigured
	}
	return s.selectAccountForRoutingPoolChain(ctx, primaryPoolID, model, "", endpoint, excludedAccountIDs...)
}

func (s *Service) SelectAccountForModelAndSessionInRoutingPool(ctx context.Context, routingPoolID int64, model, sessionID string, excludedAccountIDs ...int64) (SelectedAccount, error) {
	return s.SelectAccountForModelAndSessionInRoutingPoolAndEndpoint(ctx, routingPoolID, model, "", sessionID, excludedAccountIDs...)
}

func (s *Service) SelectAccountForModelAndSessionInRoutingPoolAndEndpoint(ctx context.Context, routingPoolID int64, model, endpoint, sessionID string, excludedAccountIDs ...int64) (SelectedAccount, error) {
	endpoint, err := NormalizeEndpoint(endpoint)
	if err != nil {
		return SelectedAccount{}, err
	}
	if routingPoolID <= 0 {
		return s.SelectAccountForModelAndSessionAndEndpoint(ctx, model, endpoint, sessionID, excludedAccountIDs...)
	}
	model = strings.TrimSpace(model)
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return s.SelectAccountForModelInRoutingPoolAndEndpoint(ctx, routingPoolID, model, endpoint, excludedAccountIDs...)
	}
	if !s.Configured() {
		return SelectedAccount{}, ErrNotConfigured
	}
	accounts, hasEnabled, notFoundErr, err := s.selectionCandidatesForRoutingPoolForEndpoint(ctx, routingPoolID, model, endpoint, excludedAccountIDs)
	if err != nil {
		return SelectedAccount{}, err
	}
	accounts, _, err = s.stickySessionCandidatesInRoutingPool(ctx, routingPoolID, accounts, model, sessionID)
	if err != nil {
		return SelectedAccount{}, err
	}
	selected, err := s.selectFromCandidates(ctx, accounts, hasEnabled, notFoundErr)
	if err != nil {
		return SelectedAccount{}, err
	}
	if err := s.repo.UpsertSessionBindingInRoutingPool(ctx, s.cfg.Provider, routingPoolID, model, sessionID, selected.AccountID); err != nil {
		return SelectedAccount{}, fmt.Errorf("upsert provider session binding: %w", err)
	}
	return selected, nil
}

func (s *Service) SelectAccountForModelAndSessionInRoutingPoolChain(ctx context.Context, primaryPoolID int64, model, sessionID string, excludedAccountIDs ...int64) (SelectedAccount, error) {
	return s.SelectAccountForModelAndSessionInRoutingPoolChainAndEndpoint(ctx, primaryPoolID, model, "", sessionID, excludedAccountIDs...)
}

func (s *Service) SelectAccountForModelAndSessionInRoutingPoolChainAndEndpoint(ctx context.Context, primaryPoolID int64, model, endpoint, sessionID string, excludedAccountIDs ...int64) (SelectedAccount, error) {
	endpoint, err := NormalizeEndpoint(endpoint)
	if err != nil {
		return SelectedAccount{}, err
	}
	if primaryPoolID <= 0 {
		return s.SelectAccountForModelAndSessionAndEndpoint(ctx, model, endpoint, sessionID, excludedAccountIDs...)
	}
	model = strings.TrimSpace(model)
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return s.SelectAccountForModelInRoutingPoolChainAndEndpoint(ctx, primaryPoolID, model, endpoint, excludedAccountIDs...)
	}
	if !s.Configured() {
		return SelectedAccount{}, ErrNotConfigured
	}
	return s.selectAccountForRoutingPoolChain(ctx, primaryPoolID, model, sessionID, endpoint, excludedAccountIDs...)
}

func (s *Service) SelectAccountByIDInRoutingPoolChain(ctx context.Context, primaryPoolID, accountID int64, model string) (SelectedAccount, error) {
	return s.SelectAccountByIDInRoutingPoolChainAndEndpoint(ctx, primaryPoolID, accountID, model, "")
}

func (s *Service) SelectAccountByIDInRoutingPoolChainAndEndpoint(ctx context.Context, primaryPoolID, accountID int64, model, endpoint string) (SelectedAccount, error) {
	endpoint, err := NormalizeEndpoint(endpoint)
	if err != nil {
		return SelectedAccount{}, err
	}
	if primaryPoolID <= 0 || accountID <= 0 {
		return SelectedAccount{}, ErrInvalidInput
	}
	candidates, chainLabel, err := s.responseAffinityCandidatesForEndpoint(ctx, primaryPoolID, model, endpoint)
	if err != nil {
		return SelectedAccount{RoutingPoolFallbackChain: chainLabel, RoutingPoolError: routingPoolDiagnosticError(err)}, err
	}
	for _, candidate := range candidates {
		if candidate.account.ID != accountID {
			continue
		}
		return s.selectedResponseAffinityAccount(ctx, candidate, chainLabel)
	}
	return SelectedAccount{RoutingPoolFallbackChain: chainLabel, RoutingPoolError: RoutingPoolErrorExhausted}, ErrAccountsUnavailable
}

func (s *Service) SelectSingleAccountInRoutingPoolChain(ctx context.Context, primaryPoolID int64, model string) (SelectedAccount, bool, error) {
	return s.SelectSingleAccountInRoutingPoolChainAndEndpoint(ctx, primaryPoolID, model, "")
}

func (s *Service) SelectSingleAccountInRoutingPoolChainAndEndpoint(ctx context.Context, primaryPoolID int64, model, endpoint string) (SelectedAccount, bool, error) {
	endpoint, err := NormalizeEndpoint(endpoint)
	if err != nil {
		return SelectedAccount{}, false, err
	}
	if primaryPoolID <= 0 {
		return SelectedAccount{}, false, ErrInvalidInput
	}
	if endpoint == "" {
		return s.selectSingleAccountInRoutingPoolChainLegacy(ctx, primaryPoolID, model)
	}
	topology, chainLabel, err := s.responseAffinityTopology(ctx, primaryPoolID)
	if err != nil {
		return SelectedAccount{RoutingPoolFallbackChain: chainLabel, RoutingPoolError: routingPoolDiagnosticError(err)}, false, err
	}
	accounts := make([]Account, 0, len(topology))
	for _, candidate := range topology {
		accounts = append(accounts, candidate.account)
	}
	var modelsByAccount map[int64][]AccountModel
	modelsAvailable := true
	if endpoint != "" {
		modelsByAccount, modelsAvailable = s.accountModelsForCandidates(ctx, model, accounts)
	}
	legal := make([]responseAffinityCandidate, 0, len(topology))
	now := time.Now()
	for _, candidate := range topology {
		if !candidate.pool.Enabled || selectionUnschedulableReasonForEndpoint(candidate.account, model, endpoint, nil, now, modelsByAccount[candidate.account.ID], modelsAvailable) != "" {
			continue
		}
		legal = append(legal, candidate)
	}
	unique := make(map[int64]struct{}, len(legal))
	for _, candidate := range legal {
		unique[candidate.account.ID] = struct{}{}
	}
	if len(unique) == 0 {
		return SelectedAccount{RoutingPoolFallbackChain: chainLabel, RoutingPoolError: RoutingPoolErrorExhausted}, false, ErrAccountsUnavailable
	}
	if len(unique) > 1 {
		return SelectedAccount{RoutingPoolFallbackChain: chainLabel}, false, nil
	}
	for _, candidate := range legal {
		selected, err := s.selectedResponseAffinityAccount(ctx, candidate, chainLabel)
		return selected, err == nil, err
	}
	return SelectedAccount{RoutingPoolFallbackChain: chainLabel, RoutingPoolError: RoutingPoolErrorExhausted}, false, ErrAccountsUnavailable
}

func (s *Service) selectSingleAccountInRoutingPoolChainLegacy(ctx context.Context, primaryPoolID int64, model string) (SelectedAccount, bool, error) {
	topology, chainLabel, err := s.responseAffinityTopology(ctx, primaryPoolID)
	if err != nil {
		return SelectedAccount{RoutingPoolFallbackChain: chainLabel, RoutingPoolError: routingPoolDiagnosticError(err)}, false, err
	}
	unique := make(map[int64]struct{}, len(topology))
	for _, candidate := range topology {
		unique[candidate.account.ID] = struct{}{}
		if len(unique) > 1 {
			return SelectedAccount{RoutingPoolFallbackChain: chainLabel}, false, nil
		}
	}
	if len(unique) == 0 {
		return SelectedAccount{RoutingPoolFallbackChain: chainLabel, RoutingPoolError: RoutingPoolErrorExhausted}, false, ErrAccountsUnavailable
	}
	now := time.Now()
	modelsByAccount, modelsAvailable := s.accountModelsForCandidates(ctx, model, []Account{topology[0].account})
	for _, candidate := range topology {
		if !candidate.pool.Enabled || selectionUnschedulableReasonForEndpoint(candidate.account, model, "", nil, now, modelsByAccount[candidate.account.ID], modelsAvailable) != "" {
			continue
		}
		selected, err := s.selectedResponseAffinityAccount(ctx, candidate, chainLabel)
		return selected, err == nil, err
	}
	return SelectedAccount{RoutingPoolFallbackChain: chainLabel, RoutingPoolError: RoutingPoolErrorExhausted}, false, ErrAccountsUnavailable
}

type responseAffinityCandidate struct {
	account Account
	pool    RoutingPool
	depth   int
}

func (s *Service) responseAffinityTopology(ctx context.Context, primaryPoolID int64) ([]responseAffinityCandidate, string, error) {
	if !s.Configured() {
		return nil, "", ErrNotConfigured
	}
	pools, chainLabel, err := s.routingPoolChain(ctx, primaryPoolID)
	if err != nil {
		return nil, chainLabel, err
	}
	topology := make([]responseAffinityCandidate, 0)
	for depth, pool := range pools {
		if depth == 0 && !pool.Enabled {
			return nil, chainLabel, ErrAccountsDisabled
		}
		accounts, err := s.repo.ListRoutingPoolAccounts(ctx, s.cfg.Provider, pool.ID)
		if err != nil {
			return nil, chainLabel, err
		}
		for _, account := range accounts {
			topology = append(topology, responseAffinityCandidate{account: account, pool: pool, depth: depth})
		}
	}
	return topology, chainLabel, nil
}

func (s *Service) responseAffinityCandidatesForEndpoint(ctx context.Context, primaryPoolID int64, model, endpoint string) ([]responseAffinityCandidate, string, error) {
	if !s.Configured() {
		return nil, "", ErrNotConfigured
	}
	pools, chainLabel, err := s.routingPoolChain(ctx, primaryPoolID)
	if err != nil {
		return nil, chainLabel, err
	}
	candidates := make([]responseAffinityCandidate, 0)
	for depth, pool := range pools {
		if !pool.Enabled {
			if depth == 0 {
				return nil, chainLabel, ErrAccountsDisabled
			}
			continue
		}
		accounts, _, _, err := s.selectionCandidatesForRoutingPoolForEndpoint(ctx, pool.ID, model, endpoint, nil)
		if err != nil {
			return nil, chainLabel, err
		}
		for _, account := range accounts {
			candidates = append(candidates, responseAffinityCandidate{account: account, pool: pool, depth: depth})
		}
	}
	return candidates, chainLabel, nil
}

func (s *Service) selectedResponseAffinityAccount(ctx context.Context, candidate responseAffinityCandidate, chainLabel string) (SelectedAccount, error) {
	selected, err := s.selectedAccount(ctx, candidate.account)
	if err != nil {
		return SelectedAccount{}, err
	}
	selected.RoutingPoolID = candidate.pool.ID
	selected.RoutingPoolName = candidate.pool.Name
	selected.RoutingPoolFallbackDepth = candidate.depth
	selected.RoutingPoolFallbackChain = chainLabel
	return selected, nil
}

func (s *Service) PreviewAccountSelection(ctx context.Context, model, sessionID string, excludedAccountIDs ...int64) (SelectionPreview, error) {
	return s.PreviewAccountSelectionForEndpoint(ctx, model, "", sessionID, excludedAccountIDs...)
}

func (s *Service) PreviewAccountSelectionForEndpoint(ctx context.Context, model, endpoint, sessionID string, excludedAccountIDs ...int64) (SelectionPreview, error) {
	endpoint, err := NormalizeEndpoint(endpoint)
	if err != nil {
		return SelectionPreview{}, err
	}
	if !s.Configured() {
		return SelectionPreview{}, ErrNotConfigured
	}
	model = strings.TrimSpace(model)
	sessionID = strings.TrimSpace(sessionID)
	now := time.Now()
	accounts, _, notFoundErr, err := s.selectionCandidatesForEndpoint(ctx, model, endpoint, excludedAccountIDs)
	if err != nil {
		return SelectionPreview{}, err
	}
	stickyBoundAccountID := int64(0)
	if sessionID != "" {
		accounts, stickyBoundAccountID, err = s.stickySessionCandidates(ctx, accounts, model, sessionID)
		if err != nil {
			return SelectionPreview{}, err
		}
	}
	if len(accounts) == 0 {
		blocked := s.unschedulableSelectionCandidatesForEndpoint(ctx, model, endpoint, nil, excludedAccountIDs, now)
		if len(blocked) > 0 {
			return SelectionPreview{
				Model:      model,
				Endpoint:   endpoint,
				SessionID:  sessionID,
				Candidates: blocked,
			}, nil
		}
		return SelectionPreview{}, notFoundErr
	}
	var modelsByAccount map[int64][]AccountModel
	modelsAvailable := true
	if endpoint != "" {
		modelsByAccount, modelsAvailable = s.accountModelsForCandidates(ctx, model, accounts)
	}

	preview := SelectionPreview{
		Model:                model,
		Endpoint:             endpoint,
		SessionID:            sessionID,
		SelectedAccountID:    accounts[0].ID,
		StickyBoundAccountID: stickyBoundAccountID,
		Candidates:           make([]SelectionCandidate, 0, len(accounts)),
	}
	for index, account := range accounts {
		var models []AccountModel
		if modelsAvailable {
			models = modelsByAccount[account.ID]
		}
		candidate := selectionCandidateForEndpoint(account, model, endpoint, models, index+1, index == 0, true, "")
		candidate.StickyBound = stickyBoundAccountID > 0 && account.ID == stickyBoundAccountID
		candidate.ScheduleReason = scheduleReason(account, candidate.Selected, candidate.StickyBound, sessionID != "")
		preview.Candidates = append(preview.Candidates, candidate)
	}
	preview.Candidates = append(preview.Candidates, s.unschedulableSelectionCandidatesForEndpoint(ctx, model, endpoint, accounts, excludedAccountIDs, now)...)
	return preview, nil
}

func (s *Service) PreviewAccountSelectionInRoutingPool(ctx context.Context, routingPoolID int64, model, sessionID string, excludedAccountIDs ...int64) (SelectionPreview, error) {
	return s.PreviewAccountSelectionInRoutingPoolForEndpoint(ctx, routingPoolID, model, "", sessionID, excludedAccountIDs...)
}

func (s *Service) PreviewAccountSelectionInRoutingPoolForEndpoint(ctx context.Context, routingPoolID int64, model, endpoint, sessionID string, excludedAccountIDs ...int64) (SelectionPreview, error) {
	endpoint, err := NormalizeEndpoint(endpoint)
	if err != nil {
		return SelectionPreview{}, err
	}
	if routingPoolID <= 0 {
		return s.PreviewAccountSelectionForEndpoint(ctx, model, endpoint, sessionID, excludedAccountIDs...)
	}
	if !s.Configured() {
		return SelectionPreview{}, ErrNotConfigured
	}
	pools, chainLabel, err := s.routingPoolChain(ctx, routingPoolID)
	if err != nil {
		return SelectionPreview{RoutingPoolFallbackChain: chainLabel, RoutingPoolError: routingPoolDiagnosticError(err)}, err
	}
	model = strings.TrimSpace(model)
	sessionID = strings.TrimSpace(sessionID)
	now := time.Now()
	var finalErr error = ErrAccountsUnavailable
	blockedChainCandidates := []SelectionCandidate{}
	hasEnabled := false
	for depth, pool := range pools {
		if !pool.Enabled {
			if depth == 0 {
				return SelectionPreview{
					Model:                    model,
					Endpoint:                 endpoint,
					SessionID:                sessionID,
					RoutingPoolID:            pool.ID,
					RoutingPoolName:          pool.Name,
					RoutingPoolFallbackDepth: depth,
					RoutingPoolFallbackChain: chainLabel,
					RoutingPoolError:         RoutingPoolErrorDisabled,
				}, ErrAccountsDisabled
			}
			continue
		}
		hasEnabled = true
		accounts, poolHasEnabled, notFoundErr, err := s.selectionCandidatesForRoutingPoolForEndpoint(ctx, pool.ID, model, endpoint, excludedAccountIDs)
		if err != nil {
			return SelectionPreview{
				Model:                    model,
				Endpoint:                 endpoint,
				SessionID:                sessionID,
				RoutingPoolID:            pool.ID,
				RoutingPoolName:          pool.Name,
				RoutingPoolFallbackDepth: depth,
				RoutingPoolFallbackChain: chainLabel,
				RoutingPoolError:         err.Error(),
			}, err
		}
		if poolHasEnabled {
			hasEnabled = true
		}
		finalErr = moreSpecificSelectionError(finalErr, notFoundErr)
		blocked := s.unschedulableSelectionCandidatesInRoutingPoolForEndpoint(ctx, pool.ID, model, endpoint, accounts, excludedAccountIDs, now)
		if len(accounts) == 0 {
			blockedChainCandidates = append(blockedChainCandidates, blocked...)
			if errors.Is(notFoundErr, ErrRoutingPoolEmpty) && depth == 0 {
				return SelectionPreview{
					Model:                    model,
					Endpoint:                 endpoint,
					SessionID:                sessionID,
					RoutingPoolID:            pool.ID,
					RoutingPoolName:          pool.Name,
					RoutingPoolFallbackDepth: depth,
					RoutingPoolFallbackChain: chainLabel,
					RoutingPoolError:         RoutingPoolErrorEmpty,
					Candidates:               blockedChainCandidates,
				}, ErrRoutingPoolEmpty
			}
			continue
		}

		stickyBoundAccountID := int64(0)
		if sessionID != "" {
			accounts, stickyBoundAccountID, err = s.stickySessionCandidatesInRoutingPool(ctx, pool.ID, accounts, model, sessionID)
			if err != nil {
				return SelectionPreview{}, err
			}
		}
		preview := SelectionPreview{
			Model:                    model,
			Endpoint:                 endpoint,
			SessionID:                sessionID,
			SelectedAccountID:        accounts[0].ID,
			StickyBoundAccountID:     stickyBoundAccountID,
			RoutingPoolID:            pool.ID,
			RoutingPoolName:          pool.Name,
			RoutingPoolFallbackDepth: depth,
			RoutingPoolFallbackChain: chainLabel,
			Candidates:               make([]SelectionCandidate, 0, len(accounts)+len(blocked)+len(blockedChainCandidates)),
		}
		var modelsByAccount map[int64][]AccountModel
		modelsAvailable := true
		if endpoint != "" {
			modelsByAccount, modelsAvailable = s.accountModelsForCandidates(ctx, model, accounts)
		}
		for index, account := range accounts {
			var models []AccountModel
			if modelsAvailable {
				models = modelsByAccount[account.ID]
			}
			candidate := selectionCandidateForEndpoint(account, model, endpoint, models, index+1, index == 0, true, "")
			candidate.StickyBound = stickyBoundAccountID > 0 && account.ID == stickyBoundAccountID
			candidate.ScheduleReason = scheduleReason(account, candidate.Selected, candidate.StickyBound, sessionID != "")
			preview.Candidates = append(preview.Candidates, candidate)
		}
		preview.Candidates = append(preview.Candidates, blockedChainCandidates...)
		preview.Candidates = append(preview.Candidates, blocked...)
		return preview, nil
	}
	if !hasEnabled {
		finalErr = ErrAccountsDisabled
	}
	return SelectionPreview{
		Model:                    model,
		Endpoint:                 endpoint,
		SessionID:                sessionID,
		RoutingPoolFallbackChain: chainLabel,
		RoutingPoolError:         RoutingPoolErrorExhausted,
		Candidates:               blockedChainCandidates,
	}, finalErr
}

func (s *Service) unschedulableSelectionCandidatesForEndpoint(ctx context.Context, model, endpoint string, selected []Account, excludedAccountIDs []int64, now time.Time) []SelectionCandidate {
	accounts, err := s.repo.ListAccounts(ctx, s.cfg.Provider)
	if err != nil {
		return nil
	}
	modelsByAccount, modelsAvailable := s.accountModelsForCandidates(ctx, model, accounts)
	selectedIDs := make(map[int64]struct{}, len(selected))
	for _, account := range selected {
		selectedIDs[account.ID] = struct{}{}
	}
	excluded := make(map[int64]struct{}, len(excludedAccountIDs))
	for _, id := range excludedAccountIDs {
		if id > 0 {
			excluded[id] = struct{}{}
		}
	}

	candidates := make([]SelectionCandidate, 0, len(accounts))
	for _, account := range accounts {
		if _, ok := selectedIDs[account.ID]; ok {
			continue
		}
		reason := selectionUnschedulableReasonForEndpoint(account, model, endpoint, excluded, now, modelsByAccount[account.ID], modelsAvailable)
		if reason == "" {
			continue
		}
		candidates = append(candidates, selectionCandidateForEndpoint(account, model, endpoint, modelsByAccount[account.ID], 0, false, false, reason))
	}
	return candidates
}

func (s *Service) unschedulableSelectionCandidatesInRoutingPoolForEndpoint(ctx context.Context, routingPoolID int64, model, endpoint string, selected []Account, excludedAccountIDs []int64, now time.Time) []SelectionCandidate {
	accounts, err := s.repo.ListRoutingPoolAccounts(ctx, s.cfg.Provider, routingPoolID)
	if err != nil {
		return nil
	}
	modelsByAccount, modelsAvailable := s.accountModelsForCandidates(ctx, model, accounts)
	selectedIDs := make(map[int64]struct{}, len(selected))
	for _, account := range selected {
		selectedIDs[account.ID] = struct{}{}
	}
	excluded := make(map[int64]struct{}, len(excludedAccountIDs))
	for _, id := range excludedAccountIDs {
		if id > 0 {
			excluded[id] = struct{}{}
		}
	}

	candidates := make([]SelectionCandidate, 0, len(accounts))
	for _, account := range accounts {
		if _, ok := selectedIDs[account.ID]; ok {
			continue
		}
		reason := selectionUnschedulableReasonForEndpoint(account, model, endpoint, excluded, now, modelsByAccount[account.ID], modelsAvailable)
		if reason == "" {
			continue
		}
		candidates = append(candidates, selectionCandidateForEndpoint(account, model, endpoint, modelsByAccount[account.ID], 0, false, false, reason))
	}
	return candidates
}

func (s *Service) accountModelsForCandidates(ctx context.Context, model string, accounts []Account) (map[int64][]AccountModel, bool) {
	if strings.TrimSpace(model) == "" {
		return nil, true
	}
	accountIDs := make([]int64, 0, len(accounts))
	for _, account := range accounts {
		accountIDs = append(accountIDs, account.ID)
	}
	modelsByAccount, err := s.repo.ListAccountModelsForAccounts(ctx, s.cfg.Provider, accountIDs)
	return modelsByAccount, err == nil
}

func selectionUnschedulableReasonForEndpoint(account Account, model, endpoint string, excluded map[int64]struct{}, now time.Time, models []AccountModel, modelsAvailable bool) string {
	if _, ok := excluded[account.ID]; ok {
		return "account excluded"
	}
	if !account.Enabled {
		return "account disabled"
	}
	if reason := accountUnschedulableReason(account, now); reason != "" {
		return reason
	}
	if strings.TrimSpace(model) == "" {
		return ""
	}
	if !modelsAvailable {
		return "model not configured"
	}
	hasModel := false
	for _, item := range models {
		if item.Model != model {
			continue
		}
		hasModel = true
		if endpointCapabilityUnsupported(item.Metadata, endpoint) {
			return "endpoint unsupported"
		}
		if item.Enabled {
			return ""
		}
	}
	if hasModel {
		return "model disabled"
	}
	return "model not configured"
}

func selectionCandidateForEndpoint(account Account, model, endpoint string, models []AccountModel, scheduleRank int, selected bool, schedulable bool, reason string) SelectionCandidate {
	account = normalizeAccountCredentialFields(account)
	endpointCapability := ""
	if strings.TrimSpace(endpoint) != "" {
		endpointCapability = endpointCapabilityForModel(models, model, endpoint)
		if reason == "endpoint unsupported" {
			endpointCapability = EndpointCapabilityUnsupported
		}
	}
	return SelectionCandidate{
		ID:                  account.ID,
		DisplayName:         accountDisplayName(account),
		AccountType:         account.AccountType,
		Priority:            selectionPriority(account),
		LoadFactor:          normalizedLoadFactor(account.LoadFactor),
		Status:              valueOrDefault(account.Status, AccountStatusActive),
		LastUsedAt:          account.LastUsedAt,
		LastTestAt:          account.LastTestAt,
		LastTestStatus:      account.LastTestStatus,
		LastTestError:       account.LastTestError,
		ScheduleRank:        scheduleRank,
		Selected:            selected,
		Schedulable:         schedulable,
		UnschedulableReason: reason,
		EndpointCapability:  endpointCapability,
	}
}

func endpointCapabilityForModel(models []AccountModel, model, endpoint string) string {
	if strings.TrimSpace(model) == "" || strings.TrimSpace(endpoint) == "" {
		return EndpointCapabilityUnknown
	}
	for _, item := range models {
		if item.Model != model {
			continue
		}
		if capability := EndpointCapability(item.Metadata, endpoint); capability == EndpointCapabilityUnsupported {
			return capability
		} else if capability == EndpointCapabilitySupported {
			return capability
		}
	}
	return EndpointCapabilityUnknown
}

func scheduleReason(account Account, selected, stickyBound, stickySession bool) string {
	tier := scheduleTier(account)
	baseTieBreakers := fmt.Sprintf("base tie-breakers least-recently-used then account ID %d", account.ID)
	if stickyBound {
		return fmt.Sprintf("reused sticky session binding for %s; new sticky FNV hashes stay within the highest exactly equal scheduling tier; %s", tier, baseTieBreakers)
	}
	if stickySession && selected {
		return fmt.Sprintf("selected by sticky FNV hash within the highest exactly equal scheduling tier: %s; %s", tier, baseTieBreakers)
	}
	if stickySession {
		return fmt.Sprintf("ordered after sticky FNV hash, which only changes order within the highest exactly equal scheduling tier: %s; %s", tier, baseTieBreakers)
	}
	if selected {
		return fmt.Sprintf("selected by %s; tie-breakers least-recently-used then account ID %d", tier, account.ID)
	}
	return fmt.Sprintf("ordered by %s; tie-breakers least-recently-used then account ID %d", tier, account.ID)
}

func scheduleTier(account Account) string {
	recentErrorTier := "clean"
	if account.LastErrorAt != nil {
		recentErrorTier = "present"
	}
	if account.RoutingPoolPriority != nil {
		return fmt.Sprintf(
			"pool priority %d, global account priority %d, scheduling preference tier %d, recent-error tier %s",
			selectionPriority(account),
			globalAccountPriority(account),
			normalizedLoadFactor(account.LoadFactor),
			recentErrorTier,
		)
	}
	return fmt.Sprintf(
		"account priority %d, scheduling preference tier %d, recent-error tier %s",
		account.Priority,
		normalizedLoadFactor(account.LoadFactor),
		recentErrorTier,
	)
}

func (s *Service) stickySessionCandidates(ctx context.Context, accounts []Account, model, sessionID string) ([]Account, int64, error) {
	if len(accounts) == 0 {
		return accounts, 0, nil
	}
	binding, err := s.repo.FindSessionBinding(ctx, s.cfg.Provider, model, sessionID)
	if err != nil && !errors.Is(err, ErrSessionBindingNotFound) {
		return nil, 0, err
	}
	if err == nil {
		for i, account := range accounts {
			if account.ID != binding.AccountID {
				continue
			}
			ordered := make([]Account, 0, len(accounts))
			ordered = append(ordered, account)
			ordered = append(ordered, accounts[:i]...)
			ordered = append(ordered, accounts[i+1:]...)
			return ordered, binding.AccountID, nil
		}
	}
	return stickySessionHashCandidates(accounts, sessionID), 0, nil
}

func (s *Service) stickySessionCandidatesInRoutingPool(ctx context.Context, routingPoolID int64, accounts []Account, model, sessionID string) ([]Account, int64, error) {
	if len(accounts) == 0 {
		return accounts, 0, nil
	}
	binding, err := s.repo.FindSessionBindingInRoutingPool(ctx, s.cfg.Provider, routingPoolID, model, sessionID)
	if err != nil && !errors.Is(err, ErrSessionBindingNotFound) {
		return nil, 0, err
	}
	if err == nil {
		for i, account := range accounts {
			if account.ID != binding.AccountID {
				continue
			}
			ordered := make([]Account, 0, len(accounts))
			ordered = append(ordered, account)
			ordered = append(ordered, accounts[:i]...)
			ordered = append(ordered, accounts[i+1:]...)
			return ordered, binding.AccountID, nil
		}
	}
	return stickySessionHashCandidates(accounts, sessionID), 0, nil
}

func (s *Service) selectAccountForRoutingPoolChain(ctx context.Context, primaryPoolID int64, model, sessionID, endpoint string, excludedAccountIDs ...int64) (SelectedAccount, error) {
	pools, chainLabel, err := s.routingPoolChain(ctx, primaryPoolID)
	if err != nil {
		return SelectedAccount{RoutingPoolFallbackChain: chainLabel, RoutingPoolError: routingPoolDiagnosticError(err)}, err
	}

	model = strings.TrimSpace(model)
	sessionID = strings.TrimSpace(sessionID)
	var finalErr error = ErrAccountsUnavailable
	hasEnabled := false
	for depth, pool := range pools {
		if !pool.Enabled {
			if depth == 0 {
				return SelectedAccount{
					RoutingPoolID:            pool.ID,
					RoutingPoolName:          pool.Name,
					RoutingPoolFallbackDepth: depth,
					RoutingPoolFallbackChain: chainLabel,
					RoutingPoolError:         RoutingPoolErrorDisabled,
				}, ErrAccountsDisabled
			}
			continue
		}
		hasEnabled = true

		accounts, poolHasEnabled, notFoundErr, err := s.selectionCandidatesForRoutingPoolForEndpoint(ctx, pool.ID, model, endpoint, excludedAccountIDs)
		if err != nil {
			return SelectedAccount{
				RoutingPoolID:            pool.ID,
				RoutingPoolName:          pool.Name,
				RoutingPoolFallbackDepth: depth,
				RoutingPoolFallbackChain: chainLabel,
				RoutingPoolError:         err.Error(),
			}, err
		}
		if poolHasEnabled {
			hasEnabled = true
		}
		finalErr = moreSpecificSelectionError(finalErr, notFoundErr)
		if len(accounts) == 0 {
			if errors.Is(notFoundErr, ErrRoutingPoolEmpty) && depth == 0 {
				return SelectedAccount{
					RoutingPoolID:            pool.ID,
					RoutingPoolName:          pool.Name,
					RoutingPoolFallbackDepth: depth,
					RoutingPoolFallbackChain: chainLabel,
					RoutingPoolError:         RoutingPoolErrorEmpty,
				}, ErrRoutingPoolEmpty
			}
			continue
		}

		if sessionID != "" {
			accounts, _, err = s.stickySessionCandidatesInRoutingPool(ctx, pool.ID, accounts, model, sessionID)
			if err != nil {
				return SelectedAccount{}, err
			}
		}
		selected, err := s.selectFromCandidates(ctx, accounts, poolHasEnabled, notFoundErr)
		if err != nil {
			finalErr = moreSpecificSelectionError(finalErr, err)
			continue
		}
		selected.RoutingPoolID = pool.ID
		selected.RoutingPoolName = pool.Name
		selected.RoutingPoolFallbackDepth = depth
		selected.RoutingPoolFallbackChain = chainLabel
		if sessionID != "" {
			if err := s.repo.UpsertSessionBindingInRoutingPool(ctx, s.cfg.Provider, pool.ID, model, sessionID, selected.AccountID); err != nil {
				return SelectedAccount{}, fmt.Errorf("upsert provider session binding: %w", err)
			}
		}
		return selected, nil
	}

	if !hasEnabled {
		finalErr = ErrAccountsDisabled
	}
	return SelectedAccount{RoutingPoolFallbackChain: chainLabel, RoutingPoolError: RoutingPoolErrorExhausted}, finalErr
}

func (s *Service) routingPoolChain(ctx context.Context, primaryPoolID int64) ([]RoutingPool, string, error) {
	visited := map[int64]struct{}{}
	pools := []RoutingPool{}
	for id := primaryPoolID; id > 0; {
		if _, ok := visited[id]; ok {
			return nil, "", ErrRoutingPoolCycle
		}
		visited[id] = struct{}{}
		pool, err := s.repo.FindRoutingPool(ctx, id)
		if err != nil {
			if errors.Is(err, ErrRoutingPoolNotFound) && len(pools) > 0 {
				return pools, routingPoolChainLabel(pools), ErrRoutingPoolExhausted
			}
			return nil, "", err
		}
		pools = append(pools, pool)
		if pool.FallbackPoolID == nil || *pool.FallbackPoolID <= 0 {
			break
		}
		id = *pool.FallbackPoolID
	}
	return pools, routingPoolChainLabel(pools), nil
}

func routingPoolChainLabel(pools []RoutingPool) string {
	labels := make([]string, 0, len(pools))
	for _, pool := range pools {
		name := strings.TrimSpace(pool.Name)
		if name == "" {
			name = "pool " + strconv.FormatInt(pool.ID, 10)
		}
		labels = append(labels, name)
	}
	return strings.Join(labels, " -> ")
}

func moreSpecificSelectionError(current, next error) error {
	if next == nil {
		return current
	}
	if current == nil || errors.Is(current, ErrAccountsUnavailable) {
		return next
	}
	if errors.Is(next, ErrModelUnavailable) {
		return next
	}
	if errors.Is(next, ErrEndpointUnavailable) {
		return next
	}
	return current
}

func routingPoolDiagnosticError(err error) string {
	switch {
	case errors.Is(err, ErrAccountsDisabled):
		return RoutingPoolErrorDisabled
	case errors.Is(err, ErrRoutingPoolCycle):
		return RoutingPoolErrorCycle
	case errors.Is(err, ErrRoutingPoolNotFound):
		return RoutingPoolErrorUnavailable
	case errors.Is(err, ErrRoutingPoolExhausted):
		return RoutingPoolErrorExhausted
	default:
		return strings.TrimSpace(err.Error())
	}
}

func stickySessionHashCandidates(accounts []Account, sessionID string) []Account {
	if len(accounts) <= 1 {
		return accounts
	}

	poolPriority := selectionPriority(accounts[0])
	globalPriority := globalAccountPriority(accounts[0])
	loadFactor := normalizedLoadFactor(accounts[0].LoadFactor)
	hasError := accounts[0].LastErrorAt != nil
	groupEnd := 0
	for groupEnd < len(accounts) &&
		selectionPriority(accounts[groupEnd]) == poolPriority &&
		globalAccountPriority(accounts[groupEnd]) == globalPriority &&
		normalizedLoadFactor(accounts[groupEnd].LoadFactor) == loadFactor &&
		(accounts[groupEnd].LastErrorAt != nil) == hasError {
		groupEnd++
	}
	if groupEnd <= 1 {
		return accounts
	}

	priorityGroup := append([]Account(nil), accounts[:groupEnd]...)
	sort.SliceStable(priorityGroup, func(i, j int) bool {
		return priorityGroup[i].ID < priorityGroup[j].ID
	})
	start := stickyAccountIndex(sessionID, len(priorityGroup))
	rotated := append([]Account(nil), priorityGroup[start:]...)
	rotated = append(rotated, priorityGroup[:start]...)
	rotated = append(rotated, accounts[groupEnd:]...)
	return rotated
}

func selectionPriority(account Account) int {
	if account.RoutingPoolPriority != nil {
		return *account.RoutingPoolPriority
	}
	return account.Priority
}

func globalAccountPriority(account Account) int {
	if account.RoutingPoolPriority != nil {
		return account.GlobalPriority
	}
	return account.Priority
}

func stickyAccountIndex(sessionID string, count int) int {
	if count <= 1 {
		return 0
	}
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(sessionID))
	return int(hash.Sum64() % uint64(count))
}

func (s *Service) selectFromCandidates(ctx context.Context, accounts []Account, hasEnabled bool, notFoundErr error) (SelectedAccount, error) {
	for _, account := range accounts {
		selected, err := s.selectedAccount(ctx, account)
		if err != nil {
			if markErr := s.recordSelectionFailure(ctx, account.ID, err); markErr != nil {
				return SelectedAccount{}, fmt.Errorf("mark provider account error: %w", markErr)
			}
			continue
		}
		return selected, nil
	}
	if !hasEnabled {
		return SelectedAccount{}, ErrAccountsDisabled
	}
	return SelectedAccount{}, notFoundErr
}

func (s *Service) recordSelectionFailure(ctx context.Context, accountID int64, err error) error {
	now := time.Now()
	reason := strings.TrimSpace(err.Error())
	if reason == "" {
		reason = "provider account selection failed"
	}
	if errors.Is(err, ErrInvalidInput) {
		until := now.Add(defaultCircuitOpen)
		ctx = withProviderEventIntent(ctx, runtimeAccountIntent(systemevent.ActionProviderAccountCircuitOpened, accountID, AccountStatusCircuitOpen))
		return s.repo.RecordAccountStatus(ctx, s.cfg.Provider, accountID, AccountStatusCircuitOpen, reason, now, nil, &until)
	}
	return s.repo.MarkAccountError(ctx, s.cfg.Provider, accountID, reason, now)
}

func (s *Service) RecordAccountUsed(ctx context.Context, accountID int64) error {
	if accountID <= 0 {
		return ErrInvalidInput
	}
	return s.repo.MarkAccountUsed(ctx, s.cfg.Provider, accountID, time.Now())
}

func (s *Service) RecordAccountRecovered(ctx context.Context, accountID int64) error {
	if accountID <= 0 {
		return ErrInvalidInput
	}
	ctx = withProviderEventIntent(ctx, runtimeAccountIntent(systemevent.ActionProviderAccountRecovered, accountID, AccountStatusActive))
	_, err := s.repo.UpdateAccount(ctx, s.cfg.Provider, accountID, AccountUpdate{ClearStatus: true})
	return err
}
