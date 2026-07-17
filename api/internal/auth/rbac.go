package auth

import "strings"

// Role is a logbook-scoped RBAC role.
type Role string

const (
	RoleOwner       Role = "owner"
	RoleAdmin       Role = "admin"
	RoleOperator    Role = "operator"
	RoleContributor Role = "contributor"
	RoleViewer      Role = "viewer"
)

// Permission is a logical action guarded by RBAC.
type Permission string

const (
	PermissionLogbookRead       Permission = "logbook.read"
	PermissionLogbookUpdate     Permission = "logbook.update"
	PermissionLogbookDelete     Permission = "logbook.delete"
	PermissionManageMembers     Permission = "logbook.members.manage"
	PermissionTransferOwnership Permission = "logbook.transfer_ownership"
	PermissionManageBilling     Permission = "logbook.billing.manage"
	PermissionQSORead           Permission = "qso.read"
	PermissionQSOCreate         Permission = "qso.create"
	PermissionQSOUpdate         Permission = "qso.update"
	PermissionQSODelete         Permission = "qso.delete"
	PermissionImportADIF        Permission = "adif.import"
	PermissionExportADIF        Permission = "adif.export"
	PermissionStatsRead         Permission = "stats.read"
)

var roleOrder = map[Role]int{
	RoleViewer:      1,
	RoleContributor: 2,
	RoleOperator:    3,
	RoleAdmin:       4,
	RoleOwner:       5,
}

var rolePermissions = map[Role]map[Permission]struct{}{
	RoleOwner: {
		PermissionLogbookRead:       {},
		PermissionLogbookUpdate:     {},
		PermissionLogbookDelete:     {},
		PermissionManageMembers:     {},
		PermissionTransferOwnership: {},
		PermissionManageBilling:     {},
		PermissionQSORead:           {},
		PermissionQSOCreate:         {},
		PermissionQSOUpdate:         {},
		PermissionQSODelete:         {},
		PermissionImportADIF:        {},
		PermissionExportADIF:        {},
		PermissionStatsRead:         {},
	},
	RoleAdmin: {
		PermissionLogbookRead:   {},
		PermissionLogbookUpdate: {},
		PermissionManageMembers: {},
		PermissionQSORead:       {},
		PermissionQSOCreate:     {},
		PermissionQSOUpdate:     {},
		PermissionQSODelete:     {},
		PermissionImportADIF:    {},
		PermissionExportADIF:    {},
		PermissionStatsRead:     {},
	},
	RoleOperator: {
		PermissionLogbookRead: {},
		PermissionQSORead:     {},
		PermissionQSOCreate:   {},
		PermissionQSOUpdate:   {},
		PermissionQSODelete:   {},
		PermissionImportADIF:  {},
		PermissionExportADIF:  {},
		PermissionStatsRead:   {},
	},
	RoleContributor: {
		PermissionLogbookRead: {},
		PermissionQSORead:     {},
		PermissionQSOCreate:   {},
		PermissionQSOUpdate:   {},
		PermissionQSODelete:   {},
		PermissionExportADIF:  {},
	},
	RoleViewer: {
		PermissionLogbookRead: {},
		PermissionQSORead:     {},
		PermissionExportADIF:  {},
	},
}

// ParseRole normalizes and validates a role value.
func ParseRole(value string) (Role, bool) {
	r := Role(strings.ToLower(strings.TrimSpace(value)))
	_, ok := roleOrder[r]
	return r, ok
}

// AtLeast returns true if role r is greater than or equal to the minimum role.
func (r Role) AtLeast(min Role) bool {
	return roleOrder[r] >= roleOrder[min]
}

// HasPermission returns true if role r grants the given permission.
func (r Role) HasPermission(permission Permission) bool {
	perms, ok := rolePermissions[r]
	if !ok {
		return false
	}
	_, ok = perms[permission]
	return ok
}
