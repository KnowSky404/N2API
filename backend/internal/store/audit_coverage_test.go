package store

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestAuditedStoreMutationsConsumeEventIntent(t *testing.T) {
	files := map[string]map[string]string{
		"admin.go": {
			"CreateAdmin": "insertIntentSystemEvent", "UpdateAdminUsername": "insertIntentSystemEvent",
			"UpdateAdminPassword": "insertIntentSystemEvent", "CreateSession": "insertIntentSystemEvent",
			"RevokeSession": "insertIntentSystemEvent", "RevokeAdminSession": "insertIntentSystemEvent",
			"RevokeOtherAdminSessions": "insertIntentSystemEvent", "CreateAPIKey": "insertIntentSystemEvent",
			"RevokeAPIKey": "insertIntentSystemEvent", "DeleteRevokedAPIKey": "insertIntentSystemEvent",
			"PurgeRevokedAPIKeys": "insertIntentSystemEvent", "UpdateAPIKeyName": "insertIntentSystemEvent",
			"SetAPIKeyDisabled": "insertIntentSystemEvent", "UpdateAPIKeyModelPolicy": "insertIntentSystemEvent",
			"UpdateAPIKeyLimits": "insertIntentSystemEvent", "UpdateAPIKeyBudgets": "insertIntentSystemEvent",
			"CreateRoutingPool": "insertIntentSystemEvent", "UpdateRoutingPool": "insertIntentSystemEvent",
			"DeleteRoutingPool": "insertIntentSystemEvent", "ReplaceRoutingPoolAccounts": "insertIntentSystemEvent",
			"UpdateAPIKeyRoutingPool": "insertIntentSystemEvent",
			"SaveUsagePricing":        "insertIntentSystemEvent", "SaveModelSettings": "insertIntentSystemEvent",
			"SaveGatewaySettings": "insertIntentSystemEvent",
		},
		"fingerprint.go": {
			"CreateFingerprintProfile": "insertIntentSystemEvent", "UpdateFingerprintProfile": "insertIntentSystemEvent",
			"DeleteFingerprintProfile": "insertIntentSystemEvent",
		},
		"error_passthrough.go": {
			"CreateErrorPassthroughRule": "insertIntentSystemEvent", "UpdateErrorPassthroughRule": "insertIntentSystemEvent",
			"DeleteErrorPassthroughRule": "insertIntentSystemEvent",
		},
		"alerting.go": {
			"CreateAction": "insertIntentSystemEvent", "UpdateAction": "insertIntentSystemEvent",
			"DeleteAction": "insertIntentSystemEvent", "FinalizeActionTest": "insertIntentSystemEvent",
			"CreateRule": "insertIntentSystemEvent", "UpdateRule": "insertIntentSystemEvent",
			"DeleteRule": "insertIntentSystemEvent",
		},
		"provider.go": {
			"SaveAccount": "insertProviderIntent", "UpdateOAuthCredential": "insertProviderIntent",
			"UpdateAccount": "insertProviderIntent", "DeleteAccount": "insertProviderIntent",
			"DeleteAccounts": "insertProviderIntent", "MarkAccountUsed": "insertProviderIntent",
			"RecordRefreshFailure": "insertProviderIntent", "RecordOAuthRefreshFailureEvent": "insertProviderIntent",
			"RecordAccountStatus": "insertProviderIntent", "RecordAccountTestResult": "insertProviderIntent",
			"RecordAccountModelTestResult": "insertProviderIntent", "SyncAccountModels": "insertProviderIntent",
			"ReplaceAccountModels": "insertProviderIntent", "CreateState": "insertProviderIntent",
		},
	}

	for filename, methods := range files {
		parsed, err := parser.ParseFile(token.NewFileSet(), filename, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", filename, err)
		}
		found := make(map[string]bool, len(methods))
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv == nil || function.Body == nil {
				continue
			}
			helper, audited := methods[function.Name.Name]
			if !audited {
				continue
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				identifier, ok := call.Fun.(*ast.Ident)
				if ok && identifier.Name == helper {
					found[function.Name.Name] = true
				}
				return true
			})
		}
		for method := range methods {
			if !found[method] {
				t.Errorf("%s %s does not consume its system event intent", filename, method)
			}
		}
	}
}
