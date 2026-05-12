package model

type AllAPIHubImportResult struct {
	CreatedSites          int      `json:"created_sites"`
	ReusedSites           int      `json:"reused_sites"`
	CreatedAccounts       int      `json:"created_accounts"`
	UpdatedAccounts       int      `json:"updated_accounts"`
	SkippedAccounts       int      `json:"skipped_accounts"`
	ScheduledSyncAccounts int      `json:"scheduled_sync_accounts"`
	Warnings              []string `json:"warnings,omitempty"`
}

type MetAPIImportResult struct {
	CreatedSites    int      `json:"created_sites"`
	ReusedSites     int      `json:"reused_sites"`
	CreatedAccounts int      `json:"created_accounts"`
	UpdatedAccounts int      `json:"updated_accounts"`
	SkippedAccounts int      `json:"skipped_accounts"`
	ImportedTokens  int      `json:"imported_tokens"`
	ImportedGroups  int      `json:"imported_groups"`
	ImportedModels  int      `json:"imported_models"`
	DisabledModels  int      `json:"disabled_models"`
	Warnings        []string `json:"warnings,omitempty"`
}
