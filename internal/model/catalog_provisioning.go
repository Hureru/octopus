package model

// CatalogGroupProvisioning 控制 CatalogSync 是否可以自动为上游模型创建分组。
type CatalogGroupProvisioning string

const (
	// CatalogGroupProvisioningManual 只为已存在分组的模型建立目录条目，不凭空建分组。
	CatalogGroupProvisioningManual CatalogGroupProvisioning = "manual"
	// CatalogGroupProvisioningAuto 为每个上游模型自动建立 Canonical Model 与同名分组。
	CatalogGroupProvisioningAuto CatalogGroupProvisioning = "auto"
)

func (p CatalogGroupProvisioning) Valid() bool {
	switch p {
	case CatalogGroupProvisioningManual, CatalogGroupProvisioningAuto:
		return true
	default:
		return false
	}
}

// ParseCatalogGroupProvisioning 解析设置值，非法值回退到手动模式。
func ParseCatalogGroupProvisioning(value string) (CatalogGroupProvisioning, bool) {
	mode := CatalogGroupProvisioning(value)
	if mode.Valid() {
		return mode, true
	}
	return CatalogGroupProvisioningManual, false
}

// DiscoveredModelStatus 描述上游模型在目录中的归属状态。
type DiscoveredModelStatus string

const (
	// DiscoveredModelUngrouped 尚未纳入任何分组，请求该模型会 404。
	DiscoveredModelUngrouped DiscoveredModelStatus = "ungrouped"
	// DiscoveredModelGrouped 以自身名字建立了 Canonical Model 与分组。
	DiscoveredModelGrouped DiscoveredModelStatus = "grouped"
	// DiscoveredModelMapped 作为别名映射到了其它 Canonical Model。
	DiscoveredModelMapped DiscoveredModelStatus = "mapped"
)

// DiscoveredModel 是「模型发现」界面的一行：一个上游模型名及其归属信息。
type DiscoveredModel struct {
	Name             string                `json:"name"`
	NormalizedName   string                `json:"normalized_name"`
	Vendor           string                `json:"vendor"`
	VendorManual     bool                  `json:"vendor_manual"`
	Status           DiscoveredModelStatus `json:"status"`
	CanonicalModelID int                   `json:"canonical_model_id,omitempty"`
	CanonicalName    string                `json:"canonical_name,omitempty"`
	GroupID          int                   `json:"group_id,omitempty"`
	GroupName        string                `json:"group_name,omitempty"`
	ChannelCount     int                   `json:"channel_count"`
	ChannelIDs       []int                 `json:"channel_ids"`
	SiteNames        []string              `json:"site_names,omitempty"`
	EndpointTypes    []string              `json:"endpoint_types,omitempty"`
}

// CatalogProvisionRequest 把选中的上游模型纳入分组。
// TargetName 为空时每个模型各自建同名分组；非空时全部映射到该分组。
type CatalogProvisionRequest struct {
	Models                  []string `json:"models" binding:"required"`
	TargetName              string   `json:"target_name,omitempty"`
	DeleteEmptySourceGroups bool     `json:"delete_empty_source_groups,omitempty"`
}

// CatalogProvisionResult 汇报供给操作的实际变更量。
type CatalogProvisionResult struct {
	CanonicalsCreated int `json:"canonicals_created"`
	GroupsCreated     int `json:"groups_created"`
	AliasesCreated    int `json:"aliases_created"`
	CanonicalsMerged  int `json:"canonicals_merged"`
	GroupsDeleted     int `json:"groups_deleted"`
	GroupItemsCreated int `json:"group_items_created"`
}

// CatalogUnprovisionRequest 把选中的上游模型移出分组体系。
// 无论模型是自建分组还是别名映射，引用它的分组条目与路由候选都会一并清除；
// DeleteGroup 为 true 时额外删除与模型同名的分组，用于清理历史自动创建的分组。
type CatalogUnprovisionRequest struct {
	Models      []string `json:"models" binding:"required"`
	DeleteGroup bool     `json:"delete_group,omitempty"`
}

// CatalogUnprovisionResult 汇报回收操作的实际变更量。
type CatalogUnprovisionResult struct {
	AliasesRemoved    int `json:"aliases_removed"`
	CanonicalsRemoved int `json:"canonicals_removed"`
	GroupsDeleted     int `json:"groups_deleted"`
	GroupItemsRemoved int `json:"group_items_removed"`
}
