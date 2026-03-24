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
