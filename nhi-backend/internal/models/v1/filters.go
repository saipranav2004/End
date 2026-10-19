package v1

type FilterResponse struct {
	Accounts          []AccountFilter          `json:"accounts"`
	Regions           []RegionFilter           `json:"regions"`
	IdentityTypes     []TypeCountFilter        `json:"identity_types"`
	FunctionalClasses []ClassCountFilter       `json:"functional_classes"`
	CredentialTypes   []TypeCountFilter        `json:"credential_types"`
	Environments      []EnvironmentCountFilter `json:"environments"`
	Statuses          []StatusCountFilter      `json:"statuses"`
}

type AccountFilter struct {
	AccountID       string `json:"account_id"`
	IdentityCount   int    `json:"identity_count"`
	CredentialCount int    `json:"credential_count"`
	IsDefault       bool   `json:"is_default"`
}

type RegionFilter struct {
	Region string `json:"region"`
	Count  int    `json:"count"`
}

type TypeCountFilter struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
}

type ClassCountFilter struct {
	Class string `json:"class"`
	Count int    `json:"count"`
}

type EnvironmentCountFilter struct {
	Environment string `json:"environment"`
	Count       int    `json:"count"`
}

type StatusCountFilter struct {
	Status string `json:"status"`
	Count  int    `json:"count"`
}
